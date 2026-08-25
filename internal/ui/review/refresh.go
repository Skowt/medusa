package review

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/git"
)

// Refresh re-reads the workspace's changes so the window tracks the agent as it
// works. It returns nil when a load is already running: a busy agent fires
// change events far faster than a full re-diff completes, and queueing them
// would put the window permanently one round behind.
func (m *Model) Refresh() tea.Cmd {
	if m.loading {
		m.refreshPending = true
		return nil
	}
	m.loading = true
	return m.load()
}

// applyLoaded merges a fresh read into the window rather than replacing it.
//
// Everything the user has produced — comments, edit buffers, where they were
// looking — has to survive a refresh, and none of it is addressed by anything
// the reload knows about. Comments and edits are keyed by path; the selection
// and the diff cursor are re-resolved by path and file line, because a reload
// changes what row any given index refers to.
func (m *Model) applyLoaded(msg filesLoaded) {
	// Remember where the user was, in terms that survive a re-diff.
	selectedPath := ""
	if entry := m.Selected(); entry != nil {
		selectedPath = entry.Path()
	}
	cursorLine, cursorText := 0, ""
	if line, ok := m.cursorDiffLine(); ok {
		cursorLine = anchorLine(line)
		if line.Kind != git.DiffLineHeader {
			cursorText = trimDiffMarker(line.Content)
		}
	}
	first := !m.loaded

	m.loaded = true
	m.loading = false
	m.err = msg.err
	m.files = m.keepAnnotatedFiles(msg.files)
	m.reanchorComments()
	m.markEditConflicts()
	// A reload moves notes and replaces every file entry, so nothing derived
	// from either survives it.
	m.commentsRev++
	m.rowsCache = rowsCache{}
	m.liveCache = liveDiffCache{}

	if first {
		m.resetDiffCursor()
		return
	}
	m.restoreSelection(selectedPath, cursorLine, cursorText)
}

// keepAnnotatedFiles re-adds files the user has commented on or edited that are
// no longer in the change set.
//
// The agent reverting a file is exactly when its comments matter most — "you
// undid this" is feedback — so dropping them because git no longer reports the
// file would delete the user's own work without telling them. The entry comes
// back with no diff and reads as gone.
func (m *Model) keepAnnotatedFiles(loaded []fileEntry) []fileEntry {
	present := make(map[string]bool, len(loaded))
	for _, f := range loaded {
		present[f.Path()] = true
	}

	var missing []string
	for path, notes := range m.comments {
		if len(notes) > 0 && !present[path] {
			missing = append(missing, path)
		}
	}
	for path, buf := range m.edits {
		if buf.Dirty() && !present[path] && !contains(missing, path) {
			missing = append(missing, path)
		}
	}
	if len(missing) == 0 {
		return loaded
	}

	out := loaded
	for _, path := range missing {
		out = append(out, fileEntry{
			Change: git.Change{Path: path},
			Gone:   true,
		})
	}
	sortByPath(out)
	return out
}

// reanchorComments moves each note to wherever its line has ended up.
//
// A comment holds the text it was written against, which is the only durable
// handle on a place in a file that the agent is still editing: line numbers
// move on every insertion above them. When the quoted text is gone the note
// keeps its last known line and is marked stale, so the window can say the
// agent has since changed what was being pointed at rather than silently
// pointing somewhere wrong.
func (m *Model) reanchorComments() {
	byPath := make(map[string]*git.DiffResult, len(m.files))
	for i := range m.files {
		byPath[m.files[i].Path()] = m.files[i].Diff
	}

	for path, notes := range m.comments {
		diff := byPath[path]
		for i := range notes {
			notes[i].Stale = true
			if diff == nil || notes[i].Quote == "" {
				continue
			}
			if line, ok := findQuotedLine(diff, notes[i].Quote, notes[i].Line); ok {
				notes[i].Line = line
				notes[i].Stale = false
			}
		}
		m.comments[path] = notes
	}
}

// findQuotedLine locates the row whose content matches a comment's quote,
// preferring the candidate nearest where the note used to sit. Nearest wins
// because a file often repeats a line — a closing brace, a blank line — and
// jumping a note to the first textual match would move it somewhere the user
// never wrote it.
func findQuotedLine(diff *git.DiffResult, quote string, was int) (int, bool) {
	best, found := 0, false
	for _, line := range diff.Lines {
		if line.Kind == git.DiffLineHeader {
			continue
		}
		if trimDiffMarker(line.Content) != quote {
			continue
		}
		candidate := anchorLine(line)
		if candidate == 0 {
			continue
		}
		if !found || absInt(candidate-was) < absInt(best-was) {
			best, found = candidate, true
		}
	}
	return best, found
}

// markEditConflicts flags open buffers whose file the agent has since rewritten.
//
// The write itself is already refused at save time, but finding that out only
// when you press save means losing a round of typing to a collision that has
// been visible for minutes. Surfacing it on the row as soon as it happens lets
// the user reconcile while they still remember what they changed.
func (m *Model) markEditConflicts() {
	for i := range m.files {
		buf, ok := m.edits[m.files[i].Path()]
		m.files[i].EditConflict = ok && buf.Dirty() && buf.Stale()
	}
}

// restoreSelection puts the cursor back on the same file and, within it, on the
// same line of the file rather than the same row of the diff. A reload changes
// which row an index names — a hunk gaining two lines shifts everything below
// it — so an index would drift a little further on every refresh.
func (m *Model) restoreSelection(path string, line int, text string) {
	if path != "" {
		for i := range m.files {
			if m.files[i].Path() == path {
				m.cursor = i
				m.rowForLine(line, text)
				return
			}
		}
	}
	// The file the user was on is gone. Clamp rather than jump: the list is
	// path-sorted, so the same index is the nearest neighbour alphabetically.
	m.cursor = clamp(m.cursor, 0, len(m.files)-1)
	m.resetDiffCursor()
}

// rowForLine points the diff cursor back at the row the reader was on.
//
// The row's *text* is tried first and its line number only as a fallback, for
// the same reason a comment re-anchors by quote: an insertion above the cursor
// moves the line the reader was looking at, and restoring by number would leave
// them staring at whatever the agent just pushed into that slot. The nearest
// textual match wins so a repeated line does not throw the cursor across the
// file.
func (m *Model) rowForLine(line int, text string) {
	entry := m.Selected()
	if entry == nil || entry.Diff == nil {
		m.resetDiffCursor()
		return
	}
	if text != "" {
		if moved, ok := findQuotedLine(entry.Diff, text, line); ok {
			line = moved
		}
	}
	if line <= 0 {
		m.resetDiffCursor()
		return
	}
	for i, row := range entry.Diff.Lines {
		if row.Kind != git.DiffLineHeader && anchorLine(row) == line {
			m.diffLine = i
			m.followDiffCursor()
			return
		}
	}
	m.resetDiffCursor()
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
