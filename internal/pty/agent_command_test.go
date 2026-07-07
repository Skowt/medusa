package pty

import (
	"strings"
	"testing"
)

// claudeSessionArgs behavior (including the resume-without-conversation
// fallback) is covered directly in agent_resume_test.go. These tests cover
// how buildAgentCommand assembles the full command around it. The
// writeConversation helper lives in agent_resume_test.go.

func TestBuildAgentCommandClaudeNewSession(t *testing.T) {
	got := buildAgentCommand(AgentClaude, "claude", "medusa-ws-1", "/cfg/profile", AgentOptions{
		ClaudeSessionID: "sess-123",
		PermissionMode:  "auto",
	})
	for _, want := range []string{
		"CLAUDE_CONFIG_DIR='/cfg/profile'",
		"MEDUSA_SESSION_NAME='medusa-ws-1'",
		"claude",
		"--session-id 'sess-123'",
		"--permission-mode 'auto'",
		"--enable-auto-mode",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("command missing %q\ngot: %s", want, got)
		}
	}
	if strings.Contains(got, "--resume") {
		t.Errorf("new session must not use --resume: %s", got)
	}
	if strings.Contains(got, "--settings") {
		t.Errorf("non-isolated must not pass --settings: %s", got)
	}
}

func TestBuildAgentCommandClaudeResumeAndIsolated(t *testing.T) {
	cfg := t.TempDir()
	// claudeSessionArgs only emits --resume when a conversation file exists.
	writeConversation(t, cfg, "proj", "sess-9")
	got := buildAgentCommand(AgentClaude, "claude", "s", cfg, AgentOptions{
		ClaudeSessionID:          "sess-9",
		Resume:                   true,
		Isolated:                 true,
		AllowUnsandboxedCommands: true,
	})
	if !strings.Contains(got, "--resume 'sess-9'") {
		t.Errorf("expected --resume: %s", got)
	}
	if strings.Contains(got, "--session-id") {
		t.Errorf("resume must not use --session-id: %s", got)
	}
	if !strings.Contains(got, "--settings ") {
		t.Errorf("isolated must pass --settings: %s", got)
	}
}

func TestBuildAgentCommandNonClaudeHasNoClaudeFlags(t *testing.T) {
	got := buildAgentCommand(AgentType("viewer"), "run.sh", "s", "", AgentOptions{
		ClaudeSessionID: "x",
		PermissionMode:  "auto",
		Fullscreen:      true,
	})
	for _, unwanted := range []string{"--session-id", "--resume", "--permission-mode", "--enable-auto-mode", "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_NO_FLICKER"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("non-claude must not contain %q: %s", unwanted, got)
		}
	}
}

func TestBuildAgentCommandFullscreenEnvVar(t *testing.T) {
	on := buildAgentCommand(AgentClaude, "claude", "s", "/cfg", AgentOptions{ClaudeSessionID: "id", Fullscreen: true})
	if !strings.Contains(on, "CLAUDE_CODE_NO_FLICKER=1") {
		t.Errorf("fullscreen launch must set CLAUDE_CODE_NO_FLICKER=1: %s", on)
	}
	cfg := t.TempDir()
	writeConversation(t, cfg, "proj", "id")
	resume := buildAgentCommand(AgentClaude, "claude", "s", cfg, AgentOptions{ClaudeSessionID: "id", Resume: true, Fullscreen: true})
	if !strings.Contains(resume, "CLAUDE_CODE_NO_FLICKER=1") || !strings.Contains(resume, "--resume 'id'") {
		t.Errorf("fullscreen must apply on resume too: %s", resume)
	}
	off := buildAgentCommand(AgentClaude, "claude", "s", "/cfg", AgentOptions{ClaudeSessionID: "id", Fullscreen: false})
	if strings.Contains(off, "CLAUDE_CODE_NO_FLICKER") {
		t.Errorf("non-fullscreen launch must not set the env var: %s", off)
	}
}
