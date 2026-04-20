package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/validation"
)

// routeGroupMsg dispatches any group-related message and reports whether it was handled.
// Keeps the main Update switch in app_input.go free of group boilerplate, which matters
// because that file already sits near the 500-line cap.
func (a *App) routeGroupMsg(msg tea.Msg, cmds *[]tea.Cmd) bool {
	switch m := msg.(type) {
	case messages.ShowCreateGroupDialog:
		a.handleShowCreateGroupDialog(m)
	case messages.ShowRenameGroupDialog:
		a.handleShowRenameGroupDialog(m)
	case messages.ShowDeleteGroupDialog:
		a.handleShowDeleteGroupDialog(m)
	case messages.ShowAssignGroupDialog:
		a.handleShowAssignGroupDialog(m)
	case messages.ToggleGroupExpanded:
		if cmd := a.handleToggleGroupExpanded(m); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.CreateGroup:
		if cmd := a.handleCreateGroup(m); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.RenameGroup:
		if cmd := a.handleRenameGroup(m); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.DeleteGroup:
		if cmd := a.handleDeleteGroup(m); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.AssignWorkspaceGroup:
		if cmd := a.handleAssignWorkspaceGroup(m); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	default:
		return false
	}
	return true
}

// handleGroupDialogResult resolves one of the group-related dialog IDs into the
// follow-up message. Called from handleDialogResult; the per-dialog context
// fields (workspace, groupName, groupRepoKey) are already cleared by the caller,
// so we receive them as arguments.
func (a *App) handleGroupDialogResult(result common.DialogResult, workspace *data.Workspace, groupName, groupRepoKey string) tea.Cmd {
	switch result.ID {
	case DialogCreateGroup:
		name := validation.SanitizeInput(result.Value)
		if name == "" || groupRepoKey == "" {
			a.pendingGroupAssign = nil
			return nil
		}
		return func() tea.Msg {
			return messages.CreateGroup{Name: name, RepoKey: groupRepoKey}
		}

	case DialogRenameGroup:
		newName := validation.SanitizeInput(result.Value)
		if newName == "" || newName == groupName || groupRepoKey == "" {
			return nil
		}
		return func() tea.Msg {
			return messages.RenameGroup{OldName: groupName, NewName: newName, RepoKey: groupRepoKey}
		}

	case DialogDeleteGroup:
		if groupName == "" || groupRepoKey == "" {
			return nil
		}
		return func() tea.Msg {
			return messages.DeleteGroup{Name: groupName, RepoKey: groupRepoKey}
		}

	case DialogAssignGroup:
		if workspace == nil {
			a.dialogGroupChoices = nil
			return nil
		}
		group, isNew, ok := a.resolveAssignGroupChoice(result.Index)
		a.dialogGroupChoices = nil
		if !ok {
			return nil
		}
		if isNew {
			ws := workspace
			a.pendingGroupAssign = ws
			repoKey := data.RepoKeyFor(ws)
			return func() tea.Msg {
				return messages.ShowCreateGroupDialog{RepoKey: repoKey}
			}
		}
		ws := workspace
		return func() tea.Msg {
			return messages.AssignWorkspaceGroup{Workspace: ws, Group: group}
		}
	}
	return nil
}

const (
	assignGroupOptionNone = "Remove from group"
	assignGroupOptionNew  = "New group…"
)

