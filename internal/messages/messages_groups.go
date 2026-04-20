package messages

import "github.com/Skowt/medusa/internal/data"

// ShowCreateGroupDialog requests the create-group input dialog for the given repo scope.
type ShowCreateGroupDialog struct {
	RepoKey string
}

// ShowRenameGroupDialog requests the rename-group input dialog for an existing group.
type ShowRenameGroupDialog struct {
	Name    string
	RepoKey string
}

// ShowDeleteGroupDialog requests a confirm dialog to delete a group.
type ShowDeleteGroupDialog struct {
	Name    string
	RepoKey string
}

// ShowAssignGroupDialog requests the select-dialog that assigns a workspace to a group.
type ShowAssignGroupDialog struct {
	Workspace *data.Workspace
}

// ToggleGroupExpanded requests toggling the expanded/collapsed state of a group.
type ToggleGroupExpanded struct {
	Name    string
	RepoKey string
}

// CreateGroup creates a new group in the given repo scope.
type CreateGroup struct {
	Name    string
	RepoKey string
}

// RenameGroup renames a group and updates membership on affected workspaces.
type RenameGroup struct {
	OldName string
	NewName string
	RepoKey string
}

// DeleteGroup removes a group and unsets it on any member workspaces.
type DeleteGroup struct {
	Name    string
	RepoKey string
}

// AssignWorkspaceGroup assigns a workspace to a group (Group="" means unassign).
// If Group is non-empty and not yet in the registry, the handler also creates it.
type AssignWorkspaceGroup struct {
	Workspace *data.Workspace
	Group     string
}

// GroupsChanged is broadcast whenever the set of groups or their state has changed,
// so consumers (dashboard) can refresh their view.
type GroupsChanged struct{}
