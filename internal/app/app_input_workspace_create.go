package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/tmux"
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
		autoActivateRoot = a.startupWorkspaceRoot()
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

// startupWorkspaceRoot picks the workspace to open on a cold start: the one
// that was active when medusa last exited, falling back to the first eligible
// workspace in the list when that one is gone, archived, orphaned, or belongs
// to another profile.
func (a *App) startupWorkspaceRoot() string {
	var first string
	for _, ws := range a.allWorkspaces {
		if ws.IsOrphaned() || ws.Archived() {
			continue
		}
		if a.config != nil && a.config.UI.LastWorkspace != "" &&
			string(ws.ID()) == a.config.UI.LastWorkspace {
			return ws.Root()
		}
		if first == "" {
			first = ws.Root()
		}
	}
	return first
}

// rememberLastWorkspace persists ws as the workspace to reopen next time.
// Writes only on a real change: activation happens every time the user moves
// between workspaces, and the config file is rewritten in full on each save.
func (a *App) rememberLastWorkspace(ws *data.Workspace) {
	if ws == nil || a.config == nil {
		return
	}
	wsID := string(ws.ID())
	if a.config.UI.LastWorkspace == wsID {
		return
	}
	a.config.UI.LastWorkspace = wsID
	if err := a.config.SaveUISettings(); err != nil {
		logging.Warn("Failed to save last workspace: %v", err)
	}
}

// handleWorkspaceActivated processes the WorkspaceActivated message.
func (a *App) handleWorkspaceActivated(msg messages.WorkspaceActivated) []tea.Cmd {
	var cmds []tea.Cmd
	alreadyActive := a.activeWorkspace != nil && msg.Workspace != nil &&
		a.activeWorkspace.ID() == msg.Workspace.ID()
	a.activeWorkspace = msg.Workspace
	a.rememberLastWorkspace(msg.Workspace)
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
			launch := a.lastUsedLaunch(msg.Workspace, a.config.UI.LastAssistant)
			cmds = append(cmds, func() tea.Msg { return launch })
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
	// Archived workspaces keep the tab snapshot taken just before archive
	// killed their tmux sessions; syncing would rewrite it to "stopped" and
	// break the agent relaunch on unarchive. (Previewing an archived
	// workspace makes it the active one, so the sync tick reaches it here.)
	if ws.Archived() {
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
