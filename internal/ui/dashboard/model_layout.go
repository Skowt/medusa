package dashboard

import (
	"github.com/Skowt/medusa/internal/data"
)

// SetSize sets the dashboard size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.clampScrollOffset()
}

// Focus sets the focus state
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the dashboard is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetWorkspaces sets the workspace list
func (m *Model) SetWorkspaces(workspaces []*data.Workspace) {
	m.workspaces = workspaces
	m.rebuildRows()
	// Keep cursor on the active workspace after re-arrangement
	if m.activeRoot != "" {
		m.moveCursorToRoot(m.activeRoot)
	}
	m.clampScrollOffset()
}

// ScrollInfo returns the scroll state needed to render a scrollbar overlay.
func (m *Model) ScrollInfo() (scrollOffset, totalLines, visible int) {
	total := 0
	for i := range m.rows {
		total += m.rowLineCount(i)
	}
	return m.scrollOffset, total, m.visibleHeight()
}

// visibleHeight returns the number of visible lines in the dashboard
func (m *Model) visibleHeight() int {
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	helpHeight := m.helpLineCount()
	toolbarHeight := m.toolbarHeight()
	visibleHeight := innerHeight - toolbarHeight - helpHeight
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	return visibleHeight
}

// cursorLineOffset returns the line offset of the cursor position
func (m *Model) cursorLineOffset() int {
	offset := 0
	for i := 0; i < m.cursor && i < len(m.rows); i++ {
		offset += m.rowLineCount(i)
	}
	return offset
}

// ClearActiveRoot resets the active workspace selection to "Home".
func (m *Model) ClearActiveRoot() {
	m.activeRoot = ""
}

// moveCursorToRoot moves the dashboard cursor to the row matching the given root.
func (m *Model) moveCursorToRoot(root string) {
	for i, row := range m.rows {
		if row.Type == RowWorkspace && row.Workspace != nil && row.Workspace.Root() == root {
			m.cursor = i
			return
		}
	}
}
