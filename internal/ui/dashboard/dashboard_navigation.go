package dashboard

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/messages"
)

// isSelectable returns whether a row type can be selected
func isSelectable(rt RowType) bool {
	switch rt {
	case RowSpacer, RowHome, RowSectionHeader:
		return false
	default:
		return true
	}
}

// findSelectableRow finds a selectable row starting from 'from' in direction 'dir'.
func (m *Model) findSelectableRow(from, dir int) int {
	if dir == 0 {
		dir = 1
	}
	for i := from; i >= 0 && i < len(m.rows); i += dir {
		if isSelectable(m.rows[i].Type) {
			return i
		}
	}
	return -1
}

// moveCursor moves the cursor by delta, skipping non-selectable rows
func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}

	steps := delta
	if steps < 0 {
		steps = -steps
	}
	direction := 1
	if delta < 0 {
		direction = -1
	}

	for step := 0; step < steps; step++ {
		next := m.findSelectableRow(m.cursor+direction, direction)
		if next == -1 {
			break
		}
		m.cursor = next
	}
}

// rowLineCount returns how many display lines a row takes
func (m *Model) rowLineCount(idx int) int {
	if idx < 0 || idx >= len(m.rows) {
		return 1
	}
	switch m.rows[idx].Type {
	case RowWorkspace:
		if m.rows[idx].Workspace != nil && m.rows[idx].Workspace.Archived() {
			return 1
		}
		return 2
	case RowSectionHeader:
		if m.rows[idx].Label == "archived" || m.rows[idx].Label == "archived-footer" {
			return 2
		}
		return 1
	case RowHome:
		return 2 // title + separator line
	case RowQuickDuplicate:
		return 2 // blank line + button
	default:
		return 1
	}
}

func (m *Model) rowIndexAt(screenX, screenY int) (int, bool) {
	borderTop := 1
	borderLeft := 1
	borderRight := 1
	paddingLeft := 0
	paddingRight := 1

	contentX := screenX - borderLeft - paddingLeft
	contentY := screenY - borderTop

	contentWidth := m.width - (borderLeft + borderRight + paddingLeft + paddingRight)
	innerHeight := m.height - 2
	if contentWidth <= 0 || innerHeight <= 0 {
		return -1, false
	}
	if contentX < 0 || contentX >= contentWidth {
		return -1, false
	}
	if contentY < 0 || contentY >= innerHeight {
		return -1, false
	}

	helpHeight := m.helpLineCount()
	toolbarHeight := m.toolbarHeight()
	rowAreaHeight := innerHeight - toolbarHeight - helpHeight
	if rowAreaHeight < 1 {
		rowAreaHeight = 1
	}

	if contentY < 0 || contentY >= rowAreaHeight {
		return -1, false
	}

	rowY := contentY

	archivedStart := m.archivedSectionStart()
	archivedLines := m.archivedSectionLineCount()
	mainRowEnd := len(m.rows)
	if archivedStart >= 0 {
		mainRowEnd = archivedStart
	}

	// Check main (scrollable) rows
	line := 0
	mainVisibleHeight := rowAreaHeight - archivedLines
	if mainVisibleHeight < 1 {
		mainVisibleHeight = 1
	}
	for i := 0; i < mainRowEnd; i++ {
		rowLines := m.rowLineCount(i)
		if line+rowLines <= m.scrollOffset {
			line += rowLines
			continue
		}
		visLine := line - m.scrollOffset
		if visLine >= mainVisibleHeight {
			break
		}
		if rowY >= visLine && rowY < visLine+rowLines {
			return i, true
		}
		line += rowLines
	}

	// Check archived rows (pinned to bottom)
	if archivedStart >= 0 {
		archivedY := rowAreaHeight - archivedLines
		aLine := archivedY
		for i := archivedStart; i < len(m.rows); i++ {
			rowLines := m.rowLineCount(i)
			if rowY >= aLine && rowY < aLine+rowLines {
				return i, true
			}
			aLine += rowLines
		}
	}

	return -1, false
}

