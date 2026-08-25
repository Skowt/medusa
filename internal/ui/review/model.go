// Package review is the interactive change-review overlay: a split pane of the
// workspace's changed files and the diff of the selected one, where the user
// annotates lines, edits files in place, and sends the result back to the agent.
//
// It owns no app state. Like internal/ui/diff it is a self-contained Bubble Tea
// sub-model that the app shows as an overlay and reads a result out of.
package review

import (
	"os"
	"path/filepath"
	"sort"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/ui/common"
)

// pane identifies which half of the window has the keyboard.
type pane int

const (
	paneFiles pane = iota
	paneDiff
	paneComment // writing a comment on the diff's cursor line
	paneEdit    // editing the selected file's contents
)

// fileEntry is one row of the file list.
type fileEntry struct {
	Change  git.Change
	Mode    git.DiffMode
	Diff    *git.DiffResult // nil until loaded
	Added   int
	Deleted int
	Err     error
	// Gone marks a file the user annotated that git no longer reports as
	// changed — the agent reverted it while the window was open. Kept in the
	// list so their comments do not vanish with it.
	Gone bool
	// EditConflict marks an open edit buffer whose file the agent has since
	// rewritten. The write is refused at save time regardless; this is what
	// says so before the user types another paragraph into it.
	EditConflict bool
}

// Path returns the entry's workspace-relative path.
func (f fileEntry) Path() string { return f.Change.Path }

// comment is one review note anchored to a line of a file.
type comment struct {
	// Line is the line in the post-image the note hangs off, which is what a
	// reader can actually go and look at. Deleted lines carry the line they
	// were removed after, so every comment names a place that still exists.
	Line int
	// Quote is the source text the note refers to, carried so the message sent
	// to the agent can show what was being pointed at rather than a bare number.
	Quote string
	Body  string
	// Stale marks a note whose quoted line the agent has since changed or
	// removed. The note keeps its last known line: it still says something
	// worth sending, but the window must not claim it still points there.
	Stale bool
}

// Model is the review overlay.
type Model struct {
	workspace *data.Workspace

	files  []fileEntry
	cursor int // index into files
	loaded bool
	err    error
	// loading is set while a read is in flight, and refreshPending records a
	// change that arrived during one. Together they collapse a burst of writes
	// into a single follow-up read instead of a queue of them.
	loading        bool
	refreshPending bool

	focus     pane
	diffLine  int // cursor row within the selected file's diff
	diffTop   int // first visible diff row
	filesTop  int // first visible file row
	comments  map[string][]comment
	edits     map[string]*editBuffer
	statusMsg string

	// commentArea is the open comment editor, nil unless focus is paneComment.
	commentArea *textarea.Model
	// commentAnchor is the line and quote the open editor will attach to,
	// captured when it opens. Re-reading the cursor at commit time instead
	// would re-anchor an edited note to wherever the cursor had drifted.
	commentAnchor comment

	width, height int
	styles        common.Styles

	// Hit targets, rebuilt by every View. They are recorded while drawing
	// rather than derived afterwards — see rowMap.
	fileRows    rowMap
	paneRowsMap rowMap
	saveHit     common.HitRegion
	discardHit  common.HitRegion

	// Caches for the two derived structures every view reads. commentsRev is
	// bumped by any change to the notes, which is what invalidates the rows.
	liveCache   liveDiffCache
	rowsCache   rowsCache
	commentsRev int

	// absPathOverride redirects path resolution when there is no workspace,
	// which is how the tests open a real file without building one.
	absPathOverride string
}

// filesLoaded carries the git status and per-file diffs back from the loader.
type filesLoaded struct {
	files []fileEntry
	err   error
}

// New creates a review overlay for a workspace. It is not visible until Show.
func New(ws *data.Workspace, width, height int) *Model {
	return &Model{
		workspace: ws,
		comments:  map[string][]comment{},
		edits:     map[string]*editBuffer{},
		width:     width,
		height:    height,
		styles:    common.DefaultStyles(),
	}
}

// Init starts loading the workspace's changes.
func (m *Model) Init() tea.Cmd {
	m.loading = true
	return m.load()
}

