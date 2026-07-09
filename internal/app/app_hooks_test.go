package app

import (
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/hooks"
)

func newHookTestApp() *App {
	return &App{
		hookLastStamp:       map[string]hookEventStamp{},
		hookWorkspaceStates: map[string]hooks.EventType{},
		hookOutstanding:     map[string]int{},
	}
}

// applyEvent mirrors handleHookActivityEvent's core: ordering guard, state
// transition, stamp recording. Returns the transition (hookTransitionNone when
// the event was dropped by the guard).
func applyEvent(a *App, ws string, msg hookActivityEvent) hookTransition {
	if !a.shouldApplyHookEvent(ws, msg.Event, msg.Timestamp) {
		return hookTransitionNone
	}
	tr := a.applyHookStateTransition(ws, msg)
	_, busyAfter := a.hookWorkspaceStates[ws]
	a.recordHookEvent(ws, msg.Timestamp, isClearHookEvent(msg.Event) && !busyAfter)
	return tr
}

// ev builds a hookActivityEvent with the outstanding background-task count.
func ev(evt hooks.EventType, outstanding int, ts time.Time) hookActivityEvent {
	return hookActivityEvent{Event: evt, Timestamp: ts, Outstanding: outstanding}
}

func stamp(n int) time.Time {
	return time.Unix(1_700_000_000, 0).Add(time.Duration(n) * time.Second)
}

// TestHookActiveIDs verifies which stored states count as busy. SubagentStop
// is never stored anymore, but a value persisted by an older Medusa version
// must not resurrect as a permanently-busy state after restart.
func TestHookActiveIDs(t *testing.T) {
	a := &App{hookWorkspaceStates: map[string]hooks.EventType{
		"ws-pretool":  hooks.EventPreToolUse,
		"ws-wait":     hooks.EventSubagentWait,
		"ws-perm":     hooks.EventNotificationPermission,
		"ws-legacy":   hooks.EventSubagentStop,
		"ws-permreq":  hooks.EventPermissionRequest,
		"ws-substart": hooks.EventSubagentStart,
	}}

	active := a.hookActiveIDs()

	for _, ws := range []string{"ws-pretool", "ws-wait", "ws-substart"} {
		if !active[ws] {
			t.Errorf("%s must count as active", ws)
		}
	}
	for _, ws := range []string{"ws-perm", "ws-permreq", "ws-legacy"} {
		if active[ws] {
			t.Errorf("%s must not count as active", ws)
		}
	}
}

// TestStopClearsAndPings verifies the core happy path: a turn's final Stop
// with no outstanding background work clears the busy state and reports the
// ready transition (which drives the sound + highlight exactly once).
func TestStopClearsAndPings(t *testing.T) {
	a := newHookTestApp()
	applyEvent(a, "ws", ev(hooks.EventUserPromptSubmit, hooks.OutstandingUnknown, stamp(0)))
	applyEvent(a, "ws", ev(hooks.EventPreToolUse, hooks.OutstandingUnknown, stamp(1)))

	tr := applyEvent(a, "ws", ev(hooks.EventStop, 0, stamp(2)))

	if tr != hookTransitionReady {
		t.Fatalf("transition = %v, want ready", tr)
	}
	if _, ok := a.hookWorkspaceStates["ws"]; ok {
		t.Fatal("state must be deleted on the final Stop")
	}
}

