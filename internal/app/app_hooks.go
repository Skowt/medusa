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
	// AutoReviewer is resolved from the tab, not from the payload: it reports
	// that this tab's approval requests go to an automatic reviewer instead of
	// to the user. Only PermissionRequest reads it — see applyHookTransition.
	AutoReviewer bool
	// ClaudeSessionID, AgentType and Cwd are carried on SessionStart.
	ClaudeSessionID string
	AgentType       string
	Cwd             string
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
			Outstanding:     he.Outstanding,
			Tool:            he.Tool,
			ClaudeSessionID: he.ClaudeSessionID,
			AgentType:       he.AgentType,
			Cwd:             he.Cwd,
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
	// Resolve session name → workspace and tab identity.
	wsID := ""
	infoBySession := a.tabSessionInfoByName()
	if info, ok := infoBySession[msg.SessionName]; ok {
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
	if !shouldApplyHookEventFor(a.hookTabLastStamp, msg.SessionName, msg.Event, msg.Timestamp) {
		return nil
	}

	// PermissionRequest alone cannot say whether a human was asked; the tab's
	// own launch options can. Resolve that here, where tab identity is already
	// in hand, so the transition rules stay a pure function of the message.
	if info, ok := infoBySession[msg.SessionName]; ok {
		msg.AutoReviewer = info.AutoReviewer
	}

	transition := applyHookTransition(a.hookTabStates, a.hookTabOutstanding, msg.SessionName, msg)
	_, busyAfter := a.hookTabStates[msg.SessionName]
	recordHookEventFor(a.hookTabLastStamp, msg.SessionName, msg.Timestamp, isClearHookEvent(msg.Event) && !busyAfter)
	a.recomputeWorkspaceHookState(wsID, infoBySession)
	state := ""
	if evt, ok := a.hookTabStates[msg.SessionName]; ok {
		state = string(evt)
	}
	a.center.SetTabHookState(wsID, msg.SessionName, state, transition == hookTransitionReady)

	var cmds []tea.Cmd
	if transition != hookTransitionNone {
		cmds = append(cmds, a.notifyWorkspaceAttention(wsID)...)
	}
	// Show a toast for notification events that carry a message.
	if msg.Message != "" {
		switch msg.Event {
		case hooks.EventNotificationElicitation:
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
//
// The payload's cwd is the second, load-bearing filter. MEDUSA_SESSION_NAME is
// inherited by every process the tab spawns, so a nested claude — one launched
// by a script or test, in any directory — fires SessionStart under the tab's
// own session name. Those sessions usually never own a transcript, and
// adopting one leaves the tab pointing at an id that cannot be resumed, which
// is how a restart came back with an empty conversation. Requiring the cwd to
// sit inside the tab's workspace rejects them while still accepting /clear,
// which reports the same cwd as the session it replaced.
func (a *App) handleSessionStart(wsID string, msg hookActivityEvent) []tea.Cmd {
	if msg.AgentType != "" || msg.ClaudeSessionID == "" {
		return nil
	}
	if !a.center.UpdateTabClaudeSessionID(wsID, msg.SessionName, msg.ClaudeSessionID, msg.Cwd) {
		return nil
	}
	logging.Info("Refreshed Claude session id for %s → %s", msg.SessionName, msg.ClaudeSessionID)
	if cmd := a.persistWorkspaceTabs(wsID); cmd != nil {
		return []tea.Cmd{cmd}
	}
	return nil
}

// refresh it (or the reconciler clears a dead session).
func (a *App) restoreHookOutstanding() {
	for wsID, evt := range a.hookWorkspaceStates {
		if evt == hooks.EventSubagentWait {
			a.hookOutstanding[wsID] = 1
		}
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
	return shouldApplyHookEventFor(a.hookLastStamp, wsID, evt, ts)
}

func shouldApplyHookEventFor(stamps map[string]hookEventStamp, key string, evt hooks.EventType, ts time.Time) bool {
	prev, seen := stamps[key]
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
	recordHookEventFor(a.hookLastStamp, wsID, ts, cleared)
}

func recordHookEventFor(stamps map[string]hookEventStamp, key string, ts time.Time, cleared bool) {
	stamps[key] = hookEventStamp{at: ts, clear: cleared}
}

// recomputeWorkspaceHookState derives the dashboard state from every agent tab
// in the workspace. A blocked tab wins over a processing tab; processing wins
// over idle. This prevents one tab's Stop event from clearing another tab.
func (a *App) recomputeWorkspaceHookState(wsID string, infoBySession map[string]tabSessionInfo) {
	var active hooks.EventType
	var latest hookEventStamp
	for sessionName, evt := range a.hookTabStates {
		info, ok := infoBySession[sessionName]
		if !ok || info.WorkspaceID != wsID {
			continue
		}
		if stamp, ok := a.hookTabLastStamp[sessionName]; ok && stamp.at.After(latest.at) {
			latest = stamp
		}
		if isNeedsInputState(evt) {
			a.hookWorkspaceStates[wsID] = hooks.EventNotificationElicitation
			a.hookLastStamp[wsID] = latest
			return
		}
		if active == "" && hooks.IsActiveEvent(evt) {
			active = evt
		}
	}
	if active != "" {
		a.hookWorkspaceStates[wsID] = active
		a.hookLastStamp[wsID] = latest
		return
	}
	delete(a.hookWorkspaceStates, wsID)
	delete(a.hookLastStamp, wsID)
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

// restoreHookStatesFromWorkspaces restores each tab independently, then derives
// the workspace indicator using the same precedence as live hook events.
func (a *App) restoreHookStatesFromWorkspaces() {
	for _, ws := range a.allWorkspaces {
		wsID := string(ws.ID())
		foundTabState := false
		for _, tab := range ws.OpenTabs {
			if tab.SessionName == "" || tab.ActivityState == "" {
				continue
			}
			foundTabState = true
			evt := hooks.EventType(tab.ActivityState)
			a.hookTabStates[tab.SessionName] = evt
			if evt == hooks.EventSubagentWait {
				a.hookTabOutstanding[tab.SessionName] = 1
			}
		}
		if foundTabState {
			a.recomputeWorkspaceHookState(wsID, a.tabSessionInfoByName())
		} else if ws.ActivityState != "" {
			// Read older workspace files which predate per-tab activity.
			a.hookWorkspaceStates[wsID] = hooks.EventType(ws.ActivityState)
		}
	}
	a.restoreHookOutstanding()
}

// handleAgentInterrupted clears the hook state for a workspace whose agent was
// interrupted via Ctrl+C or Esc, or whose tab was restarted. Claude Code's Stop
// hook does not fire on user interrupts, and a restart does not resume the turn
// that was running, so the spinner would otherwise keep running indefinitely.
// The clear is silent: the user caused it and is looking at the workspace.
func (a *App) handleAgentInterrupted(wsID string, sessions ...string) []tea.Cmd {
	sessionName := ""
	if len(sessions) > 0 {
		sessionName = sessions[0]
	}
	if sessionName != "" {
		delete(a.hookTabOutstanding, sessionName)
		delete(a.hookTabStates, sessionName)
		delete(a.hookTabLastStamp, sessionName)
		a.center.SetTabHookState(wsID, sessionName, "", false)
		a.recomputeWorkspaceHookState(wsID, a.tabSessionInfoByName())
	} else {
		// Compatibility for callers without a session identity.
		for session, info := range a.tabSessionInfoByName() {
			if info.WorkspaceID == wsID {
				delete(a.hookTabOutstanding, session)
				delete(a.hookTabStates, session)
				delete(a.hookTabLastStamp, session)
			}
		}
		delete(a.hookWorkspaceStates, wsID)
	}
	delete(a.hookOutstanding, wsID)
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
