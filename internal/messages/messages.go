package messages

import (
	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
)

// PaneType identifies the focused pane
type PaneType int

const (
	PaneDashboard PaneType = iota
	PaneCenter
	PaneSidebar
	PaneTerminal // Terminal pane (below center pane)
	PaneMonitor
)

// WorkspacesLoaded is sent when workspaces have been loaded/reloaded
type WorkspacesLoaded struct {
	Workspaces []*data.Workspace
}

// WorkspaceActivated is sent when a workspace is selected
type WorkspaceActivated struct {
	Workspace *data.Workspace
}

// WorkspacePreviewed is sent when a workspace is previewed (cursor movement)
type WorkspacePreviewed struct {
	Workspace *data.Workspace
}

// WorkspaceCreated is sent when a new workspace is created
type WorkspaceCreated struct {
	Workspace *data.Workspace
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
}

// GitStatusRequest requests a git status refresh
type GitStatusRequest struct {
	Root string
}

// GitStatusResult contains the result of a git status command
type GitStatusResult struct {
	Root   string
	Status *git.StatusResult
	Err    error
}

// FocusPane requests focus change to a specific pane
type FocusPane struct {
	Pane PaneType
}

// CreateAgentTab requests creation of a new agent tab
type CreateAgentTab struct {
	Assistant string
	Workspace *data.Workspace
}

// TabCreated is sent when a new tab is created
type TabCreated struct {
	Index int
	Name  string
}

// TabClosed is sent when a tab is closed
type TabClosed struct {
	Index int
}

// TabDetached is sent when a tab is detached (tmux session remains).
type TabDetached struct {
	Index int
}

// TabReattached is sent when a detached tab is reattached.
type TabReattached struct {
	WorkspaceID string
	TabID       string
}

// TabStateChanged indicates a tab state change that should be persisted.
type TabStateChanged struct {
	WorkspaceID string
	TabID       string
}

// ToastLevel identifies the type of toast notification to display.
type ToastLevel string

const (
	ToastInfo    ToastLevel = "info"
	ToastSuccess ToastLevel = "success"
	ToastError   ToastLevel = "error"
	ToastWarning ToastLevel = "warning"
)

// Toast requests a toast notification in the UI.
type Toast struct {
	Message string
	Level   ToastLevel
}

// TabSessionStatus reports a tmux session status change for a tab.
type TabSessionStatus struct {
	WorkspaceID string
	SessionName string
	Status      string
}

// TabSelectionChanged indicates the active tab changed for a workspace.
type TabSelectionChanged struct {
	WorkspaceID string
	ActiveIndex int
}

// SwitchTab requests switching to a specific tab
type SwitchTab struct {
	Index int
}

// Error represents an application error
type Error struct {
	Err     error
	Context string
}

func (e Error) Error() string {
	if e.Context != "" {
		return e.Context + ": " + e.Err.Error()
	}
	return e.Err.Error()
}

// ShowWelcome requests showing the welcome screen
type ShowWelcome struct{}

// ToggleMonitor requests toggling monitor mode
type ToggleMonitor struct{}

// ToggleHelp requests toggling the help overlay
type ToggleHelp struct{}

// ShowQuitDialog requests showing the quit confirmation dialog
type ShowQuitDialog struct{}

// PTYWatchdogTick triggers a periodic check for stalled PTY readers.
type PTYWatchdogTick struct{}

// TmuxSyncTick triggers a periodic tmux session sync for the active workspace.
type TmuxSyncTick struct {
	Token int
}

// SidebarPTYRestart requests restarting a sidebar PTY reader.
type SidebarPTYRestart struct {
	WorkspaceID string
	TabID       string
}

// ToggleKeymapHints toggles display of keymap helper text
type ToggleKeymapHints struct{}

// ToggleTerminalCollapse toggles the terminal pane collapsed state
type ToggleTerminalCollapse struct{}

// RefreshDashboard requests a dashboard refresh
type RefreshDashboard struct{}

