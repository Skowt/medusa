package pty

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCodexSessionID = "01a0295f-6cf3-7bd3-89a4-242aa1f40223"

// writeCodexRollout creates the rollout transcript Codex would have written for
// a session, in the layout it actually uses: sessions/<y>/<m>/<d>/rollout-<ts>-<id>.jsonl.
func writeCodexRollout(t *testing.T, codexHome, sessionID string) {
	t.Helper()
	dir := filepath.Join(codexHome, "sessions", "2026", "08", "22")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	name := "rollout-2026-08-22T14-08-34-" + sessionID + ".jsonl"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// A restart resumes the recorded session when its rollout is on disk — that is
// what keeps the conversation across a tab restart.
func TestCodexSessionArgsResumesExistingRollout(t *testing.T) {
	home := t.TempDir()
	writeCodexRollout(t, home, testCodexSessionID)

	got := codexSessionArgs(home, AgentOptions{Resume: true, ClaudeSessionID: testCodexSessionID})
	if want := " resume '" + testCodexSessionID + "'"; got != want {
		t.Errorf("codexSessionArgs = %q, want %q", got, want)
	}
}

// `codex resume <id>` on an unknown id exits 1 ("No saved session found with
// ID …") and drops the tab to a bare shell, so an id with no rollout must
// resolve to a fresh session instead.
func TestCodexSessionArgsStartsFreshWithoutRollout(t *testing.T) {
	home := t.TempDir()
	writeCodexRollout(t, home, "01a02931-d4a3-7131-a380-835d79fc0c1e") // a different session

	if got := codexSessionArgs(home, AgentOptions{Resume: true, ClaudeSessionID: testCodexSessionID}); got != "" {
		t.Errorf("codexSessionArgs = %q, want no resume for a session with no rollout", got)
	}
}

// A fresh tab has no id to resume: Codex mints its own, and Medusa only learns
// it from the SessionStart hook.
func TestCodexSessionArgsIgnoresIDWithoutResume(t *testing.T) {
	home := t.TempDir()
	writeCodexRollout(t, home, testCodexSessionID)

	if got := codexSessionArgs(home, AgentOptions{ClaudeSessionID: testCodexSessionID}); got != "" {
		t.Errorf("codexSessionArgs = %q, want none when Resume is false", got)
	}
}

func TestCodexPolicyArgs(t *testing.T) {
	tests := []struct {
		name string
		opts AgentOptions
		want string
	}{
		{"defaults are left to codex's own config", AgentOptions{}, ""},
		{"auto owns its workspace sandbox", AgentOptions{CodexAuto: true, CodexSandbox: CodexSandboxReadOnly}, ""},
		{"workspace sandbox", AgentOptions{CodexSandbox: CodexSandboxWorkspace}, " --sandbox workspace-write"},
		{
			"full access",
			AgentOptions{CodexSandbox: CodexSandboxFullAccess},
			" --sandbox danger-full-access",
		},
		{
			// Codex exits on an unrecognised policy rather than ignoring it, and
			// these values come from persisted settings a Medusa upgrade may
			// have renamed.
			"unknown values are dropped, not forwarded",
			AgentOptions{CodexSandbox: "bypassPermissions"},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codexPolicyArgs(tt.opts); got != tt.want {
				t.Errorf("codexPolicyArgs = %q, want %q", got, tt.want)
			}
		})
	}
}

// CODEX_HOME is what makes a Codex tab profile-scoped — auth, hooks, and the
// session rollouts a resume resolves against all live under it — so it must be
// on every Codex command line.
func TestBuildAgentCommandCodex(t *testing.T) {
	home := t.TempDir()
	cmd := buildAgentCommand(AgentCodex, "codex", "medusa-ws-1", home, AgentOptions{
		CodexSandbox: CodexSandboxReadOnly,
		CodexAuto:    true,
	})

	for _, want := range []string{
		"CODEX_HOME='" + home + "'",
		"MEDUSA_SESSION_NAME='medusa-ws-1'",
		`codex --no-alt-screen --search --approve-for-me`,
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("codex command missing %q: %s", want, cmd)
		}
	}
	if strings.Contains(cmd, " --sandbox ") {
		t.Errorf("Codex Auto cannot be combined with an explicit sandbox: %s", cmd)
	}
	// Every Claude-only flag must stay out: codex rejects unknown flags and the
	// tab would drop straight to a shell.
	for _, unwanted := range []string{"CLAUDE", "--session-id", "--permission-mode", "--enable-auto-mode", "--settings"} {
		if strings.Contains(cmd, unwanted) {
			t.Errorf("codex command carries Claude-only %q: %s", unwanted, cmd)
		}
	}
}

func TestBuildAgentCommandCodexAlwaysUsesAutomaticApproval(t *testing.T) {
	cmd := buildAgentCommand(AgentCodex, "codex", "medusa-ws-1", t.TempDir(), AgentOptions{
		CodexAuto: true,
	})
	if !strings.Contains(cmd, " --approve-for-me") {
		t.Errorf("Codex command must enable automatic approval: %s", cmd)
	}
	if strings.Contains(cmd, "--ask-for-approval") {
		t.Errorf("Codex command must not combine automatic and manual approval policies: %s", cmd)
	}
}

// The resume subcommand has to precede the flags, which is the only order
// `codex resume` accepts.
func TestBuildAgentCommandCodexResumeOrder(t *testing.T) {
	home := t.TempDir()
	writeCodexRollout(t, home, testCodexSessionID)

	cmd := buildAgentCommand(AgentCodex, "codex", "medusa-ws-1", home, AgentOptions{
		Resume:          true,
		ClaudeSessionID: testCodexSessionID,
		CodexSandbox:    CodexSandboxWorkspace,
		CodexAuto:       true,
	})
	inlineAt := strings.Index(cmd, " --no-alt-screen")
	autoAt := strings.Index(cmd, " --approve-for-me")
	resumeAt := strings.Index(cmd, " resume ")
	if inlineAt < 0 || autoAt < 0 || resumeAt < 0 {
		t.Fatalf("want inline mode, automatic approval, and resume in: %s", cmd)
	}
	if inlineAt > autoAt || autoAt > resumeAt {
		t.Errorf("global flags must precede resume: %s", cmd)
	}
	if strings.Contains(cmd, " --sandbox ") {
		t.Errorf("Codex Auto cannot be combined with an explicit sandbox: %s", cmd)
	}
}

// A Claude tab must be untouched by any of this.
func TestBuildAgentCommandClaudeUnaffectedByCodexOptions(t *testing.T) {
	cmd := buildAgentCommand(AgentClaude, "claude", "medusa-ws-1", t.TempDir(), AgentOptions{
		CodexSandbox: CodexSandboxFullAccess,
		CodexAuto:    true,
	})
	for _, unwanted := range []string{"--sandbox danger-full-access", "--search", "CODEX_HOME"} {
		if strings.Contains(cmd, unwanted) {
			t.Errorf("claude command carries Codex-only %q: %s", unwanted, cmd)
		}
	}
}
