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
	// A default-mode tab must say 0 rather than leave the var out: Claude's /tui
	// command persists "tui" in the profile's settings.json, and that setting
	// wins when the env var is absent — so an omitted var launches fullscreen
	// for anyone whose profile has ever run /tui fullscreen.
	off := buildAgentCommand(AgentClaude, "claude", "s", "/cfg", AgentOptions{ClaudeSessionID: "id", Fullscreen: false})
	if !strings.Contains(off, "CLAUDE_CODE_NO_FLICKER=0") {
		t.Errorf("default-mode launch must set CLAUDE_CODE_NO_FLICKER=0 to override a persisted tui setting: %s", off)
	}
}

// TestBuildAgentCommandTerminalEnv guards the env that undoes Claude Code's
// tmux pessimism: without it Claude clamps every color to the 256-color cube
// under $TMUX, and prints ~~literal tildes~~ unless the tmux server happened to
// inherit LC_TERMINAL=iTerm2. It must be on every Claude launch, fresh or
// resumed, and on nothing else.
func TestBuildAgentCommandTerminalEnv(t *testing.T) {
	cfg := t.TempDir()
	writeConversation(t, cfg, "proj", "id")
	for name, cmd := range map[string]string{
		"fresh":   buildAgentCommand(AgentClaude, "claude", "s", "/cfg", AgentOptions{ClaudeSessionID: "id"}),
		"resume":  buildAgentCommand(AgentClaude, "claude", "s", cfg, AgentOptions{ClaudeSessionID: "id", Resume: true}),
		"default": buildAgentCommand(AgentClaude, "claude", "s", "", AgentOptions{}),
	} {
		for _, want := range []string{"CLAUDE_CODE_TMUX_TRUECOLOR=1", "CLAUDE_CODE_FORCE_STRIKETHROUGH=1"} {
			if !strings.Contains(cmd, want) {
				t.Errorf("%s claude launch must set %s: %s", name, want, cmd)
			}
		}
	}
	if other := buildAgentCommand(AgentType("viewer"), "run.sh", "s", "", AgentOptions{}); strings.Contains(other, "CLAUDE_CODE_TMUX_TRUECOLOR") {
		t.Errorf("non-claude launch must not carry Claude's terminal env: %s", other)
	}
}
