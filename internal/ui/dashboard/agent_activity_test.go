package dashboard

import "testing"

// TestHasActiveAgentsHookStates verifies which hook states drive the activity
// spinner. SubagentStop fires mid-turn (a subagent finished, the main agent
// keeps working), so it must keep the spinner running.
func TestHasActiveAgentsHookStates(t *testing.T) {
	cases := []struct {
		state string
		want  bool
	}{
		{"PreToolUse", true},
		{"PostToolUse", true},
		{"UserPromptSubmit", true},
		{"SubagentStart", true},
		{"SubagentStop", true},
		{"SubagentWait", true},
		{"NotificationPermission", false},
		{"PermissionRequest", false},
	}
	for _, tc := range cases {
		m := New()
		m.SetHookStates(map[string]string{"ws1": tc.state})
		if got := m.hasActiveAgents(); got != tc.want {
			t.Errorf("hasActiveAgents with state %q = %v, want %v", tc.state, got, tc.want)
		}
	}
}
