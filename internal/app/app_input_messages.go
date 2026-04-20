package app

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/ide"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

var randomAnimals = []string{
	"falcon", "otter", "panda", "wolf", "hawk", "lynx", "fox", "bear",
	"eagle", "cobra", "raven", "tiger", "shark", "crane", "bison", "viper",
	"whale", "heron", "moose", "gecko", "horse", "finch", "manta", "newt",
}

var randomColors = []string{
	"red", "blue", "green", "amber", "coral", "ivory", "onyx", "jade",
	"gold", "teal", "plum", "sage", "ruby", "slate", "peach", "rust",
	"cyan", "lime", "navy", "sand", "rose", "mint", "dusk", "gray",
}

// generateWorkspaceName generates a unique random name.
func generateWorkspaceName(repos []data.RepoRef) string {
	prefix := "ws"
	if len(repos) > 0 {
		prefix = filepath.Base(repos[0].Path)
	}
	return fmt.Sprintf("%s-%s-%s",
		prefix,
		randomAnimals[rand.IntN(len(randomAnimals))],
		randomColors[rand.IntN(len(randomColors))],
	)
}

// handleWorkspacesLoaded processes the WorkspacesLoaded message.
// handleShowSetWorkspaceProfileDialog shows the profile picker for a workspace.
func (a *App) handleShowSetWorkspaceProfileDialog(msg messages.ShowSetWorkspaceProfileDialog) {
	a.dialogWorkspace = msg.Workspace
	currentProfile := ""
	if msg.Workspace != nil {
		currentProfile = msg.Workspace.Profile
	}

	profiles := a.listProfiles()
	if len(profiles) > 0 {
		a.dialog = common.NewProfilePicker(DialogSetProfile, profiles, currentProfile)
	} else {
		a.dialogDefaultName = "Default"
		a.dialog = common.NewInputDialog(DialogSetProfile, "Set Profile", "Default")
		a.dialog.SetMessage("Profile isolates Claude settings (permissions, memory) for this workspace.")
	}
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowSetWorkspaceNoteDialog opens the note input dialog.
func (a *App) handleShowSetWorkspaceNoteDialog(msg messages.ShowSetWorkspaceNoteDialog) {
	a.dialogWorkspace = msg.Workspace
	placeholder := ""
	if msg.Workspace != nil {
		placeholder = msg.Workspace.Note
	}
	a.dialog = common.NewInputDialog(DialogSetNote, "Set Note", placeholder)
	a.dialog.SetMessage("Short note shown at the top of the Info tab (leave empty to clear).")
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleSetWorkspaceNote persists a note on a workspace and refreshes the info tab.
func (a *App) handleSetWorkspaceNote(msg messages.SetWorkspaceNote) tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}
	msg.Workspace.Note = msg.Note
	if err := a.workspaces.Save(msg.Workspace); err != nil {
		logging.Error("Failed to save workspace: %v", err)
		return a.toast.ShowError("Failed to save note")
	}
	a.center.SetInfoContent(a.renderWorkspaceInfo())
	if a.dashboard != nil {
		a.dashboard.SetWorkspaces(a.allWorkspaces)
	}
	return nil
}

// handleSetWorkspaceProfile persists a profile for a workspace and reloads.
func (a *App) handleSetWorkspaceProfile(msg messages.SetWorkspaceProfile) tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}
	profile := strings.TrimSpace(msg.Profile)
	wsID := string(msg.Workspace.ID())

	if err := a.registry.SetProfile(wsID, profile); err != nil {
		logging.Error("Failed to set profile: %v", err)
		return a.toast.ShowError("Failed to set profile: " + err.Error())
	}

	// Create profile directory if non-empty
	if profile != "" {
		profileDir := filepath.Join(a.config.Paths.ProfilesRoot, profile)
		if err := os.MkdirAll(profileDir, 0755); err != nil {
			logging.Warn("Failed to create profile directory: %v", err)
		}

		a.config.UI.LastProfile = profile
		if err := a.config.SaveUISettings(); err != nil {
			logging.Warn("Failed to save last profile: %v", err)
		}

		if a.config.UI.SyncProfilePlugins {
			_ = config.SyncProfileSharedDirs(a.config.Paths.ProfilesRoot, profile)
		}
	}

	// Update profile in-place on current workspaces
	msg.Workspace.Profile = profile
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == wsID {
		a.activeWorkspace.Profile = profile
	}

	var cmds []tea.Cmd
	if profile != "" {
		cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Profile set to '%s'", profile)))
	} else {
		cmds = append(cmds, a.toast.ShowSuccess("Profile cleared"))
	}
	cmds = append(cmds, a.loadWorkspaces())

	// Resume a pending agent launch that was blocked on profile selection.
	if a.pendingProfileLaunch != "" && profile != "" {
		assistant := a.pendingProfileLaunch
		root := a.pendingProfileLaunchRoot
		a.pendingProfileLaunch = ""
		a.pendingProfileLaunchRoot = ""
		allowEdits := a.config.UI.LastAllowEdits
		isolated := a.config.UI.LastIsolated
		skipPerms := a.config.UI.LastSkipPermissions
		for _, ws := range a.allWorkspaces {
			if ws.Root() == root {
				w := ws
				cmds = append(cmds, func() tea.Msg {
					return messages.LaunchAgent{
						Assistant:       assistant,
						Workspace:       w,
						AllowEdits:      allowEdits,
						Isolated:        isolated,
						SkipPermissions: skipPerms,
					}
				})
				break
			}
		}
	} else {
		a.pendingProfileLaunch = ""
		a.pendingProfileLaunchRoot = ""
	}

	return a.safeBatch(cmds...)
}

