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
	// Pending is Claude Code's pending_subagent_count on SubagentStop
	// (other subagents still running), or hooks.PendingUnknown.
	Pending int
	// ClaudeSessionID and AgentType are carried on SessionStart.
	ClaudeSessionID string
	AgentType       string
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
			SessionName:     he.SessionName,
			Event:           he.Event,
			Timestamp:       he.Timestamp,
			Message:         he.Message,
			Pending:         he.Pending,
			ClaudeSessionID: he.ClaudeSessionID,
			AgentType:       he.AgentType,
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

	// SessionStart is not an activity signal — it refreshes the tab's stored
	// Claude session id so a restart resumes the current conversation after a
	// /clear (or in-session /resume) mints a new id.
	if msg.Event == hooks.EventSessionStart {
		return a.handleSessionStart(wsID, msg)
	}

	// Keep the outstanding-subagent counter current even for events the
	// state machine below rejects as reordered: the counter has its own
	// ordering stamp, and a dropped SubagentStop would otherwise leave the
	// count inflated so the final Stop never clears.
	a.updateSubagentsPending(wsID, msg)

	// Reject out-of-order delivery. Events reach us over per-connection socket
	// goroutines (internal/hooks/server.go), so a turn's terminal Stop can be
	// enqueued before a trailing tool event from the same turn; applying that
	// stale active event revives the spinner with nothing left to clear it.
	if !a.shouldApplyHookEvent(wsID, msg.Event, msg.Timestamp) {
		return nil
	}

	a.recordHookEvent(wsID, msg.Timestamp, a.applyHookStateTransition(wsID, msg.Event))

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

// handleSessionStart refreshes a tab's persisted Claude session id from a
// SessionStart hook. Medusa mints the id at tab creation and only ever resumes
// that one, so a /clear (or in-session /resume) — which starts a new Claude
// session with a new id — would otherwise leave the tab resuming the stale
// pre-clear conversation. Sessions started with `claude --agent <name>` carry
// an agent_type and are skipped: they fire under the same session name but are
// not the tab's main conversation, so adopting their id would be wrong.
func (a *App) handleSessionStart(wsID string, msg hookActivityEvent) []tea.Cmd {
	if msg.AgentType != "" || msg.ClaudeSessionID == "" {
		return nil
	}
	if !a.center.UpdateTabClaudeSessionID(wsID, msg.SessionName, msg.ClaudeSessionID) {
		return nil
	}
	logging.Info("Refreshed Claude session id for %s → %s", msg.SessionName, msg.ClaudeSessionID)
	if cmd := a.persistWorkspaceTabs(wsID); cmd != nil {
		return []tea.Cmd{cmd}
	}
	return nil
}

// applyHookStateTransition updates hookWorkspaceStates for an applied event
// and reports whether the event cleared the busy state.
//
// NotificationIdle and Stop are "clear" signals: the agent finished responding
// and went idle, so any prior notification (permission/question) has been
// resolved. Delete the state so the '!' indicator disappears. Exception: a
// Stop that arrives while background subagents are still outstanding only
// ends the turn, not the session's work — Claude Code resumes when the
// subagents report back. Mark the workspace as waiting instead of clearing,
// so it neither pings "ready for review" nor drops its spinner.
func (a *App) applyHookStateTransition(wsID string, evt hooks.EventType) (cleared bool) {
	switch evt {
	case hooks.EventStop, hooks.EventStopFailure:
		if a.subagentsPending[wsID] > 0 {
			a.hookWorkspaceStates[wsID] = hooks.EventSubagentWait
			return false
		}
		delete(a.hookWorkspaceStates, wsID)
		return true
	case hooks.EventNotificationIdle:
		// Claude only reports idle when it is truly waiting for input, so a
		// leftover counter here means we missed a SubagentStop — resync.
		delete(a.subagentsPending, wsID)
		delete(a.hookWorkspaceStates, wsID)
		return true
	default:
		a.hookWorkspaceStates[wsID] = evt
		return false
	}
}

// hookEventStamp records when the last hook event was applied for a workspace
// and whether it was a "clear" (Stop/StopFailure/NotificationIdle) rather than
// an active event. Used to reject stale, out-of-order deliveries.
type hookEventStamp struct {
	at    time.Time
	clear bool
}

// isClearHookEvent reports whether an event is of the clearing kind (turn
// ended / went idle). Whether it actually clears also depends on the
// outstanding-subagent counter — see handleHookActivityEvent.
func isClearHookEvent(evt hooks.EventType) bool {
	switch evt {
	case hooks.EventStop, hooks.EventStopFailure, hooks.EventNotificationIdle:
		return true
	}
	return false
}

// updateSubagentsPending maintains the per-workspace outstanding-subagent
// counter: SubagentStart increments; SubagentStop resyncs from Claude Code's
// authoritative pending_subagent_count, falling back to a clamped decrement
// when the payload predates the field. Updates are ordered by their own
// stamp so a stale SubagentStart delivered after a newer SubagentStop cannot
// re-inflate a count the stop already settled.
func (a *App) updateSubagentsPending(wsID string, msg hookActivityEvent) {
	switch msg.Event {
	case hooks.EventSubagentStart, hooks.EventSubagentStop:
	default:
		return
	}
	if msg.Timestamp.Before(a.subagentsPendingStamp[wsID]) {
		return
	}
	a.subagentsPendingStamp[wsID] = msg.Timestamp
	if msg.Event == hooks.EventSubagentStart {
		a.subagentsPending[wsID]++
		return
	}
	if msg.Pending >= 0 {
		a.subagentsPending[wsID] = msg.Pending
	} else if a.subagentsPending[wsID] > 0 {
		a.subagentsPending[wsID]--
	}
	if a.subagentsPending[wsID] == 0 {
		delete(a.subagentsPending, wsID)
	}
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

// recordHookEvent stamps the last applied hook event for a workspace. cleared
// records whether the event actually cleared the busy state (a Stop with
// outstanding subagents is clear-kind but leaves the workspace busy).
func (a *App) recordHookEvent(wsID string, ts time.Time, cleared bool) {
	a.hookLastStamp[wsID] = hookEventStamp{at: ts, clear: cleared}
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
	delete(a.subagentsPending, wsID)
	delete(a.subagentsPendingStamp, wsID)
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
// based on hook state. See hooks.IsActiveEvent for what counts as active.
func (a *App) hookActiveIDs() map[string]bool {
	active := make(map[string]bool)
	for wsID, evt := range a.hookWorkspaceStates {
		if hooks.IsActiveEvent(evt) {
			active[wsID] = true
		}
	}
	return active
}
