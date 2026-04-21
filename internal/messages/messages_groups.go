package messages

import (
	"github.com/Skowt/medusa/internal/data"
)

// ShowSetWorkspaceGroupDialog requests showing the group-label input dialog for a workspace.
type ShowSetWorkspaceGroupDialog struct {
	Workspace *data.Workspace
}

// SetWorkspaceGroup requests setting a group label on a workspace. Empty label = ungrouped.
type SetWorkspaceGroup struct {
	Workspace *data.Workspace
	Label     string
}

// ShowRenameGroupDialog requests the rename dialog for a user group.
type ShowRenameGroupDialog struct {
	Label string
}

// RenameGroup relabels all workspaces whose Group == OldLabel to NewLabel.
// Empty NewLabel behaves as DeleteGroup (clear label on all members).
type RenameGroup struct {
	OldLabel string
	NewLabel string
}

// ShowDeleteGroupDialog requests the delete-group confirm dialog.
type ShowDeleteGroupDialog struct {
	Label string
}

// DeleteGroup clears the Group field on all workspaces whose Group == Label.
type DeleteGroup struct {
	Label string
}

// ToggleGroupCollapse flips the collapse state for the given group label.
// Empty label targets the Ungrouped section.
type ToggleGroupCollapse struct {
	Label string
}

// DuplicateWorkspace triggers quick-duplication of a specific workspace.
type DuplicateWorkspace struct {
	Workspace *data.Workspace
}
