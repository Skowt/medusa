package app

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/hooks"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/supervisor"
)

// hookActivityEvent is a Bubble Tea message carrying a parsed hook event.
type hookActivityEvent struct {
	SessionName string
	Event       hooks.EventType
	Timestamp   time.Time
	Message     string
}

// initHooksWatcher creates and registers the hook event receivers: the Unix
// socket server (primary transport) and the directory watcher (legacy
// sessions started before the socket upgrade, plus the nc-less file
// fallback). Both feed the same event queue.
func (a *App) initHooksWatcher() {
	hooksDir := a.config.Paths.HooksDir
	if hooksDir == "" {
		return
	}

	// Clean up stale files from previous sessions
	hooks.CleanStaleFiles(hooksDir, 24*time.Hour)

	onEvent := func(he hooks.HookEvent) {
		a.enqueueExternalMsg(hookActivityEvent{
			SessionName: he.SessionName,
			Event:       he.Event,
			Timestamp:   he.Timestamp,
			Message:     he.Message,
		})
	}

	srv, err := hooks.NewServer(hooks.SocketPath(hooksDir), onEvent)
	if err != nil {
		logging.Warn("Hooks socket server disabled: %v", err)
	} else {
		a.hooksServer = srv
		a.supervisor.Start("hooks.server", srv.Run, supervisor.WithBackoff(500*time.Millisecond))
	}

	w, err := hooks.NewWatcher(hooksDir, onEvent)
	if err != nil {
		logging.Warn("Hooks watcher disabled: %v", err)
		return
	}
	a.hooksWatcher = w
	a.supervisor.Start("hooks.watcher", w.Run, supervisor.WithBackoff(500*time.Millisecond))
}

// handleHookActivityEvent processes a hook activity event, resolves the session
// name to a workspace ID via tabSessionInfoByName, and updates hookWorkspaceStates.
func (a *App) handleHookActivityEvent(msg hookActivityEvent) []tea.Cmd {
	// Resolve session name → workspace ID
	wsID := ""
	if info, ok := a.tabSessionInfoByName()[msg.SessionName]; ok {
		wsID = info.WorkspaceID
	}
	if wsID == "" {
		return nil
	}

	// NotificationIdle and Stop are "clear" signals: the agent finished
	// responding and went idle, so any prior notification (permission/question)
	// has been resolved. Delete the state so the '!' indicator disappears.
	switch msg.Event {
	case hooks.EventStop, hooks.EventStopFailure, hooks.EventNotificationIdle:
		delete(a.hookWorkspaceStates, wsID)
	default:
		a.hookWorkspaceStates[wsID] = msg.Event
	}

	var cmds []tea.Cmd
	// Show a toast for notification events that carry a message.
	if msg.Message != "" {
		switch msg.Event {
		case hooks.EventNotificationPermission, hooks.EventNotificationElicitation:
			wsName := wsID
			if ws := a.findWorkspaceByID(wsID); ws != nil {
				wsName = ws.Name
			}
			cmds = append(cmds, a.toast.ShowInfo(fmt.Sprintf("[%s] %s", wsName, msg.Message)))
		}
	}
	// Persist the hook state change to the workspace JSON.
	if cmd := a.persistHookState(wsID); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := a.syncActiveWorkspacesToDashboard(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// persistHookState updates the workspace's HookState field and triggers a debounced save.
func (a *App) persistHookState(wsID string) tea.Cmd {
	ws := a.findWorkspaceByID(wsID)
	if ws == nil {
		return nil
	}
	if evt, ok := a.hookWorkspaceStates[wsID]; ok {
		ws.ActivityState = string(evt)
	} else {
		ws.ActivityState = ""
	}
	return a.persistWorkspaceTabs(wsID)
}

// restoreHookStatesFromWorkspaces populates hookWorkspaceStates from persisted
// workspace HookState fields so that indicators (like '!') survive app restarts.
func (a *App) restoreHookStatesFromWorkspaces() {
	for _, ws := range a.allWorkspaces {
		if ws.ActivityState == "" {
			continue
		}
		a.hookWorkspaceStates[string(ws.ID())] = hooks.EventType(ws.ActivityState)
	}
}

// handleAgentInterrupted clears the hook state for a workspace whose agent was
// interrupted via Ctrl+C. Claude Code's Stop hook does not fire on user
// interrupts, so the spinner would otherwise keep running indefinitely.
func (a *App) handleAgentInterrupted(wsID string) []tea.Cmd {
	if _, ok := a.hookWorkspaceStates[wsID]; !ok {
		return nil
	}
	delete(a.hookWorkspaceStates, wsID)
	var cmds []tea.Cmd
	if cmd := a.persistHookState(wsID); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := a.syncActiveWorkspacesToDashboard(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// hookActiveIDs returns the set of workspace IDs that are currently active
// based on hook state (PreToolUse or UserPromptSubmit = agent actively working).
// SubagentStop counts as active: it fires mid-turn when a subagent finishes
// while the main agent keeps working; treating it as inactive caused false
// "ready for review" pings and a prematurely stopped spinner.
func (a *App) hookActiveIDs() map[string]bool {
	active := make(map[string]bool)
	for wsID, evt := range a.hookWorkspaceStates {
		switch evt {
		case hooks.EventPreToolUse, hooks.EventPostToolUse, hooks.EventUserPromptSubmit, hooks.EventSubagentStop:
			active[wsID] = true
		}
	}
	return active
}
