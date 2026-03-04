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

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/ide"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/tmux"
	"github.com/andyrewlee/medusa/internal/ui/common"
	"github.com/andyrewlee/medusa/internal/validation"
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
func (a *App) handleWorkspacesLoaded(msg messages.WorkspacesLoaded) []tea.Cmd {
	a.allWorkspaces = msg.Workspaces
	a.dashboard.SetWorkspaces(a.allWorkspaces)
	var cmds []tea.Cmd
	// Request git status for all workspaces (skip when sidebar is hidden, skip orphans)
	if !a.layout.SidebarHidden() {
		for _, ws := range a.allWorkspaces {
			if ws.IsOrphaned() {
				continue
			}
			cmds = append(cmds, a.requestGitStatus(ws.PrimaryWorktreeRoot()))
		}
	}

	// Determine which workspace will be auto-activated (to skip eager restore for it).
	var autoActivateRoot string
	if a.pendingAutoLaunch != "" {
		autoActivateRoot = a.pendingAutoLaunch
	} else if a.showWelcome && a.activeWorkspace == nil && len(a.allWorkspaces) > 0 {
		autoActivateRoot = a.allWorkspaces[0].Root()
	}

	// Eagerly restore agent tabs for all workspaces on startup.
	// Skip the workspace that will be auto-activated (activation handles its own restore).
	// Skip orphaned workspaces — they have no live sessions.
	for _, ws := range a.allWorkspaces {
		if ws.IsOrphaned() {
			continue
		}
		if autoActivateRoot != "" && ws.Root() == autoActivateRoot {
			continue
		}
		if workspaceHasLiveTabs(ws) {
			if restoreCmd := a.center.RestoreTabsFromWorkspace(ws); restoreCmd != nil {
				cmds = append(cmds, restoreCmd)
			}
		}
	}

	// Start watching workspace permissions if enabled
	if a.config.UI.GlobalPermissions && a.permissionWatcher != nil {
		for _, ws := range a.allWorkspaces {
			if ws.IsOrphaned() {
				continue
			}
			_ = a.permissionWatcher.Watch(ws.Root())
		}
	}

	// Auto-activate a newly created workspace for auto-launch.
	if a.pendingAutoLaunch != "" {
		for _, ws := range a.allWorkspaces {
			if ws.Root() == a.pendingAutoLaunch {
				a.pendingAutoLaunch = ""
				a.pendingAgentLaunch = ws.Root()
				w := ws
				cmds = append(cmds, func() tea.Msg {
					return messages.WorkspaceActivated{
						Workspace: w,
					}
				})
				break
			}
		}
	} else if autoActivateRoot != "" {
		// Auto-activate the first workspace on initial startup
		w := a.allWorkspaces[0]
		cmds = append(cmds, func() tea.Msg {
			return messages.WorkspaceActivated{
				Workspace: w,
			}
		})
	}

	return cmds
}

