package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/validation"
)

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
	if err := config.InjectHooks(profileDir, a.config.Paths.HooksDir, config.ResolveHookEmitBinary()); err != nil {
		logging.Error("Failed to inject hooks: %v", err)
		return a.toast.ShowError("Profile config corrupt: " + err.Error())
	}

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
	a.dialog = common.NewInputDialog(DialogRenameWorkspace, "Rename Workspace", msg.Workspace.Name)
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateWorkspaceName(s); err != nil {
			return err.Error()
		}
		if a.workspaceNameExists(s, msg.Workspace.ID()) {
			return "workspace with this name already exists"
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

// handleShowArchiveWorkspaceDialog shows the archive workspace dialog.
func (a *App) handleShowArchiveWorkspaceDialog(msg messages.ShowArchiveWorkspaceDialog) {
	a.dialogWorkspace = msg.Workspace
	a.dialog = common.NewConfirmDialog(
		DialogArchiveWorkspace,
		"Archive Workspace",
		fmt.Sprintf("Archive '%s'? Agent sessions will be stopped.", msg.Workspace.Name),
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowArchivedWorkspaceDialog shows the archived-workspace actions dialog.
func (a *App) handleShowArchivedWorkspaceDialog(msg messages.ShowArchivedWorkspaceDialog) {
	a.dialogWorkspace = msg.Workspace
	a.dialog = common.NewSelectDialog(
		DialogArchivedWorkspace,
		"Archived Workspace",
		fmt.Sprintf("'%s' is archived. Unarchive to restart agents, or delete it permanently.", msg.Workspace.Name),
		[]string{"Unarchive", "Delete", "Cancel"},
	)
	a.dialog.SetVerticalLayout(true)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
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

// handleShowCustomizeTabDialog shows the customize tab dialog: a Sandbox
// checkbox with an "Allow unsandboxed commands" sub-checkbox, plus a
// "Starting Mode" select that wires through to claude --permission-mode.
func (a *App) handleShowCustomizeTabDialog() {
	if a.activeWorkspace == nil {
		return
	}
	a.dialog = common.NewInputDialog(DialogCustomizeTab, "New Claude Tab", "")
	a.dialog.SetInputHidden(true)
	a.dialog.SetMessage("Configure settings for this tab.")
	a.dialog.SetSelect("Starting Mode:", permissionModeOptions(), defaultPermissionMode(a.config.UI.LastPermissionMode))
	a.dialog.SetCheckbox("Sandboxed", a.config.UI.LastIsolated)
	a.dialog.SetCheckboxDescription(1, "Sandboxes subprocess calls including Bash commands. Tool use does not use sandbox (e.g. Write, Edit).")
	a.dialog.SetCheckbox2("Allow unsandboxed commands", a.config.UI.LastAllowUnsandboxedCommands)
	a.dialog.SetCheckboxDescription(2, "Allows Claude to try run blocked commands outside of the sandbox, using the user's allowed permissions. Do not use in 'Bypass Permissions' mode.")
	a.dialog.SetCheckbox2RequiresFirst(true)
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

// showCloseTabDialog shows the tab-actions dialog: close, restart, or
// cancel. Restart tears down the current tmux session and spawns a fresh
// one: for agent tabs it reuses the Claude session ID via `claude --resume`
// so the conversation continues; for script tabs it reruns the same shell
// command. Dialog copy is tailored to the target tab's type.
func (a *App) showCloseTabDialog() {
	if a.dialog != nil && a.dialog.Visible() {
		return
	}
	description := "Restart launches a fresh claude process using the same session " +
		"(useful after upgrading claude). Close ends the session."
	if a.center.TabAssistantAt(a.dialogCloseTabIdx) == "script" {
		description = "Restart kills the current process and re-runs the same command " +
			"in a fresh tmux session. Close stops the process and removes the tab."
	}
	a.dialog = common.NewSelectDialog(
		DialogCloseTab,
		"Tab Actions",
		description,
		[]string{"Close", "Restart", "Cancel"},
	)
	a.dialog.SetVerticalLayout(true)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowSettingsDialog shows the settings dialog.
