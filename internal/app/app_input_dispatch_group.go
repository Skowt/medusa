package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/messages"
)

// routeGroupMsg dispatches workspace-group related messages (Set/Rename/Delete
// group, collapse toggle, manual reordering, duplicate-workspace). Returns
// handled=true when the message was consumed here.
func (a *App) routeGroupMsg(msg tea.Msg, cmds *[]tea.Cmd) bool {
	switch msg := msg.(type) {
	case messages.ShowSetWorkspaceGroupDialog:
		a.handleShowSetWorkspaceGroupDialog(msg)
	case messages.SetWorkspaceGroup:
		if cmd := a.handleSetWorkspaceGroup(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ShowRenameGroupDialog:
		a.handleShowRenameGroupDialog(msg)
	case messages.RenameGroup:
		if cmd := a.handleRenameGroup(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ShowDeleteGroupDialog:
		a.handleShowDeleteGroupDialog(msg)
	case messages.DeleteGroup:
		if cmd := a.handleDeleteGroup(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ToggleGroupCollapse:
		if cmd := a.handleToggleGroupCollapse(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ReorderWorkspaces:
		if cmd := a.handleReorderWorkspaces(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.CreateGroupForWorkspace:
		if cmd := a.handleCreateGroupForWorkspace(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ReorderGroups:
		if cmd := a.handleReorderGroups(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.DuplicateWorkspace:
		if cmd := a.handleDuplicateWorkspace(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	default:
		return false
	}
	return true
}
