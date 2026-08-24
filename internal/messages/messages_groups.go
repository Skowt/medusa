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

// ReorderWorkspaces sets the manual order of one group's members. OrderedRoots
// lists every member root of Group in its new order; any workspace named there
// that currently belongs to another group is moved into Group as part of the
// same commit, which is how a cross-group drag is expressed.
type ReorderWorkspaces struct {
	Group        string
	OrderedRoots []string
}

// CreateGroupForWorkspace moves one workspace into a brand-new group, pins that
// group where it was created, and opens the rename dialog on it. Label is the
// generated placeholder the group is created with, so there is something real to
// show and persist before the user picks a name.
//
// This is one message rather than a batch of the three steps because the order
// matters: the rename dialog has to open on a group that already has its member,
// and batched commands arrive in no particular order.
type CreateGroupForWorkspace struct {
	Root  string
	Label string
	Order []string // section order that keeps the new group where it was dropped
}

// ReorderGroups sets the display order of the dashboard's group sections.
// Labels holds every currently visible section key in its new order, with ""
// standing for the Ungrouped section.
type ReorderGroups struct {
	Labels []string
}
