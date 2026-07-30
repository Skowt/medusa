package skillstats

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// ClaudeProfileLabel is the profile name reported for sessions that ran against
// Claude Code's own ~/.claude config rather than a Medusa profile dir — Claude
// Code started outside Medusa, so CLAUDE_CONFIG_DIR was never set. Naming it
// after the path keeps it from colliding with a real profile (a user profile
// called "Default" is entirely plausible) and sorts it below the named
// profiles, since `~` outranks letters.
const ClaudeProfileLabel = "~/.claude"

// skillMarker prefilters transcript lines before the JSON parse. Claude Code
// writes compact JSON, so a tool_use block for the Skill tool always contains
// this byte sequence verbatim. Transcripts run to hundreds of megabytes and
// only a tiny fraction of lines are skill invocations, so this check is what
// keeps a full rescan seconds rather than minutes.
var skillMarker = []byte(`"name":"Skill"`)

// transcriptRoot is one directory holding a profile's project transcripts.
type transcriptRoot struct {
	dir     string
	profile string
}

// transcriptEntry is the subset of a transcript JSONL entry the scanner reads.
type transcriptEntry struct {
	Type        string `json:"type"`
	UUID        string `json:"uuid"`
	Timestamp   string `json:"timestamp"`
	SessionID   string `json:"sessionId"`
	CWD         string `json:"cwd"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// contentBlock is one entry of an assistant message's content array.
type contentBlock struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Input struct {
		Skill string `json:"skill"`
	} `json:"input"`
}

// fileState records what the scanner already consumed from one transcript, so
// the common case — an appended-to file — reparses only the new tail.
type fileState struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
	Offset  int64     `json:"offset"`
}

// discoverRoots lists the transcript directories to scan: one per Medusa
// profile, plus ~/.claude/projects for sessions run outside Medusa. Missing
// directories are skipped rather than reported — a fresh install has neither.
func discoverRoots(profilesRoot, claudeProjects string) []transcriptRoot {
	var roots []transcriptRoot
	entries, err := os.ReadDir(profilesRoot)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(profilesRoot, entry.Name(), "projects")
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				roots = append(roots, transcriptRoot{dir: dir, profile: entry.Name()})
			}
		}
	}
	if info, err := os.Stat(claudeProjects); err == nil && info.IsDir() {
		roots = append(roots, transcriptRoot{dir: claudeProjects, profile: ClaudeProfileLabel})
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].profile < roots[j].profile })
	return roots
}

// transcriptFiles lists every .jsonl transcript under a root, one level of
// project directories deep plus any nesting Claude Code may add.
func transcriptFiles(root string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Unreadable subtree: skip it, keep scanning the rest.
		}
		if !d.IsDir() && filepath.Ext(path) == ".jsonl" {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

// scanFile parses skill invocations from one transcript, starting at the offset
// already consumed. It returns the events found and the new offset, which
// advances only past newline-terminated lines: a transcript being written to
// right now can end mid-line, and that partial line must be reparsed next time
// rather than skipped.
func scanFile(path, profile string, from int64, res *resolver) ([]Event, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, from, err
	}
	defer func() { _ = f.Close() }()

	if from > 0 {
		if _, err := f.Seek(from, io.SeekStart); err != nil {
			return nil, from, err
		}
	}

	var (
		events   []Event
		consumed = from
		reader   = bufio.NewReaderSize(f, 256<<10)
	)
	for {
		// ReadBytes rather than Scanner: transcript lines embed whole tool
		// responses and routinely exceed any fixed token limit.
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			consumed += int64(len(line))
			events = append(events, parseLine(line, profile, res)...)
		}
		if err != nil {
			if err == io.EOF {
				return events, consumed, nil
			}
			return events, consumed, err
		}
	}
}

// parseLine extracts every skill invocation from one transcript line. A single
// assistant message can invoke several skills in one turn, so all matching
// blocks are returned.
func parseLine(line []byte, profile string, res *resolver) []Event {
	if !bytes.Contains(line, skillMarker) {
		return nil
	}
	var entry transcriptEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return nil
	}
	if entry.UUID == "" || len(entry.Message.Content) == 0 {
		return nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(entry.Message.Content, &blocks); err != nil {
		return nil // Content was a plain string, not a block array.
	}
	ts, err := time.Parse(time.RFC3339, entry.Timestamp)
	if err != nil {
		return nil // Without a timestamp the event cannot be bucketed.
	}

	var events []Event
	for i, block := range blocks {
		if block.Type != "tool_use" || block.Name != "Skill" {
			continue
		}
		plugin, skill := res.split(block.Input.Skill, entry.CWD)
		if skill == "" {
			continue
		}
		events = append(events, Event{
			UUID:      entryEventID(entry.UUID, i),
			TS:        ts,
			Profile:   profile,
			Session:   entry.SessionID,
			Skill:     skill,
			Plugin:    plugin,
			CWD:       entry.CWD,
			Sidechain: entry.IsSidechain,
		})
	}
	return events
}

// entryEventID derives a stable dedup key for the nth skill block of a
// transcript entry. The entry uuid alone would collapse two skills invoked in
// the same assistant message into one event.
func entryEventID(uuid string, index int) string {
	if index == 0 {
		return uuid
	}
	return uuid + "#" + strconv.Itoa(index)
}
