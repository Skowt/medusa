package center

import (
	"os"
	"path/filepath"
	"testing"
)

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
		if !m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "new-sid", "/repo/ws") {
			t.Fatal("expected update to report a change")
		}
		if tab.ClaudeSessionID != "new-sid" {
			t.Fatalf("ClaudeSessionID = %q, want new-sid", tab.ClaudeSessionID)
		}
	})

	t.Run("same id is a no-op", func(t *testing.T) {
		if m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "new-sid", "/repo/ws") {
			t.Error("re-reporting the same id must not signal a change")
		}
	})

	t.Run("empty id is ignored", func(t *testing.T) {
		if m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "", "/repo/ws") {
			t.Error("empty id must be ignored")
		}
		if tab.ClaudeSessionID != "new-sid" {
			t.Errorf("id must be unchanged, got %q", tab.ClaudeSessionID)
		}
	})

	t.Run("unknown session name is a no-op", func(t *testing.T) {
		if m.UpdateTabClaudeSessionID(wsID, "no-such-session", "x", "/repo/ws") {
			t.Error("unknown session must not signal a change")
		}
	})

	// /clear mints a new session id in the same directory. That is the case the
	// refresh exists for, so the cwd filter must not touch it.
	t.Run("clear in the workspace is adopted", func(t *testing.T) {
		if !m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "cleared-sid", "/repo/ws") {
			t.Fatal("a /clear in the tab's own directory must be adopted")
		}
		if tab.ClaudeSessionID != "cleared-sid" {
			t.Fatalf("ClaudeSessionID = %q, want cleared-sid", tab.ClaudeSessionID)
		}
	})

	t.Run("a subdirectory of the worktree is adopted", func(t *testing.T) {
		if !m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "sub-sid", "/repo/ws/internal/app") {
			t.Fatal("a cwd inside the worktree must be adopted")
		}
	})

	// The bug this filter exists for: a nested claude inherits
	// MEDUSA_SESSION_NAME, so it fires SessionStart under this tab's session
	// name while running somewhere else entirely. Adopting its id leaves the
	// tab pointing at a session it cannot resume.
	t.Run("another workspace's cwd is rejected", func(t *testing.T) {
		before := tab.ClaudeSessionID
		if m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "foreign-sid", "/repo/other-ws") {
			t.Error("an id reported from outside the workspace must not be adopted")
		}
		if tab.ClaudeSessionID != before {
			t.Errorf("ClaudeSessionID = %q, want %q", tab.ClaudeSessionID, before)
		}
	})

	// A sibling directory that merely shares the root's prefix is outside it.
	t.Run("prefix-sharing sibling is rejected", func(t *testing.T) {
		if m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "sibling-sid", "/repo/ws-other") {
			t.Error("/repo/ws-other is not inside /repo/ws")
		}
	})

	// Hook emitters that predate the cwd field send none; rejecting those would
	// stop tracking /clear entirely for anyone still running one.
	t.Run("missing cwd is accepted", func(t *testing.T) {
		if !m.UpdateTabClaudeSessionID(wsID, "medusa-ws-1", "no-cwd-sid", "") {
			t.Error("an event without a cwd must still be adopted")
		}
	})
}

func TestCwdWithinWorkspace(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")

	cases := []struct {
		name string
		cwd  string
		want bool
	}{
		{"the root itself", "/repo/ws", true},
		{"a subdirectory", "/repo/ws/internal", true},
		{"an unclean path", "/repo/ws/internal/..", true},
		{"a sibling", "/repo/other", false},
		{"a prefix-sharing sibling", "/repo/ws-other", false},
		{"the parent", "/repo", false},
		{"unknown cwd", "", true},
	}
	for _, tc := range cases {
		if got := cwdWithinWorkspace(ws, tc.cwd); got != tc.want {
			t.Errorf("%s: cwdWithinWorkspace(%q) = %v, want %v", tc.name, tc.cwd, got, tc.want)
		}
	}

	if !cwdWithinWorkspace(nil, "/anywhere") {
		t.Error("a nil workspace carries no evidence and must be accepted")
	}
}

// The registry can hold a symlinked path while Claude Code reports the resolved
// one (/tmp vs /private/tmp on macOS). A spurious mismatch there would silently
// stop session-id tracking, so both sides are resolved before comparison.
func TestCwdWithinWorkspaceResolvesSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	ws := newTestWorkspace("ws", link)
	if !cwdWithinWorkspace(ws, real) {
		t.Errorf("resolved cwd %q must match symlinked root %q", real, link)
	}
	if !cwdWithinWorkspace(ws, filepath.Join(real, "sub")) {
		t.Errorf("a subdirectory of the resolved root must match")
	}
}
