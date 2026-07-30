package skillstats

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	eventsFileName = "events.jsonl"
	stateFileName  = "scan-state.json"
)

// RetentionMonths is how much history the durable log keeps. Calendar months
// rather than a day count, so the cutoff lands on the same day of the month
// regardless of month lengths.
const RetentionMonths = 6

// RetentionCutoff returns the oldest event timestamp the store will keep or
// accept. Enforced on both paths — load and commit — so an event that ages out
// cannot reappear from a full rescan of a transcript that still has it.
func RetentionCutoff(now time.Time) time.Time {
	return now.AddDate(0, -RetentionMonths, 0)
}

// Store is the durable record of skill invocations plus the scan bookkeeping
// that keeps rescans incremental. It is the reason the dashboard can report over
// months while Claude Code prunes its transcripts after thirty days: once an
// event has been scanned it lives here, transcript or no transcript.
//
// All exported methods are safe for concurrent use — HTTP handlers read while a
// scan appends.
type Store struct {
	mu     sync.Mutex
	dir    string
	events []Event // Sorted by TS ascending.
	seen   map[string]bool
	state  map[string]fileState
	res    *resolver

	profilesRoot   string
	claudeProjects string
	lastScan       time.Time
}

// ScanResult reports what one scan pass did.
type ScanResult struct {
	FilesConsidered int `json:"filesConsidered"`
	FilesRead       int `json:"filesRead"`
	NewEvents       int `json:"newEvents"`
	// PrunedEvents counts records dropped for aging past RetentionMonths.
	PrunedEvents int           `json:"prunedEvents"`
	Duration     time.Duration `json:"duration"`
	Errors       []string      `json:"errors,omitempty"`
}

// Open loads the store at dir, creating it if absent. profilesRoot is
// ~/.medusa/profiles and claudeProjects is ~/.claude/projects; either may be
// empty to exclude that source.
func Open(dir, profilesRoot, claudeProjects string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}
	s := &Store{
		dir:            dir,
		seen:           map[string]bool{},
		state:          map[string]fileState{},
		res:            newResolver(),
		profilesRoot:   profilesRoot,
		claudeProjects: claudeProjects,
	}
	if err := s.loadEvents(); err != nil {
		return nil, err
	}
	s.loadState()
	return s, nil
}

// loadEvents reads the append-only event log. A truncated final line (killed
// mid-write) is dropped rather than failing the load: the scan state still
// points before that transcript's tail, so the event is recovered on the next
// scan.
func (s *Store) loadEvents() error {
	f, err := os.Open(filepath.Join(s.dir, eventsFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open events: %w", err)
	}
	defer func() { _ = f.Close() }()

	cutoff := RetentionCutoff(time.Now())
	expired := 0
	reader := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			var e Event
			if json.Unmarshal(line, &e) == nil && e.UUID != "" && !s.seen[e.UUID] {
				if e.TS.Before(cutoff) {
					expired++
					continue
				}
				s.seen[e.UUID] = true
				s.events = append(s.events, e)
			}
		}
		if err != nil {
			break
		}
	}
	sort.Slice(s.events, func(i, j int) bool { return s.events[i].TS.Before(s.events[j].TS) })

	// Rewrite so aged-out lines actually leave the file rather than being
	// filtered on every load forever.
	if expired > 0 {
		s.mu.Lock()
		s.rewriteLocked()
		s.mu.Unlock()
	}
	return nil
}

// loadState reads scan bookkeeping. A missing or corrupt state file costs one
// full rescan, which is recoverable, so it is never a fatal error.
func (s *Store) loadState() {
	data, err := os.ReadFile(filepath.Join(s.dir, stateFileName))
	if err != nil {
		return
	}
	var state map[string]fileState
	if err := json.Unmarshal(data, &state); err == nil && state != nil {
		s.state = state
	}
}

// Scan reparses transcripts that changed since the last pass and appends any
// newly seen invocations. Unchanged files are skipped on size+mtime, and a
// grown file is read from its stored offset, so a steady-state scan touches
// almost nothing.
func (s *Store) Scan() ScanResult {
	start := time.Now()
	result := ScanResult{}

	roots := discoverRoots(s.profilesRoot, s.claudeProjects)
	var fresh []Event
	newState := map[string]fileState{}

	s.mu.Lock()
	prevState := make(map[string]fileState, len(s.state))
	for k, v := range s.state {
		prevState[k] = v
	}
	s.mu.Unlock()

	for _, root := range roots {
		for _, path := range transcriptFiles(root.dir) {
			result.FilesConsidered++
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			prev, had := prevState[path]
			if had && prev.Size == info.Size() && prev.ModTime.Equal(info.ModTime()) {
				newState[path] = prev
				continue
			}
			// A file smaller than what was consumed was replaced, not appended
			// to, so its offset is meaningless and it must be reread whole.
			from := prev.Offset
			if !had || info.Size() < prev.Offset {
				from = 0
			}
			events, offset, err := scanFile(path, root.profile, from, s.res)
			result.FilesRead++
			if err != nil {
				result.Errors = append(result.Errors, path+": "+err.Error())
			}
			fresh = append(fresh, events...)
			newState[path] = fileState{Size: info.Size(), ModTime: info.ModTime(), Offset: offset}
		}
	}

	result.NewEvents, result.PrunedEvents = s.commit(fresh, newState)
	result.Duration = time.Since(start)
	return result
}

