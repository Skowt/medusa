package messages

import (
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
)

// Workspace lifecycle: the messages that carry a workspace from the New
// Workspace dialog through creation to deletion. Creation is a chain rather
// than one message — the base branch is resolved, then the worktree is made,
// then gitignored files are copied — so each hop has its own message and they
// belong together.

// WorkspaceCreated is sent when a new workspace is created
type WorkspaceCreated struct {
	Workspace *data.Workspace
}

// WorkspaceWorktreeDone is sent after the worktree is created but before gitignored files are copied.
type WorkspaceWorktreeDone struct {
	Workspace *data.Workspace
	Repos     []data.RepoRef
}

// WorkspaceSetupComplete is sent when async setup scripts finish
type WorkspaceSetupComplete struct {
	Workspace *data.Workspace
	Err       error
}

// WorkspaceCreateFailed is sent when a workspace creation fails
type WorkspaceCreateFailed struct {
	Workspace *data.Workspace
	Err       error
}

// WorkspaceDeleted is sent when a workspace is deleted
type WorkspaceDeleted struct {
	Workspace     *data.Workspace
	BranchWarning string // non-empty if branch cleanup failed
	Silent        bool   // suppress user-facing warnings (e.g. auto-pruned archived workspaces)
}

// WorkspaceDeleteFailed is sent when a workspace deletion fails
type WorkspaceDeleteFailed struct {
	Workspace *data.Workspace
	Err       error
}

// DeleteOrphanWorkspace requests deletion of an orphaned workspace.
type DeleteOrphanWorkspace struct {
	Workspace *data.Workspace
}

// OrphanWorkspaceDeleted is sent after an orphaned workspace has been cleaned up.
type OrphanWorkspaceDeleted struct {
	Workspace *data.Workspace
	Err       error
}

// ShowQuickDuplicateDialog requests showing the quick duplicate dialog with pre-filled repos and profile.
type ShowQuickDuplicateDialog struct {
	Repos       []data.RepoRef
	Profile     string
	CopyIgnored bool
	Group       string // Source workspace's group (inherited by duplicate)
}

// ShowCreateWorkspaceDialog requests showing the create workspace dialog
type ShowCreateWorkspaceDialog struct{}

// ShowDeleteWorkspaceDialog requests showing the delete workspace confirmation
type ShowDeleteWorkspaceDialog struct {
	Workspace *data.Workspace
}

// CreateWorkspace requests creating a new workspace
type CreateWorkspace struct {
	Name         string
	Repos        []data.RepoRef
	BranchMode   git.BranchMode
	CustomBranch string
	CopyIgnored  bool
	Group        string // Optional user group label (empty = ungrouped)
}

// DeleteWorkspace requests deleting a workspace
type DeleteWorkspace struct {
	Workspace *data.Workspace
}

// WorkspaceCreatedWithWarning indicates workspace was created but setup had issues
type WorkspaceCreatedWithWarning struct {
	Workspace *data.Workspace
	Warning   string
}

// WorkspaceFetchDone is sent after remote bases have been fetched for workspace creation.
type WorkspaceFetchDone struct {
	Name        string
	Repos       []data.RepoRef
	Bases       []string // parallel to Repos
	Profile     string
	CopyIgnored bool
	Group       string // User-assigned group label (inherited from duplicated workspace)

	// Worktree is false when the workspace works in the source repo's own
	// checkout. Nothing is created on disk for it, so Bases carries the branch
	// the repo is already on rather than a ref to branch from.
	Worktree bool

	// PullNotice explains why the pre-open pull did not happen, for a checkout
	// workspace. Empty when it pulled or there was nothing to pull.
	PullNotice string

	// CustomBranch is the branch the user asked for (BranchModeCustom only).
	CustomBranch string
	// FallbackRepos names the repos that did not have CustomBranch and were
	// based on their default branch instead. Empty for every other branch mode.
	FallbackRepos []string
}
