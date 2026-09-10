package gitreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Skowt/medusa/internal/logging"
)

// storeVersion is the on-disk format version, so a later change can migrate
// rather than having to guess what it is reading.
//
// v2 added Reply.Sent. A v1 file has no such field, so every reply in it decodes
// as unsent -- which would make the first submit after an upgrade announce the
// entire reply history to the agent. Version 2 exists so that case can be told
// apart from a genuinely undelivered reply and treated as the past instead.
const storeVersion = 2

// commentsFilename sits beside workspace.json in the workspace's metadata
// directory, which is where everything else about a workspace already lives.
const commentsFilename = "review-comments.json"

// maxPersistedThreads caps how much review history one workspace keeps.
//
// Threads are never large, but they are also never cleaned up by anything else:
// a long-lived workspace reviewed daily would grow this file without bound. The
// cap drops resolved threads first, because an unresolved comment is still
// outstanding work and is the last thing that should quietly disappear.
const maxPersistedThreads = 200

// store persists review threads per workspace.
//
// Comments survive a medusa restart because they are outstanding work: an agent
// has been told about a sent thread and may still be acting on it, and a draft
// is something the user typed and has not sent yet. Losing either to a restart
// makes the review page untrustworthy for anything that takes more than one
// sitting.
type store struct {
	// dir is the metadata root (~/.medusa/workspaces-metadata). An empty dir
	// disables persistence rather than failing, so a Service built without one
	// -- in tests, or if the path could not be resolved -- still works.
	dir string
}

// persisted is the on-disk form.
type persisted struct {
	Version int       `json:"version"`
	Root    string    `json:"root"`
	SavedAt time.Time `json:"savedAt"`
	Threads []*Thread `json:"threads"`
	// Viewed maps a path to the diff digest it was ticked against. It is review
	// progress, which is exactly the argument for persisting the threads: a
	// review spanning two sittings that forgets what was already read is worse
	// than one that never offered to remember.
	Viewed map[string]string `json:"viewed,omitempty"`
}

// path returns the file a workspace's comments live in.
//
// The workspace id is preferred because it is what the rest of Medusa keys
// metadata by, so the comments land beside that workspace's workspace.json. A
// review opened without one falls back to a hash of the root, which keeps
// persistence working rather than silently dropping it.
func (s *store) path(workspaceID, root string) string {
	if s.dir == "" {
		return ""
	}
	key := workspaceID
	if key == "" {
		sum := sha256.Sum256([]byte(root))
		key = "review-" + hex.EncodeToString(sum[:8])
	}
	return filepath.Join(s.dir, key, commentsFilename)
}

// load reads a workspace's saved threads and viewed marks. A missing or
// unreadable file is not an error: it means there is no review history, which is
// the normal first case.
func (s *store) load(workspaceID, root string) ([]*Thread, map[string]string) {
	path := s.path(workspaceID, root)
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logging.Warn("Review comments unreadable at %s: %v", path, err)
		}
		return nil, nil
	}
	var data persisted
	if err := json.Unmarshal(raw, &data); err != nil {
		// A corrupt file must not take the review down with it. Keeping the
		// file rather than deleting it leaves the user's comments recoverable
		// by hand.
		logging.Warn("Review comments malformed at %s: %v", path, err)
		return nil, nil
	}
	threads := make([]*Thread, 0, len(data.Threads))
	for _, t := range data.Threads {
		if t == nil || t.ID == "" {
			continue
		}
		if t.Replies == nil {
			t.Replies = []Reply{}
		}
		if data.Version < 2 {
			// Written before replies were tracked. Read as history: whatever
			// happened to these has happened, and re-announcing them all is a
			// worse failure than losing the delivery state of one reply the
			// reader wrote just before a restart.
			for i := range t.Replies {
				t.Replies[i].Sent = true
			}
		}
		threads = append(threads, t)
	}
	return prune(threads), data.Viewed
}

// save writes a workspace's threads and viewed marks, replacing whatever was
// there.
func (s *store) save(workspaceID, root string, threads []*Thread, viewed map[string]string) {
	path := s.path(workspaceID, root)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logging.Warn("Could not create review comment dir: %v", err)
		return
	}

	raw, err := json.MarshalIndent(persisted{
		Version: storeVersion,
		Root:    root,
		SavedAt: time.Now(),
		Threads: prune(threads),
		Viewed:  viewed,
	}, "", "  ")
	if err != nil {
		logging.Warn("Could not encode review comments: %v", err)
		return
	}

	// Temp plus rename, so a crash mid-write cannot leave a half-written file
	// where the comments used to be.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		logging.Warn("Could not write review comments: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		logging.Warn("Could not replace review comments: %v", err)
		_ = os.Remove(tmp)
	}
}

// prune enforces maxPersistedThreads, dropping resolved threads before
// unresolved ones and older before newer.
//
// It returns a new slice and leaves the input order alone: the caller's slice is
// the live thread list, whose order is what the page renders.
func prune(threads []*Thread) []*Thread {
	if len(threads) <= maxPersistedThreads {
		return threads
	}
	// Rank by what is safe to lose: resolved first, then oldest.
	order := make([]*Thread, len(threads))
	copy(order, threads)
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].Resolved != order[j].Resolved {
			return order[i].Resolved // resolved sorts earlier, i.e. dropped first
		}
		return order[i].CreatedAt.Before(order[j].CreatedAt)
	})
	doomed := make(map[string]bool, len(threads)-maxPersistedThreads)
	for _, t := range order[:len(threads)-maxPersistedThreads] {
		doomed[t.ID] = true
	}

	kept := make([]*Thread, 0, maxPersistedThreads)
	for _, t := range threads {
		if !doomed[t.ID] {
			kept = append(kept, t)
		}
	}
	return kept
}
