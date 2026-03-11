package center

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/messages"
)

// Info tab cursor positions (settings only; Branch/Path are not selectable)
const (
	InfoCursorNote     = 0
	InfoCursorStatus   = 1
	InfoCursorProfile  = 2
	InfoCursorAddRepos = 3
)

// infoCursorMax returns the maximum cursor position for the info tab.
// Single-repo workspaces hide the "Edit Repos" row.
func (m *Model) infoCursorMax() int {
	if m.workspace != nil && !m.workspace.IsMultiRepo() {
		return 2
	}
	return 3
}

// infoTabCursorDown moves the info tab cursor down one position.
func (m *Model) infoTabCursorDown() {
	if m.infoCursor < m.infoCursorMax() {
		m.infoCursor++
	}
}

// infoTabCursorUp moves the info tab cursor up one position.
func (m *Model) infoTabCursorUp() {
	if m.infoCursor > 0 {
		m.infoCursor--
	}
}

// infoTabActivateSetting activates the currently selected setting.
func (m *Model) infoTabActivateSetting() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	switch m.infoCursor {
	case InfoCursorStatus:
		var next data.WorkspaceStatus
		switch ws.Status {
		case data.StatusNone, data.StatusStarted:
			next = data.StatusBlocked
		case data.StatusBlocked:
			next = data.StatusReview
		case data.StatusReview:
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
	case InfoCursorNote:
		return func() tea.Msg {
			return messages.ShowSetWorkspaceNoteDialog{Workspace: ws}
		}
	case InfoCursorProfile:
		return func() tea.Msg {
			return messages.ShowSetWorkspaceProfileDialog{Workspace: ws}
		}
	case InfoCursorAddRepos:
		if !ws.IsMultiRepo() {
			return nil
		}
		return func() tea.Msg {
			return messages.ShowAddReposToWorkspaceDialog{Workspace: ws}
		}
	}
	return nil
}

// infoTabNote emits a ShowSetWorkspaceNoteDialog message.
func (m *Model) infoTabNote() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ShowSetWorkspaceNoteDialog{Workspace: ws}
	}
}

// infoTabRename emits a ShowRenameWorkspaceDialog message.
func (m *Model) infoTabRename() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ShowRenameWorkspaceDialog{Workspace: ws}
	}
}

// infoTabDelete emits archive or delete dialog depending on workspace state.
func (m *Model) infoTabDelete() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	if ws.Archived() || ws.IsOrphaned() {
		return func() tea.Msg {
			return messages.ShowDeleteWorkspaceDialog{Workspace: ws}
		}
	}
	return func() tea.Msg {
		return messages.ShowArchiveWorkspaceDialog{Workspace: ws}
	}
}
