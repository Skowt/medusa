package app

import (
	"fmt"
	"sort"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// activeGroupLabels returns the sorted, deduped set of group labels that have
// at least one live (non-archived, non-orphaned) member. This mirrors the
// sidebar's visibility rule so pickers never surface a group the user can't
// see elsewhere.
func activeGroupLabels(workspaces []*data.Workspace) []string {
	seen := make(map[string]struct{})
	groups := make([]string, 0)
	for _, ws := range workspaces {
		if ws == nil || ws.Group == "" || ws.Archived() || ws.IsOrphaned() {
			continue
		}
		if _, ok := seen[ws.Group]; ok {
			continue
		}
		seen[ws.Group] = struct{}{}
		groups = append(groups, ws.Group)
	}
	sort.Strings(groups)
	return groups
}

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
// migrates the collapse-state key. Uses snapshot-and-rollback to maintain
// consistency on partial failure: if Save fails mid-loop, all previously-saved
// workspaces are reverted to their original group value.
func (a *App) handleRenameGroup(msg messages.RenameGroup) tea.Cmd {
	if msg.OldLabel == "" || msg.NewLabel == msg.OldLabel {
		return nil
	}

	// Snapshot: build list of targets and record original values.
	type target struct {
		ws       *data.Workspace
		oldGroup string
	}
	var targets []target
	for _, ws := range a.allWorkspaces {
		if ws.Group == msg.OldLabel {
			targets = append(targets, target{ws: ws, oldGroup: ws.Group})
		}
	}

	// Mutate and save.
	saved := 0
	for _, t := range targets {
		t.ws.Group = msg.NewLabel
		if err := a.workspaces.Save(t.ws); err != nil {
			logging.Error("Failed to save workspace during group rename: %v", err)
			// Rollback: revert in-memory mutation for current target.
			t.ws.Group = t.oldGroup
			// Rollback on-disk: re-save already-persisted targets with original values.
			for i := 0; i < saved; i++ {
				targets[i].ws.Group = targets[i].oldGroup
				if rerr := a.workspaces.Save(targets[i].ws); rerr != nil {
					logging.Error("Rollback failed for workspace during rename: %v", rerr)
				}
			}
			return a.toast.ShowError("Failed to rename group: " + err.Error())
		}
		saved++
	}

	// Migrate collapse state.
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

// handleDeleteGroup clears Group on all member workspaces. Uses snapshot-and-rollback
// to maintain consistency on partial failure: if Save fails mid-loop, all
// previously-saved workspaces are reverted to their original group value.
func (a *App) handleDeleteGroup(msg messages.DeleteGroup) tea.Cmd {
	if msg.Label == "" {
		return nil
	}

	// Snapshot: build list of targets and record original values.
	type target struct {
		ws       *data.Workspace
		oldGroup string
	}
	var targets []target
	for _, ws := range a.allWorkspaces {
		if ws.Group == msg.Label {
			targets = append(targets, target{ws: ws, oldGroup: ws.Group})
		}
	}

	// Mutate and save.
	saved := 0
	for _, t := range targets {
		t.ws.Group = ""
		if err := a.workspaces.Save(t.ws); err != nil {
			logging.Error("Failed to save workspace during group delete: %v", err)
			// Rollback: revert in-memory mutation for current target.
			t.ws.Group = t.oldGroup
			// Rollback on-disk: re-save already-persisted targets with original values.
			for i := 0; i < saved; i++ {
				targets[i].ws.Group = targets[i].oldGroup
				if rerr := a.workspaces.Save(targets[i].ws); rerr != nil {
					logging.Error("Rollback failed during delete: %v", rerr)
				}
			}
			return a.toast.ShowError("Failed to delete group: " + err.Error())
		}
		saved++
	}

	// Clean up collapse state.
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

// handleToggleGroupCollapse flips the collapse state for a group and persists it.
func (a *App) handleToggleGroupCollapse(msg messages.ToggleGroupCollapse) tea.Cmd {
	if a.config.UI.CollapsedGroups == nil {
		a.config.UI.CollapsedGroups = make(map[string]bool)
	}
	if a.config.UI.CollapsedGroups[msg.Label] {
		delete(a.config.UI.CollapsedGroups, msg.Label)
	} else {
		a.config.UI.CollapsedGroups[msg.Label] = true
	}
	if a.dashboard != nil {
		a.dashboard.SetCollapsedGroups(a.config.UI.CollapsedGroups)
		a.dashboard.SetWorkspaces(a.allWorkspaces)
	}
	if err := a.config.SaveUISettings(); err != nil {
		return a.toast.ShowWarning("Failed to save collapse state")
	}
	return nil
}

// handleDuplicateWorkspace dispatches ShowQuickDuplicateDialog seeded from the source workspace.
//
// A multi-repo source is refused: duplicating it would create a second
// multi-repo workspace, which is the one thing the New Workspace flow no longer
// offers. The existing one keeps working; it just cannot be cloned.
func (a *App) handleDuplicateWorkspace(msg messages.DuplicateWorkspace) tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}
	if msg.Workspace.IsMultiRepo() {
		return a.toast.ShowError("Multi-repo workspaces cannot be duplicated")
	}
	ws := msg.Workspace
	return func() tea.Msg {
		return messages.ShowQuickDuplicateDialog{
			Repos:       ws.Repos,
			Profile:     ws.Profile,
			CopyIgnored: ws.CopyIgnored,
			Group:       ws.Group,
		}
	}
}

// handleShowSetWorkspaceGroupDialog opens the group picker dialog.
func (a *App) handleShowSetWorkspaceGroupDialog(msg messages.ShowSetWorkspaceGroupDialog) {
	if msg.Workspace == nil {
		return
	}
	groups := activeGroupLabels(a.allWorkspaces)

	a.dialogWorkspace = msg.Workspace
	a.dialog = common.NewGroupPicker(DialogSetWorkspaceGroup, groups, msg.Workspace.Group)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleSetWorkspaceGroup persists a group label on a workspace.
func (a *App) handleSetWorkspaceGroup(msg messages.SetWorkspaceGroup) tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}
	oldGroup := msg.Workspace.Group
	msg.Workspace.Group = msg.Label
	if err := a.workspaces.Save(msg.Workspace); err != nil {
		msg.Workspace.Group = oldGroup // revert in-memory on save failure
		logging.Error("Failed to save workspace group: %v", err)
		return a.toast.ShowError("Failed to save group")
	}

	// Prune CollapsedGroups if the old group is now empty.
	if oldGroup != "" && oldGroup != msg.Label {
		stillExists := false
		for _, ws := range a.allWorkspaces {
			if ws.Group == oldGroup {
				stillExists = true
				break
			}
		}
		if !stillExists && a.config.UI.CollapsedGroups != nil {
			if _, ok := a.config.UI.CollapsedGroups[oldGroup]; ok {
				delete(a.config.UI.CollapsedGroups, oldGroup)
				if err := a.config.SaveUISettings(); err != nil {
					logging.Warn("Failed to save collapse settings after group prune: %v", err)
					// Don't block the main flow on the UI-settings save.
				}
			}
		}
	}

	if a.dashboard != nil {
		a.dashboard.SetWorkspaces(a.allWorkspaces)
	}
	return nil
}

// expandGroup clears the collapsed state for label so a group that is about to
// gain a workspace is open when the user looks at it. rebuildRows drops the
// members of a collapsed group entirely, so a workspace created into one — and
// its creation placeholder — would otherwise appear nowhere. No-op when the
// group is already expanded: creation happens often and the config file is
// rewritten in full on each save.
func (a *App) expandGroup(label string) tea.Cmd {
	if a.config == nil || !a.config.UI.CollapsedGroups[label] {
		return nil
	}
	delete(a.config.UI.CollapsedGroups, label)
	if a.dashboard != nil {
		a.dashboard.SetCollapsedGroups(a.config.UI.CollapsedGroups)
		a.dashboard.SetWorkspaces(a.allWorkspaces)
	}
	if err := a.config.SaveUISettings(); err != nil {
		return a.toast.ShowWarning("Failed to save collapse state")
	}
	return nil
}
