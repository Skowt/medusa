package review

import (
	tea "charm.land/bubbletea/v2"
)

// Frame geometry, in the window's own coordinates. The border costs one column
// and one row on each side, and Padding(0,1) another column — see frame().
const (
	frameBorder = 1
	frameLeft   = frameBorder + 1 // border + left padding
	titleRow    = frameBorder     // the title sits directly under the top border
	bodyTop     = titleRow + 1
)

// rowMap records which navigable item each rendered row of a pane belongs to,
// built during View.
//
// Recording it beats recomputing it. A comment wraps to as many rows as its
// text needs, a removal draws rows of its own, and the editor inserts lines the
// buffer does not have — so the mapping from screen row to item is a product of
// rendering, and a second implementation of it would be wrong the first time
// either renderer changed.
type rowMap struct {
	top   int   // screen row of the first entry
	items []int // item index per screen row, -1 for a row that selects nothing
}

// at returns the item at a screen row, and whether the row maps to one.
func (r rowMap) at(y int) (int, bool) {
	i := y - r.top
	if i < 0 || i >= len(r.items) || r.items[i] < 0 {
		return 0, false
	}
	return r.items[i], true
}

// handleClick routes a left click by where it landed.
func (m *Model) handleClick(msg tea.MouseClickMsg) (*Model, tea.Cmd, *Result) {
	if msg.Button != tea.MouseLeft {
		return m, nil, nil
	}
	// A click while a note is being typed commits it first. Leaving the editor
	// open while the selection moves out from under it would attach the note to
	// whatever the click landed on.
	if m.focus == paneComment {
		m.commitComment()
	}

	switch {
	case m.saveHit.Contains(msg.X, msg.Y):
		if !m.HasFeedback() {
			m.statusMsg = "Nothing to send yet — comment on a line or edit a file"
			return m, nil, nil
		}
		return m.save()
	case m.discardHit.Contains(msg.X, msg.Y):
		return m, nil, &Result{}
	}

	if idx, ok := m.fileRows.at(msg.Y); ok && msg.X < frameLeft+m.filesWidth() {
		m.focus = paneFiles
		if idx != m.cursor {
			m.cursor = idx
			m.resetDiffCursor()
		}
		return m, nil, nil
	}
	if idx, ok := m.paneRowsMap.at(msg.Y); ok {
		// Clicking a row of the diff both focuses that pane and selects the
		// row, so a note can be reached and acted on with the mouse alone.
		m.focus = paneDiff
		m.diffLine = idx
		m.followDiffCursor()
		return m, nil, nil
	}
	return m, nil, nil
}
