package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/center"
	"github.com/Skowt/medusa/internal/ui/dashboard"
	"github.com/Skowt/medusa/internal/ui/sidebar"
)

// routePTYMsg dispatches tab/PTY/tick/tmux-related messages. Returns handled=true
// when the message was consumed here.
func (a *App) routePTYMsg(msg tea.Msg, cmds *[]tea.Cmd) bool {
	switch msg := msg.(type) {
	case messages.TabCreated:
		if cmd := a.handleTabCreated(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		if cmd := a.persistActiveWorkspaceTabs(); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.TabClosed:
		logging.Info("Tab closed: %d", msg.Index)
		if cmd := a.persistActiveWorkspaceTabs(); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.TabDetached:
		logging.Info("Tab detached: %d", msg.Index)
		*cmds = append(*cmds, a.persistActiveWorkspaceTabs())
	case messages.TabReattached:
		*cmds = append(*cmds, a.persistWorkspaceTabs(msg.WorkspaceID))
	case messages.TabStateChanged:
		*cmds = append(*cmds, a.persistWorkspaceTabs(msg.WorkspaceID))
	case messages.TabSelectionChanged:
		*cmds = append(*cmds, a.persistWorkspaceTabs(msg.WorkspaceID))
	case persistDebounceMsg:
		*cmds = append(*cmds, a.handlePersistDebounce(msg))
	case center.PTYOutput, center.PTYTick, center.PTYFlush, center.PTYStopped:
		if cmd := a.handlePTYMessages(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		// Sync active agents state to dashboard (show spinner only when actively outputting)
		if startCmd := a.syncActiveWorkspacesToDashboard(); startCmd != nil {
			*cmds = append(*cmds, startCmd)
		}
	case center.TabInputFailed:
		*cmds = append(*cmds, a.handleTabInputFailed(msg)...)
	case messages.AgentInterrupted:
		*cmds = append(*cmds, a.handleAgentInterrupted(msg.WorkspaceID)...)
	case messages.Toast:
		switch msg.Level {
		case messages.ToastSuccess:
			*cmds = append(*cmds, a.toast.ShowSuccess(msg.Message))
		case messages.ToastError:
			*cmds = append(*cmds, a.toast.ShowError(msg.Message))
		case messages.ToastWarning:
			*cmds = append(*cmds, a.toast.ShowWarning(msg.Message))
		default:
			*cmds = append(*cmds, a.toast.ShowInfo(msg.Message))
		}
	case messages.SidebarPTYOutput, messages.SidebarPTYTick, messages.SidebarPTYFlush, messages.SidebarPTYStopped, messages.SidebarPTYRestart, sidebar.SidebarTerminalCreated, sidebar.SidebarTerminalCreateFailed:
		if cmd := a.handleSidebarPTYMessages(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case sidebar.OpenFileInEditor:
		if cmd := a.handleOpenFileInEditor(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case dashboard.SpinnerTickMsg:
		*cmds = append(*cmds, a.handleSpinnerTick(msg)...)
	case messages.GitStatusTick:
		*cmds = append(*cmds, a.handleGitStatusTick()...)
	case messages.PTYWatchdogTick:
		*cmds = append(*cmds, a.handlePTYWatchdogTick()...)
	case hookActivityEvent:
		*cmds = append(*cmds, a.handleHookActivityEvent(msg)...)
	case tmuxAvailableResult:
		*cmds = append(*cmds, a.handleTmuxAvailableResult(msg)...)
	case messages.TmuxSyncTick:
		*cmds = append(*cmds, a.handleTmuxSyncTick(msg)...)
	case tmuxTabsSyncResult:
		*cmds = append(*cmds, a.handleTmuxTabsSyncResult(msg)...)
	case tmuxTabsDiscoverResult:
		*cmds = append(*cmds, a.handleTmuxTabsDiscoverResult(msg)...)
	default:
		return false
	}
	return true
}
