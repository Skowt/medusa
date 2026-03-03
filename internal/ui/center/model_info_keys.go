package center

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/messages"
)

// Info tab cursor positions (settings only; Branch/Path are not selectable)
const (
	InfoCursorStatus   = 0
	InfoCursorProfile  = 1
	InfoCursorAddRepos = 2
	InfoCursorMax      = 2
)

// infoTabCursorDown moves the info tab cursor down one position.
func (m *Model) infoTabCursorDown() {
	if m.infoCursor < InfoCursorMax {
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
	case InfoCursorProfile:
		return func() tea.Msg {
			return messages.ShowSetWorkspaceProfileDialog{Workspace: ws}
		}
	case InfoCursorAddRepos:
		return func() tea.Msg {
			return messages.ShowAddReposToWorkspaceDialog{Workspace: ws}
		}
	}
	return nil
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

// infoTabDelete emits a ShowDeleteWorkspaceDialog message.
func (m *Model) infoTabDelete() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ShowDeleteWorkspaceDialog{Workspace: ws}
	}
}
