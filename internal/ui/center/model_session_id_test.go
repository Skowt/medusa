package center

import "testing"

// TestUpdateTabClaudeSessionID verifies the session-id refresh that keeps a
// tab resumable after /clear mints a new Claude session id.
func TestUpdateTabClaudeSessionID(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		Assistant:       "claude",
		Workspace:       ws,
		SessionName:     "medusa-ws-1",
		ClaudeSessionID: "old-sid",
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	t.Run("new id is adopted and reported changed", func(t *testing.T) {
		if !m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "new-sid") {
			t.Fatal("expected update to report a change")
		}
		if tab.ClaudeSessionID != "new-sid" {
			t.Fatalf("ClaudeSessionID = %q, want new-sid", tab.ClaudeSessionID)
		}
	})

	t.Run("same id is a no-op", func(t *testing.T) {
		if m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "new-sid") {
			t.Error("re-reporting the same id must not signal a change")
		}
	})

	t.Run("empty id is ignored", func(t *testing.T) {
		if m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "") {
			t.Error("empty id must be ignored")
		}
		if tab.ClaudeSessionID != "new-sid" {
			t.Errorf("id must be unchanged, got %q", tab.ClaudeSessionID)
		}
	})

	t.Run("unknown session name is a no-op", func(t *testing.T) {
		if m.UpdateTabClaudeSessionID(wsID, "no-such-session", "x") {
			t.Error("unknown session must not signal a change")
		}
	})
}