// profileHasActiveWorkspaces checks if any workspace using the given profile
// has active sessions.
func (a *App) profileHasActiveWorkspaces(profile string) bool {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return false
	}

	runningRoots := make(map[string]bool)
	for _, root := range a.center.GetRunningWorkspaceRoots() {
		runningRoots[root] = true
	}

	for _, ws := range a.allWorkspaces {
		wsProfile := strings.TrimSpace(ws.Profile)
		if wsProfile != profile {
			continue
		}
		wsID := string(ws.ID())

		if runningRoots[ws.Root()] {
			return true
		}
		if a.center.HasTabsForWorkspace(wsID) {
			if a.center.HasRunningTabsInWorkspace(wsID) {
				return true
			}
		}
		if workspaceHasLiveTabs(ws) {
			return true
		}
	}
	return false
}

// listProfiles returns the names of existing profile directories.
func (a *App) listProfiles() []string {
	entries, err := os.ReadDir(a.config.Paths.ProfilesRoot)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "shared" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	last := a.config.UI.LastProfile
	if last != "" {
		for i, name := range names {
			if name == last {
				names = append(names[:i], names[i+1:]...)
				names = append([]string{last}, names...)
				break
			}
		}
	}
	return names
}

// handleShowRenameProfileDialog shows the rename profile input dialog.
func (a *App) handleCreateWorkspace(msg messages.CreateWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	if len(msg.Repos) > 0 && msg.Name != "" {
		var pending *data.Workspace
		if len(msg.Repos) == 1 {
			workspacePath := filepath.Join(a.config.Paths.WorkspacesRoot, msg.Name)
			pending = data.NewWorkspace(msg.Name, msg.Name, "", msg.Repos[0].Path, workspacePath)
		} else {
			worktrees := make([]data.WorktreeRef, len(msg.Repos))
			for i, repo := range msg.Repos {
				worktrees[i] = data.WorktreeRef{
					Branch: msg.Name,
					Root:   filepath.Join(a.config.Paths.WorkspacesRoot, msg.Name, repo.Name),
				}
			}
			pending = data.NewMultiRepoWorkspace(msg.Name, msg.Repos, worktrees)
		}
		if cmd := a.dashboard.SetWorkspaceCreating(pending, true); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Start the fetch+create flow based on branch mode
	switch msg.BranchMode {
	case git.BranchModeCheckedOut:
		cmds = append(cmds, a.fetchCheckedOutBase(msg.Repos, msg.Name, "", msg.CopyIgnored))
	case git.BranchModeCustom:
		cmds = append(cmds, a.fetchCustomBase(msg.Repos, msg.Name, "", msg.CustomBranch, msg.CopyIgnored))
	default: // BranchModeRemoteMain
		cmds = append(cmds, a.fetchRemoteBase(msg.Repos, msg.Name, "", msg.CopyIgnored))
	}
	return cmds
}

// handleGitStatusResult handles the GitStatusResult message.
func (a *App) handleGitStatusResult(msg messages.GitStatusResult) tea.Cmd {
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	if a.activeWorkspace != nil && msg.Root == a.activeWorkspace.PrimaryWorktreeRoot() {
		a.sidebar.SetGitStatus(msg.Status)
	}
	return cmd
}

// handleOpenDiff handles the OpenDiff message.
func (a *App) handleOpenDiff(msg messages.OpenDiff) tea.Cmd {
	logging.Info("Opening diff: %s", msg.File)
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return cmd
}

// handleLaunchAgent handles the LaunchAgent message.
func (a *App) handleLaunchAgent(msg messages.LaunchAgent) tea.Cmd {
	logging.Info("Launching agent: %s", msg.Assistant)
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return cmd
}

// handleTabCreated handles the TabCreated message.
func (a *App) handleTabCreated(msg messages.TabCreated) tea.Cmd {
	logging.Info("Tab created: %s", msg.Name)
	cmd := a.center.StartPTYReaders()
	if a.activeWorkspace != nil {
		if a.monitorMode {
			a.focusPane(messages.PaneMonitor)
		} else {
			a.focusPane(messages.PaneCenter)
		}
	}
	return cmd
}

// workspaceHasLiveTabs checks persisted OpenTabs for any non-stopped tabs.
func workspaceHasLiveTabs(ws *data.Workspace) bool {
	if ws == nil {
		return false
	}
	for _, tab := range ws.OpenTabs {
		if tab.Assistant == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(tab.Status), "stopped") {
			continue
		}
		return true
	}
	return false
}

