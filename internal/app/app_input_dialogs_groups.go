package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/validation"
)

// handleGroupDialogResult handles dialog-result dispatch for the group-related
// dialogs: DialogSetWorkspaceGroup, DialogSetGroupForCreate, DialogRenameGroup,
// DialogDeleteGroup. Returns (cmd, true) if the dialog ID was handled,
// (nil, false) otherwise — the caller falls through to its main switch when
// unhandled.
func (a *App) handleGroupDialogResult(id string, confirmed bool, value string, workspace *data.Workspace, defaultName string) (tea.Cmd, bool) {
	if !confirmed {
		// All four group dialogs just cancel on a non-confirmed result.
		switch id {
		case DialogSetWorkspaceGroup, DialogSetGroupForCreate, DialogRenameGroup, DialogDeleteGroup:
			return nil, true
		}
		return nil, false
	}

	switch id {
	case DialogSetWorkspaceGroup:
		if workspace == nil {
			return nil, true
		}
		switch value {
		case common.NewGroupOption:
			// Swap to a text input for the new label. Reuse the same ID so the
			// second submission lands back here.
			a.dialogWorkspace = workspace
			a.dialog = common.NewInputDialog(DialogSetWorkspaceGroup, "New Group", "")
			a.dialog.SetMessage("Label for the new group.")
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
			return nil, true
		case common.UngroupedOption:
			ws := workspace
			return func() tea.Msg {
				return messages.SetWorkspaceGroup{Workspace: ws, Label: ""}
			}, true
		default:
			// Either a picked existing group OR a typed custom label from the input-dialog fallback.
			label := validation.SanitizeInput(value)
			ws := workspace
			return func() tea.Msg {
				return messages.SetWorkspaceGroup{Workspace: ws, Label: label}
			}, true
		}

	case DialogSetGroupForCreate:
		if workspace == nil || len(workspace.Repos) == 0 {
			return nil, true
		}
		switch value {
		case common.NewGroupOption:
			// Swap to a text input for the new label. Reuse the same ID so the
			// second submission lands back here as an "other value" below.
			a.dialogWorkspace = workspace
			a.dialogDefaultName = defaultName
			a.dialog = common.NewInputDialog(DialogSetGroupForCreate, "New Group", "")
			a.dialog.SetMessage("Label for the new group.")
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
			return nil, true
		case common.UngroupedOption:
			// Stash empty group and advance to the branch-mode dialog.
			workspace.Group = ""
			a.dialogWorkspace = workspace
			a.dialogDefaultName = defaultName
			a.showBranchModeDialogForCreate()
			return nil, true
		default:
			// Either a picked existing group OR a typed custom label from the input fallback.
			workspace.Group = validation.SanitizeInput(value)
			a.dialogWorkspace = workspace
			a.dialogDefaultName = defaultName
			a.showBranchModeDialogForCreate()
			return nil, true
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

// showGroupPickerForCreate shows the Group picker step of the Create Workspace
// flow. It sits between the profile picker and the branch-mode dialog,
// reusing the common.NewGroupPicker primitive. Options are derived from the
// existing workspaces (deduped + sorted), matching handleShowSetWorkspaceGroupDialog.
// The caller must set a.dialogWorkspace / a.dialogDefaultName beforehand so
// they persist across the next dialog hop.
func (a *App) showGroupPickerForCreate() {
	groups := activeGroupLabels(a.allWorkspaces)

	a.dialog = common.NewGroupPicker(DialogSetGroupForCreate, groups, "")
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// showBranchModeDialogForCreate shows the branch-mode selection dialog used by
// the Create Workspace flow. Extracted so both the profile-picker result
// handler and the group-picker result handler can advance into it.
func (a *App) showBranchModeDialogForCreate() {
	a.dialog = common.NewSelectDialog(
		DialogSelectBranchMode,
		"Base Branch",
		"Which branch should this worktree be based on?",
		branchModeOptions,
	)
	a.dialog.SetOptionHints(branchModeHints)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}
