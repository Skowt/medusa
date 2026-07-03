package hooks

import "time"

// EventType represents the type of Claude Code hook event.
type EventType string

const (
	EventStop                    EventType = "Stop"
	EventStopFailure             EventType = "StopFailure"
	EventSubagentStop            EventType = "SubagentStop"
	EventNotificationIdle        EventType = "NotificationIdle"
	EventNotificationPermission  EventType = "NotificationPermission"
	EventNotificationElicitation EventType = "NotificationElicitation"
	EventPermissionRequest       EventType = "PermissionRequest"
	EventPreToolUse              EventType = "PreToolUse"
	EventPostToolUse             EventType = "PostToolUse"
	EventUserPromptSubmit        EventType = "UserPromptSubmit"
)

// HookEvent is the parsed event delivered to the server callback.
type HookEvent struct {
	SessionName string
	Event       EventType
	Timestamp   time.Time
	Message     string // Optional message (e.g. from Notification hooks)
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
