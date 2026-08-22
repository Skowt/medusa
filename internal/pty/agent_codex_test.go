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
		{
			"sandbox and approval",
			AgentOptions{CodexSandbox: CodexSandboxWorkspace, CodexApproval: CodexApprovalOnRequest},
			" --sandbox workspace-write --ask-for-approval on-request",
		},
		{
			"full access with no approvals needs no bypass flag",
			AgentOptions{CodexSandbox: CodexSandboxFullAccess, CodexApproval: CodexApprovalNever},
			" --sandbox danger-full-access --ask-for-approval never",
		},
		{"web search", AgentOptions{CodexSearch: true}, " --search"},
		{
			// Codex exits on an unrecognised policy rather than ignoring it, and
			// these values come from persisted settings a Medusa upgrade may
			// have renamed.
			"unknown values are dropped, not forwarded",
			AgentOptions{CodexSandbox: "bypassPermissions", CodexApproval: "acceptEdits"},
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
	})

	for _, want := range []string{
		"CODEX_HOME='" + home + "'",
		"MEDUSA_SESSION_NAME='medusa-ws-1'",
		"codex --sandbox read-only",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("codex command missing %q: %s", want, cmd)
		}
	}
	// Every Claude-only flag must stay out: codex rejects unknown flags and the
	// tab would drop straight to a shell.
	for _, unwanted := range []string{"CLAUDE", "--session-id", "--permission-mode", "--enable-auto-mode", "--settings"} {
		if strings.Contains(cmd, unwanted) {
			t.Errorf("codex command carries Claude-only %q: %s", unwanted, cmd)
		}
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
	})
	resumeAt := strings.Index(cmd, " resume ")
	sandboxAt := strings.Index(cmd, " --sandbox ")
	if resumeAt < 0 || sandboxAt < 0 {
		t.Fatalf("want both resume and sandbox in: %s", cmd)
	}
	if resumeAt > sandboxAt {
		t.Errorf("resume must come before the flags: %s", cmd)
	}
}

// A Claude tab must be untouched by any of this.
func TestBuildAgentCommandClaudeUnaffectedByCodexOptions(t *testing.T) {
	cmd := buildAgentCommand(AgentClaude, "claude", "medusa-ws-1", t.TempDir(), AgentOptions{
		CodexSandbox: CodexSandboxFullAccess,
		CodexSearch:  true,
	})
	for _, unwanted := range []string{"--sandbox danger-full-access", "--search", "CODEX_HOME"} {
		if strings.Contains(cmd, unwanted) {
			t.Errorf("claude command carries Codex-only %q: %s", unwanted, cmd)
		}
	}
}