// ShowSettingsDialog requests showing the settings dialog
type ShowSettingsDialog struct{}

// ShowQuickDuplicateDialog requests showing the quick duplicate dialog with pre-filled repos and profile.
type ShowQuickDuplicateDialog struct {
	Repos   []data.RepoRef
	Profile string
}

// ShowCreateWorkspaceDialog requests showing the create workspace dialog
type ShowCreateWorkspaceDialog struct{}

// ShowDeleteWorkspaceDialog requests showing the delete workspace confirmation
type ShowDeleteWorkspaceDialog struct {
	Workspace *data.Workspace
}

// ShowRenameWorkspaceDialog requests showing the rename workspace dialog
type ShowRenameWorkspaceDialog struct {
	Workspace *data.Workspace
}

// RenameWorkspace requests renaming a workspace
type RenameWorkspace struct {
	Workspace *data.Workspace
	NewName   string
}

// WorkspaceRenameFailed is sent when a workspace rename fails.
type WorkspaceRenameFailed struct {
	Workspace *data.Workspace
	Err       error
}

// CreateWorkspace requests creating a new workspace
type CreateWorkspace struct {
	Name         string
	Repos        []data.RepoRef
	BranchMode   git.BranchMode
	CustomBranch string
}

// DeleteWorkspace requests deleting a workspace
type DeleteWorkspace struct {
	Workspace *data.Workspace
}

// SetWorkspaceStatus requests changing a workspace status
type SetWorkspaceStatus struct {
	Workspace *data.Workspace
	Status    data.WorkspaceStatus
}


// ShowAddReposToWorkspaceDialog requests showing the add repos dialog for a workspace
type ShowAddReposToWorkspaceDialog struct {
	Workspace *data.Workspace
}

// AddReposToWorkspace requests adding repos to an existing workspace
type AddReposToWorkspace struct {
	Workspace *data.Workspace
	Repos     []data.RepoRef
}

// ReposAddedToWorkspace is sent after repos are successfully added to a workspace
type ReposAddedToWorkspace struct {
	Workspace *data.Workspace
}

// ReposAddFailed is sent when adding repos to a workspace fails
type ReposAddFailed struct {
	Err error
}

// ShowSetWorkspaceProfileDialog requests showing the profile dialog for a workspace
type ShowSetWorkspaceProfileDialog struct {
	Workspace *data.Workspace
}

// SetWorkspaceProfile requests setting a profile on a workspace
type SetWorkspaceProfile struct {
	Workspace *data.Workspace
	Profile   string
}

// ShowRenameProfileDialog requests showing the rename profile dialog
type ShowRenameProfileDialog struct {
	Profile string
}

// RenameProfile requests renaming a profile
type RenameProfile struct {
	OldName string
	NewName string
}

// ShowCreateProfileDialog requests showing the create profile dialog
type ShowCreateProfileDialog struct{}

// CreateProfile requests creating a new profile
type CreateProfile struct {
	Name string
}

// ShowDeleteProfileDialog requests showing the delete profile confirmation
type ShowDeleteProfileDialog struct {
	Profile string
}

// DeleteProfile requests deleting a profile
type DeleteProfile struct {
	Profile string
}

// ShowCustomizeTabDialog requests showing the customize tab dialog.
type ShowCustomizeTabDialog struct{}

// LaunchAgent requests launching an agent in a new tab
type LaunchAgent struct {
	Assistant       string
	Workspace       *data.Workspace
	AllowEdits      bool
	Isolated        bool
	SkipPermissions bool
}

// OpenDiff requests opening a diff viewer for a file
type OpenDiff struct {
	File       string
	StatusCode string

	Change    *git.Change
	Mode      git.DiffMode
	Workspace *data.Workspace
}

// CloseTab requests closing the current tab
type CloseTab struct{}

// CloseTabAt requests closing a specific tab by index (from tab bar click)
type CloseTabAt struct {
	Index int
}

// ConfirmCloseTab is sent after user confirms tab closure
type ConfirmCloseTab struct {
	Index int // -1 means close active tab
}

