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
	// Outstanding is the number of background tasks still running when a
	// Stop/StopFailure/SubagentStop fired (from Claude Code's
	// background_tasks payload), or hooks.OutstandingUnknown.
	Outstanding int
	// Tool is the tool name on PreToolUse/PostToolUse.
	Tool string
	// ClaudeSessionID and AgentType are carried on SessionStart.
	ClaudeSessionID string
	AgentType       string
}

// hookTransition is the user-visible outcome of applying a hook event: it
// drives the notification sound and the unread (orange) highlight. Pings are
// asserted by explicit state transitions — never inferred from a workspace
// dropping out of an "active set", which is what made every stray event a
// potential false sound.
type hookTransition int

const (
	hookTransitionNone       hookTransition = iota
	hookTransitionReady                     // turn complete, nothing outstanding — "what's next?"
	hookTransitionNeedsInput                // blocked on the user (permission / question / elicitation)
)

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
			Outstanding:     he.Outstanding,
			Tool:            he.Tool,
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

	// Reject out-of-order delivery. Events reach us over per-connection socket
	// goroutines (internal/hooks/server.go), so a turn's terminal Stop can be
	// enqueued before a trailing tool event from the same turn; applying that
	// stale active event revives the spinner with nothing left to clear it.
	if !a.shouldApplyHookEvent(wsID, msg.Event, msg.Timestamp) {
		return nil
	}

	transition := a.applyHookStateTransition(wsID, msg)
	_, busyAfter := a.hookWorkspaceStates[wsID]
	a.recordHookEvent(wsID, msg.Timestamp, isClearHookEvent(msg.Event) && !busyAfter)

	var cmds []tea.Cmd
	if transition != hookTransitionNone {
		cmds = append(cmds, a.notifyWorkspaceAttention(wsID)...)
	}
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

// isNeedsInputState reports whether a stored state means the agent is blocked
// on the user (permission dialog, question, MCP elicitation).
func isNeedsInputState(evt hooks.EventType) bool {
	switch evt {
	case hooks.EventPermissionRequest, hooks.EventNotificationPermission, hooks.EventNotificationElicitation:
		return true
	}
	return false
}

// applyHookStateTransition updates hookWorkspaceStates for an applied event
// and reports the user-visible transition.
//
// The state is derived from payload truth, not event counting: Stop carries
// the authoritative count of still-running background tasks (background_tasks
// since Claude Code 2.1.145), so a turn that ends with live background agents
// parks in SubagentWait — busy, no ping — and the auto-resumed turn's final
// Stop clears. SubagentStop is deliberately inert: Claude Code is known to
// fire phantom/duplicate SubagentStop events after a turn's Stop (upstream
// #59719, #70151), and any rule that lets SubagentStop set a state creates a
// busy value with nothing left to clear it.
func (a *App) applyHookStateTransition(wsID string, msg hookActivityEvent) hookTransition {
	prev, hadPrev := a.hookWorkspaceStates[wsID]
	evt := msg.Event
	switch {
	case evt == hooks.EventStop || evt == hooks.EventStopFailure:
		if msg.Outstanding > 0 {
			a.hookWorkspaceStates[wsID] = hooks.EventSubagentWait
			return hookTransitionNone
		}
		delete(a.hookWorkspaceStates, wsID)
		// Ping only when leaving a busy state: after needs-input the user was
		// already pinged, and with no state at all there is nothing to report.
		if hadPrev && !isNeedsInputState(prev) {
			return hookTransitionReady
		}
		return hookTransitionNone

	case evt == hooks.EventSubagentStop:
		// Inert by design; the auto-resumed turn's events carry the state.
		return hookTransitionNone

	case evt == hooks.EventNotificationIdle:
		// Claude reports idle only when truly waiting for input, so this is
		// the self-healing clear for any wedged busy state (missed Stop, lost
		// event). It must not wipe a pending '!': the permission dialog is
		// still on screen.
		if hadPrev && isNeedsInputState(prev) {
			return hookTransitionNone
		}
		delete(a.hookWorkspaceStates, wsID)
		if hadPrev {
			return hookTransitionReady
		}
		return hookTransitionNone

	case isNeedsInputState(evt),
		evt == hooks.EventPreToolUse && msg.Tool == "AskUserQuestion":
		// AskUserQuestion is a question dialog, not work: it arrives as
		// PreToolUse (plus PermissionRequest) but blocks on the user.
		state := evt
		if evt == hooks.EventPreToolUse {
			state = hooks.EventPermissionRequest
		}
		a.hookWorkspaceStates[wsID] = state
		if hadPrev && isNeedsInputState(prev) {
			return hookTransitionNone // already waiting; one ping is enough
		}
		return hookTransitionNeedsInput

	default:
		// Tool activity, prompt submission, subagent start: busy.
		a.hookWorkspaceStates[wsID] = evt
		return hookTransitionNone
	}
}

// notifyWorkspaceAttention marks a workspace unread (orange highlight) and
// plays the notification sound. The dashboard skips the workspace the user is
// currently looking at; the sound plays only when the unread flag was newly
// set, so one attention event produces at most one ping.
func (a *App) notifyWorkspaceAttention(wsID string) []tea.Cmd {
	if a.dashboard == nil || !a.dashboard.MarkUnread(wsID) {
		return nil
	}
	if a.config != nil && a.config.UI.NotificationSound != "" {
		return []tea.Cmd{playNotificationSound(a.config.UI.NotificationSound)}
	}
	return nil
}

// hookEventStamp records when the last hook event was applied for a workspace
// and whether it left the workspace cleared (no busy/needs-input state).
// Used to reject stale, out-of-order deliveries.
type hookEventStamp struct {
	at    time.Time
	clear bool
}

// isClearHookEvent reports whether an event is of the clearing kind (turn
// ended / went idle). Whether it actually clears also depends on the
// outstanding background-task count — see applyHookStateTransition.
func isClearHookEvent(evt hooks.EventType) bool {
	switch evt {
	case hooks.EventStop, hooks.EventStopFailure, hooks.EventNotificationIdle:
		return true
	}
	return false
}

// shouldApplyHookEvent guards against out-of-order hook delivery. It drops any
// event older than the last one applied for the workspace. The emit binary
// stamps events with nanosecond timestamps, making ties vanishingly rare;
// legacy shell hooks degrade to second resolution, where a trailing tool event
// and the turn's Stop frequently share the same second. In that tie a clear
// wins, so a reordered PreToolUse/PostToolUse can never override a Stop
// applied at the same timestamp. The only cost is that a brand-new turn
// started at the very same timestamp an agent stopped may drop its first
// active event — the next tool event restarts the spinner.
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
// records whether the event actually left the workspace cleared (a Stop with
// outstanding background work is clear-kind but leaves the workspace busy).
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
// interrupted via Ctrl+C or Esc. Claude Code's Stop hook does not fire on user
// interrupts, so the spinner would otherwise keep running indefinitely. The
// clear is silent: the user caused it and is looking at the workspace.
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
