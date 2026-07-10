package app

import (
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/update"
)

func (a *App) handleShowSettingsDialog() {
	a.settingsDialog = common.NewSettingsDialog(
		common.ThemeID(a.config.UI.Theme),
		a.config.UI.ShowKeymapHints,
		a.config.UI.HideSidebar,
		a.config.UI.HideTerminal,
		a.config.UI.AutoStartAgent,
		a.config.UI.SyncProfilePlugins,
		a.config.UI.GlobalPermissions,
		a.config.UI.CompoundApprove,
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
	a.settingsDialog.SetSelfUpdateBlocked(a.selfUpdate.Blocked(), update.ReinstallCommand)

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
				if !a.activeWorkspace.Archived() {
					sidebarCmds = append(sidebarCmds, a.requestGitStatus(a.activeWorkspace.PrimaryWorktreeRoot()))
					if a.fileWatcher != nil {
						_ = a.fileWatcher.Watch(a.activeWorkspace.PrimaryWorktreeRoot())
					}
				}
			}
		}

		oldCompoundApprove := a.config.UI.CompoundApprove
		a.config.UI.CompoundApprove = msg.CompoundApprove
		if msg.CompoundApprove != oldCompoundApprove {
			if exe, err := os.Executable(); err == nil {
				hookBin := filepath.Join(filepath.Dir(exe), "medusa-approve-compound")
				if msg.CompoundApprove {
					_ = config.InjectCompoundApproveHookAllProfiles(a.config.Paths.ProfilesRoot, hookBin)
				} else {
					_ = config.RemoveCompoundApproveHookAllProfiles(a.config.Paths.ProfilesRoot, hookBin)
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