// commit adds the unseen events to the in-memory set and the durable log, and
// replaces the scan state. Events already recorded are dropped here, which is
// what makes a rescan — of a file or of everything — idempotent.
func (s *Store) commit(fresh []Event, newState map[string]fileState) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := RetentionCutoff(time.Now())
	var added []Event
	for _, e := range fresh {
		// Refusing out-of-retention events here is what stops a full rescan from
		// resurrecting history that was already pruned.
		if e.UUID == "" || s.seen[e.UUID] || e.TS.Before(cutoff) {
			continue
		}
		s.seen[e.UUID] = true
		added = append(added, e)
	}
	s.state = newState
	s.lastScan = time.Now()

	if len(added) > 0 {
		s.events = append(s.events, added...)
		sort.Slice(s.events, func(i, j int) bool { return s.events[i].TS.Before(s.events[j].TS) })
	}

	// A rewrite already persists the freshly added events, so it replaces the
	// append rather than following it.
	expired := s.pruneLocked(cutoff)
	switch {
	case expired > 0:
		s.rewriteLocked()
	case len(added) > 0:
		s.appendLocked(added)
	}
	s.persistStateLocked()
	return len(added), expired
}

// pruneLocked drops events that have aged past the cutoff, returning how many
// went. Their uuids leave the seen set too: the commit-path cutoff check keeps
// them from being re-added, so retaining the keys would only grow memory.
// Events are kept sorted oldest-first, so this is a prefix scan.
func (s *Store) pruneLocked(cutoff time.Time) int {
	drop := 0
	for drop < len(s.events) && s.events[drop].TS.Before(cutoff) {
		drop++
	}
	if drop == 0 {
		return 0
	}
	for _, e := range s.events[:drop] {
		delete(s.seen, e.UUID)
	}
	s.events = append(s.events[:0], s.events[drop:]...)
	return drop
}

// rewriteLocked replaces the log with the current in-memory set via a temp file
// and rename, so an interrupted prune cannot truncate real history.
func (s *Store) rewriteLocked() {
	path := filepath.Join(s.dir, eventsFileName)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	for _, e := range s.events {
		buf, err := json.Marshal(e)
		if err != nil {
			continue
		}
		_, _ = w.Write(append(buf, '\n'))
	}
	flushErr := w.Flush()
	closeErr := f.Close()
	if flushErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// appendLocked writes events to the log. A failed write is not fatal: the scan
// state is written regardless, so a lost line stays lost rather than being
// silently double-counted later, and the dashboard still has the events in
// memory for this run.
func (s *Store) appendLocked(events []Event) {
	f, err := os.OpenFile(filepath.Join(s.dir, eventsFileName),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	w := bufio.NewWriter(f)
	for _, e := range events {
		buf, err := json.Marshal(e)
		if err != nil {
			continue
		}
		_, _ = w.Write(append(buf, '\n'))
	}
	_ = w.Flush()
}

// persistStateLocked writes scan bookkeeping via a temp file and rename, so an
// interrupted write cannot leave a half-parsed state that skips real events.
func (s *Store) persistStateLocked() {
	buf, err := json.Marshal(s.state)
	if err != nil {
		return
	}
	tmp := filepath.Join(s.dir, stateFileName+".tmp")
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, filepath.Join(s.dir, stateFileName)); err != nil {
		_ = os.Remove(tmp)
	}
}

// Events returns a snapshot of every recorded invocation, oldest first.
func (s *Store) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

// Profiles returns the profiles that have at least one recorded invocation.
func (s *Store) Profiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, e := range s.events {
		if e.Profile != "" && !seen[e.Profile] {
			seen[e.Profile] = true
			out = append(out, e.Profile)
		}
	}
	sort.Strings(out)
	return out
}

// LastScan reports when the last scan pass finished, zero if none has run.
func (s *Store) LastScan() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastScan
}