// TestStopWithOutstandingBackgroundWork verifies a Stop that reports live
// background tasks keeps the workspace busy (SubagentWait) with no ping —
// this replaces the old lossy SubagentStart/Stop counter with the payload's
// authoritative background_tasks count.
func TestStopWithOutstandingBackgroundWork(t *testing.T) {
	a := newHookTestApp()
	applyEvent(a, "ws", ev(hooks.EventUserPromptSubmit, hooks.OutstandingUnknown, stamp(0)))

	if tr := applyEvent(a, "ws", ev(hooks.EventStop, 2, stamp(1))); tr != hookTransitionNone {
		t.Fatalf("Stop with outstanding work must not ping, got %v", tr)
	}
	if a.hookWorkspaceStates["ws"] != hooks.EventSubagentWait {
		t.Fatalf("state = %q, want SubagentWait", a.hookWorkspaceStates["ws"])
	}

	// Background agent tool calls keep the workspace busy...
	applyEvent(a, "ws", ev(hooks.EventPostToolUse, hooks.OutstandingUnknown, stamp(30)))
	// ...its SubagentStop never clears busy (Claude auto-resumes next)...
	if tr := applyEvent(a, "ws", ev(hooks.EventSubagentStop, 0, stamp(31))); tr != hookTransitionNone {
		t.Fatalf("SubagentStop must never ping, got %v", tr)
	}
	if !a.hookActiveIDs()["ws"] {
		t.Fatal("workspace must stay busy after the last SubagentStop (auto-resume turn follows)")
	}
	// ...and the resumed turn's final Stop clears with a single ping.
	if tr := applyEvent(a, "ws", ev(hooks.EventStop, 0, stamp(33))); tr != hookTransitionReady {
		t.Fatalf("final Stop must report ready, got %v", tr)
	}
}

// TestPhantomSubagentStopIsInert reproduces the upstream Claude Code bugs
// (#59719, #70151) where a phantom SubagentStop fires seconds after a turn's
// Stop with no subagents involved. It must not revive the spinner — the old
// design stored it as an active state with nothing left to clear it.
func TestPhantomSubagentStopIsInert(t *testing.T) {
	a := newHookTestApp()
	applyEvent(a, "ws", ev(hooks.EventPreToolUse, hooks.OutstandingUnknown, stamp(0)))
	applyEvent(a, "ws", ev(hooks.EventStop, 0, stamp(1)))

	// Phantom arrives 3 seconds later — a genuinely newer timestamp, so the
	// ordering guard alone cannot save us.
	tr := applyEvent(a, "ws", ev(hooks.EventSubagentStop, 0, stamp(4)))

	if tr != hookTransitionNone {
		t.Fatalf("phantom SubagentStop transition = %v, want none", tr)
	}
	if a.hookActiveIDs()["ws"] {
		t.Fatal("phantom SubagentStop must not revive the busy spinner")
	}
}

// TestForegroundSubagentFlow verifies a mid-turn (foreground) subagent finish
// leaves the current busy state untouched.
func TestForegroundSubagentFlow(t *testing.T) {
	a := newHookTestApp()
	applyEvent(a, "ws", ev(hooks.EventPreToolUse, hooks.OutstandingUnknown, stamp(0)))
	applyEvent(a, "ws", ev(hooks.EventSubagentStart, hooks.OutstandingUnknown, stamp(1)))
	applyEvent(a, "ws", ev(hooks.EventSubagentStop, 0, stamp(5)))

	if !a.hookActiveIDs()["ws"] {
		t.Fatal("workspace must stay busy while the main turn continues")
	}
}

// TestStopLegacyOutstandingUnknown verifies fallback shell hooks (no
// outstanding field) degrade to the simple behavior: Stop clears.
func TestStopLegacyOutstandingUnknown(t *testing.T) {
	a := newHookTestApp()
	applyEvent(a, "ws", ev(hooks.EventPreToolUse, hooks.OutstandingUnknown, stamp(0)))
	if tr := applyEvent(a, "ws", ev(hooks.EventStop, hooks.OutstandingUnknown, stamp(1))); tr != hookTransitionReady {
		t.Fatalf("legacy Stop must clear and report ready, got %v", tr)
	}
}

