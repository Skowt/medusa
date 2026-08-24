package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// sortKeyStride spaces the persisted keys of one group's members. Every drop
// renumbers the whole group, so the gaps buy nothing on their own — they are
// there so a workspace whose registry entry is edited by hand can be slotted
// between two others without a full renumber.
const sortKeyStride = 10

// handleReorderWorkspaces persists the manual order of one group's members.
// Every member listed is renumbered, and any that was dragged in from another
// group is relabelled in the same pass, so a cross-group drop is one commit.
func (a *App) handleReorderWorkspaces(msg messages.ReorderWorkspaces) tea.Cmd {
	var cmds []tea.Cmd
	sourceGroups := make(map[string]bool)
	var failed bool

	for i, root := range msg.OrderedRoots {
		ws := a.workspaceByRoot(root)
		if ws == nil {
			// The workspace went away between the drop and here (archived from
			// another pane, deleted on disk). Skip it; the rest still order.
			continue
		}
		oldGroup, oldKey := ws.Group, ws.SortKey
		if oldGroup != msg.Group {
			sourceGroups[oldGroup] = true
		}
		ws.Group = msg.Group
		ws.SortKey = (i + 1) * sortKeyStride
		if oldGroup == ws.Group && oldKey == ws.SortKey {
			continue
		}
		if err := a.workspaces.Save(ws); err != nil {
			ws.Group, ws.SortKey = oldGroup, oldKey
			logging.Error("Failed to save workspace order for %s: %v", ws.Name, err)
			failed = true
		}
	}

	if failed {
		// Some members are now written and some are not, so memory no longer
		// matches disk. Reload rather than leave the pane showing an order that
		// will not survive a restart.
		cmds = append(cmds, a.toast.ShowError("Failed to save order"))
		cmds = append(cmds, func() tea.Msg { return messages.RefreshDashboard{} })
		return tea.Batch(cmds...)
	}

	if cmd := a.expandGroup(msg.Group); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := a.pruneEmptyGroups(sourceGroups); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if a.dashboard != nil {
		a.dashboard.SetWorkspaces(a.allWorkspaces)
	}
	return tea.Batch(cmds...)
}

// handleCreateGroupForWorkspace applies a drop on "New group": it moves the
// workspace in, pins the group where it was created, and then opens the rename
// dialog on it.
//
// The dialog comes last and only if the move actually stuck. Opening it on a
// group that does not exist yet would leave the rename cascading over nothing,
// and offering to rename a group whose creation just failed to save is worse
// than silence.
func (a *App) handleCreateGroupForWorkspace(msg messages.CreateGroupForWorkspace) tea.Cmd {
	var cmds []tea.Cmd
	if cmd := a.handleReorderWorkspaces(messages.ReorderWorkspaces{
		Group:        msg.Label,
		OrderedRoots: []string{msg.Root},
	}); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if len(msg.Order) > 0 {
		if cmd := a.handleReorderGroups(messages.ReorderGroups{Labels: msg.Order}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if ws := a.workspaceByRoot(msg.Root); ws != nil && ws.Group == msg.Label {
		a.showNameNewGroupDialog(msg.Label)
	}
	return tea.Batch(cmds...)
}

// showNameNewGroupDialog opens the rename dialog on a group that was just
// created by a drop. It reuses DialogRenameGroup so the existing result path
// applies the name, but says what is actually happening: the generated label is
// a placeholder, not a name the user chose and is now editing, and an empty
// submission puts the workspace back in Ungrouped rather than doing nothing.
func (a *App) showNameNewGroupDialog(label string) {
	a.dialogDefaultName = label
	a.dialog = common.NewInputDialog(DialogRenameGroup, "Name Group", label)
	a.dialog.SetMessage("Leave empty to put the workspace back in Ungrouped.")
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleReorderGroups persists the display order of the dashboard's sections.
func (a *App) handleReorderGroups(msg messages.ReorderGroups) tea.Cmd {
	if a.config == nil {
		return nil
	}
	previous := a.config.UI.GroupOrder
	a.config.UI.GroupOrder = msg.Labels
	if a.dashboard != nil {
		a.dashboard.SetGroupOrder(msg.Labels)
	}
	if err := a.config.SaveUISettings(); err != nil {
		a.config.UI.GroupOrder = previous
		if a.dashboard != nil {
			a.dashboard.SetGroupOrder(previous)
		}
		logging.Error("Failed to save group order: %v", err)
		return a.toast.ShowError("Failed to save group order")
	}
	return nil
}

// pruneEmptyGroups drops the collapse state of groups a move emptied, matching
// what handleSetWorkspaceGroup does for a single relabel. The manual group order
// keeps its entry: a group whose last member moved out is usually on its way
// back, and sectionOrder ignores keys with no members anyway.
func (a *App) pruneEmptyGroups(groups map[string]bool) tea.Cmd {
	if a.config == nil || a.config.UI.CollapsedGroups == nil {
		return nil
	}
	changed := false
	for group := range groups {
		if group == "" {
			continue
		}
		if _, tracked := a.config.UI.CollapsedGroups[group]; !tracked {
			continue
		}
		if a.groupHasMembers(group) {
			continue
		}
		delete(a.config.UI.CollapsedGroups, group)
		changed = true
	}
	if !changed {
		return nil
	}
	if a.dashboard != nil {
		a.dashboard.SetCollapsedGroups(a.config.UI.CollapsedGroups)
	}
	if err := a.config.SaveUISettings(); err != nil {
		logging.Warn("Failed to save collapse settings after group prune: %v", err)
	}
	return nil
}

func (a *App) groupHasMembers(group string) bool {
	for _, ws := range a.allWorkspaces {
		if ws != nil && ws.Group == group {
			return true
		}
	}
	return false
}

func (a *App) workspaceByRoot(root string) *data.Workspace {
	if root == "" {
		return nil
	}
	for _, ws := range a.allWorkspaces {
		if ws != nil && ws.Root() == root {
			return ws
		}
	}
	return nil
}
