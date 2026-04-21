package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/validation"
)

func (a *App) handleWorkspacesLoaded(msg messages.WorkspacesLoaded) []tea.Cmd {
	a.allWorkspaces = msg.Workspaces
	a.dashboard.SetWorkspaces(a.allWorkspaces)
	// Restore hook states from workspace JSON so indicators (e.g. '!') survive restarts.
	a.restoreHookStatesFromWorkspaces()
	var cmds []tea.Cmd
	if syncCmd := a.syncActiveWorkspacesToDashboard(); syncCmd != nil {
		cmds = append(cmds, syncCmd)
	}
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
		for _, ws := range a.allWorkspaces {
			if !ws.IsOrphaned() && !ws.Archived() {
				autoActivateRoot = ws.Root()
				break
			}
		}
	}

	// Eagerly restore agent tabs for all workspaces on startup.
	// Skip the workspace that will be auto-activated (activation handles its own restore).
	// Skip orphaned workspaces — they have no live sessions.
	for _, ws := range a.allWorkspaces {
		if ws.IsOrphaned() || ws.Archived() {
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
		// Auto-activate the first eligible workspace on initial startup
		for _, ws := range a.allWorkspaces {
			if ws.Root() == autoActivateRoot {
				w := ws
				cmds = append(cmds, func() tea.Msg {
					return messages.WorkspaceActivated{
						Workspace: w,
					}
				})
				break
			}
		}
	}

	return cmds
}

// handleWorkspaceActivated processes the WorkspaceActivated message.
func (a *App) handleWorkspaceActivated(msg messages.WorkspaceActivated) []tea.Cmd {
	var cmds []tea.Cmd
	alreadyActive := a.activeWorkspace != nil && msg.Workspace != nil &&
		a.activeWorkspace.ID() == msg.Workspace.ID()
	a.activeWorkspace = msg.Workspace
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorkspace(msg.Workspace)
	a.sidebar.SetWorkspace(msg.Workspace)
	// Invalidate any pending delayed mark-read from preview scrolling.
	a.markReadToken++
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

	// Refresh git status and set up file watching (skip when sidebar is hidden
	// or the workspace is archived — archived workspaces don't change on disk).
	if msg.Workspace != nil && !a.layout.SidebarHidden() && !msg.Workspace.Archived() {
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
		// Only agent tabs suppress auto-launch — a script tab created by the
		// `run` command shouldn't prevent the initial Claude tab from opening.
		if !a.center.HasAgentTabsForWorkspace(wsID) && !workspaceHasLiveAgentTabs(msg.Workspace) {
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

	// Focus center pane when workspace has active tabs.
	// If the workspace was already active and activated via mouse click,
	// keep focus on the dashboard instead. Enter key always focuses center.
	if msg.Workspace != nil && (!alreadyActive || !msg.ViaClick) {
		wsID := string(msg.Workspace.ID())
		if a.center.HasTabsForWorkspace(wsID) || workspaceHasLiveTabs(msg.Workspace) {
			if a.monitorMode {
				a.focusPane(messages.PaneMonitor)
			} else {
				a.focusPane(messages.PaneCenter)
			}
		}
	} else if alreadyActive && msg.ViaClick {
		a.focusPane(messages.PaneDashboard)
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
				if !state.Exists || !state.HasLivePane {
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
	// Delay marking as read so quickly scrolling through workspaces
	// does not clear the unread indicator. Only mark read after 1s.
	a.markReadToken++
	if msg.Workspace != nil {
		token := a.markReadToken
		wsID := string(msg.Workspace.ID())
		cmds = append(cmds, tea.Tick(time.Second, func(time.Time) tea.Msg {
			return markReadMsg{token: token, wsID: wsID}
		}))
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
	a.dialogWorkspace = &data.Workspace{Repos: msg.Repos, Profile: msg.Profile, CopyIgnored: msg.CopyIgnored, Group: msg.Group}
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
		if a.workspaceNameExists(s) {
			return "workspace with this name already exists"
		}
		for _, repo := range a.dialogWorkspace.Repos {
			if git.BranchExists(repo.Path, s) {
				return fmt.Sprintf("branch already exists in %s", repo.Name)
			}
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
		if git.IsWorktree(path) {
			return "Worktrees cannot be used as workspace sources"
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
		if git.IsWorktree(path) {
			return "Worktrees cannot be used as workspace sources"
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