// load reads git status and every file's diff off the UI thread. Diffs are
// fetched up front rather than per selection so the file list can show real
// +/- counts, which is most of what makes it scannable.
func (m *Model) load() tea.Cmd {
	ws := m.workspace
	return func() tea.Msg {
		if ws == nil {
			return filesLoaded{err: nil}
		}
		root := ws.PrimaryWorktreeRoot()
		status, err := git.GetStatus(root)
		if err != nil {
			return filesLoaded{err: err}
		}
		var files []fileEntry
		for _, entry := range collectChanges(status) {
			entry.Diff, entry.Err = loadDiff(root, entry.Change, entry.Mode)
			if entry.Diff != nil {
				entry.Added = entry.Diff.AddedLines()
				entry.Deleted = entry.Diff.DeletedLines()
			}
			files = append(files, entry)
		}
		return filesLoaded{files: files}
	}
}

// collectChanges flattens a status into one row per path.
//
// A file can be both staged and unstaged; the review is about the working tree
// as a whole, so it appears once and its diff spans both. Ordering is by path
// so the list does not reshuffle when a file moves between the two.
func collectChanges(status *git.StatusResult) []fileEntry {
	if status == nil {
		return nil
	}
	byPath := map[string]fileEntry{}
	add := func(c git.Change, mode git.DiffMode) {
		if existing, ok := byPath[c.Path]; ok {
			// Seen on the other side already: widen to cover both.
			existing.Mode = git.DiffModeBoth
			byPath[c.Path] = existing
			return
		}
		byPath[c.Path] = fileEntry{Change: c, Mode: mode}
	}
	for _, c := range status.Staged {
		add(c, git.DiffModeStaged)
	}
	for _, c := range status.Unstaged {
		add(c, git.DiffModeUnstaged)
	}
	for _, c := range status.Untracked {
		add(c, git.DiffModeUnstaged)
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]fileEntry, 0, len(paths))
	for _, path := range paths {
		out = append(out, byPath[path])
	}
	return out
}

// sortByPath keeps the file list in path order, which is what makes a refresh
// non-disruptive: a row's neighbours do not change when a file's contents do.
func sortByPath(files []fileEntry) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path() < files[j].Path() })
}

// loadDiff dispatches to the right git call for a change, mirroring the
// selection internal/ui/diff/model.go makes — an untracked file has no diff to
// ask for, so its whole content is shown as added instead.
func loadDiff(root string, change git.Change, mode git.DiffMode) (*git.DiffResult, error) {
	switch {
	case change.Kind == git.ChangeUntracked:
		return git.GetUntrackedFileContent(root, change.Path)
	case mode == git.DiffModeBranch:
		return git.GetBranchFileDiff(root, change.Path)
	default:
		return git.GetFileDiff(root, change.Path, mode)
	}
}

// SetSize updates the overlay dimensions.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
}

// Selected returns the entry under the file cursor, or nil when there is none.
func (m *Model) Selected() *fileEntry {
	if m.cursor < 0 || m.cursor >= len(m.files) {
		return nil
	}
	return &m.files[m.cursor]
}

// CommentCount returns how many comments the review holds.
func (m *Model) CommentCount() int {
	n := 0
	for _, list := range m.comments {
		n += len(list)
	}
	return n
}

// EditedPaths returns the files with unsaved hand edits, in list order so the
// message sent to the agent is stable.
func (m *Model) EditedPaths() []string {
	var out []string
	for _, f := range m.files {
		if buf, ok := m.edits[f.Path()]; ok && buf.Dirty() {
			out = append(out, f.Path())
		}
	}
	return out
}

// HasFeedback reports whether there is anything to save.
func (m *Model) HasFeedback() bool {
	return m.CommentCount() > 0 || len(m.EditedPaths()) > 0
}

// absPath resolves a workspace-relative path against the primary worktree.
func (m *Model) absPath(rel string) string {
	if m.absPathOverride != "" {
		return filepath.Join(m.absPathOverride, rel)
	}
	if m.workspace == nil {
		return rel
	}
	return filepath.Join(m.workspace.PrimaryWorktreeRoot(), rel)
}

// fileExists reports whether a path is present on disk, used to keep deleted
// files out of edit mode.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
