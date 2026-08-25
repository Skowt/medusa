package app

import (
	"github.com/Skowt/medusa/internal/hooks"
)

// This file holds the rules that turn one hook event into workspace state: what
// counts as needing the user, what counts as busy, and what earns a ping. They
// are kept apart from app_hooks.go's plumbing (socket, routing, persistence)
// because they are the part that has to be reasoned about against two
// assistants' differing lifecycle semantics — see the Codex tabs section of
// CLAUDE.md.

// hookTransition is the user-visible outcome of applying a hook event: it
// drives the notification sound and the unread (orange) highlight. Pings are
// asserted by explicit state transitions — never inferred from a workspace
// dropping out of an "active set", which is what made every stray event a
// potential false sound.
type hookTransition int

const (
	hookTransitionNone       hookTransition = iota
	hookTransitionReady                     // turn complete, nothing outstanding — "what's next?"
	hookTransitionNeedsInput                // blocked on the user (permission / question / elicitation)
)

// setHookOutstandingFor records the latest authoritative background-task count
// for a key. OutstandingUnknown (legacy shell hooks) leaves the previous
// knowledge untouched; zero deletes the entry to keep the map from growing.
func setHookOutstandingFor(outstandingByKey map[string]int, key string, outstanding int) {
	switch {
	case outstanding < 0:
	case outstanding == 0:
		delete(outstandingByKey, key)
	default:
		outstandingByKey[key] = outstanding
	}
}

// restoreHookOutstanding rebuilds background-work knowledge from restored
// states: a persisted SubagentWait means the turn ended with live background
// tasks, so the first idle_prompt after a restart must not false-ping. The
// exact count is unknown; 1 is enough to gate the idle until real events

// isNeedsInputState reports whether a stored state means the agent is blocked
// on the user. Every needs-input signal — an MCP elicitation, a question tool,
// an approval the user must answer — is stored under this one name, so there is
// a single value for the dashboard and the persisted ActivityState to carry.
func isNeedsInputState(evt hooks.EventType) bool {
	switch evt {
	case hooks.EventNotificationElicitation:
		return true
	}
	return false
}

// applyHookStateTransition updates hookWorkspaceStates for an applied event
// and reports the user-visible transition.
//
// The state is derived from payload truth, not event counting: Stop carries
// the authoritative count of still-running background tasks (background_tasks
// since Claude Code 2.1.145), so a turn that ends with live background agents
// parks in SubagentWait — busy, no ping — and the auto-resumed turn's final
// Stop clears. SubagentStop is deliberately inert: Claude Code is known to
// fire phantom/duplicate SubagentStop events after a turn's Stop (upstream
// #59719, #70151), and any rule that lets SubagentStop set a state creates a
// busy value with nothing left to clear it.
func (a *App) applyHookStateTransition(wsID string, msg hookActivityEvent) hookTransition {
	return applyHookTransition(a.hookWorkspaceStates, a.hookOutstanding, wsID, msg)
}

func applyHookTransition(states map[string]hooks.EventType, outstanding map[string]int, key string, msg hookActivityEvent) hookTransition {
	prev, hadPrev := states[key]
	evt := msg.Event
	switch {
	case evt == hooks.EventStop || evt == hooks.EventStopFailure:
		setHookOutstandingFor(outstanding, key, msg.Outstanding)
		if msg.Outstanding > 0 {
			states[key] = hooks.EventSubagentWait
			return hookTransitionNone
		}
		delete(states, key)
		// Ping only when leaving a busy state: after needs-input the user was
		// already pinged, and with no state at all there is nothing to report.
		if hadPrev && !isNeedsInputState(prev) {
			return hookTransitionReady
		}
		return hookTransitionNone

	case evt == hooks.EventSubagentStop:
		// Inert for the state; only its outstanding count is trusted (it is
		// authoritative and assignment-only, so a phantom SubagentStop's own
		// payload still describes the session truthfully).
		setHookOutstandingFor(outstanding, key, msg.Outstanding)
		return hookTransitionNone

	case evt == hooks.EventNotificationIdle:
		// Claude fires idle_prompt ~60s after the REPL goes quiet — including
		// while background agents still work (the REPL is idle between
		// auto-resumes). With outstanding work it must read as still-busy, not
		// done; the auto-resumed turn's final Stop delivers the real ping.
		// With nothing outstanding it is the self-healing clear for any
		// wedged busy state (missed Stop, lost event). It must never wipe a
		// pending '!': the question dialog is still on screen.
		if hadPrev && isNeedsInputState(prev) {
			return hookTransitionNone
		}
		if outstanding[key] > 0 {
			states[key] = hooks.EventSubagentWait
			return hookTransitionNone
		}
		delete(states, key)
		if hadPrev {
			return hookTransitionReady
		}
		return hookTransitionNone

	case evt == hooks.EventPermissionRequest && msg.AutoReviewer:
		// Codex runs PermissionRequest hooks *before* it picks a reviewer, so
		// in Auto mode (--approve-for-me) this fires for approvals its
		// automatic reviewer then resolves on its own, with no prompt ever
		// shown. Treating those as needs-input would ping on every sandbox
		// escape a Codex tab makes, which is Medusa's default Codex mode. The
		// review is real work, so the tab stays busy until it resolves.
		states[key] = evt
		return hookTransitionNone

	case isNeedsInputState(evt),
		evt == hooks.EventPermissionRequest,
		evt == hooks.EventPreToolUse && hooks.IsQuestionTool(msg.Tool):
		// A question tool is a dialog, not work: it arrives as PreToolUse but
		// blocks on the user. Together with PermissionRequest it is the only
		// needs-input signal a Codex tab has — Codex fires no Notification
		// event, so elicitation never reaches it.
		state := evt
		if evt != hooks.EventNotificationElicitation {
			// Every needs-input signal is stored under one name so the
			// dashboard, the persisted ActivityState and isNeedsInputState
			// all have a single value to reason about.
			state = hooks.EventNotificationElicitation
		}
		states[key] = state
		if hadPrev && isNeedsInputState(prev) {
			return hookTransitionNone // already waiting; one ping is enough
		}
		return hookTransitionNeedsInput

	default:
		// Tool activity, prompt submission, subagent start: busy.
		states[key] = evt
		return hookTransitionNone
	}
}
