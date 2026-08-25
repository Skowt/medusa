package app

import (
	"testing"

	"github.com/Skowt/medusa/internal/hooks"
)

// Needs-input is the one transition that has to hold across two assistants
// whose lifecycle events do not line up — see the Codex tabs section of
// CLAUDE.md for what each one actually fires.

// TestNeedsInputTransitions verifies permission/elicitation events ping once,
// don't re-ping while already waiting, and resolve silently.
func TestNeedsInputTransitions(t *testing.T) {
	t.Run("permission pings once", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventPreToolUse, hooks.OutstandingUnknown, stamp(0)))
		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationElicitation, hooks.OutstandingUnknown, stamp(1))); tr != hookTransitionNeedsInput {
			t.Fatalf("PermissionRequest transition = %v, want needsInput", tr)
		}
		// The matching Notification(permission_prompt) follows — no second ping.
		if tr := applyEvent(a, "ws", ev(hooks.EventNotificationElicitation, hooks.OutstandingUnknown, stamp(2))); tr != hookTransitionNone {
			t.Fatalf("second needs-input event must not re-ping, got %v", tr)
		}
		if a.hookActiveIDs()["ws"] {
			t.Fatal("needs-input state must not count as busy")
		}
	})

	t.Run("approval resumes work silently", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventNotificationElicitation, hooks.OutstandingUnknown, stamp(0)))
		if tr := applyEvent(a, "ws", ev(hooks.EventPostToolUse, hooks.OutstandingUnknown, stamp(1))); tr != hookTransitionNone {
			t.Fatalf("resumed tool use must not ping, got %v", tr)
		}
		if !a.hookActiveIDs()["ws"] {
			t.Fatal("workspace must be busy again after approval")
		}
	})

	t.Run("denial ending the turn clears without a second ping", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventNotificationElicitation, hooks.OutstandingUnknown, stamp(0)))
		if tr := applyEvent(a, "ws", ev(hooks.EventStop, 0, stamp(1))); tr != hookTransitionNone {
			t.Fatalf("Stop after needs-input must clear silently, got %v", tr)
		}
		if _, ok := a.hookWorkspaceStates["ws"]; ok {
			t.Fatal("state must be cleared by Stop")
		}
	})

	// Both assistants deliver a question as an ordinary PreToolUse, so the tool
	// name is the only thing separating it from work. Codex's is the only
	// needs-input signal a Codex tab has: it fires no Notification event, so
	// elicitation never reaches it.
	for _, tool := range []string{"AskUserQuestion", "request_user_input"} {
		t.Run(tool+" needs input despite arriving as PreToolUse", func(t *testing.T) {
			a := newHookTestApp()
			applyEvent(a, "ws", ev(hooks.EventUserPromptSubmit, hooks.OutstandingUnknown, stamp(0)))
			msg := hookActivityEvent{Event: hooks.EventPreToolUse, Timestamp: stamp(1), Outstanding: hooks.OutstandingUnknown, Tool: tool}
			if tr := applyEvent(a, "ws", msg); tr != hookTransitionNeedsInput {
				t.Fatalf("PreToolUse(%s) transition = %v, want needsInput", tool, tr)
			}
			if a.hookActiveIDs()["ws"] {
				t.Fatalf("%s must not count as busy", tool)
			}
		})
	}

	// PermissionRequest is the assistants' one shared approval signal, but it
	// does not mean the same thing on both. Claude Code fires it only when it
	// is about to prompt a human. Codex fires it before it picks a reviewer,
	// so in Auto mode (--approve-for-me) it also covers approvals its own
	// automatic reviewer resolves silently — pinging on those would fire on
	// every sandbox escape a Codex tab makes, and Auto is Medusa's default.
	t.Run("PermissionRequest needs input when the user is the reviewer", func(t *testing.T) {
		a := newHookTestApp()
		applyEvent(a, "ws", ev(hooks.EventPreToolUse, hooks.OutstandingUnknown, stamp(0)))
		msg := hookActivityEvent{Event: hooks.EventPermissionRequest, Timestamp: stamp(1), Outstanding: hooks.OutstandingUnknown}
		if tr := applyEvent(a, "ws", msg); tr != hookTransitionNeedsInput {
			t.Fatalf("PermissionRequest transition = %v, want needsInput", tr)
		}
		if a.hookActiveIDs()["ws"] {
			t.Fatal("a pending approval must not count as busy")
		}
		// Approving resumes work silently; the user was already pinged.
		if tr := applyEvent(a, "ws", ev(hooks.EventPostToolUse, hooks.OutstandingUnknown, stamp(2))); tr != hookTransitionNone {
			t.Fatalf("resumed tool use must not ping, got %v", tr)
		}
		if !a.hookActiveIDs()["ws"] {
			t.Fatal("workspace must be busy again after approval")
		}
	})

	t.Run("PermissionRequest stays busy when a reviewer answers it", func(t *testing.T) {
		a := newHookTestApp()
		msg := hookActivityEvent{Event: hooks.EventPermissionRequest, Timestamp: stamp(0), Outstanding: hooks.OutstandingUnknown, AutoReviewer: true}
		if tr := applyEvent(a, "ws", msg); tr != hookTransitionNone {
			t.Fatalf("auto-reviewed PermissionRequest transition = %v, want none", tr)
		}
		if !a.hookActiveIDs()["ws"] {
			t.Fatal("an automatic review is work in progress and must read as busy")
		}
		if tr := applyEvent(a, "ws", ev(hooks.EventStop, 0, stamp(1))); tr != hookTransitionReady {
			t.Fatalf("Stop after an automatic review must report ready, got %v", tr)
		}
	})

	t.Run("an ordinary tool stays busy", func(t *testing.T) {
		a := newHookTestApp()
		msg := hookActivityEvent{Event: hooks.EventPreToolUse, Timestamp: stamp(0), Outstanding: hooks.OutstandingUnknown, Tool: "shell"}
		if tr := applyEvent(a, "ws", msg); tr != hookTransitionNone {
			t.Fatalf("PreToolUse(shell) transition = %v, want none", tr)
		}
		if !a.hookActiveIDs()["ws"] {
			t.Fatal("an ordinary tool must read as busy")
		}
	})
}
