package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/validation"
)

// handleGroupDialogResult handles dialog-result dispatch for the three group-related
// dialogs: DialogSetWorkspaceGroup, DialogRenameGroup, DialogDeleteGroup.
// Returns (cmd, true) if the dialog ID was handled, (nil, false) otherwise —
// the caller falls through to its main switch when unhandled.
func (a *App) handleGroupDialogResult(id string, confirmed bool, value string, workspace *data.Workspace, defaultName string) (tea.Cmd, bool) {
	if !confirmed {
		// All three group dialogs just cancel on a non-confirmed result.
		switch id {
		case DialogSetWorkspaceGroup, DialogRenameGroup, DialogDeleteGroup:
			return nil, true
		}
		return nil, false
	}

	switch id {
	case DialogSetWorkspaceGroup:
		if workspace != nil {
			label := validation.SanitizeInput(value)
			ws := workspace
			return func() tea.Msg {
				return messages.SetWorkspaceGroup{Workspace: ws, Label: label}
			}, true
		}

	case DialogRenameGroup:
		old := defaultName
		if old == "" {
			return nil, true
		}
		newLabel := validation.SanitizeInput(value)
		return func() tea.Msg {
			return messages.RenameGroup{OldLabel: old, NewLabel: newLabel}
		}, true

	case DialogDeleteGroup:
		label := defaultName
		if label == "" {
			return nil, true
		}
		return func() tea.Msg {
			return messages.DeleteGroup{Label: label}
		}, true
	}

	return nil, false
}
