package pty

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
)

func prepareTestConfig(t *testing.T) *config.Config {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	home := t.TempDir()
	return &config.Config{
		Paths: &config.Paths{
			ProfilesRoot: filepath.Join(home, "profiles"),
			HooksDir:     filepath.Join(home, "hooks"),
		},
		Assistants: map[string]config.AssistantConfig{
			"claude": {Command: "claude"},
			"codex":  {Command: "codex"},
		},
	}
}

func TestPrepareAgentRequiresAProfile(t *testing.T) {
	cfg := prepareTestConfig(t)
	ws := &data.Workspace{Name: "ws"}
	if _, err := PrepareAgent(cfg, ws, AgentClaude, "s", AgentOptions{}); err == nil {
		t.Fatal("PrepareAgent started a Claude agent with no profile")
	}
}

func TestPrepareAgentBuildsTheFullClaudeLaunch(t *testing.T) {
	cfg := prepareTestConfig(t)
	root := t.TempDir()
	ws := data.NewCheckoutWorkspace("ws", "main", root)
	ws.Profile = "Work"

	p, err := PrepareAgent(cfg, ws, AgentClaude, "medusa-ws-1", AgentOptions{
		ClaudeSessionID:   "sess-1",
		Fullscreen:        true,
		InitialPromptFile: "/tmp/p",
	})
	if err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}
	profileDir := filepath.Join(cfg.Paths.ProfilesRoot, "Work")
	for _, want := range []string{
		"CLAUDE_CONFIG_DIR='" + profileDir + "'",
		"CLAUDE_CODE_NO_FLICKER=1",
		"--session-id 'sess-1'",
		`-- "$(cat '/tmp/p'`,
		"exec ", // drops to a shell when the agent exits
	} {
		if !strings.Contains(p.Command, want) {
			t.Errorf("command missing %q\n%s", want, p.Command)
		}
	}
	if !p.Fullscreen || p.SessionName != "medusa-ws-1" {
		t.Errorf("prepared = %+v, want fullscreen session medusa-ws-1", p)
	}
	if !containsEnv(p.Env, "WORKSPACE_ROOT="+root) {
		t.Errorf("env %v lacks WORKSPACE_ROOT", p.Env)
	}
}

func TestPrepareAgentNeverMarksCodexFullscreen(t *testing.T) {
	cfg := prepareTestConfig(t)
	ws := data.NewCheckoutWorkspace("ws", "main", t.TempDir())
	ws.Profile = "Work"
	p, err := PrepareAgent(cfg, ws, AgentCodex, "s", AgentOptions{Fullscreen: true})
	if err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}
	if p.Fullscreen {
		t.Fatal("a Codex tab was marked fullscreen")
	}
}

func containsEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
