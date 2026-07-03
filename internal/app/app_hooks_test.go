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
	newApp := newHookTestApp
	apply := func(a *App, ws string, evt hooks.EventType, ts time.Time) bool {
		if !a.shouldApplyHookEvent(ws, evt, ts) {
			return false
		}
		a.recordHookEvent(ws, ts, a.applyHookStateTransition(ws, evt))
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

	t.Run("same-second active after non-clearing stop applies", func(t *testing.T) {
		// A Stop while background subagents run does not clear, so it must not
		// suppress a same-second SubagentStop or the resumed turn's tool events.
		a := newApp()
		a.subagentsPending["ws"] = 1
		apply(a, "ws", hooks.EventStop, sec(5))
		if !apply(a, "ws", hooks.EventPreToolUse, sec(5)) {
			t.Error("a Stop that left the workspace busy must not drop same-second active events")
		}
	})
}

func newHookTestApp() *App {
	return &App{
		hookLastStamp:         map[string]hookEventStamp{},
		hookWorkspaceStates:   map[string]hooks.EventType{},
		subagentsPending:      map[string]int{},
		subagentsPendingStamp: map[string]time.Time{},
	}
}

// TestStopWithOutstandingSubagents verifies the background-agent flow: a Stop
// that arrives while subagents are still outstanding must keep the workspace
// busy (SubagentWait) instead of clearing it — clearing flips it out of the
// active set, which fires the false "ready for review" ping. Only the Stop
// after the last SubagentStop clears.
func TestStopWithOutstandingSubagents(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	sec := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }
	a := newHookTestApp()
	step := func(evt hooks.EventType, pending int, ts time.Time) {
		msg := hookActivityEvent{Event: evt, Timestamp: ts, Pending: pending}
		a.updateSubagentsPending("ws", msg)
		if a.shouldApplyHookEvent("ws", evt, ts) {
			a.recordHookEvent("ws", ts, a.applyHookStateTransition("ws", evt))
		}
	}
	mustBeActive := func(context string) {
		t.Helper()
		if !a.hookActiveIDs()["ws"] {
			t.Fatalf("workspace must stay active %s (state=%q, pending=%d)",
				context, a.hookWorkspaceStates["ws"], a.subagentsPending["ws"])
		}
	}

	step(hooks.EventUserPromptSubmit, hooks.PendingUnknown, sec(0))
	step(hooks.EventSubagentStart, hooks.PendingUnknown, sec(1))
	step(hooks.EventSubagentStart, hooks.PendingUnknown, sec(1))
	// Main turn ends while both background agents still run.
	step(hooks.EventStop, hooks.PendingUnknown, sec(2))
	mustBeActive("after Stop with 2 outstanding subagents")
	if a.hookWorkspaceStates["ws"] != hooks.EventSubagentWait {
		t.Fatalf("state after Stop with outstanding subagents = %q, want SubagentWait", a.hookWorkspaceStates["ws"])
	}

	// First background agent finishes; main agent resumes and stops again.
	step(hooks.EventSubagentStop, 1, sec(60))
	step(hooks.EventPreToolUse, hooks.PendingUnknown, sec(61))
	step(hooks.EventStop, hooks.PendingUnknown, sec(62))
	mustBeActive("after Stop with 1 outstanding subagent")

	// Last background agent finishes; main agent resumes and finishes for real.
	step(hooks.EventSubagentStop, 0, sec(120))
	step(hooks.EventPreToolUse, hooks.PendingUnknown, sec(121))
	step(hooks.EventStop, hooks.PendingUnknown, sec(122))
	if a.hookActiveIDs()["ws"] {
		t.Fatal("workspace must clear on the final Stop with no outstanding subagents")
	}
	if _, ok := a.hookWorkspaceStates["ws"]; ok {
		t.Fatal("hook state must be deleted on the final Stop")
	}
}

// TestUpdateSubagentsPending verifies the counter semantics: SubagentStart
// increments, SubagentStop resyncs from Claude Code's authoritative
// pending_subagent_count (or decrements when the field is absent), and stale
// out-of-order updates are dropped.
func TestUpdateSubagentsPending(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	sec := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }
	ev := func(evt hooks.EventType, pending int, ts time.Time) hookActivityEvent {
		return hookActivityEvent{Event: evt, Timestamp: ts, Pending: pending}
	}

	t.Run("start increments, authoritative stop resyncs", func(t *testing.T) {
		a := newHookTestApp()
		a.updateSubagentsPending("ws", ev(hooks.EventSubagentStart, hooks.PendingUnknown, sec(0)))
		a.updateSubagentsPending("ws", ev(hooks.EventSubagentStart, hooks.PendingUnknown, sec(0)))
		if a.subagentsPending["ws"] != 2 {
			t.Fatalf("pending after two starts = %d, want 2", a.subagentsPending["ws"])
		}
		// Authoritative count heals any drift (e.g. a lost SubagentStart).
		a.updateSubagentsPending("ws", ev(hooks.EventSubagentStop, 3, sec(1)))
		if a.subagentsPending["ws"] != 3 {
			t.Fatalf("pending after authoritative stop = %d, want 3", a.subagentsPending["ws"])
		}
	})

	t.Run("stop without count decrements clamped at zero", func(t *testing.T) {
		a := newHookTestApp()
		a.updateSubagentsPending("ws", ev(hooks.EventSubagentStop, hooks.PendingUnknown, sec(0)))
		if a.subagentsPending["ws"] != 0 {
			t.Fatalf("pending must clamp at 0, got %d", a.subagentsPending["ws"])
		}
	})

	t.Run("stale start after newer stop is dropped", func(t *testing.T) {
		a := newHookTestApp()
		a.updateSubagentsPending("ws", ev(hooks.EventSubagentStop, 0, sec(5)))
		a.updateSubagentsPending("ws", ev(hooks.EventSubagentStart, hooks.PendingUnknown, sec(4)))
		if a.subagentsPending["ws"] != 0 {
			t.Fatalf("stale SubagentStart must not re-inflate a settled count, got %d", a.subagentsPending["ws"])
		}
	})

	t.Run("non-subagent events do not touch the counter", func(t *testing.T) {
		a := newHookTestApp()
		a.subagentsPending["ws"] = 2
		a.updateSubagentsPending("ws", ev(hooks.EventPreToolUse, hooks.PendingUnknown, sec(9)))
		if a.subagentsPending["ws"] != 2 {
			t.Fatalf("counter changed by non-subagent event, got %d", a.subagentsPending["ws"])
		}
	})
}
