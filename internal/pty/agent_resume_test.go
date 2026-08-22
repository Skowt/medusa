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

// A recorded id with no transcript does not mean the tab has no conversation:
// a stray SessionStart can leave the tab holding an id that never owned one
// (see handleSessionStart). Starting fresh there silently discards the tab's
// real conversation, so the fallback continues the worktree's most recent one
// whenever there is one to continue.
func TestClaudeSessionArgsContinuesWorktreeConversation(t *testing.T) {
	configDir := t.TempDir()
	cwd := "/Users/me/.medusa/workspaces/ws"
	phantom := "11111111-2222-3333-4444-555555555555"

	opts := AgentOptions{ClaudeSessionID: phantom, Resume: true, Cwd: cwd}

	// Nothing on disk for this worktree yet: a fresh session under the
	// recorded id is still the only safe answer, since --continue would exit
	// with "No conversation found" and drop the tab to a bare shell.
	if got, want := claudeSessionArgs(configDir, opts), " --session-id "+shellutil.Quote(phantom); got != want {
		t.Errorf("empty worktree: claudeSessionArgs = %q, want %q", got, want)
	}

	// A conversation exists under this worktree's project directory, just not
	// under the recorded id.
	writeConversation(t, configDir, "-Users-me--medusa-workspaces-ws", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if got, want := claudeSessionArgs(configDir, opts), " --continue"; got != want {
		t.Errorf("orphaned id: claudeSessionArgs = %q, want %q", got, want)
	}

	// The recorded id itself still wins when it does resolve.
	opts.ClaudeSessionID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	if got, want := claudeSessionArgs(configDir, opts), " --resume "+shellutil.Quote(opts.ClaudeSessionID); got != want {
		t.Errorf("resumable id: claudeSessionArgs = %q, want %q", got, want)
	}

	// A conversation in a different worktree must not be continued here.
	other := AgentOptions{ClaudeSessionID: phantom, Resume: true, Cwd: "/Users/me/.medusa/workspaces/other"}
	if got, want := claudeSessionArgs(configDir, other), " --session-id "+shellutil.Quote(phantom); got != want {
		t.Errorf("other worktree: claudeSessionArgs = %q, want %q", got, want)
	}

	// No cwd at all (a caller that never set it) cannot locate a project
	// directory, so it must not guess.
	noCwd := AgentOptions{ClaudeSessionID: phantom, Resume: true}
	if got, want := claudeSessionArgs(configDir, noCwd), " --session-id "+shellutil.Quote(phantom); got != want {
		t.Errorf("no cwd: claudeSessionArgs = %q, want %q", got, want)
	}
}

// Claude Code encodes a cwd into its project directory name by replacing every
// '/' and '.' with '-'; the doubled dash comes from the dot in .medusa.
func TestClaudeProjectDir(t *testing.T) {
	got := claudeProjectDir("/cfg", "/Users/me/.medusa/workspaces/ws")
	want := "/cfg/projects/-Users-me--medusa-workspaces-ws"
	if got != want {
		t.Errorf("claudeProjectDir = %q, want %q", got, want)
	}
	if claudeProjectDir("", "/x") != "" || claudeProjectDir("/cfg", "") != "" {
		t.Error("an unknown config dir or cwd must yield no project directory")
	}
}
