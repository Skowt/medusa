package review

import "github.com/Skowt/medusa/internal/git"

// rowKind distinguishes what a navigable row of the diff pane holds.
type rowKind int

const (
	rowDiff    rowKind = iota // a line of the unified diff
	rowComment                // one of the user's notes, hanging off the line above
)

// paneRow is one selectable row of the diff pane.
//
// The cursor moves over these rather than over git.DiffResult.Lines directly,
// which is what makes a comment reachable: notes are drawn between diff lines,
// so a cursor that only knew about diff lines could never land on one and there
// was no way to edit or delete a note once it was written.
type paneRow struct {
	Kind rowKind
	// Line is the diff row this represents, or the row a comment hangs off.
	Line git.DiffLine
	// LineIdx indexes git.DiffResult.Lines; -1 for a comment row.
	LineIdx int
	// CommentIdx indexes the file's comment slice; -1 for a diff row.
	CommentIdx int
}

// paneRows builds the navigable rows for the selected file: every diff line,
// each followed by the notes anchored to it.
func (m *Model) paneRows() []paneRow {
	entry := m.Selected()
	if entry == nil {
		return nil
	}
	// Cached against the two things it is built from. The cursor, the renderer
	// and every hit test ask for this list, so rebuilding it per call cost
	// several passes over the file per frame.
	diff := m.displayDiff(entry)
	key := rowsKey{path: entry.Path(), diff: diff, commentsRev: m.commentsRev}
	if m.rowsCache.key == key && m.rowsCache.rows != nil {
		return m.rowsCache.rows
	}
	rows := m.buildPaneRows(entry, diff)
	m.rowsCache = rowsCache{key: key, rows: rows}
	return rows
}

// rowsKey identifies the state a row list was built from. The diff is compared
// by pointer, which is exact because liveDiff hands back a cached value that
// only changes when the buffer does.
type rowsKey struct {
	path        string
	diff        *git.DiffResult
	commentsRev int
}

type rowsCache struct {
	key  rowsKey
	rows []paneRow
}

func (m *Model) buildPaneRows(entry *fileEntry, diff *git.DiffResult) []paneRow {
	notes := m.comments[entry.Path()]

	// Group note indices by the line they hang off, so the walk below stays
	// linear rather than rescanning every note per diff line.
	byLine := make(map[int][]int, len(notes))
	for i, note := range notes {
		byLine[note.Line] = append(byLine[note.Line], i)
	}

	if diff == nil {
		// A file the agent reverted has no diff left, but its notes are the
		// reason the row is still in the list, so they stay reachable.
		rows := make([]paneRow, 0, len(notes))
		for i := range notes {
			rows = append(rows, paneRow{Kind: rowComment, LineIdx: -1, CommentIdx: i})
		}
		return rows
	}

	rows := make([]paneRow, 0, len(diff.Lines)+len(notes))
	for i, line := range diff.Lines {
		rows = append(rows, paneRow{Kind: rowDiff, Line: line, LineIdx: i, CommentIdx: -1})
		if line.Kind == git.DiffLineHeader {
			continue
		}
		for _, idx := range byLine[anchorLine(line)] {
			rows = append(rows, paneRow{Kind: rowComment, Line: line, LineIdx: i, CommentIdx: idx})
		}
	}
	return rows
}

// cursorRow returns the row under the diff cursor.
func (m *Model) cursorRow() (paneRow, bool) {
	rows := m.paneRows()
	if m.diffLine < 0 || m.diffLine >= len(rows) {
		return paneRow{}, false
	}
	return rows[m.diffLine], true
}

// cursorComment returns the note under the cursor, if the cursor is on one.
func (m *Model) cursorComment() (*comment, bool) {
	row, ok := m.cursorRow()
	if !ok || row.Kind != rowComment {
		return nil, false
	}
	entry := m.Selected()
	notes := m.comments[entry.Path()]
	if row.CommentIdx < 0 || row.CommentIdx >= len(notes) {
		return nil, false
	}
	return &notes[row.CommentIdx], true
}

// deleteCursorComment removes the note under the cursor and reports whether one
// was there. The cursor stays put: the rows below shift up by one, so it lands
// on whatever followed, which is where a reader working through a list expects
// to be after removing an entry.
func (m *Model) deleteCursorComment() bool {
	row, ok := m.cursorRow()
	if !ok || row.Kind != rowComment {
		return false
	}
	entry := m.Selected()
	notes := m.comments[entry.Path()]
	if row.CommentIdx < 0 || row.CommentIdx >= len(notes) {
		return false
	}
	m.comments[entry.Path()] = append(notes[:row.CommentIdx], notes[row.CommentIdx+1:]...)
	if len(m.comments[entry.Path()]) == 0 {
		delete(m.comments, entry.Path())
	}
	m.commentsRev++
	m.diffLine = clamp(m.diffLine, 0, maxInt(0, len(m.paneRows())-1))
	m.followDiffCursor()
	return true
}