// handleWorkspaceActivated processes the WorkspaceActivated message.
func (a *App) handleWorkspaceActivated(msg messages.WorkspaceActivated) []tea.Cmd {
	var cmds []tea.Cmd
	a.activeWorkspace = msg.Workspace
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorkspace(msg.Workspace)
	a.sidebar.SetWorkspace(msg.Workspace)
	if msg.Workspace != nil {
		a.dashboard.MarkRead(string(msg.Workspace.ID()))
	}
	// Discover shared tmux tabs first; restore/sync happens below.
	if discoverCmd := a.discoverWorkspaceTabsFromTmux(msg.Workspace); discoverCmd != nil {
		cmds = append(cmds, discoverCmd)
	}
	if syncCmd := a.syncWorkspaceTabsFromTmux(msg.Workspace); syncCmd != nil {
		cmds = append(cmds, syncCmd)
	}
	if restoreCmd := a.center.RestoreTabsFromWorkspace(msg.Workspace); restoreCmd != nil {
		cmds = append(cmds, restoreCmd)
	}
	// Set up sidebar terminal for the workspace (skip when sidebar is hidden)
	if !a.layout.SidebarHidden() {
		if termCmd := a.sidebarTerminal.SetWorkspace(msg.Workspace); termCmd != nil {
			cmds = append(cmds, termCmd)
		}
	}
	// Sync active workspaces to dashboard (fixes spinner race condition)
	if startCmd := a.syncActiveWorkspacesToDashboard(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	cmds = append(cmds, cmd)

	// Refresh git status and set up file watching (skip when sidebar is hidden)
	if msg.Workspace != nil && !a.layout.SidebarHidden() {
		cmds = append(cmds, a.requestGitStatus(msg.Workspace.PrimaryWorktreeRoot()))
		if a.fileWatcher != nil {
			_ = a.fileWatcher.Watch(msg.Workspace.PrimaryWorktreeRoot())
		}
	}
	// Watch workspace permissions if enabled
	if msg.Workspace != nil && a.config.UI.GlobalPermissions && a.permissionWatcher != nil {
		_ = a.permissionWatcher.Watch(msg.Workspace.Root())
	}
	// Ensure spinner starts if needed after sync
	if startCmd := a.dashboard.StartSpinnerIfNeeded(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}

	// Auto-start agent when activating a workspace with no tabs.
	autoLaunch := false
	if a.pendingAgentLaunch != "" && msg.Workspace != nil && msg.Workspace.Root() == a.pendingAgentLaunch {
		a.pendingAgentLaunch = ""
		autoLaunch = true
	} else if a.config.UI.AutoStartAgent && msg.Workspace != nil && !msg.Workspace.IsPrimaryCheckout() {
		autoLaunch = true
	}
	if autoLaunch {
		wsID := string(msg.Workspace.ID())
		if !a.center.HasTabsForWorkspace(wsID) && !workspaceHasLiveTabs(msg.Workspace) {
			ws := msg.Workspace
			allowEdits := a.config.UI.LastAllowEdits
			isolated := a.config.UI.LastIsolated
			skipPerms := a.config.UI.LastSkipPermissions
			cmds = append(cmds, func() tea.Msg {
				return messages.LaunchAgent{
					Assistant:       "claude",
					Workspace:       ws,
					AllowEdits:      allowEdits,
					Isolated:        isolated,
					SkipPermissions: skipPerms,
				}
			})
		}
	}

	// Focus center pane when workspace has active tabs
	if msg.Workspace != nil {
		wsID := string(msg.Workspace.ID())
		if a.center.HasTabsForWorkspace(wsID) || workspaceHasLiveTabs(msg.Workspace) {
			if a.monitorMode {
				a.focusPane(messages.PaneMonitor)
			} else {
				a.focusPane(messages.PaneCenter)
			}
		}
	}

	return cmds
}

// Concurrency safety: takes a snapshot of ws.OpenTabs in the Update loop before
// spawning the Cmd.
func (a *App) syncWorkspaceTabsFromTmux(ws *data.Workspace) tea.Cmd {
	if ws == nil || len(ws.OpenTabs) == 0 {
		return nil
	}
	if !a.tmuxAvailable {
		return nil
	}
	wsID := string(ws.ID())
	tabsSnapshot := make([]data.TabInfo, len(ws.OpenTabs))
	copy(tabsSnapshot, ws.OpenTabs)
	opts := a.tmuxOptions
	return func() tea.Msg {
		var updates []tmuxTabStatusUpdate
		for _, tab := range tabsSnapshot {
			if tab.SessionName == "" {
				continue
			}
			state, err := tmux.SessionStateFor(tab.SessionName, opts)
			if err != nil {
				continue
			}
			if strings.EqualFold(tab.Status, "detached") {
				if !(state.Exists && state.HasLivePane) {
					updates = append(updates, tmuxTabStatusUpdate{
						SessionName:   tab.SessionName,
						Status:        "stopped",
						NotifyStopped: true,
					})
				}
				continue
			}
			status := "stopped"
			if state.Exists && state.HasLivePane {
				status = "running"
			}
			if tab.Status != status {
				updates = append(updates, tmuxTabStatusUpdate{
					SessionName:   tab.SessionName,
					Status:        status,
					NotifyStopped: status == "stopped",
				})
			}
		}
		if len(updates) == 0 {
			return nil
		}
		return tmuxTabsSyncResult{
			WorkspaceID: wsID,
			Updates:     updates,
		}
	}
}

type tmuxTabStatusUpdate struct {
	SessionName   string
	Status        string
	NotifyStopped bool
}

type tmuxTabsSyncResult struct {
	WorkspaceID string
	Updates     []tmuxTabStatusUpdate
}

// handleWorkspacePreviewed processes the WorkspacePreviewed message.
func (a *App) handleWorkspacePreviewed(msg messages.WorkspacePreviewed) []tea.Cmd {
	var cmds []tea.Cmd
	a.activeWorkspace = msg.Workspace
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorkspace(msg.Workspace)
	a.sidebar.SetWorkspace(msg.Workspace)
	a.sidebarTerminal.SetWorkspacePreview(msg.Workspace)
	if msg.Workspace != nil {
		a.dashboard.MarkRead(string(msg.Workspace.ID()))
	}
	// Sync active workspaces to dashboard
	if startCmd := a.syncActiveWorkspacesToDashboard(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}
	if msg.Workspace != nil && a.statusManager != nil {
		if cached := a.statusManager.GetCached(msg.Workspace.PrimaryWorktreeRoot()); cached != nil {
			a.sidebar.SetGitStatus(cached)
		} else {
			a.sidebar.SetGitStatus(nil)
			a.dashboard.InvalidateStatus(msg.Workspace.PrimaryWorktreeRoot())
		}
	} else {
		a.sidebar.SetGitStatus(nil)
	}

	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Ensure spinner starts if needed after sync
	if startCmd := a.dashboard.StartSpinnerIfNeeded(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}

	return cmds
}

// handleShowQuickDuplicateDialog shows a name input dialog for quick duplicate with pre-filled repos and profile.
func (a *App) handleShowQuickDuplicateDialog(msg messages.ShowQuickDuplicateDialog) {
	a.dialogWorkspace = &data.Workspace{Repos: msg.Repos, Profile: msg.Profile}
	a.dialogDefaultName = generateWorkspaceName(msg.Repos)
	a.dialog = common.NewInputDialog(DialogQuickDuplicate, "Quick Duplicate", a.dialogDefaultName)
	a.dialog.SetMessage("Enter a name for the new workspace.")
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateWorkspaceName(s); err != nil {
			return err.Error()
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowCreateWorkspaceDialog shows a recents picker or falls back to file picker.
func (a *App) handleShowCreateWorkspaceDialog() {
	// Check for recent repo combinations
	if a.recents != nil {
		recents, _ := a.recents.List()
		if len(recents) > 0 {
			logging.Info("Showing recent repos dialog with %d entries", len(recents))
			// Store snapshot so the dialog result handler uses the same list
			a.dialogRecents = recents
			options := make([]string, 0, len(recents)+1)
			for _, entry := range recents {
				options = append(options, formatRecentLabel(entry.Repos))
			}
			options = append(options, "Select repos…")
			a.dialog = common.NewSelectDialog(
				DialogSelectRecentRepos,
				"New Workspace",
				"Choose a recent repo combination or select new repos.",
				options,
			)
			a.dialog.SetVerticalLayout(true)
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
			return
		}
	}
	// No recents — go straight to file picker
	a.showRepoFilePicker()
}

// showRepoFilePicker opens the file picker for selecting repos.
func (a *App) showRepoFilePicker() {
	logging.Info("Showing Add Repos file picker")
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	a.filePicker = common.NewFilePicker(DialogAddRepos, home, true)
	a.filePicker.SetTitle("Select Repos")
	a.filePicker.SetPrimaryActionLabel("Add repo")
	a.filePicker.SetMultiSelect(true)
	a.filePicker.SetValidatePath(func(path string, existing []string) string {
		if !git.IsGitRepository(path) {
			return "Not a git repository"
		}
		for _, p := range existing {
			if p == path {
				return "Already added"
			}
			if strings.HasPrefix(path, p+"/") {
				return "Nested inside " + filepath.Base(p)
			}
			if strings.HasPrefix(p, path+"/") {
				return "Contains already-added " + filepath.Base(p)
			}
		}
		return ""
	})
	a.filePicker.SetSize(a.width, a.height)
	a.filePicker.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.filePicker.Show()
}

// formatRecentLabel formats a recent entry's repos as a display label.
func formatRecentLabel(repos []data.RepoRef) string {
	if len(repos) == 1 {
		return repos[0].Name
	}
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}

// showAddReposToWorkspaceFilePicker opens a file picker for adding repos to an existing workspace.
func (a *App) showAddReposToWorkspaceFilePicker(ws *data.Workspace) {
	if ws == nil {
		return
	}
	if !ws.IsMultiRepo() {
		return
	}
	a.dialogWorkspace = ws

	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}

	// Track existing repos so we can exclude them
	existingPaths := make(map[string]bool, len(ws.Repos))
	for _, r := range ws.Repos {
		existingPaths[r.Path] = true
	}

	a.filePicker = common.NewFilePicker(DialogAddReposToWorkspace, home, true)
	a.filePicker.SetTitle("Add Repos to " + ws.Name)
	a.filePicker.SetPrimaryActionLabel("Add repo")
	a.filePicker.SetMultiSelect(true)
	a.filePicker.SetValidatePath(func(path string, existing []string) string {
		if !git.IsGitRepository(path) {
			return "Not a git repository"
		}
		if existingPaths[path] {
			return "Already in this workspace"
		}
		for _, p := range existing {
			if p == path {
				return "Already added"
			}
			if strings.HasPrefix(path, p+"/") {
				return "Nested inside " + filepath.Base(p)
			}
			if strings.HasPrefix(p, path+"/") {
				return "Contains already-added " + filepath.Base(p)
			}
		}
		return ""
	})
	a.filePicker.SetSize(a.width, a.height)
	a.filePicker.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.filePicker.Show()
}

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
func (a *App) handleShowRenameProfileDialog(msg messages.ShowRenameProfileDialog) {
	a.dialogProfile = msg.Profile
	a.dialog = common.NewInputDialog(DialogRenameProfile, "Rename Profile", msg.Profile)
	a.dialog.SetMessage("Enter a new name for the profile.")
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateProfileName(s); err != nil {
			return err.Error()
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	a.dialog.SetValue(msg.Profile)
}

// handleRenameProfile renames a profile directory and updates all workspaces using it.
func (a *App) handleRenameProfile(msg messages.RenameProfile) tea.Cmd {
	oldName := strings.TrimSpace(msg.OldName)
	newName := strings.TrimSpace(msg.NewName)

	if oldName == "" || newName == "" || oldName == newName {
		return nil
	}

	if a.profileHasActiveWorkspaces(oldName) {
		return a.toast.ShowError("Cannot rename profile while workspaces have active sessions")
	}

	oldDir := filepath.Join(a.config.Paths.ProfilesRoot, oldName)
	newDir := filepath.Join(a.config.Paths.ProfilesRoot, newName)

	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		return a.toast.ShowError("Profile not found: " + oldName)
	}
	if _, err := os.Stat(newDir); err == nil {
		return a.toast.ShowError("Profile already exists: " + newName)
	}

	if err := os.Rename(oldDir, newDir); err != nil {
		logging.Error("Failed to rename profile directory: %v", err)
		return a.toast.ShowError("Failed to rename profile: " + err.Error())
	}

	if err := a.registry.RenameProfile(oldName, newName); err != nil {
		logging.Error("Failed to update workspaces with renamed profile: %v", err)
		_ = os.Rename(newDir, oldDir)
		return a.toast.ShowError("Failed to update workspaces: " + err.Error())
	}

	if a.config.UI.LastProfile == oldName {
		a.config.UI.LastProfile = newName
		_ = a.config.SaveUISettings()
	}

	// Update in-memory state
	for _, ws := range a.allWorkspaces {
		if ws.Profile == oldName {
			ws.Profile = newName
		}
	}

	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Profile renamed to '%s'", newName)))
	cmds = append(cmds, a.loadWorkspaces())
	cmds = append(cmds, func() tea.Msg { return common.ShowProfileManager{} })
	return a.safeBatch(cmds...)
}

// handleShowCreateProfileDialog shows the create profile input dialog.
func (a *App) handleShowCreateProfileDialog() {
	a.dialog = common.NewInputDialog(DialogCreateProfile, "Create Profile", "")
	a.dialog.SetMessage("Enter a name for the new profile.")
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateProfileName(s); err != nil {
			return err.Error()
		}
		profileDir := filepath.Join(a.config.Paths.ProfilesRoot, s)
		if _, err := os.Stat(profileDir); err == nil {
			return "profile already exists"
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleCreateProfile creates a new profile directory.
func (a *App) handleCreateProfile(msg messages.CreateProfile) tea.Cmd {
	name := strings.TrimSpace(msg.Name)
	if name == "" {
		return func() tea.Msg { return common.ShowProfileManager{} }
	}

	profileDir := filepath.Join(a.config.Paths.ProfilesRoot, name)
	if _, err := os.Stat(profileDir); err == nil {
		return a.toast.ShowError("Profile already exists: " + name)
	}
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		logging.Error("Failed to create profile directory: %v", err)
		return a.toast.ShowError("Failed to create profile: " + err.Error())
	}
	_ = config.InjectHooks(profileDir, a.config.Paths.HooksDir)

	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Profile '%s' created", name)))
	cmds = append(cmds, func() tea.Msg { return common.ShowProfileManager{} })
	return a.safeBatch(cmds...)
}

// handleShowDeleteProfileDialog shows the delete profile confirmation dialog.
func (a *App) handleShowDeleteProfileDialog(msg messages.ShowDeleteProfileDialog) {
	a.dialogProfile = msg.Profile
	a.dialog = common.NewConfirmDialog(
		DialogDeleteProfile,
		"Delete Profile",
		fmt.Sprintf("Delete profile '%s'? Workspaces using this profile will have their profile cleared.", msg.Profile),
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleDeleteProfile deletes a profile directory and clears it from all workspaces.
func (a *App) handleDeleteProfile(msg messages.DeleteProfile) tea.Cmd {
	profile := strings.TrimSpace(msg.Profile)
	if profile == "" {
		return nil
	}

	if a.profileHasActiveWorkspaces(profile) {
		return a.toast.ShowError("Cannot delete profile while workspaces have active sessions")
	}

	profileDir := filepath.Join(a.config.Paths.ProfilesRoot, profile)
	if _, err := os.Stat(profileDir); os.IsNotExist(err) {
		return a.toast.ShowError("Profile not found: " + profile)
	}

	if err := a.registry.ClearProfile(profile); err != nil {
		logging.Error("Failed to clear profile from workspaces: %v", err)
		return a.toast.ShowError("Failed to update workspaces: " + err.Error())
	}

	if err := os.RemoveAll(profileDir); err != nil {
		logging.Error("Failed to delete profile directory: %v", err)
		return a.toast.ShowError("Failed to delete profile: " + err.Error())
	}

	if a.config.UI.LastProfile == profile {
		a.config.UI.LastProfile = ""
		_ = a.config.SaveUISettings()
	}

	// Update in-memory state
	for _, ws := range a.allWorkspaces {
		if ws.Profile == profile {
			ws.Profile = ""
		}
	}

	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Profile '%s' deleted", profile)))
	cmds = append(cmds, a.loadWorkspaces())
	cmds = append(cmds, func() tea.Msg { return common.ShowProfileManager{} })
	return a.safeBatch(cmds...)
}

// handleShowRenameWorkspaceDialog shows the rename workspace dialog.
func (a *App) handleShowRenameWorkspaceDialog(msg messages.ShowRenameWorkspaceDialog) tea.Cmd {
	if msg.Workspace.IsPrimaryCheckout() {
		return a.toast.ShowError("Cannot rename the primary checkout")
	}
	if msg.Workspace.IsMainBranch() {
		return a.toast.ShowError("Cannot rename main/master branch")
	}
	a.dialogWorkspace = msg.Workspace
	a.dialog = common.NewInputDialog(DialogRenameWorkspace, "Rename Worktree", msg.Workspace.Name)
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateWorkspaceName(s); err != nil {
			return err.Error()
		}
		return ""
	})
	tabsInfo, _ := a.center.GetTabsInfoForWorkspace(string(msg.Workspace.ID()))
	hasAgentTabs := false
	for _, t := range tabsInfo {
		if t.Assistant != "" && t.Status != "stopped" {
			hasAgentTabs = true
			break
		}
	}
	if hasAgentTabs {
		a.dialog.SetMessage("Running agent sessions will be restarted.")
	}
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	a.dialog.SetValue(msg.Workspace.Name)
	return nil
}

// handleShowDeleteWorkspaceDialog shows the delete workspace dialog.
func (a *App) handleShowDeleteWorkspaceDialog(msg messages.ShowDeleteWorkspaceDialog) {
	a.dialogWorkspace = msg.Workspace

	title := "Delete Worktree"
	body := fmt.Sprintf("Delete worktree '%s' and its branch?", msg.Workspace.Name)

	if msg.Workspace.IsOrphaned() {
		switch msg.Workspace.Orphan {
		case data.OrphanMetadata:
			title = "Clean Up Orphan"
			body = fmt.Sprintf("Remove metadata for '%s'? (worktree directory is already missing)", msg.Workspace.Name)
		case data.OrphanDirectory:
			title = "Clean Up Orphan"
			body = fmt.Sprintf("Delete orphaned directory '%s'? (no workspace metadata references it)", msg.Workspace.Name)
		}
	}

	a.dialog = common.NewConfirmDialog(
		DialogDeleteWorkspace,
		title,
		body,
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowCustomizeTabDialog shows the customize tab dialog with 3 checkboxes.
func (a *App) handleShowCustomizeTabDialog() {
	if a.activeWorkspace == nil {
		return
	}
	a.dialog = common.NewInputDialog(DialogCustomizeTab, "New Claude Tab", "")
	a.dialog.SetInputHidden(true)
	a.dialog.SetMessage("Configure settings for this tab.")
	a.dialog.SetCheckbox("Immediately allow edits", a.config.UI.LastAllowEdits)
	a.dialog.SetCheckbox2("Sandboxed", a.config.UI.LastIsolated)
	a.dialog.SetCheckbox3("Bypass permissions", a.config.UI.LastSkipPermissions)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowCleanupTmuxDialog shows the tmux cleanup dialog.
func (a *App) handleShowCleanupTmuxDialog() {
	if a.dialog != nil && a.dialog.Visible() {
		return
	}
	a.dialog = common.NewConfirmDialog(
		DialogCleanupTmux,
		"Cleanup tmux sessions",
		fmt.Sprintf("Kill all medusa-* tmux sessions on server %q?", a.tmuxOptions.ServerName),
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// showCloseTabConfirmation shows a confirmation dialog before closing an agent tab.
func (a *App) showCloseTabConfirmation() {
	if a.dialog != nil && a.dialog.Visible() {
		return
	}
	a.dialog = common.NewConfirmDialog(
		DialogCloseTab,
		"Close Tab",
		"Close this agent tab? The running agent will be terminated.",
	)
	a.dialog.SetDefaultConfirm(true)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowSettingsDialog shows the settings dialog.
func (a *App) handleShowSettingsDialog() {
	a.settingsDialog = common.NewSettingsDialog(
		common.ThemeID(a.config.UI.Theme),
		a.config.UI.ShowKeymapHints,
		a.config.UI.HideSidebar,
		a.config.UI.HideTerminal,
		a.config.UI.AutoStartAgent,
		a.config.UI.SyncProfilePlugins,
		a.config.UI.GlobalPermissions,
		a.config.UI.NotificationSound,
		a.config.UI.TmuxPersistence,
	)
	a.settingsDialog.SetSize(a.width, a.height)
	a.settingsDialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)

	if a.updateAvailable != nil {
		a.settingsDialog.SetUpdateInfo(
			a.updateAvailable.CurrentVersion,
			a.updateAvailable.LatestVersion,
			a.updateAvailable.UpdateAvailable,
		)
	} else {
		a.settingsDialog.SetUpdateInfo(a.version, "", false)
	}

	a.settingsDialog.Show()
}

// handleShowThemeEditor opens the theme selection dialog.
func (a *App) handleShowThemeEditor() {
	currentTheme := common.ThemeID(a.config.UI.Theme)
	if currentTheme == "" {
		currentTheme = common.ThemeGruvbox
	}
	a.themeDialog = common.NewThemeDialog(currentTheme)
	a.themeDialog.SetSize(a.width, a.height)
	a.themeDialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.themeDialog.Show()
}

// handleThemeResult handles theme dialog completion.
func (a *App) handleThemeResult(msg common.ThemeResult) tea.Cmd {
	a.themeDialog = nil
	if msg.Confirmed {
		common.SetCurrentTheme(msg.Theme)
		a.config.UI.Theme = string(msg.Theme)
		a.styles = common.DefaultStyles()
		a.dashboard.SetStyles(a.styles)
		a.sidebar.SetStyles(a.styles)
		a.sidebarTerminal.SetStyles(a.styles)
		a.center.SetStyles(a.styles)
		a.toast.SetStyles(a.styles)
		a.helpOverlay.SetStyles(a.styles)
		if a.filePicker != nil {
			a.filePicker.SetStyles(a.styles)
		}
		if a.settingsDialog != nil {
			a.settingsDialog.SetTheme(msg.Theme)
			a.settingsDialog.Show()
		}
		if err := a.config.SaveUISettings(); err != nil {
			return a.toast.ShowWarning("Failed to save theme")
		}
		return nil
	}
	if a.settingsDialog != nil {
		a.settingsDialog.Show()
	}
	return nil
}

// handleThemePreview handles live theme preview.
func (a *App) handleThemePreview(msg common.ThemePreview) {
	common.SetCurrentTheme(msg.Theme)
	a.styles = common.DefaultStyles()
	a.dashboard.SetStyles(a.styles)
	a.sidebar.SetStyles(a.styles)
	a.sidebarTerminal.SetStyles(a.styles)
	a.center.SetStyles(a.styles)
	a.toast.SetStyles(a.styles)
	a.helpOverlay.SetStyles(a.styles)
	if a.filePicker != nil {
		a.filePicker.SetStyles(a.styles)
	}
}

// handleShowSoundPicker opens the sound selection dialog.
func (a *App) handleShowSoundPicker() {
	a.soundPicker = common.NewSoundPicker(a.config.UI.NotificationSound)
	a.soundPicker.SetSize(a.width, a.height)
	a.soundPicker.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.soundPicker.Show()
}

// handleSoundPickerResult handles sound picker dialog completion.
func (a *App) handleSoundPickerResult(msg common.SoundPickerResult) tea.Cmd {
	a.soundPicker = nil
	if msg.Confirmed && a.settingsDialog != nil {
		a.settingsDialog.SetNotificationSound(msg.Sound)
	}
	if a.settingsDialog != nil {
		a.settingsDialog.Show()
	}
	return nil
}

// handleSoundPreview plays a preview of the selected sound.
func (a *App) handleSoundPreview(msg common.SoundPreview) tea.Cmd {
	if msg.Sound == "" {
		return nil
	}
	sound := msg.Sound
	return func() tea.Msg {
		_ = exec.Command("killall", "afplay").Run()
		_ = exec.Command("afplay", "/System/Library/Sounds/"+sound+".aiff").Start()
		return nil
	}
}

// handleSettingsResult handles settings dialog result.
func (a *App) handleSettingsResult(msg common.SettingsResult) tea.Cmd {
	a.settingsDialog = nil
	if msg.Confirmed {
		common.SetCurrentTheme(msg.Theme)
		a.config.UI.Theme = string(msg.Theme)
		a.styles = common.DefaultStyles()
		a.dashboard.SetStyles(a.styles)
		a.sidebar.SetStyles(a.styles)
		a.sidebarTerminal.SetStyles(a.styles)
		a.center.SetStyles(a.styles)
		a.toast.SetStyles(a.styles)
		a.helpOverlay.SetStyles(a.styles)
		if a.filePicker != nil {
			a.filePicker.SetStyles(a.styles)
		}

		a.setKeymapHintsEnabled(msg.ShowKeymapHints)
		a.config.UI.AutoStartAgent = msg.AutoStartAgent

		oldSync := a.config.UI.SyncProfilePlugins
		a.config.UI.SyncProfilePlugins = msg.SyncProfilePlugins
		if msg.SyncProfilePlugins && !oldSync {
			_ = config.SyncAllProfiles(a.config.Paths.ProfilesRoot)
		} else if !msg.SyncProfilePlugins && oldSync {
			_ = config.UnsyncAllProfiles(a.config.Paths.ProfilesRoot)
		}

		a.config.UI.NotificationSound = msg.NotificationSound
		oldGlobalPerms := a.config.UI.GlobalPermissions
		a.config.UI.GlobalPermissions = msg.GlobalPermissions

		wasHidden := a.config.UI.HideSidebar
		a.config.UI.HideSidebar = msg.HideSidebar
		a.layout.SetSidebarHidden(msg.HideSidebar)
		if msg.HideSidebar && a.focusedPane == messages.PaneSidebar {
			a.focusPane(messages.PaneCenter)
		}

		a.config.UI.HideTerminal = msg.HideTerminal
		a.layout.SetTerminalHidden(msg.HideTerminal)
		if msg.HideTerminal && a.focusedPane == messages.PaneTerminal {
			a.focusPane(messages.PaneCenter)
		}

		a.layout.Resize(a.width, a.height)
		a.updateLayout()

		var sidebarCmds []tea.Cmd
		if msg.HideSidebar && !wasHidden {
			a.sidebarTerminal.CloseAll()
			if a.fileWatcher != nil {
				a.unwatchAllWorkspaces()
			}
		} else if !msg.HideSidebar && wasHidden {
			if a.activeWorkspace != nil {
				if termCmd := a.sidebarTerminal.SetWorkspace(a.activeWorkspace); termCmd != nil {
					sidebarCmds = append(sidebarCmds, termCmd)
				}
				sidebarCmds = append(sidebarCmds, a.requestGitStatus(a.activeWorkspace.PrimaryWorktreeRoot()))
				if a.fileWatcher != nil {
					_ = a.fileWatcher.Watch(a.activeWorkspace.PrimaryWorktreeRoot())
				}
			}
		}

		tmuxPersistenceChanged := a.config.UI.TmuxPersistence != msg.TmuxPersistence
		a.config.UI.TmuxPersistence = msg.TmuxPersistence

		if msg.GlobalPermissions && !oldGlobalPerms {
			if a.permissionWatcher == nil {
				a.initPermissionWatcher()
			}
			sidebarCmds = append(sidebarCmds, a.startPermissionWatcher())
			a.watchAllWorkspacePermissions()
			global, err := config.LoadGlobalPermissions(a.config.Paths.GlobalPermissionsPath)
			if err == nil {
				_ = config.InjectIntoAllProfiles(a.config.Paths.ProfilesRoot, global)
			}
		} else if !msg.GlobalPermissions && oldGlobalPerms {
			a.unwatchAllWorkspacePermissions()
		}

		if err := a.config.SaveUISettings(); err != nil {
			return a.toast.ShowWarning("Failed to save settings")
		}
		cmds := append(sidebarCmds, a.toast.ShowSuccess("Settings saved"))
		if tmuxPersistenceChanged {
			cmds = append(cmds, a.toast.ShowInfo("Restart Medusa to apply tmux persistence change"))
		}
		return a.safeBatch(cmds...)
	}
	return nil
}

// handleCreateWorkspace handles the CreateWorkspace message.
func (a *App) handleCreateWorkspace(msg messages.CreateWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	if len(msg.Repos) > 0 && msg.Name != "" {
		workspacePath := filepath.Join(a.config.Paths.WorkspacesRoot, msg.Name)
		if len(msg.Repos) > 1 {
			workspacePath = filepath.Join(workspacePath, msg.Repos[0].Name)
		}
		pending := data.NewWorkspace(msg.Name, msg.Name, "", msg.Repos[0].Path, workspacePath)
		if cmd := a.dashboard.SetWorkspaceCreating(pending, true); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Start the fetch+create flow based on branch mode
	switch msg.BranchMode {
	case git.BranchModeCheckedOut:
		cmds = append(cmds, a.fetchCheckedOutBase(msg.Repos, msg.Name, ""))
	case git.BranchModeCustom:
		cmds = append(cmds, a.fetchCustomBase(msg.Repos, msg.Name, "", msg.CustomBranch))
	default: // BranchModeRemoteMain
		cmds = append(cmds, a.fetchRemoteBase(msg.Repos, msg.Name, ""))
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

