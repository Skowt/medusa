package app

import (
	"fmt"
	"path/filepath"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/perf"
	"github.com/Skowt/medusa/internal/ui/common"
)

// Update handles all messages with panic recovery.
func (a *App) Update(msg tea.Msg) (model tea.Model, cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("panic in app.Update: %v\n%s", r, debug.Stack())
			a.err = fmt.Errorf("internal error: %v", r)
			model = a
			cmd = nil
		}
	}()
	return a.update(msg)
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer perf.Time("update")()
	var cmds []tea.Cmd
	if perf.Enabled() {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.KeyReleaseMsg, tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.PasteMsg:
			a.markInput()
		}
	}

	// Handle dialog result first (arrives after dialog is hidden)
	if result, ok := msg.(common.DialogResult); ok {
		logging.Info("Received DialogResult: id=%s confirmed=%v", result.ID, result.Confirmed)
		switch result.ID {
		case DialogAddRepos, DialogAddReposToWorkspace, DialogCreateWorkspace, DialogDeleteWorkspace, DialogCustomizeTab, DialogQuit, DialogCleanupTmux, DialogSetProfile, DialogRenameWorkspace, DialogRenameProfile, DialogCreateProfile, DialogDeleteProfile, DialogCommit,
			DialogSelectBranchMode, DialogCustomBranch, DialogSelectRecentRepos, DialogCloseTab, DialogSetProfileForCreate, DialogQuickDuplicate,
			DialogArchiveWorkspace, DialogArchivedWorkspace, DialogSetNote,
			DialogSetWorkspaceGroup, DialogSetGroupForCreate, DialogRenameGroup, DialogDeleteGroup:
			return a, a.safeCmd(a.handleDialogResult(result))
		}
		// If not an App-level dialog, let it fall through to components
		// Currently only Center uses custom dialogs
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return a, a.safeCmd(cmd)
	}

	// Handle help overlay input (highest priority when visible)
	if a.helpOverlay.Visible() {
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			var cmd tea.Cmd
			a.helpOverlay, _, cmd = a.helpOverlay.Update(msg)
			return a, a.safeCmd(cmd)
		case tea.MouseWheelMsg:
			a.helpOverlay, _, _ = a.helpOverlay.Update(msg)
			return a, nil
		case tea.MouseClickMsg:
			if msg.Button == tea.MouseLeft {
				// First check if clicking on a link inside the dialog
				var cmd tea.Cmd
				a.helpOverlay, _, cmd = a.helpOverlay.Update(msg)
				if cmd != nil {
					return a, a.safeCmd(cmd)
				}
				// Close if clicking outside the dialog
				if !a.helpOverlay.ContainsClick(msg.X, msg.Y) {
					a.helpOverlay.Hide()
				}
				return a, nil
			}
		}
	}

	// Allow clicking to dismiss error overlays
	if mouseMsg, ok := msg.(tea.MouseClickMsg); ok && mouseMsg.Button == tea.MouseLeft {
		if a.err != nil {
			a.err = nil
			return a, nil
		}
	}

	if a.routeOverlayInput(msg, &cmds) {
		return a, a.safeBatch(cmds...)
	}

	switch msg := msg.(type) {
	case tea.KeyboardEnhancementsMsg:
		a.keyboardEnhancements = msg
		logging.Info("Keyboard enhancements: disambiguation=%t event_types=%t", msg.SupportsKeyDisambiguation(), msg.SupportsEventTypes())

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		a.layout.Resize(msg.Width, msg.Height)
		a.updateLayout()
		// Update help overlay size for accurate hit-testing after resize
		if a.helpOverlay.Visible() {
			a.helpOverlay.SetSize(a.width, a.height)
		}
		if a.creationOverlay != nil {
			a.creationOverlay.SetSize(a.width, a.height)
		}

	case tea.MouseClickMsg:
		if a.monitorMode {
			if cmd := a.handleMonitorModeClick(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
			break
		}
		if cmd := a.routeMouseClick(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseWheelMsg:
		if a.monitorMode {
			break
		}
		if cmd := a.routeMouseWheel(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseMotionMsg:
		if a.monitorMode {
			break
		}
		if cmd := a.routeMouseMotion(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseReleaseMsg:
		if a.monitorMode {
			break
		}
		if cmd := a.routeMouseRelease(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.PasteMsg:
		// Handle paste in monitor mode - forward to selected tile
		if a.monitorMode && a.focusedPane == messages.PaneMonitor {
			tabs := a.filterMonitorTabs(a.center.MonitorTabs())
			if len(tabs) > 0 {
				idx := a.center.MonitorSelectedIndex(len(tabs))
				if cmd := a.center.HandleMonitorInput(tabs[idx].ID, msg); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			break
		}
		// Non-monitor paste handling falls through to focused pane
		switch a.focusedPane {
		case messages.PaneCenter:
			newCenter, cmd := a.center.Update(msg)
			a.center = newCenter
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneTerminal:
			newTerm, cmd := a.sidebarTerminal.Update(msg)
			a.sidebarTerminal = newTerm
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case prefixTimeoutMsg:
		if msg.token == a.prefixToken && a.prefixActive {
			a.exitPrefix()
		}

	case markReadMsg:
		if msg.token == a.markReadToken {
			a.dashboard.MarkRead(msg.wsID)
		}

	case tea.KeyPressMsg:
		if cmd := a.handleKeyPress(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.WorkspacesLoaded:
		cmds = append(cmds, a.handleWorkspacesLoaded(msg)...)

	case messages.WorkspaceActivated:
		cmds = append(cmds, a.handleWorkspaceActivated(msg)...)

	case messages.WorkspacePreviewed:
		cmds = append(cmds, a.handleWorkspacePreviewed(msg)...)

	case messages.ShowWelcome:
		a.goHome()

	case messages.ToggleMonitor:
		if cmd := a.toggleMonitorMode(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ToggleHelp:
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()

	case messages.ToggleKeymapHints:
		a.setKeymapHintsEnabled(!a.config.UI.ShowKeymapHints)
		if err := a.config.SaveUISettings(); err != nil {
			cmds = append(cmds, a.toast.ShowWarning("Failed to save keymap setting"))
		}

	case messages.ToggleTerminalCollapse:
		a.layout.ToggleTerminalCollapsed()
		a.updateLayout()

	case messages.ShowQuitDialog:
		a.showQuitDialog()

	case messages.RefreshDashboard:
		cmds = append(cmds, a.loadWorkspaces())

	case messages.WorkspaceCreatedWithWarning:
		cmds = append(cmds, a.handleWorkspaceCreatedWithWarning(msg)...)

	case messages.WorkspaceCreated:
		cmds = append(cmds, a.handleWorkspaceCreated(msg)...)

	case messages.WorkspaceSetupComplete:
		if cmd := a.handleWorkspaceSetupComplete(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.WorkspaceCreateFailed:
		if cmd := a.handleWorkspaceCreateFailed(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.GitStatusResult:
		if cmd := a.handleGitStatusResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowQuickDuplicateDialog:
		a.handleShowQuickDuplicateDialog(msg)

	case messages.ShowCreateWorkspaceDialog:
		a.handleShowCreateWorkspaceDialog()

	case messages.ShowRenameWorkspaceDialog:
		if cmd := a.handleShowRenameWorkspaceDialog(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.RenameWorkspace:
		cmds = append(cmds, a.handleRenameWorkspace(msg)...)

	case messages.WorkspaceRenameFailed:
		if cmd := a.handleWorkspaceRenameFailed(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowArchiveWorkspaceDialog:
		a.handleShowArchiveWorkspaceDialog(msg)

	case messages.ArchiveWorkspace:
		cmds = append(cmds, a.handleArchiveWorkspace(msg)...)

	case messages.ShowArchivedWorkspaceDialog:
		a.handleShowArchivedWorkspaceDialog(msg)

	case messages.UnarchiveWorkspace:
		cmds = append(cmds, a.handleUnarchiveWorkspace(msg)...)

	case messages.ShowDeleteWorkspaceDialog:
		a.handleShowDeleteWorkspaceDialog(msg)

	case messages.ShowSetWorkspaceProfileDialog:
		a.handleShowSetWorkspaceProfileDialog(msg)

	case messages.SetWorkspaceProfile:
		if cmd := a.handleSetWorkspaceProfile(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.SetWorkspaceStatus:
		if cmd := a.handleSetWorkspaceStatus(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowSetWorkspaceNoteDialog:
		a.handleShowSetWorkspaceNoteDialog(msg)

	case messages.SetWorkspaceNote:
		if cmd := a.handleSetWorkspaceNote(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowAddReposToWorkspaceDialog:
		a.showAddReposToWorkspaceFilePicker(msg.Workspace)

	case messages.AddReposToWorkspace:
		cmds = append(cmds, a.handleAddReposToWorkspace(msg))

	case messages.ReposAddedToWorkspace:
		cmds = append(cmds, a.loadWorkspaces())
		if msg.Workspace != nil {
			a.activeWorkspace = msg.Workspace
			a.center.SetWorkspace(msg.Workspace)
			a.center.SetInfoContent(a.renderWorkspaceInfo())
		}

	case messages.ReposAddFailed:
		cmds = append(cmds, a.toast.ShowError("Failed to add repos: "+msg.Err.Error()))

	case messages.ShowRenameProfileDialog:
		a.handleShowRenameProfileDialog(msg)

	case messages.RenameProfile:
		if cmd := a.handleRenameProfile(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowCreateProfileDialog:
		a.handleShowCreateProfileDialog()

	case messages.CreateProfile:
		if cmd := a.handleCreateProfile(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowDeleteProfileDialog:
		a.handleShowDeleteProfileDialog(msg)

	case messages.DeleteProfile:
		if cmd := a.handleDeleteProfile(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowCustomizeTabDialog:
		a.handleShowCustomizeTabDialog()

	case messages.ShowSettingsDialog:
		a.handleShowSettingsDialog()

	case messages.ShowCleanupTmuxDialog:
		a.handleShowCleanupTmuxDialog()

	case common.ThemePreview:
		a.handleThemePreview(msg)

	case common.ShowThemeEditor:
		a.handleShowThemeEditor()

	case common.TriggerUpgradeRequest:
		if cmd := a.handleTriggerUpgrade(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case common.ThemeResult:
		if cmd := a.handleThemeResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case common.ShowSoundPicker:
		a.handleShowSoundPicker()

	case common.SoundPreview:
		if cmd := a.handleSoundPreview(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case common.SoundPickerResult:
		if cmd := a.handleSoundPickerResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case common.SettingsResult:
		if cmd := a.handleSettingsResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.WorkspaceFetchDone:
		cmds = append(cmds, a.handleWorkspaceFetchDone(msg)...)

	case messages.WorkspaceWorktreeDone:
		if cmd := a.handleWorkspaceWorktreeDone(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.CreateWorkspace:
		cmds = append(cmds, a.handleCreateWorkspace(msg)...)

	case messages.DeleteWorkspace:
		cmds = append(cmds, a.handleDeleteWorkspace(msg)...)

	case messages.DeleteOrphanWorkspace:
		if msg.Workspace != nil {
			if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root(), true); cmd != nil {
				cmds = append(cmds, cmd)
			}
			cmds = append(cmds, a.deleteOrphanWorkspace(msg.Workspace))
		}

	case messages.CleanupTmuxSessions:
		if cmd := a.cleanupAllTmuxSessions(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.OpenDiff:
		if cmd := a.handleOpenDiff(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.CloseTab:
		a.dialogCloseTabIdx = -1
		a.showCloseTabDialog()

	case messages.CloseTabAt:
		a.dialogCloseTabIdx = msg.Index
		a.showCloseTabDialog()

	case messages.ConfirmCloseTab:
		if msg.Index == -1 {
			cmds = append(cmds, a.center.CloseActiveTab())
		} else {
			cmds = append(cmds, a.center.CloseTabAtIndex(msg.Index))
		}

	case messages.ConfirmRestartTab:
		if msg.Index == -1 {
			cmds = append(cmds, a.center.RestartActiveTab())
		} else {
			cmds = append(cmds, a.center.RestartTabAtIndex(msg.Index))
		}

	case messages.LaunchAgent:
		if msg.Workspace != nil && msg.Workspace.Profile == "" {
			a.pendingProfileLaunch = msg.Assistant
			a.pendingProfileLaunchRoot = msg.Workspace.Root()
			a.handleShowSetWorkspaceProfileDialog(messages.ShowSetWorkspaceProfileDialog{Workspace: msg.Workspace})
			break
		}
		// Trust the workspace root (parent of all repo worktrees) — always needed with uniform layout
		if msg.Workspace != nil {
			profileDir := ""
			if msg.Workspace.Profile != "" {
				profileDir = filepath.Join(a.config.Paths.ProfilesRoot, msg.Workspace.Profile)
			}
			if err := config.InjectTrustedDirectory(msg.Workspace.Root(), profileDir); err != nil {
				logging.Error("Failed to inject trusted directory: %v", err)
				cmds = append(cmds, a.toast.ShowError("Profile config corrupt: "+err.Error()))
			}
			if msg.AllowEdits {
				_ = config.InjectAllowEdits(msg.Workspace.Root())
			}
		}
		if cmd := a.handleLaunchAgent(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	default:
		if a.routePTYMsg(msg, &cmds) {
			break
		}
		if a.routeSystemMsg(msg, &cmds) {
			break
		}
		if a.routeGroupMsg(msg, &cmds) {
			break
		}
		// Forward unknown messages to center pane (e.g., commit viewer internal messages)
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return a, a.safeBatch(cmds...)
}
