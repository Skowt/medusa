package dashboard

import "testing"

// TestHasActiveAgentsHookStates verifies which hook states drive the activity
// spinner. SubagentStop is no longer stored as a state by the app; a value
// persisted by an older Medusa version must not keep the spinner running
// forever after a restart.
func TestHasActiveAgentsHookStates(t *testing.T) {
	cases := []struct {
		state string
		want  bool
	}{
		{"PreToolUse", true},
		{"PostToolUse", true},
		{"UserPromptSubmit", true},
		{"SubagentStart", true},
		{"SubagentStop", false},
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

// TestMarkUnread verifies the asserted-unread contract: marking is idempotent
// (one ping per attention event) and viewing a workspace clears it.
func TestMarkUnread(t *testing.T) {
	m := New()

	if !m.MarkUnread("ws1") {
		t.Fatal("first MarkUnread must report newly marked")
	}
	if m.MarkUnread("ws1") {
		t.Fatal("second MarkUnread must be a no-op (already unread)")
	}

	m.MarkRead("ws1")
	if !m.MarkUnread("ws1") {
		t.Fatal("MarkUnread after MarkRead must mark again")
	}
}
