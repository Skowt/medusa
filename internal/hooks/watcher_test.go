package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func startTestWatcher(t *testing.T, dir string) chan HookEvent {
	t.Helper()
	events := make(chan HookEvent, 16)
	w, err := NewWatcher(dir, func(he HookEvent) { events <- he })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = w.Close()
	})
	return events
}

// writeEventFile mimics the injected hook command: write to a .tmp file, then
// rename into place so the watcher only ever sees complete content.
func writeEventFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func recvEvent(t *testing.T, events chan HookEvent) HookEvent {
	t.Helper()
	select {
	case he := <-events:
		return he
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hook event")
		return HookEvent{}
	}
}

// TestWatcherSessionFromPayload verifies that per-event files (unique names)
// resolve the session from the JSON payload, not the filename, and are
// removed after processing so the hooks dir does not accumulate files.
func TestWatcherSessionFromPayload(t *testing.T) {
	dir := t.TempDir()
	events := startTestWatcher(t, dir)

	writeEventFile(t, dir, "evt-123-1700000000.json",
		`{"event":"PreToolUse","ts":1700000000,"session":"medusa-ws1-tab1"}`)

	he := recvEvent(t, events)
	if he.SessionName != "medusa-ws1-tab1" {
		t.Errorf("SessionName = %q, want %q (from payload, not filename)", he.SessionName, "medusa-ws1-tab1")
	}
	if he.Event != EventPreToolUse {
		t.Errorf("Event = %q, want PreToolUse", he.Event)
	}

	// Processed per-event files must be deleted.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "evt-123-1700000000.json")); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("per-event file was not removed after processing")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestWatcherOldFormatFallback verifies backward compatibility: sessions
// started before an upgrade still write <session>.json without a session
// field, and the session name is derived from the filename. These files are
// rewritten in place by old hooks, so they must NOT be deleted.
func TestWatcherOldFormatFallback(t *testing.T) {
	dir := t.TempDir()
	events := startTestWatcher(t, dir)

	writeEventFile(t, dir, "medusa-ws2-tab1.json", `{"event":"Stop","ts":1700000000}`)

	he := recvEvent(t, events)
	if he.SessionName != "medusa-ws2-tab1" {
		t.Errorf("SessionName = %q, want %q (fallback to filename)", he.SessionName, "medusa-ws2-tab1")
	}
	if he.Event != EventStop {
		t.Errorf("Event = %q, want Stop", he.Event)
	}

	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "medusa-ws2-tab1.json")); err != nil {
		t.Errorf("old-format file should not be deleted: %v", err)
	}
}

// TestWatcherRapidEventsAllDelivered verifies the loss property that motivated
// per-event files: two events for the same session arriving within the
// debounce window must BOTH be delivered (the old single-file design kept
// only the last one).
func TestWatcherRapidEventsAllDelivered(t *testing.T) {
	dir := t.TempDir()
	events := startTestWatcher(t, dir)

	writeEventFile(t, dir, "evt-100-1700000000.json",
		`{"event":"SubagentStop","ts":1700000000,"session":"medusa-ws3-tab1"}`)
	writeEventFile(t, dir, "evt-101-1700000000.json",
		`{"event":"PreToolUse","ts":1700000000,"session":"medusa-ws3-tab1"}`)

	got := map[EventType]bool{}
	for i := 0; i < 2; i++ {
		he := recvEvent(t, events)
		got[he.Event] = true
	}
	if !got[EventSubagentStop] || !got[EventPreToolUse] {
		t.Errorf("expected both SubagentStop and PreToolUse, got %v", got)
	}
}
