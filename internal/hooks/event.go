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
	EventNotificationElicitation EventType = "NotificationElicitation"
	EventPreToolUse              EventType = "PreToolUse"
	EventPostToolUse             EventType = "PostToolUse"
	EventUserPromptSubmit        EventType = "UserPromptSubmit"
	// EventPermissionRequest fires when a tool call needs an approval
	// decision. Whether it means the *user* was asked differs by assistant —
	// see hookActivityEvent.AutoReviewer.
	EventPermissionRequest EventType = "PermissionRequest"
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
	// Tool is the assistant's tool_name on PreToolUse/PostToolUse. Used to
	// recognize the question tools, which need input rather than showing busy
	// — see IsQuestionTool.
	Tool string
	// ClaudeSessionID is Claude Code's live session_id, carried on
	// SessionStart so the app can refresh a tab's persisted id.
	ClaudeSessionID string
	// AgentType is Claude Code's agent_type on SessionStart, set only for
	// agent sessions (claude --agent <name>). Non-empty means the event is
	// not the tab's main conversation and its id must not be adopted.
	AgentType string
	// Cwd is the hook payload's cwd, carried on SessionStart. A nested claude
	// inherits MEDUSA_SESSION_NAME from the tab it was launched in, so the
	// session name alone cannot say whether an event belongs to the tab's own
	// conversation; the cwd can. Empty for hook emitters that predate it.
	Cwd string
}

// questionTools are the tool names whose PreToolUse means the agent has put a
// question on screen and is blocked on the user, rather than doing work.
//
// Both assistants deliver it the same way — as an ordinary PreToolUse for an
// ordinary function tool — so the tool name is the only thing that separates a
// question from a file read. Codex's request_user_input goes through its
// generic function-tool dispatch with no hook-name override, so it arrives
// under its own name; Claude Code's is AskUserQuestion.
var questionTools = map[string]bool{
	"AskUserQuestion":    true, // Claude Code
	"request_user_input": true, // Codex
}

// IsQuestionTool reports whether a PreToolUse tool_name is a question dialog.
func IsQuestionTool(tool string) bool {
	return questionTools[tool]
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
	case EventPermissionRequest:
		// Only ever stored when the approval was routed to an automatic
		// reviewer rather than the user: the review is work in progress, so
		// the tab keeps its spinner. A request the user must answer is stored
		// as EventNotificationElicitation instead.
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
