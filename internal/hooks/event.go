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

	// EventSubagentWait is synthetic — never emitted by a Claude Code hook.
	// The app derives it when a Stop arrives while background subagents are
	// still outstanding: the turn ended but the session is not ready for
	// input, so the workspace must keep reading as busy.
	EventSubagentWait EventType = "SubagentWait"
)

// PendingUnknown marks a SubagentStop event whose payload carried no
// pending_subagent_count (hooks injected by older Medusa versions, or a
// Claude Code version that predates the field).
const PendingUnknown = -1

// HookEvent is the parsed event delivered to the server callback.
type HookEvent struct {
	SessionName string
	Event       EventType
	Timestamp   time.Time
	Message     string // Optional message (e.g. from Notification hooks)
	// Pending is the number of other subagents still running when a
	// SubagentStop fired (Claude Code's pending_subagent_count), or
	// PendingUnknown when the payload did not carry the field.
	Pending int
}

// IsActiveEvent reports whether an event means the agent is still busy — the
// workspace should show a spinner rather than ping "ready for review".
// SubagentStop counts as active: it fires when a subagent finishes while the
// main agent keeps working (or is about to resume); treating it as inactive
// caused false pings. SubagentWait is the derived turn-ended-but-background-
// agents-outstanding state.
func IsActiveEvent(evt EventType) bool {
	switch evt {
	case EventPreToolUse, EventPostToolUse, EventUserPromptSubmit,
		EventSubagentStart, EventSubagentStop, EventSubagentWait:
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
