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

// initHooksServer registers the hook event receiver: a Unix socket server,
// which is the sole transport for Claude Code lifecycle events.
func (a *App) initHooksServer() {
	hooksDir := a.config.Paths.HooksDir
	if hooksDir == "" {
		return
	}

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
		return
	}
	a.hooksServer = srv
	a.supervisor.Start("hooks.server", srv.Run, supervisor.WithBackoff(500*time.Millisecond))
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

	// Reject out-of-order delivery. Events reach us over per-connection socket
	// goroutines (internal/hooks/server.go), so a turn's terminal Stop can be
	// enqueued before a trailing tool event from the same turn; applying that
	// stale active event revives the spinner with nothing left to clear it.
	if !a.shouldApplyHookEvent(wsID, msg.Event, msg.Timestamp) {
		return nil
	}
	a.recordHookEvent(wsID, msg.Event, msg.Timestamp)

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

// hookEventStamp records when the last hook event was applied for a workspace
// and whether it was a "clear" (Stop/StopFailure/NotificationIdle) rather than
// an active event. Used to reject stale, out-of-order deliveries.
type hookEventStamp struct {
	at    time.Time
	clear bool
}

// isClearHookEvent reports whether an event clears the busy/active state.
func isClearHookEvent(evt hooks.EventType) bool {
	switch evt {
	case hooks.EventStop, hooks.EventStopFailure, hooks.EventNotificationIdle:
		return true
	}
	return false
}

// shouldApplyHookEvent guards against out-of-order hook delivery. It drops any
// event older than the last one applied for the workspace. Because hook
// timestamps are second-resolution (the hook emits `date +%s`), a trailing
// tool event and the turn's Stop frequently share the same second; in that tie
// a clear wins, so a reordered PreToolUse/PostToolUse can never override a Stop
// applied in the same second. The only cost is that a brand-new turn started in
// the very same wall-clock second an agent stopped may drop its first active
// event — the next tool event a second later restarts the spinner.
func (a *App) shouldApplyHookEvent(wsID string, evt hooks.EventType, ts time.Time) bool {
	prev, seen := a.hookLastStamp[wsID]
	if !seen {
		return true
	}
	if ts.Before(prev.at) {
		return false
	}
	if ts.Equal(prev.at) && prev.clear && !isClearHookEvent(evt) {
		return false
	}
	return true
}

// recordHookEvent stamps the last applied hook event for a workspace.
func (a *App) recordHookEvent(wsID string, evt hooks.EventType, ts time.Time) {
	a.hookLastStamp[wsID] = hookEventStamp{at: ts, clear: isClearHookEvent(evt)}
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
