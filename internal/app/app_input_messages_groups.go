package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// handleShowRenameGroupDialog opens the rename-group input dialog.
func (a *App) handleShowRenameGroupDialog(msg messages.ShowRenameGroupDialog) {
	if msg.Label == "" {
		return
	}
	a.dialogDefaultName = msg.Label
	a.dialog = common.NewInputDialog(DialogRenameGroup, "Rename Group", msg.Label)
	a.dialog.SetMessage("All workspaces in this group will be relabeled.")
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleRenameGroup cascades a group rename across all member workspaces and
// migrates the collapse-state key.
func (a *App) handleRenameGroup(msg messages.RenameGroup) tea.Cmd {
	if msg.OldLabel == "" || msg.NewLabel == msg.OldLabel {
		return nil
	}
	for _, ws := range a.allWorkspaces {
		if ws.Group == msg.OldLabel {
			ws.Group = msg.NewLabel
			if err := a.workspaces.Save(ws); err != nil {
				logging.Error("Failed to save workspace during group rename: %v", err)
				return a.toast.ShowError("Failed to rename group: " + err.Error())
			}
		}
	}
	if a.config.UI.CollapsedGroups != nil {
		was := a.config.UI.CollapsedGroups[msg.OldLabel]
		delete(a.config.UI.CollapsedGroups, msg.OldLabel)
		if msg.NewLabel != "" && was {
			a.config.UI.CollapsedGroups[msg.NewLabel] = true
		}
		if err := a.config.SaveUISettings(); err != nil {
			return a.toast.ShowWarning("Renamed but failed to persist collapse state")
		}
	}
	if a.dashboard != nil {
		a.dashboard.SetWorkspaces(a.allWorkspaces)
	}
	return nil
}

// handleShowDeleteGroupDialog opens a confirm dialog for deleting a group.
func (a *App) handleShowDeleteGroupDialog(msg messages.ShowDeleteGroupDialog) {
	if msg.Label == "" {
		return
	}
	n := 0
	for _, ws := range a.allWorkspaces {
		if ws.Group == msg.Label {
			n++
		}
	}
	body := fmt.Sprintf("Move %d workspace(s) from %q to Ungrouped?", n, msg.Label)
	a.dialogDefaultName = msg.Label
	a.dialog = common.NewConfirmDialog(DialogDeleteGroup, "Delete Group", body)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleDeleteGroup clears Group on all member workspaces.
func (a *App) handleDeleteGroup(msg messages.DeleteGroup) tea.Cmd {
	if msg.Label == "" {
		return nil
	}
	for _, ws := range a.allWorkspaces {
		if ws.Group == msg.Label {
			ws.Group = ""
			if err := a.workspaces.Save(ws); err != nil {
				logging.Error("Failed to save workspace during group delete: %v", err)
				return a.toast.ShowError("Failed to delete group: " + err.Error())
			}
		}
	}
	if a.config.UI.CollapsedGroups != nil {
		delete(a.config.UI.CollapsedGroups, msg.Label)
		if err := a.config.SaveUISettings(); err != nil {
			return a.toast.ShowWarning("Deleted but failed to persist collapse state")
		}
	}
	if a.dashboard != nil {
		a.dashboard.SetWorkspaces(a.allWorkspaces)
	}
	return nil
}
