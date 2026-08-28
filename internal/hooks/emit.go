package hooks

import (
	"encoding/json"
	"time"
)

// emitPayload is the subset of a Claude Code hook stdin payload the emitter
// forwards. Everything else (tool_response bodies, transcript paths, …) is
// deliberately dropped to keep event lines small.
type emitPayload struct {
	ToolName        string           `json:"tool_name"`
	Message         string           `json:"message"`
	SessionID       string           `json:"session_id"`
	AgentID         string           `json:"agent_id"`
	AgentType       string           `json:"agent_type"`
	Cwd             string           `json:"cwd"`
	BackgroundTasks []backgroundTask `json:"background_tasks"`
}

// backgroundTask mirrors one entry of the payload's background_tasks list
// (present on Stop/SubagentStop since Claude Code v2.1.145).
type backgroundTask struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// eventLine is the wire format written to the medusa socket, one JSON object
// per line. Field names must match the server's parse struct in server.go.
type eventLine struct {
	Event           string `json:"event"`
	TS              int64  `json:"ts"`
	Session         string `json:"session"`
	Message         string `json:"message,omitempty"`
	Tool            string `json:"tool,omitempty"`
	Outstanding     *int   `json:"outstanding,omitempty"`
	ClaudeSessionID string `json:"claude_session_id,omitempty"`
	AgentType       string `json:"agent_type,omitempty"`
	// Cwd is the hook payload's cwd, forwarded on SessionStart only: it is
	// what lets the app tell a tab's own session from a nested claude that
	// merely inherited MEDUSA_SESSION_NAME.
	Cwd string `json:"cwd,omitempty"`
}

// terminalTaskStatuses are background_tasks statuses that mean the task is no
// longer running. Unknown statuses count as outstanding: the next Stop or
// SubagentStop re-evaluates from scratch, so over-counting self-heals while
// under-counting would fire a false "ready" ping mid-work.
var terminalTaskStatuses = map[string]bool{
	"completed": true,
	"failed":    true,
	"cancelled": true,
	"killed":    true,
}

// nonWorkTaskTypes are background_tasks entries that do not represent the
// agent working. A "monitor" is a live-update subscription — an artifact watch
// is one, auto-armed by publishing an artifact — and it reports
// status "running" for as long as the session stays subscribed, which is until
// the user stops it. Counting one parks the workspace in SubagentWait on every
// Stop, so the spinner never clears and only the 3-minute staleness reconciler
// ends it, minutes after the watch is finally killed.
//
// Types are denied rather than allowed, matching the rule for statuses: an
// unrecognised type still counts, because over-counting self-heals on the next
// Stop while under-counting fires a false "ready" ping in the middle of work.
// The sibling session_crons list is excluded by simply never being parsed.
var nonWorkTaskTypes = map[string]bool{
	"monitor": true,
}

// carriesOutstanding reports whether an event's payload is authoritative for
// the outstanding-background-work count.
func carriesOutstanding(event string) bool {
	switch event {
	case "Stop", "StopFailure", "SubagentStop":
		return true
	}
	return false
}

// BuildEventLine converts a Claude Code hook invocation (event name from the
// hook rule, session from $MEDUSA_SESSION_NAME, raw stdin payload) into the
// newline-terminated JSON line the medusa socket server consumes. Returns nil
// when the event must not be emitted (no session name → not a Medusa session).
//
// On Stop/StopFailure/SubagentStop the payload's background_tasks list is
// reduced to an `outstanding` count of still-running tasks. A SubagentStop
// excludes its own agent_id — Claude Code lists the stopping agent as still
// "running" in its own payload. Monitor tasks are excluded too (see
// nonWorkTaskTypes), as are session_crons: both stay "running" while Claude
// sits waiting for input, so either would keep the workspace busy forever. When the payload predates background_tasks
// (Claude Code < 2.1.145) the field is omitted so the app treats it as
// unknown rather than zero.
//
// A malformed payload still emits the bare event: the lifecycle transition is
// more important than the enrichment fields.
func BuildEventLine(event, session string, stdin []byte, now time.Time) []byte {
	if event == "" || session == "" {
		return nil
	}
	line := eventLine{
		Event:   event,
		TS:      now.UnixNano(),
		Session: session,
	}

	var payload emitPayload
	// Detect field presence for outstanding: only trust background_tasks when
	// the key exists at all.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(stdin, &probe); err == nil {
		_ = json.Unmarshal(stdin, &payload)
		line.Message = payload.Message
		line.Tool = payload.ToolName
		line.AgentType = payload.AgentType
		if event == "SessionStart" {
			line.ClaudeSessionID = payload.SessionID
			line.Cwd = payload.Cwd
		}
		if _, ok := probe["background_tasks"]; ok && carriesOutstanding(event) {
			outstanding := 0
			for _, task := range payload.BackgroundTasks {
				if task.ID != "" && task.ID == payload.AgentID {
					continue
				}
				if nonWorkTaskTypes[task.Type] {
					continue
				}
				if terminalTaskStatuses[task.Status] {
					continue
				}
				outstanding++
			}
			line.Outstanding = &outstanding
		}
	}

	buf, err := json.Marshal(line)
	if err != nil {
		return nil
	}
	return append(buf, '\n')
}