// ShowCleanupTmuxDialog requests confirmation before cleaning tmux sessions.
type ShowCleanupTmuxDialog struct{}

// CleanupTmuxSessions requests cleanup of medusa tmux sessions.
type CleanupTmuxSessions struct{}

// WorkspaceCreatedWithWarning indicates workspace was created but setup had issues
type WorkspaceCreatedWithWarning struct {
	Workspace *data.Workspace
	Warning   string
}

// RunScript requests running a script for the active workspace
type RunScript struct {
	ScriptType string // "setup", "run", or "archive"
}

// ScriptOutput contains output from a running script
type ScriptOutput struct {
	Output string
	Done   bool
	Err    error
}

// GitStatusTick triggers periodic git status refresh
type GitStatusTick struct{}

// FileWatcherEvent is sent when a watched file changes
type FileWatcherEvent struct {
	Root string
}

// SidebarPTYOutput contains PTY output for sidebar terminal
type SidebarPTYOutput struct {
	WorkspaceID string
	TabID       string
	Data        []byte
}

// SidebarPTYTick triggers a sidebar PTY read
type SidebarPTYTick struct {
	WorkspaceID string
	TabID       string
}

// SidebarPTYFlush applies buffered PTY output for sidebar terminal
type SidebarPTYFlush struct {
	WorkspaceID string
	TabID       string
}

// SidebarPTYStopped signals that the sidebar PTY read loop has stopped
type SidebarPTYStopped struct {
	WorkspaceID string
	TabID       string
	Err         error
}

// SidebarTerminalCreated signals that the sidebar terminal was created
type SidebarTerminalCreated struct {
	WorkspaceID string
}

// SidebarTerminalTabCreated signals that a sidebar terminal tab was created
type SidebarTerminalTabCreated struct {
	WorkspaceID string
	TabID       string
}

// UpdateCheckComplete is sent when the background update check finishes
type UpdateCheckComplete struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	ReleaseNotes    string
	Err             error
}

// TriggerUpgrade is sent when the user requests an upgrade
type TriggerUpgrade struct{}

// UpgradeComplete is sent when the upgrade finishes
type UpgradeComplete struct {
	NewVersion string
	Err        error
}

// OpenFileInVim requests opening a file in vim in the center pane
type OpenFileInVim struct {
	Path      string
	Workspace *data.Workspace
}

// PermissionWatcherEvent is sent when a watched settings.local.json changes
type PermissionWatcherEvent struct {
	Root     string
	NewAllow []string
}

// PermissionDetected is sent when new permissions are found in a workspace
type PermissionDetected struct {
	WorkspaceRoot string
	WorkspaceName string
	NewAllow      []string
}

// ShowPermissionsDialog requests showing the pending permissions dialog
type ShowPermissionsDialog struct{}

// PermissionsDialogResult contains the user's actions on pending permissions
type PermissionsDialogResult struct {
	Actions []PermissionAction
}

// PermissionAction represents the user's choice for a single pending permission
type PermissionAction struct {
	Permission string
	Action     PermissionActionType
}

// PermissionActionType identifies how to handle a detected permission
type PermissionActionType int

const (
	PermissionAllow PermissionActionType = iota
	PermissionDeny
	PermissionSkip
)

// PermissionsEditorResult contains the updated allow/deny lists from the editor
type PermissionsEditorResult struct {
	Confirmed bool
	Allow     []string
	Deny      []string
}

// SandboxRulesEditorResult contains the updated sandbox rules from the editor
type SandboxRulesEditorResult struct {
	Confirmed bool
	Rules     []config.SandboxRule
}

// ActionBarCopyDir requests copying the workspace directory to clipboard
type ActionBarCopyDir struct {
	WorkspaceRoot string
}

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

// WorkspaceFetchDone is sent after remote bases have been fetched for workspace creation.
type WorkspaceFetchDone struct {
	Name    string
	Repos   []data.RepoRef
	Bases   []string // parallel to Repos
	Profile string
}
