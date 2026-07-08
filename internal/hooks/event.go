package hooks

import "time"

// EventType represents the type of Claude Code hook event.
type EventType string

const (
	EventStop                    EventType = "Stop"
	EventStopFailure             EventType = "StopFailure"
	EventSubagentStart           EventType = "SubagentStart"
	EventSubagentStop            EventType = "SubagentStop"
	EventNotificationIdle        EventType = "NotificationIdle"
	EventNotificationPermission  EventType = "NotificationPermission"
	EventNotificationElicitation EventType = "NotificationElicitation"
	EventPermissionRequest       EventType = "PermissionRequest"
	EventPreToolUse              EventType = "PreToolUse"
	EventPostToolUse             EventType = "PostToolUse"
	EventUserPromptSubmit        EventType = "UserPromptSubmit"
	// EventSessionStart carries the live Claude session id so the tab's
	// persisted id can be refreshed when it changes mid-session (e.g. /clear
	// mints a new session). It does not affect busy/idle activity state.
	EventSessionStart EventType = "SessionStart"

	// EventSubagentWait is synthetic — never emitted by a Claude Code hook.
	// The app derives it when a Stop arrives while background subagents are
	// still outstanding: the turn ended but the session is not ready for
	// input, so the workspace must keep reading as busy.
	EventSubagentWait EventType = "SubagentWait"
)

// OutstandingUnknown marks a Stop/SubagentStop event whose payload carried no
// background_tasks list (legacy shell hooks, or a Claude Code version that
// predates the field, < 2.1.145).
const OutstandingUnknown = -1

// HookEvent is the parsed event delivered to the server callback.
type HookEvent struct {
	SessionName string
	Event       EventType
	Timestamp   time.Time
	Message     string // Optional message (e.g. from Notification hooks)
	// Outstanding is the number of background tasks still running when a
	// Stop/StopFailure/SubagentStop fired (from Claude Code's
	// background_tasks payload list, excluding the stopping agent itself),
	// or OutstandingUnknown when the payload did not carry the field.
	Outstanding int
	// Tool is Claude Code's tool_name on PreToolUse/PostToolUse. Used to
	// recognize AskUserQuestion, which needs input rather than showing busy.
	Tool string
	// ClaudeSessionID is Claude Code's live session_id, carried on
	// SessionStart so the app can refresh a tab's persisted id.
	ClaudeSessionID string
	// AgentType is Claude Code's agent_type on SessionStart, set only for
	// agent sessions (claude --agent <name>). Non-empty means the event is
	// not the tab's main conversation and its id must not be adopted.
	AgentType string
}

// IsActiveEvent reports whether a stored state means the agent is still busy —
// the workspace should show a spinner rather than read as ready. SubagentStop
// is deliberately absent: the app never stores it (phantom SubagentStop events
// after a turn's Stop are a known Claude Code bug), and a value persisted by
// an older Medusa version must not resurrect as permanently busy. SubagentWait
// is the derived turn-ended-but-background-work-outstanding state.
func IsActiveEvent(evt EventType) bool {
	switch evt {
	case EventPreToolUse, EventPostToolUse, EventUserPromptSubmit,
		EventSubagentStart, EventSubagentWait:
		return true
	}
	return false
}

// parseHookTS normalizes a hook timestamp to a time.Time regardless of the unit
// the emitting hook used. The hook emits `date +%s%N`, which yields nanoseconds
// where date supports %N and falls back to seconds where it does not; hooks
// injected by older Medusa versions emitted plain seconds. We disambiguate by
// magnitude — epoch seconds are ~10 digits, so any value large enough to be
// milli/micro/nanoseconds is read as such — so events from any hook version
// land on the same timescale and the ordering guard compares like with like.
func parseHookTS(ts int64) time.Time {
	switch {
	case ts >= 1_000_000_000_000_000_000: // >= 1e18: nanoseconds
		return time.Unix(0, ts)
	case ts >= 1_000_000_000_000_000: // >= 1e15: microseconds
		return time.UnixMicro(ts)
	case ts >= 1_000_000_000_000: // >= 1e12: milliseconds
		return time.UnixMilli(ts)
	default: // seconds
		return time.Unix(ts, 0)
	}
}
