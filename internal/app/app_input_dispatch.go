package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// routeSystemMsg dispatches low-activity, well-scoped messages to their
// handlers. Returns handled=true when the message was consumed here.
func (a *App) routeSystemMsg(msg tea.Msg, cmds *[]tea.Cmd) bool {
	switch msg := msg.(type) {
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
		if msg.Err != nil {
			*cmds = append(*cmds, a.toast.ShowError(fmt.Sprintf("Orphan cleanup failed: %v", msg.Err)))
		} else {
			*cmds = append(*cmds, a.toast.ShowSuccess("Orphan cleaned up"))
		}
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
	case messages.ActionBarOpenIDE:
		if cmd := a.handleActionBarOpenIDE(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.OpenReviewChanges:
		if cmd := a.openReviewOverlay(); cmd != nil {
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
