package pty

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Skowt/medusa/internal/shellutil"
)

func writeConversation(t *testing.T, configDir, project, sessionID string) {
	t.Helper()
	dir := filepath.Join(configDir, "projects", project)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatalf("write conversation: %v", err)
	}
}

func TestClaudeConversationExists(t *testing.T) {
	configDir := t.TempDir()
	writeConversation(t, configDir, "-tmp-ws", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")

	if !claudeConversationExists(configDir, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee") {
		t.Fatalf("expected conversation to be found")
	}
	if claudeConversationExists(configDir, "11111111-2222-3333-4444-555555555555") {
		t.Fatalf("expected missing session ID to not be found")
	}
	if claudeConversationExists(filepath.Join(configDir, "does-not-exist"), "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee") {
		t.Fatalf("expected missing config dir to report no conversation")
	}
}

// A tab's Claude session ID is pre-generated at launch (--session-id) and only
// becomes a resumable conversation once the user actually converses. Resuming
// an ID that has no conversation file makes claude exit with "No conversation
// found" and drop the tab to a bare shell — so restore/restart must fall back
// to starting fresh under the same ID.
func TestClaudeSessionArgs(t *testing.T) {
	configDir := t.TempDir()
	existing := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	missing := "11111111-2222-3333-4444-555555555555"
	writeConversation(t, configDir, "-tmp-ws", existing)

	cases := []struct {
		name string
		opts AgentOptions
		want string
	}{
		{"no session id", AgentOptions{}, ""},
		{"new tab", AgentOptions{ClaudeSessionID: missing}, " --session-id " + shellutil.Quote(missing)},
		{"resume with conversation", AgentOptions{ClaudeSessionID: existing, Resume: true}, " --resume " + shellutil.Quote(existing)},
		{"resume without conversation falls back", AgentOptions{ClaudeSessionID: missing, Resume: true}, " --session-id " + shellutil.Quote(missing)},
	}
	for _, tc := range cases {
		if got := claudeSessionArgs(configDir, tc.opts); got != tc.want {
			t.Errorf("%s: claudeSessionArgs = %q, want %q", tc.name, got, tc.want)
		}
	}
}