// TestNeedsInputTransitions verifies permission/elicitation events ping once,
// don't re-ping while already waiting, and resolve silently.
func TestNeedsInputTransitions(t *testing.T) {
	t.Run("permission pings once", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventPreToolUse, hooks.OutstandingUnknown, stamp(0)))
		if tr := applyEvent(a, "ws", ev(hooks.EventPermissionRequest, hooks.OutstandingUnknown, stamp(1))); tr != hookTransitionNeedsInput {
			t.Fatalf("PermissionRequest transition = %v, want needsInput", tr)
		}
		// The matching Notification(permission_prompt) follows — no second ping.
		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationPermission, hooks.OutstandingUnknown, stamp(2))); tr != hookTransitionNone {
			t.Fatalf("second needs-input event must not re-ping, got %v", tr)
		}
		if a.hookActiveIDs()["ws"] {
			t.Fatal("needs-input state must not count as busy")
		}
	})

	t.Run("approval resumes work silently", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventPermissionRequest, hooks.OutstandingUnknown, stamp(0)))
		if tr := applyEvent(a, "ws", ev(hooks.EventPostToolUse, hooks.OutstandingUnknown, stamp(1))); tr != hookTransitionNone {
			t.Fatalf("resumed tool use must not ping, got %v", tr)
		}
		if !a.hookActiveIDs()["ws"] {
			t.Fatal("workspace must be busy again after approval")
		}
	})

	t.Run("denial ending the turn clears without a second ping", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventPermissionRequest, hooks.OutstandingUnknown, stamp(0)))
		if tr := applyEvent(a, "ws", ev(hooks.EventStop, 0, stamp(1))); tr != hookTransitionNone {
			t.Fatalf("Stop after needs-input must clear silently, got %v", tr)
		}
		if _, ok := a.hookWorkspaceStates["ws"]; ok {
			t.Fatal("state must be cleared by Stop")
		}
	})

	t.Run("AskUserQuestion needs input despite arriving as PreToolUse", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventUserPromptSubmit, hooks.OutstandingUnknown, stamp(0)))
		msg := hookActivityEvent{Event: hooks.EventPreToolUse, Timestamp: stamp(1), Outstanding: hooks.OutstandingUnknown, Tool: "AskUserQuestion"}
		if tr := applyEvent(a, "ws", msg); tr != hookTransitionNeedsInput {
			t.Fatalf("PreToolUse(AskUserQuestion) transition = %v, want needsInput", tr)
		}
		if a.hookActiveIDs()["ws"] {
			t.Fatal("AskUserQuestion must not count as busy")
		}
	})
}

// TestIdleNotificationWithOutstandingWork verifies the idle notification is
// outstanding-aware: Claude Code fires idle_prompt ~60s after the REPL goes
// idle even while background agents are still working (the REPL is idle
// between auto-resumes), so it must not be trusted as "done" when the last
// authoritative Stop/SubagentStop reported live background tasks.
func TestIdleNotificationWithOutstandingWork(t *testing.T) {
	t.Run("idle while background agents run parks busy without a ping", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventUserPromptSubmit, hooks.OutstandingUnknown, stamp(0)))
		applyEvent(a, "ws", ev(hooks.EventStop, 2, stamp(1)))

		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationIdle, hooks.OutstandingUnknown, stamp(70))); tr != hookTransitionNone {
			t.Fatalf("idle with outstanding work must not ping, got %v", tr)
		}
		if a.hookWorkspaceStates["ws"] != hooks.EventSubagentWait {
			t.Fatalf("state = %q, want SubagentWait (still waiting on background work)", a.hookWorkspaceStates["ws"])
		}
	})

	t.Run("idle after background agent activity overwrote the wait state", func(t *testing.T) {
		// Background-agent tool events replace SubagentWait with a tool state;
		// the outstanding count must survive that and still gate the idle.
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventStop, 1, stamp(0)))
		applyEvent(a, "ws", ev(hooks.EventPostToolUse, hooks.OutstandingUnknown, stamp(10)))

		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationIdle, hooks.OutstandingUnknown, stamp(75))); tr != hookTransitionNone {
			t.Fatalf("idle with outstanding work must not ping, got %v", tr)
		}
		if !a.hookActiveIDs()["ws"] {
			t.Fatal("workspace must stay busy while background work is outstanding")
		}
	})

	t.Run("idle rescue works once outstanding drains to zero", func(t *testing.T) {
		// The last SubagentStop reports nothing outstanding but the auto-resume
		// never happens (wedged session): idle must still rescue with a ping.
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventStop, 1, stamp(0)))
		applyEvent(a, "ws", ev(hooks.EventSubagentStop, 0, stamp(30)))

		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationIdle, hooks.OutstandingUnknown, stamp(95))); tr != hookTransitionReady {
			t.Fatalf("idle with nothing outstanding must rescue with ready, got %v", tr)
		}
	})

	t.Run("legacy events without outstanding keep idle as full clear", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventPreToolUse, hooks.OutstandingUnknown, stamp(0)))
		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationIdle, hooks.OutstandingUnknown, stamp(65))); tr != hookTransitionReady {
			t.Fatalf("idle without any outstanding knowledge must clear and ping, got %v", tr)
		}
	})

	t.Run("interrupt clears outstanding so idle does not park a dead session", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventStop, 2, stamp(0)))
		a.handleAgentInterrupted("ws")

		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationIdle, hooks.OutstandingUnknown, stamp(70))); tr != hookTransitionNone {
			t.Fatalf("idle after interrupt must be silent, got %v", tr)
		}
		if _, ok := a.hookWorkspaceStates["ws"]; ok {
			t.Fatal("idle after interrupt must not revive a busy state")
		}
	})
}

