package messages

// ActionBarOpenIDE requests opening the workspace folder in the user's IDE
type ActionBarOpenIDE struct {
	WorkspaceRoot string
}

// ActionBarMergeToMain requests merging the worktree branch into main
type ActionBarMergeToMain struct {
	RepoPath   string
	BranchName string
}

// ActionBarCommit requests staging all changes and creating a commit
type ActionBarCommit struct {
	WorkspaceRoot string
	Message       string
}

// ActionBarCommitResult contains the result of a commit operation
type ActionBarCommitResult struct {
	Success    bool
	CommitHash string
	Err        error
}

// ActionBarMergeResult contains the result of a merge operation
type ActionBarMergeResult struct {
	Success bool
	Err     error
}

// ActionBarOpenMR requests opening a merge/pull request in browser
type ActionBarOpenMR struct {
	WorkspaceRoot string
	BranchName    string
}

// ShowCommitDialog requests showing the commit message dialog
type ShowCommitDialog struct {
	WorkspaceRoot string
}

// OpenReviewChanges requests the interactive review window for a workspace.
// Raised by the info bar's [Review Changes] button and by the prefix binding.
type OpenReviewChanges struct {
	WorkspaceID string
}
