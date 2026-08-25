package app

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
)

// TestTabAutoReviewer covers the one thing that decides whether a
// PermissionRequest hook means a human is waiting. Codex fires that hook before
// it picks a reviewer, so an Auto tab (--approve-for-me) sees it for approvals
// its own reviewer then resolves silently; treating those as needs-input would
// ping on every sandbox escape, and Auto is Medusa's default Codex mode.
func TestTabAutoReviewer(t *testing.T) {
	cases := []struct {
		name string
		tab  data.TabInfo
		want bool
	}{
		{"codex auto", data.TabInfo{Assistant: "codex", CodexAuto: true}, true},
		{"codex default mode prompts the user", data.TabInfo{Assistant: "codex"}, false},
		// Claude Code fires PermissionRequest only when it is about to prompt a
		// human, so nothing about a Claude tab may suppress the ping — not even
		// a CodexAuto value left on it by a restart that switched assistants.
		{"claude", data.TabInfo{Assistant: "claude"}, false},
		{"claude with a stale codex flag", data.TabInfo{Assistant: "claude", CodexAuto: true}, false},
		{"shell tab", data.TabInfo{}, false},
	}
	for _, tc := range cases {
		if got := tabAutoReviewer(tc.tab); got != tc.want {
			t.Errorf("%s: tabAutoReviewer = %v, want %v", tc.name, got, tc.want)
		}
	}
}
