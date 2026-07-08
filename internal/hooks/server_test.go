package hooks

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// shortTempDir returns a temp dir with a short path: macOS caps Unix socket
// paths at 104 bytes, and t.TempDir() under /var/folders can exceed that.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "medusa-hooks-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func startTestServer(t *testing.T, socketPath string) chan HookEvent {
	t.Helper()
	events := make(chan HookEvent, 16)
	srv, err := NewServer(socketPath, func(he HookEvent) { events <- he })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = srv.Close()
	})
	return events
}

func sendLine(t *testing.T, socketPath, line string) {
	t.Helper()
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
}

// TestServerDeliversEvent verifies the socket transport end to end: one JSON
// line per connection, session and message carried in the payload.
func TestServerDeliversEvent(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), SocketName)
	events := startTestServer(t, sock)

	sendLine(t, sock, `{"event":"NotificationPermission","ts":1700000000,"session":"medusa-ws1-tab1","message":"needs approval"}`)

	select {
	case he := <-events:
		if he.SessionName != "medusa-ws1-tab1" {
			t.Errorf("SessionName = %q", he.SessionName)
		}
		if he.Event != EventNotificationPermission {
			t.Errorf("Event = %q", he.Event)
		}
		if he.Message != "needs approval" {
			t.Errorf("Message = %q", he.Message)
		}
		if he.Outstanding != OutstandingUnknown {
			t.Errorf("Outstanding without field = %d, want OutstandingUnknown", he.Outstanding)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// TestServerParsesOutstandingAndTool verifies the outstanding background-work
// count (Stop/SubagentStop) and tool name (PreToolUse) land on the event, and
// that a negative sentinel normalizes to OutstandingUnknown.
func TestServerParsesOutstandingAndTool(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), SocketName)
	events := startTestServer(t, sock)

	recv := func() HookEvent {
		t.Helper()
		select {
		case he := <-events:
			return he
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for event")
			return HookEvent{}
		}
	}

	sendLine(t, sock, `{"event":"Stop","ts":1700000000,"session":"medusa-ws1-tab1","outstanding":2}`)
	if he := recv(); he.Outstanding != 2 {
		t.Errorf("Outstanding = %d, want 2", he.Outstanding)
	}

	sendLine(t, sock, `{"event":"SubagentStop","ts":1700000001,"session":"medusa-ws1-tab1","outstanding":-1}`)
	if he := recv(); he.Outstanding != OutstandingUnknown {
		t.Errorf("Outstanding = %d, want OutstandingUnknown", he.Outstanding)
	}

	// Legacy shell hooks (fallback mode) emit no outstanding field at all.
	sendLine(t, sock, `{"event":"Stop","ts":1700000002,"session":"medusa-ws1-tab1"}`)
	if he := recv(); he.Outstanding != OutstandingUnknown {
		t.Errorf("Outstanding without field = %d, want OutstandingUnknown", he.Outstanding)
	}

	sendLine(t, sock, `{"event":"PreToolUse","ts":1700000003,"session":"medusa-ws1-tab1","tool":"AskUserQuestion"}`)
	if he := recv(); he.Tool != "AskUserQuestion" {
		t.Errorf("Tool = %q, want AskUserQuestion", he.Tool)
	}
}

// TestServerParsesSessionStart verifies the SessionStart fields (the live
// Claude session id and the agent_type discriminator) land on the event.
func TestServerParsesSessionStart(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), SocketName)
	events := startTestServer(t, sock)

	sendLine(t, sock, `{"event":"SessionStart","ts":1700000000,"session":"medusa-ws1-tab1","claude_session_id":"sid-9","agent_type":""}`)
	select {
	case he := <-events:
		if he.Event != EventSessionStart {
			t.Errorf("Event = %q", he.Event)
		}
		if he.ClaudeSessionID != "sid-9" {
			t.Errorf("ClaudeSessionID = %q, want sid-9", he.ClaudeSessionID)
		}
		if he.AgentType != "" {
			t.Errorf("AgentType = %q, want empty", he.AgentType)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// TestServerMalformedInputIgnored verifies garbage input neither crashes the
// server nor blocks subsequent events.
func TestServerMalformedInputIgnored(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), SocketName)
	events := startTestServer(t, sock)

	sendLine(t, sock, `not json at all`)
	sendLine(t, sock, `{"event":"","ts":1,"session":"s"}`)
	sendLine(t, sock, `{"event":"Stop","ts":1700000000,"session":"medusa-ws1-tab1"}`)

	select {
	case he := <-events:
		if he.Event != EventStop {
			t.Errorf("Event = %q, want Stop (malformed lines must be dropped)", he.Event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
	select {
	case he := <-events:
		t.Errorf("unexpected extra event %+v", he)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestServerRemovesStaleSocket verifies a leftover socket file from an
// unclean shutdown does not prevent startup.
func TestServerRemovesStaleSocket(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), SocketName)
	if err := os.WriteFile(sock, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}

	events := startTestServer(t, sock)
	sendLine(t, sock, `{"event":"Stop","ts":1,"session":"medusa-ws1-tab1"}`)

	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not start over a stale socket file")
	}
}

// TestServerRefusesSecondInstance verifies that a live listener on the socket
// is detected and reported instead of being silently stolen.
func TestServerRefusesSecondInstance(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), SocketName)
	startTestServer(t, sock)

	if _, err := NewServer(sock, nil); err == nil {
		t.Fatal("expected error when the socket is held by a live listener")
	}
}
