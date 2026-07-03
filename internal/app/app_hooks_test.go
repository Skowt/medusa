package app

import (
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/hooks"
)

// TestHookActiveIDsSubagentStop verifies that SubagentStop counts as an active
// (busy) state: it fires mid-turn whenever a subagent finishes while the main
// agent keeps working, so it must not drop the workspace out of the active set
// (which would trigger a false "ready for review" ping).
func TestHookActiveIDsSubagentStop(t *testing.T) {
	a := &App{hookWorkspaceStates: map[string]hooks.EventType{
		"ws-subagent": hooks.EventSubagentStop,
		"ws-pretool":  hooks.EventPreToolUse,
		"ws-perm":     hooks.EventNotificationPermission,
	}}

	active := a.hookActiveIDs()

	if !active["ws-subagent"] {
		t.Error("SubagentStop should count as active: main agent continues after a subagent finishes")
	}
	if !active["ws-pretool"] {
		t.Error("PreToolUse should count as active")
	}
	if active["ws-perm"] {
		t.Error("NotificationPermission should not count as active: agent is blocked on the user")
	}
}

// TestShouldApplyHookEventReordering verifies the out-of-order delivery guard.
// Hook events reach the app over per-connection socket goroutines
// (internal/hooks/server.go), so a turn's terminal Stop can be enqueued before
// a trailing PreToolUse/PostToolUse from the same turn. Applying that stale
// active event would revive the activity spinner with nothing left to clear it.
func TestShouldApplyHookEventReordering(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	sec := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }

	// A helper mirroring handleHookActivityEvent: guard, then stamp on apply.
	newApp := func() *App { return &App{hookLastStamp: map[string]hookEventStamp{}} }
	apply := func(a *App, ws string, evt hooks.EventType, ts time.Time) bool {
		if !a.shouldApplyHookEvent(ws, evt, ts) {
			return false
		}
		a.recordHookEvent(ws, evt, ts)
		return true
	}

	t.Run("first event always applies", func(t *testing.T) {
		a := newApp()
		if !apply(a, "ws", hooks.EventPreToolUse, sec(0)) {
			t.Fatal("first event must apply")
		}
	})

	t.Run("strictly older event is dropped", func(t *testing.T) {
		a := newApp()
		apply(a, "ws", hooks.EventStop, sec(5))
		if apply(a, "ws", hooks.EventPostToolUse, sec(4)) {
			t.Error("an event older than the last applied one must be dropped as a stale reorder")
		}
	})

	t.Run("newer event applies", func(t *testing.T) {
		a := newApp()
		apply(a, "ws", hooks.EventStop, sec(5))
		if !apply(a, "ws", hooks.EventUserPromptSubmit, sec(6)) {
			t.Error("a genuinely newer event (next turn) must apply")
		}
	})

	t.Run("same-second active after clear is dropped", func(t *testing.T) {
		// The core bug: PostToolUse fired before Stop (same wall-clock second),
		// but was delivered after it. It must not revive the spinner.
		a := newApp()
		apply(a, "ws", hooks.EventStop, sec(5))
		if apply(a, "ws", hooks.EventPostToolUse, sec(5)) {
			t.Error("an active event tying a clear in the same second must not revive activity")
		}
	})

	t.Run("same-second clear after active wins", func(t *testing.T) {
		// Stop must be able to clear a PostToolUse it ties in the same second.
		a := newApp()
		apply(a, "ws", hooks.EventPostToolUse, sec(5))
		if !apply(a, "ws", hooks.EventStop, sec(5)) {
			t.Error("a clear event must apply over an active event in the same second")
		}
	})

	t.Run("same-second active after active applies", func(t *testing.T) {
		// Rapid tool calls in one second are all active; none is a false clear.
		a := newApp()
		apply(a, "ws", hooks.EventPreToolUse, sec(5))
		if !apply(a, "ws", hooks.EventPostToolUse, sec(5)) {
			t.Error("consecutive active events in the same second must apply")
		}
	})
}
