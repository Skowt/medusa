package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// routeSystemMsg dispatches low-activity, well-scoped messages to their
// handlers. Returns handled=true when the message was consumed here.
func (a *App) routeSystemMsg(msg tea.Msg, cmds *[]tea.Cmd) bool {
	switch msg := msg.(type) {
	case messages.PermissionWatcherEvent:
		*cmds = append(*cmds, a.handlePermissionWatcherEvent(msg)...)
	case messages.PermissionDetected:
		if cmd := a.handlePermissionDetected(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ShowPermissionsDialog:
		if len(a.pendingPermissions) > 0 {
			a.permissionsDialog = common.NewPermissionsDialog(a.pendingPermissions)
			a.permissionsDialog.SetSize(a.width, a.height)
			a.permissionsDialog.Show()
		}
	case common.ShowPermissionsEditor:
		global, err := config.LoadGlobalPermissions(a.config.Paths.GlobalPermissionsPath)
		if err != nil {
			*cmds = append(*cmds, a.toast.ShowError("Failed to load global permissions"))
		} else {
			a.permissionsEditor = common.NewPermissionsEditor(global.Allow, global.Deny)
			a.permissionsEditor.SetSize(a.width, a.height)
			a.permissionsEditor.Show()
		}
	case common.ShowSandboxRulesEditor:
		rules, err := config.LoadSandboxRules(a.config.Paths.SandboxRulesPath)
		if err != nil {
			*cmds = append(*cmds, a.toast.ShowError("Failed to load sandbox rules"))
		} else {
			a.sandboxRulesEditor = common.NewSandboxRulesEditor(rules.Rules)
			a.sandboxRulesEditor.SetSize(a.width, a.height)
			a.sandboxRulesEditor.Show()
		}
	case messages.SandboxRulesEditorResult:
		if cmd := a.handleSandboxRulesEditorResult(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case common.ShowProfileManager:
		profiles := a.listProfiles()
		a.profileManager = common.NewProfileManager(profiles)
		a.profileManager.SetSize(a.width, a.height)
		a.profileManager.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
		a.profileManager.Show()
	case common.ProfileManagerResult:
		a.profileManager = nil
		// Re-show settings dialog after closing profile manager
		a.handleShowSettingsDialog()
	case messages.PermissionsDialogResult:
		if cmd := a.handlePermissionsDialogResult(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.PermissionsEditorResult:
		if cmd := a.handlePermissionsEditorResult(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.FileWatcherEvent:
		*cmds = append(*cmds, a.handleFileWatcherEvent(msg)...)
	case messages.WorkspaceDeleted:
		*cmds = append(*cmds, a.handleWorkspaceDeleted(msg)...)
	case messages.OrphanWorkspaceDeleted:
		if msg.Workspace != nil {
			if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root(), false); cmd != nil {
				*cmds = append(*cmds, cmd)
			}
		}
		*cmds = append(*cmds, a.loadWorkspaces())
		*cmds = append(*cmds, a.toast.ShowSuccess("Orphan cleaned up"))
	case messages.WorkspaceDeleteFailed:
		if cmd := a.handleWorkspaceDeleteFailed(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.UpdateCheckComplete:
		if cmd := a.handleUpdateCheckComplete(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.TriggerUpgrade:
		if cmd := a.handleTriggerUpgrade(); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.UpgradeComplete:
		if cmd := a.handleUpgradeComplete(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.Error:
		a.err = msg.Err
		logging.Error("Error in %s: %v", msg.Context, msg.Err)
	case messages.ActionBarCopyDir:
		if cmd := a.handleActionBarCopyDir(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ActionBarOpenIDE:
		if cmd := a.handleActionBarOpenIDE(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ActionBarMergeToMain:
		*cmds = append(*cmds, a.handleActionBarMergeToMain(msg))
	case messages.ActionBarCommitResult:
		if cmd := a.handleActionBarCommitResult(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ActionBarMergeResult:
		if cmd := a.handleActionBarMergeResult(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ActionBarOpenMR:
		if cmd := a.handleActionBarOpenMR(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ShowCommitDialog:
		a.handleShowCommitDialog(msg)
	default:
		return false
	}
	return true
}
