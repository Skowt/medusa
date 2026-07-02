package app

import (
	"testing"

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