// TestRestoreInfersOutstandingForSubagentWait verifies a persisted SubagentWait
// state restores its background-work knowledge: without it, the first
// idle_prompt after a Medusa restart would false-ping while agents still run.
func TestRestoreInfersOutstandingForSubagentWait(t *testing.T) {
	a := newHookTestApp()
	a.hookWorkspaceStates["ws"] = hooks.EventSubagentWait
	a.restoreHookOutstanding()

	if tr := applyEvent(a, "ws", ev(hooks.EventNotificationIdle, hooks.OutstandingUnknown, stamp(70))); tr != hookTransitionNone {
		t.Fatalf("idle over a restored SubagentWait must not ping, got %v", tr)
	}
	if a.hookWorkspaceStates["ws"] != hooks.EventSubagentWait {
		t.Fatal("restored SubagentWait must survive the idle notification")
	}
}

// TestIdleNotification verifies the 60s idle notification acts as the
// self-healing clear: it rescues a wedged busy state (with a ping — the user
// was never told the agent finished) but never wipes a pending '!' indicator.
func TestIdleNotification(t *testing.T) {
	t.Run("rescues wedged busy state", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventPreToolUse, hooks.OutstandingUnknown, stamp(0)))
		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationIdle, hooks.OutstandingUnknown, stamp(60))); tr != hookTransitionReady {
			t.Fatalf("idle over busy state must report ready, got %v", tr)
		}
	})

	t.Run("does not clear needs-input", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventPermissionRequest, hooks.OutstandingUnknown, stamp(0)))
		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationIdle, hooks.OutstandingUnknown, stamp(60))); tr != hookTransitionNone {
			t.Fatalf("idle over needs-input must be silent, got %v", tr)
		}
		if a.hookWorkspaceStates["ws"] != hooks.EventPermissionRequest {
			t.Fatal("idle must not wipe the pending '!' indicator")
		}
	})

	t.Run("idle after Stop does not double-ping", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventPreToolUse, hooks.OutstandingUnknown, stamp(0)))
		applyEvent(a, "ws", ev(hooks.EventStop, 0, stamp(1)))
		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationIdle, hooks.OutstandingUnknown, stamp(61))); tr != hookTransitionNone {
			t.Fatalf("idle after Stop already pinged must be silent, got %v", tr)
		}
	})
}

// TestShouldApplyHookEventReordering verifies the out-of-order delivery guard.
// Hook events reach the app over per-connection socket goroutines, so a turn's
// terminal Stop can be enqueued before a trailing tool event from the same
// turn; applying that stale active event would revive the spinner.
func TestShouldApplyHookEventReordering(t *testing.T) {
	t.Run("strictly older event is dropped", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventStop, 0, stamp(5)))
		if tr := applyEvent(a, "ws", ev(hooks.EventPostToolUse, hooks.OutstandingUnknown, stamp(4))); tr != hookTransitionNone {
			t.Fatal("older event must be dropped")
		}
		if a.hookActiveIDs()["ws"] {
			t.Error("stale reordered event must not revive activity")
		}
	})

	t.Run("same-timestamp active after clear is dropped", func(t *testing.T) {
		// Legacy shell hooks have second resolution; a trailing tool event and
		// the turn's Stop frequently share a timestamp. The clear wins the tie.
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventStop, 0, stamp(5)))
		applyEvent(a, "ws", ev(hooks.EventPostToolUse, hooks.OutstandingUnknown, stamp(5)))
		if a.hookActiveIDs()["ws"] {
			t.Error("an active event tying a clear must not revive activity")
		}
	})

	t.Run("same-timestamp clear after active wins", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventPostToolUse, hooks.OutstandingUnknown, stamp(5)))
		if tr := applyEvent(a, "ws", ev(hooks.EventStop, 0, stamp(5))); tr != hookTransitionReady {
			t.Error("a clear event must apply over an active event with the same timestamp")
		}
	})

	t.Run("same-timestamp active after non-clearing stop applies", func(t *testing.T) {
		// A Stop that left the workspace busy (outstanding background work)
		// must not suppress same-timestamp tool events from those agents.
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventStop, 1, stamp(5)))
		if !a.shouldApplyHookEvent("ws", hooks.EventPreToolUse, stamp(5)) {
			t.Error("a Stop that kept the workspace busy must not drop same-timestamp active events")
		}
	})
}