// workspaceHasLiveAgentTabs is like workspaceHasLiveTabs but excludes script
// tabs created by run commands — used by the agent auto-launch gate so a
// dev-server tab doesn't suppress the initial agent tab.
func workspaceHasLiveAgentTabs(ws *data.Workspace) bool {
	if ws == nil {
		return false
	}
	for _, tab := range ws.OpenTabs {
		if tab.Assistant == "" || tab.Assistant == "script" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(tab.Status), "stopped") {
			continue
		}
		return true
	}
	return false
}

// handleShowCommitDialog shows the commit message dialog.
func (a *App) handleShowCommitDialog(msg messages.ShowCommitDialog) {
	a.dialogWorkspaceRoot = msg.WorkspaceRoot
	a.dialog = common.NewInputDialog(DialogCommit, "Commit", "")
	a.dialog.SetMessage("Enter a commit message")
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleActionBarCopyDir copies the workspace directory to clipboard.
func (a *App) handleActionBarCopyDir(msg messages.ActionBarCopyDir) tea.Cmd {
	root := msg.WorkspaceRoot
	return func() tea.Msg {
		if err := common.CopyToClipboard(root); err != nil {
			return messages.Toast{Message: "Failed to copy to clipboard: " + err.Error(), Level: messages.ToastError}
		}
		return messages.Toast{Message: "Copied directory to clipboard", Level: messages.ToastSuccess}
	}
}

// handleActionBarOpenIDE opens the workspace folder in the user's IDE.
func (a *App) handleActionBarOpenIDE(msg messages.ActionBarOpenIDE) tea.Cmd {
	root := msg.WorkspaceRoot
	configured := a.config.UI.IDE
	return func() tea.Msg {
		ideName := ide.GetOrDetect(configured)
		if ideName == "" {
			return messages.Toast{Message: "No IDE detected (install VS Code, Cursor, or Zed)", Level: messages.ToastWarning}
		}
		if err := ide.Open(ideName, root); err != nil {
			return messages.Toast{Message: "Failed to open IDE: " + err.Error(), Level: messages.ToastError}
		}
		return messages.Toast{Message: "Opened in " + ideName, Level: messages.ToastSuccess}
	}
}

// handleActionBarMergeToMain handles the merge to main action.
func (a *App) handleActionBarMergeToMain(msg messages.ActionBarMergeToMain) tea.Cmd {
	return func() tea.Msg {
		if err := git.MergeBranchToMain(msg.RepoPath, msg.BranchName); err != nil {
			return messages.ActionBarMergeResult{Success: false, Err: err}
		}
		return messages.ActionBarMergeResult{Success: true}
	}
}

// handleActionBarCommitResult handles the commit result.
func (a *App) handleActionBarCommitResult(msg messages.ActionBarCommitResult) tea.Cmd {
	if !msg.Success {
		errMsg := "unknown error"
		if msg.Err != nil {
			errMsg = msg.Err.Error()
		}
		return a.toast.ShowError("Commit failed: " + errMsg)
	}
	short := msg.CommitHash
	if len(short) > 7 {
		short = short[:7]
	}
	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Committed %s", short)))
	if a.activeWorkspace != nil {
		cmds = append(cmds, a.requestGitStatus(a.activeWorkspace.PrimaryWorktreeRoot()))
	}
	return a.safeBatch(cmds...)
}

// handleActionBarMergeResult handles the merge result.
func (a *App) handleActionBarMergeResult(msg messages.ActionBarMergeResult) tea.Cmd {
	if !msg.Success {
		errMsg := "unknown error"
		if msg.Err != nil {
			errMsg = msg.Err.Error()
		}
		return a.toast.ShowError("Merge failed: " + errMsg)
	}
	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowSuccess("Merged to main"))
	if a.activeWorkspace != nil {
		cmds = append(cmds, a.requestGitStatus(a.activeWorkspace.PrimaryWorktreeRoot()))
	}
	return a.safeBatch(cmds...)
}

// handleActionBarOpenMR handles the open MR/PR action.
func (a *App) handleActionBarOpenMR(msg messages.ActionBarOpenMR) tea.Cmd {
	return func() tea.Msg {
		url, err := git.GetPRURL(msg.WorkspaceRoot, msg.BranchName)
		if err != nil {
			return messages.Toast{Message: "Could not get MR/PR URL: " + err.Error(), Level: messages.ToastError}
		}
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "linux":
			cmd = exec.Command("xdg-open", url)
		default:
			return messages.Toast{Message: "Open browser not supported", Level: messages.ToastWarning}
		}
		if err := cmd.Run(); err != nil {
			return messages.Toast{Message: "Failed to open browser: " + err.Error(), Level: messages.ToastError}
		}
		return messages.Toast{Message: "Opened in browser", Level: messages.ToastSuccess}
	}
}
