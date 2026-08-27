package messages

import (
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
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
	ViaClick  bool // true when triggered by mouse click (not Enter key)
}

// WorkspacePreviewed is sent when a workspace is previewed (cursor movement)
type WorkspacePreviewed struct {
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

// OpenSkillUsage requests opening the skill-usage dashboard in the browser.
// The server starts on first request and stays up for the rest of the session.
type OpenSkillUsage struct{}

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

// ShowArchiveWorkspaceDialog requests showing the archive workspace confirmation
type ShowArchiveWorkspaceDialog struct {
	Workspace *data.Workspace
}

// ArchiveWorkspace requests archiving a workspace
type ArchiveWorkspace struct {
	Workspace *data.Workspace
}

// ShowArchivedWorkspaceDialog requests showing the archived-workspace actions dialog
type ShowArchivedWorkspaceDialog struct {
	Workspace *data.Workspace
}

// UnarchiveWorkspace requests unarchiving a workspace
type UnarchiveWorkspace struct {
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

// SetWorkspaceStatus requests changing a workspace status
type SetWorkspaceStatus struct {
	Workspace *data.Workspace
	Status    data.WorkspaceStatus
}

// ShowSetWorkspaceNoteDialog requests showing the note input dialog for a workspace
type ShowSetWorkspaceNoteDialog struct {
	Workspace *data.Workspace
}

// SetWorkspaceNote requests setting a note on a workspace
type SetWorkspaceNote struct {
	Workspace *data.Workspace
	Note      string
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
	Assistant                string
	Workspace                *data.Workspace
	Isolated                 bool
	AllowUnsandboxedCommands bool   // only meaningful when Isolated is true
	PermissionMode           string // claude --permission-mode value (acceptEdits, plan, auto, bypassPermissions)
	Fullscreen               bool   // run Claude's fullscreen renderer (CLAUDE_CODE_NO_FLICKER=1)
	CodexSandbox             string // codex --sandbox policy
	CodexAuto                bool   // codex --approve-for-me
}

// LaunchScript requests running a shell command in a new visible tab
type LaunchScript struct {
	Workspace   *data.Workspace
	Command     string
	DisplayName string
	Env         map[string]string
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

// ConfirmRestartTab is sent after the user chooses "Restart" in the
// close-tab dialog. The existing ClaudeSessionID is preserved so the
// conversation resumes via `claude --resume`. Index -1 means restart the
// active tab.
type ConfirmRestartTab struct {
	Index int
}

// ShowCleanupTmuxDialog requests confirmation before cleaning tmux sessions.
type ShowCleanupTmuxDialog struct{}

// CleanupTmuxSessions requests cleanup of medusa tmux sessions.
type CleanupTmuxSessions struct{}

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

// AgentInterrupted is sent when a workspace's agent stops mid-turn without
// Claude Code's Stop hook firing — Ctrl+C forwarded to the terminal, or a tab
// restart — signaling that the agent's activity spinner should be cleared.
type AgentInterrupted struct {
	WorkspaceID string
	SessionName string
}