// TestReconcileStaleHookStates verifies the safety net: a busy state that has
// seen no hook event for the staleness window (and shows no PTY output) is
// silently degraded to ready — no sound, no unread highlight.
func TestReconcileStaleHookStates(t *testing.T) {
	t.Run("stale busy state clears silently", func(t *testing.T) {
		a := newHookTestApp()
		a.hookWorkspaceStates["ws"] = hooks.EventSubagentWait
		a.hookLastStamp["ws"] = hookEventStamp{at: time.Now().Add(-2 * staleBusyTimeout)}

		cleared := a.staleBusyWorkspaces()

		if len(cleared) != 1 || cleared[0] != "ws" {
			t.Fatalf("staleBusyWorkspaces = %v, want [ws]", cleared)
		}
	})

	t.Run("fresh busy state survives", func(t *testing.T) {
		a := newHookTestApp()
		a.hookWorkspaceStates["ws"] = hooks.EventPreToolUse
		a.hookLastStamp["ws"] = hookEventStamp{at: time.Now().Add(-time.Second)}
		if cleared := a.staleBusyWorkspaces(); len(cleared) != 0 {
			t.Fatalf("fresh state must survive, got %v", cleared)
		}
	})

	t.Run("reconcile clears outstanding knowledge with the state", func(t *testing.T) {
		// A dead session must not leave a stale outstanding count behind that
		// would swallow the idle rescue of a future run.
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventStop, 2, stamp(0)))
		a.hookLastStamp["ws"] = hookEventStamp{at: time.Now().Add(-2 * staleBusyTimeout)}

		if cleared := a.staleBusyWorkspaces(); len(cleared) != 1 {
			t.Fatalf("stale SubagentWait must clear, got %v", cleared)
		}
		if n := a.hookOutstanding["ws"]; n != 0 {
			t.Fatalf("outstanding after reconcile = %d, want cleared", n)
		}
	})

	t.Run("needs-input state is never reconciled away", func(t *testing.T) {
		a := newHookTestApp()
		a.hookWorkspaceStates["ws"] = hooks.EventPermissionRequest
		a.hookLastStamp["ws"] = hookEventStamp{at: time.Now().Add(-2 * staleBusyTimeout)}
		if cleared := a.staleBusyWorkspaces(); len(cleared) != 0 {
			t.Fatalf("needs-input must never be reconciled away, got %v", cleared)
		}
	})

	t.Run("restored state without a stamp gets a grace window", func(t *testing.T) {
		// After a restart, persisted busy states have no stamp; the first pass
		// starts the clock instead of clearing immediately.
		a := newHookTestApp()
		a.hookWorkspaceStates["ws"] = hooks.EventPreToolUse
		if cleared := a.staleBusyWorkspaces(); len(cleared) != 0 {
			t.Fatalf("stampless state must get a grace window, got %v", cleared)
		}
		if _, ok := a.hookLastStamp["ws"]; !ok {
			t.Fatal("grace window must start the staleness clock")
		}
	})
}

// TestAgentInterruptedClearsState verifies Ctrl+C/Esc clears the busy state
// (Claude Code fires no Stop hook on user interrupts).
func TestAgentInterruptedClearsState(t *testing.T) {
	a := newHookTestApp()
	a.hookWorkspaceStates["ws"] = hooks.EventPreToolUse
	a.handleAgentInterrupted("ws")
	if _, ok := a.hookWorkspaceStates["ws"]; ok {
		t.Fatal("interrupt must clear the hook state")
	}
}