// previewCurrentRow returns a command to preview the currently selected row.
func (m *Model) previewCurrentRow() tea.Cmd {
	if m.toolbarFocused {
		return func() tea.Msg { return messages.ShowWelcome{} }
	}

	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	switch row.Type {
	case RowHome:
		return func() tea.Msg { return messages.ShowWelcome{} }
	case RowWorkspace:
		if row.Workspace != nil && row.Workspace.IsOrphaned() {
			return func() tea.Msg { return messages.ShowWelcome{} }
		}
		return func() tea.Msg {
			return messages.WorkspacePreviewed{
				Workspace: row.Workspace,
			}
		}
	case RowCreate:
		return func() tea.Msg { return messages.ShowWelcome{} }
	case RowQuickDuplicate:
		return func() tea.Msg { return messages.ShowWelcome{} }
	}

	return nil
}

// handleEnter handles the enter key
func (m *Model) handleEnter() tea.Cmd {
	return m.activateRow(false)
}

// handleClick handles a mouse click on a row
func (m *Model) handleClick() tea.Cmd {
	return m.activateRow(true)
}

// activateRow activates the row at the cursor. viaClick indicates whether
// the activation was triggered by a mouse click (affects focus behavior).
func (m *Model) activateRow(viaClick bool) tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	switch row.Type {
	case RowHome:
		return func() tea.Msg { return messages.ShowWelcome{} }
	case RowWorkspace:
		if row.Workspace != nil && row.Workspace.IsOrphaned() {
			ws := row.Workspace
			return func() tea.Msg {
				return messages.ShowDeleteWorkspaceDialog{Workspace: ws}
			}
		}
		if row.Workspace != nil && row.Workspace.Archived() {
			ws := row.Workspace
			return func() tea.Msg {
				return messages.ShowUnarchiveWorkspaceDialog{Workspace: ws}
			}
		}
		ws := row.Workspace
		return func() tea.Msg {
			return messages.WorkspaceActivated{
				Workspace: ws,
				ViaClick:  viaClick,
			}
		}
	case RowCreate:
		return func() tea.Msg {
			return messages.ShowCreateWorkspaceDialog{}
		}
	case RowQuickDuplicate:
		repos := row.GroupRepos
		profile := row.GroupProfile
		copyIgnored := row.GroupCopyIgnored
		return func() tea.Msg {
			return messages.ShowQuickDuplicateDialog{
				Repos:       repos,
				Profile:     profile,
				CopyIgnored: copyIgnored,
			}
		}
	}

	return nil
}

// handleDelete handles the delete key
func (m *Model) handleDelete() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	if row.Type == RowWorkspace && row.Workspace != nil {
		ws := row.Workspace
		// Archived or orphaned workspaces get permanently deleted
		if ws.Archived() || ws.IsOrphaned() {
			return func() tea.Msg {
				return messages.ShowDeleteWorkspaceDialog{Workspace: ws}
			}
		}
		// Active workspaces get archived first
		return func() tea.Msg {
			return messages.ShowArchiveWorkspaceDialog{Workspace: ws}
		}
	}

	return nil
}

// handleSetProfile opens the profile dialog for the current workspace
func (m *Model) handleSetProfile() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	if row.Type == RowWorkspace && row.Workspace != nil {
		// Check if workspace has an active session
		wsID := string(row.Workspace.ID())
		if m.activeWorkspaceIDs[wsID] {
			return func() tea.Msg {
				return messages.Toast{
					Message: "Cannot change profile while workspace has active sessions",
					Level:   messages.ToastError,
				}
			}
		}
		ws := row.Workspace
		return func() tea.Msg {
			return messages.ShowSetWorkspaceProfileDialog{Workspace: ws}
		}
	}
	return nil
}

// handleToggleStatus cycles the status of the currently selected workspace.
func (m *Model) handleToggleStatus() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}
	row := m.rows[m.cursor]
	if row.Type == RowWorkspace && row.Workspace != nil {
		ws := row.Workspace
		var next data.WorkspaceStatus
		switch ws.Status {
		case data.StatusNone, data.StatusStarted:
			next = data.StatusBlocked
		case data.StatusBlocked:
			next = data.StatusMerged
		case data.StatusMerged:
			next = data.StatusNone
		case data.StatusArchived:
			next = data.StatusNone
		default:
			next = data.StatusStarted
		}
		return func() tea.Msg {
			return messages.SetWorkspaceStatus{Workspace: ws, Status: next}
		}
	}
	return nil
}

// handleRename requests renaming the currently selected workspace.
func (m *Model) handleRename() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}
	row := m.rows[m.cursor]
	if row.Type == RowWorkspace && row.Workspace != nil {
		return func() tea.Msg {
			return messages.ShowRenameWorkspaceDialog{
				Workspace: row.Workspace,
			}
		}
	}
	return nil
}

