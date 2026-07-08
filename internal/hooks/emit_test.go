package hooks

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

// decodeLine parses an emitted event line back into a generic map so tests can
// assert on the exact wire fields.
func decodeLine(t *testing.T, line []byte) map[string]any {
	t.Helper()
	if len(line) == 0 || line[len(line)-1] != '\n' {
		t.Fatalf("event line must be newline-terminated, got %q", line)
	}
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber() // int64 nanosecond timestamps exceed float64 precision
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("event line is not valid JSON: %v (%q)", err, line)
	}
	return m
}

// num extracts an integer field decoded via json.Number.
func num(t *testing.T, m map[string]any, key string) (int64, bool) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	jn, ok := v.(json.Number)
	if !ok {
		t.Fatalf("field %s = %v is not a number", key, v)
	}
	n, err := jn.Int64()
	if err != nil {
		t.Fatalf("field %s = %v is not an integer: %v", key, v, err)
	}
	return n, true
}

// TestBuildEventLineBasics verifies the minimal wire contract: event name,
// session, and a nanosecond timestamp taken from the supplied clock.
func TestBuildEventLineBasics(t *testing.T) {
	now := time.Unix(1_700_000_000, 123_456_789)
	line := BuildEventLine("PreToolUse", "medusa-ws1-tab1", []byte(`{"tool_name":"Bash"}`), now)

	m := decodeLine(t, line)
	if m["event"] != "PreToolUse" {
		t.Errorf("event = %v", m["event"])
	}
	if m["session"] != "medusa-ws1-tab1" {
		t.Errorf("session = %v", m["session"])
	}
	if ts, _ := num(t, m, "ts"); ts != now.UnixNano() {
		t.Errorf("ts = %d, want %d (nanoseconds)", ts, now.UnixNano())
	}
	if m["tool"] != "Bash" {
		t.Errorf("tool = %v, want Bash", m["tool"])
	}
}

// TestBuildEventLineEmptyInputs verifies the builder refuses to emit without a
// session or event name (non-Medusa sessions must stay silent no-ops).
func TestBuildEventLineEmptyInputs(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if line := BuildEventLine("Stop", "", []byte(`{}`), now); line != nil {
		t.Errorf("empty session must produce no event, got %q", line)
	}
	if line := BuildEventLine("", "medusa-ws1-tab1", []byte(`{}`), now); line != nil {
		t.Errorf("empty event must produce no event, got %q", line)
	}
}

// TestBuildEventLineMalformedStdin verifies a garbage payload still emits the
// bare event: the lifecycle signal matters more than the enrichment fields.
func TestBuildEventLineMalformedStdin(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	line := BuildEventLine("Stop", "medusa-ws1-tab1", []byte(`not json`), now)
	m := decodeLine(t, line)
	if m["event"] != "Stop" {
		t.Errorf("event = %v", m["event"])
	}
	if _, ok := m["outstanding"]; ok {
		t.Error("malformed stdin must not fabricate an outstanding count")
	}
}

// TestBuildEventLineStopOutstanding verifies Stop events carry the count of
// still-running background tasks from the payload — the authoritative
// "turn ended but work continues" discriminator.
func TestBuildEventLineStopOutstanding(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	stdin := []byte(`{
		"hook_event_name": "Stop",
		"stop_hook_active": false,
		"background_tasks": [
			{"id": "a1", "type": "subagent", "status": "running"},
			{"id": "a2", "type": "subagent", "status": "completed"},
			{"id": "a3", "type": "bash", "status": "running"}
		],
		"session_crons": [{"id": "cron1"}]
	}`)
	m := decodeLine(t, BuildEventLine("Stop", "medusa-ws1-tab1", stdin, now))
	// Two tasks still running; the completed one is done. Session crons are
	// deliberately excluded: a recurring cron would otherwise keep the
	// workspace busy forever even though Claude is waiting for input.
	if got, ok := num(t, m, "outstanding"); !ok || got != 2 {
		t.Errorf("outstanding = %d (present=%v), want 2", got, ok)
	}
}

// TestBuildEventLineStopNoBackgroundField verifies that a payload without
// background_tasks (older Claude Code) omits outstanding so the app can treat
// it as unknown rather than zero.
func TestBuildEventLineStopNoBackgroundField(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	m := decodeLine(t, BuildEventLine("Stop", "medusa-ws1-tab1", []byte(`{"hook_event_name":"Stop"}`), now))
	if v, ok := m["outstanding"]; ok {
		t.Errorf("outstanding without background_tasks must be omitted, got %v", v)
	}
}

// TestBuildEventLineStopEmptyBackground verifies an explicit empty list emits
// outstanding=0 — the genuine all-done signal.
func TestBuildEventLineStopEmptyBackground(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	stdin := []byte(`{"background_tasks": [], "session_crons": []}`)
	m := decodeLine(t, BuildEventLine("Stop", "medusa-ws1-tab1", stdin, now))
	if got, ok := num(t, m, "outstanding"); !ok || got != 0 {
		t.Errorf("outstanding = %d (present=%v), want 0", got, ok)
	}
}

// TestBuildEventLineSubagentStopExcludesSelf verifies a SubagentStop does not
// count itself: Claude Code lists the stopping agent as still "running" in its
// own SubagentStop payload, which would otherwise wedge the workspace busy.
func TestBuildEventLineSubagentStopExcludesSelf(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	stdin := []byte(`{
		"agent_id": "a1",
		"agent_type": "general-purpose",
		"background_tasks": [{"id": "a1", "type": "subagent", "status": "running"}]
	}`)
	m := decodeLine(t, BuildEventLine("SubagentStop", "medusa-ws1-tab1", stdin, now))
	if got, ok := num(t, m, "outstanding"); !ok || got != 0 {
		t.Errorf("outstanding = %d (present=%v), want 0 (own agent_id excluded)", got, ok)
	}
	if m["agent_type"] != "general-purpose" {
		t.Errorf("agent_type = %v", m["agent_type"])
	}
}

// TestBuildEventLineToolEventsOmitOutstanding verifies non-stop events never
// carry an outstanding count even if the field were present.
func TestBuildEventLineToolEventsOmitOutstanding(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	stdin := []byte(`{"tool_name":"Bash","background_tasks":[{"id":"x","status":"running"}]}`)
	m := decodeLine(t, BuildEventLine("PostToolUse", "medusa-ws1-tab1", stdin, now))
	if v, ok := m["outstanding"]; ok {
		t.Errorf("outstanding on a tool event must be omitted, got %v", v)
	}
}

// TestBuildEventLineNotificationMessage verifies the notification message
// survives characters that broke the old grep/sed extraction.
func TestBuildEventLineNotificationMessage(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	stdin := []byte(`{"message":"Claude needs permission to run \"rm -rf\" — approve?"}`)
	m := decodeLine(t, BuildEventLine("NotificationPermission", "medusa-ws1-tab1", stdin, now))
	if m["message"] != `Claude needs permission to run "rm -rf" — approve?` {
		t.Errorf("message = %v", m["message"])
	}
}

// TestBuildEventLineSessionStart verifies the live session id and agent_type
// discriminator are forwarded for SessionStart.
func TestBuildEventLineSessionStart(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	stdin := []byte(`{"session_id":"sid-42","source":"clear"}`)
	m := decodeLine(t, BuildEventLine("SessionStart", "medusa-ws1-tab1", stdin, now))
	if m["claude_session_id"] != "sid-42" {
		t.Errorf("claude_session_id = %v", m["claude_session_id"])
	}
}