// handleShowCreateGroupDialog opens the input dialog for a new group name.
func (a *App) handleShowCreateGroupDialog(msg messages.ShowCreateGroupDialog) {
	if msg.RepoKey == "" {
		return
	}
	a.dialogGroupRepoKey = msg.RepoKey
	a.dialog = common.NewInputDialog(DialogCreateGroup, "New Group", "")
	a.dialog.SetMessage("Enter a name for the new group.")
	a.dialog.SetInputValidate(func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}
		if a.groupNameExists(s, msg.RepoKey) {
			return "group with this name already exists in this repo"
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowRenameGroupDialog opens the rename input dialog prefilled with the current name.
func (a *App) handleShowRenameGroupDialog(msg messages.ShowRenameGroupDialog) {
	a.dialogGroupName = msg.Name
	a.dialogGroupRepoKey = msg.RepoKey
	a.dialog = common.NewInputDialog(DialogRenameGroup, "Rename Group", msg.Name)
	a.dialog.SetInputValidate(func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" || s == msg.Name {
			return ""
		}
		if a.groupNameExists(s, msg.RepoKey) {
			return "group with this name already exists in this repo"
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	a.dialog.SetValue(msg.Name)
}

// handleShowDeleteGroupDialog opens a confirm dialog for deleting a group.
func (a *App) handleShowDeleteGroupDialog(msg messages.ShowDeleteGroupDialog) {
	a.dialogGroupName = msg.Name
	a.dialogGroupRepoKey = msg.RepoKey

	count := 0
	for _, ws := range a.allWorkspaces {
		if ws.Group == msg.Name && data.RepoKeyFor(ws) == msg.RepoKey {
			count++
		}
	}
	body := fmt.Sprintf("Delete group '%s'?", msg.Name)
	if count > 0 {
		body = fmt.Sprintf("Delete group '%s'? %d workspace(s) will be ungrouped.", msg.Name, count)
	}
	a.dialog = common.NewConfirmDialog(DialogDeleteGroup, "Delete Group", body)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowAssignGroupDialog opens the select dialog for assigning a workspace to a group.
func (a *App) handleShowAssignGroupDialog(msg messages.ShowAssignGroupDialog) {
	if msg.Workspace == nil {
		return
	}
	repoKey := data.RepoKeyFor(msg.Workspace)
	a.dialogWorkspace = msg.Workspace
	a.dialogGroupRepoKey = repoKey

	groups := a.groupsForRepoKey(repoKey)
	options := make([]string, 0, len(groups)+2)
	for _, g := range groups {
		options = append(options, g)
	}
	options = append(options, assignGroupOptionNew, assignGroupOptionNone)
	a.dialogGroupChoices = options

	a.dialog = common.NewSelectDialog(
		DialogAssignGroup,
		"Assign to Group",
		fmt.Sprintf("Choose a group for '%s'.", msg.Workspace.Name),
		options,
	)
	a.dialog.SetVerticalLayout(true)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleToggleGroupExpanded toggles expand/collapse and refreshes the dashboard.
func (a *App) handleToggleGroupExpanded(msg messages.ToggleGroupExpanded) tea.Cmd {
	groups, err := a.registry.ListGroups()
	if err != nil {
		logging.Error("Failed to list groups: %v", err)
		return nil
	}
	expanded := true
	for _, g := range groups {
		if g.Name == msg.Name && g.RepoKey == msg.RepoKey {
			expanded = g.Expanded
			break
		}
	}
	if err := a.registry.SetGroupExpanded(msg.Name, msg.RepoKey, !expanded); err != nil {
		logging.Error("Failed to toggle group: %v", err)
		return a.toast.ShowError("Failed to toggle group: " + err.Error())
	}
	return a.refreshGroups()
}

// handleCreateGroup creates a new group in the registry. When a.pendingGroupAssign
// is set (chained from the "New group…" path of the assign dialog), the workspace
// is also moved into the newly created group.
func (a *App) handleCreateGroup(msg messages.CreateGroup) tea.Cmd {
	name := strings.TrimSpace(msg.Name)
	if name == "" || msg.RepoKey == "" {
		a.pendingGroupAssign = nil
		return nil
	}
	if err := a.registry.AddGroup(name, msg.RepoKey); err != nil {
		logging.Error("Failed to create group: %v", err)
		a.pendingGroupAssign = nil
		return a.toast.ShowError("Failed to create group: " + err.Error())
	}
	cmds := []tea.Cmd{
		a.refreshGroups(),
		a.toast.ShowSuccess(fmt.Sprintf("Group '%s' created", name)),
	}
	if pending := a.pendingGroupAssign; pending != nil && data.RepoKeyFor(pending) == msg.RepoKey {
		a.pendingGroupAssign = nil
		cmds = append(cmds, func() tea.Msg {
			return messages.AssignWorkspaceGroup{Workspace: pending, Group: name}
		})
	} else {
		a.pendingGroupAssign = nil
	}
	return a.safeBatch(cmds...)
}

// handleRenameGroup renames a group in the registry and rewrites membership on workspaces.
func (a *App) handleRenameGroup(msg messages.RenameGroup) tea.Cmd {
	oldName := strings.TrimSpace(msg.OldName)
	newName := strings.TrimSpace(msg.NewName)
	if oldName == "" || newName == "" || oldName == newName {
		return nil
	}
	if err := a.registry.RenameGroup(oldName, newName, msg.RepoKey); err != nil {
		logging.Error("Failed to rename group: %v", err)
		return a.toast.ShowError("Failed to rename group: " + err.Error())
	}
	for _, ws := range a.allWorkspaces {
		if ws.Group == oldName && data.RepoKeyFor(ws) == msg.RepoKey {
			ws.Group = newName
			if err := a.workspaces.Save(ws); err != nil {
				logging.Warn("Failed to update workspace %s after group rename: %v", ws.Name, err)
			}
		}
	}
	return a.safeBatch(
		a.refreshGroups(),
		a.toast.ShowSuccess(fmt.Sprintf("Renamed group to '%s'", newName)),
	)
}

// handleDeleteGroup removes a group and clears the Group field on member workspaces.
func (a *App) handleDeleteGroup(msg messages.DeleteGroup) tea.Cmd {
	if err := a.registry.RemoveGroup(msg.Name, msg.RepoKey); err != nil {
		logging.Error("Failed to delete group: %v", err)
		return a.toast.ShowError("Failed to delete group: " + err.Error())
	}
	for _, ws := range a.allWorkspaces {
		if ws.Group == msg.Name && data.RepoKeyFor(ws) == msg.RepoKey {
			ws.Group = ""
			if err := a.workspaces.Save(ws); err != nil {
				logging.Warn("Failed to clear group on workspace %s: %v", ws.Name, err)
			}
		}
	}
	return a.safeBatch(
		a.refreshGroups(),
		a.toast.ShowSuccess(fmt.Sprintf("Deleted group '%s'", msg.Name)),
	)
}

// handleAssignWorkspaceGroup assigns (or unassigns, with Group="") a workspace to a group.
// If the group does not yet exist in the registry, it is created.
func (a *App) handleAssignWorkspaceGroup(msg messages.AssignWorkspaceGroup) tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}
	ws := msg.Workspace
	name := strings.TrimSpace(msg.Group)
	ws.Group = name
	if err := a.workspaces.Save(ws); err != nil {
		logging.Error("Failed to save workspace: %v", err)
		return a.toast.ShowError("Failed to assign group: " + err.Error())
	}
	if name != "" {
		// Best-effort: ensure the group exists in the registry so collapse state persists.
		repoKey := data.RepoKeyFor(ws)
		if err := a.registry.AddGroup(name, repoKey); err != nil {
			logging.Warn("Failed to ensure group exists: %v", err)
		}
	}
	toastMsg := "Removed from group"
	if name != "" {
		toastMsg = fmt.Sprintf("Assigned to group '%s'", name)
	}
	return a.safeBatch(
		a.refreshGroups(),
		a.toast.ShowSuccess(toastMsg),
	)
}

// refreshGroups reloads groups from the registry and pushes the fresh list
// plus the latest workspaces into the dashboard so row layout reflects the change.
func (a *App) refreshGroups() tea.Cmd {
	groups, err := a.registry.ListGroups()
	if err != nil {
		logging.Error("Failed to reload groups: %v", err)
		return nil
	}
	if a.dashboard != nil {
		a.dashboard.SetGroups(groups)
		a.dashboard.SetWorkspaces(a.allWorkspaces)
	}
	return nil
}

// groupsForRepoKey returns group names for a repo scope in stored order.
func (a *App) groupsForRepoKey(repoKey string) []string {
	groups, err := a.registry.ListGroups()
	if err != nil {
		return nil
	}
	type scoped struct {
		name  string
		order int
	}
	var matches []scoped
	for _, g := range groups {
		if g.RepoKey == repoKey {
			matches = append(matches, scoped{g.Name, g.Order})
		}
	}
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j-1].order > matches[j].order; j-- {
			matches[j-1], matches[j] = matches[j], matches[j-1]
		}
	}
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m.name
	}
	return names
}

// groupNameExists reports whether (name, repoKey) is already in the registry.
func (a *App) groupNameExists(name, repoKey string) bool {
	for _, existing := range a.groupsForRepoKey(repoKey) {
		if existing == name {
			return true
		}
	}
	return false
}

// resolveAssignGroupChoice translates a select-dialog result index back into the
// group name the user picked (or "" to unassign).
// Returns (group, isNew, ok) — isNew indicates the "New group…" option so the
// caller can chain into a create dialog; ok is false for invalid indices.
func (a *App) resolveAssignGroupChoice(index int) (group string, isNew, ok bool) {
	choices := a.dialogGroupChoices
	if index < 0 || index >= len(choices) {
		return "", false, false
	}
	switch choices[index] {
	case assignGroupOptionNew:
		return "", true, true
	case assignGroupOptionNone:
		return "", false, true
	default:
		return choices[index], false, true
	}
}
