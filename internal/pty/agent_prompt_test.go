package pty

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A prompt is free text from a script: it may start with a dash, contain
// quotes, or span lines, and must reach the agent as its first prompt rather
// than as a flag or as shell syntax.
const awkwardPrompt = "-x it's \"quoted\"\nsecond line $HOME `id`"

func promptFile(t *testing.T, prompt string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prompt")
	if err := os.WriteFile(path, []byte(prompt), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInitialPromptIsTheLastArgumentOfAFreshClaudeLaunch(t *testing.T) {
	got := buildAgentCommand(AgentClaude, "claude", "medusa-ws-1", "/cfg", AgentOptions{
		ClaudeSessionID:   "sess-1",
		InitialPromptFile: "/tmp/p",
	})
	if want := ` -- "$(cat '/tmp/p'; rm -f '/tmp/p')"`; !strings.HasSuffix(got, want) {
		t.Fatalf("command does not end with the prompt\nwant suffix: %s\ngot: %s", want, got)
	}
}

func TestInitialPromptIsNeverReplayedOnResume(t *testing.T) {
	cfg := t.TempDir()
	writeConversation(t, cfg, "/work", "sess-1")
	got := buildAgentCommand(AgentClaude, "claude", "medusa-ws-1", cfg, AgentOptions{
		ClaudeSessionID:   "sess-1",
		Resume:            true,
		Cwd:               "/work",
		InitialPromptFile: "/tmp/p",
	})
	if strings.Contains(got, "/tmp/p") || strings.Contains(got, " -- ") {
		t.Fatalf("a resumed Claude tab replayed its first prompt: %s", got)
	}
}

func TestInitialPromptOnCodex(t *testing.T) {
	fresh := buildAgentCommand(AgentCodex, "codex", "medusa-ws-1", t.TempDir(), AgentOptions{InitialPromptFile: "/tmp/p"})
	if !strings.HasSuffix(fresh, ` -- "$(cat '/tmp/p'; rm -f '/tmp/p')"`) {
		t.Fatalf("Codex command does not end with the prompt: %s", fresh)
	}
	resumed := buildAgentCommand(AgentCodex, "codex", "medusa-ws-1", t.TempDir(), AgentOptions{
		ClaudeSessionID: "abc", Resume: true, InitialPromptFile: "/tmp/p",
	})
	if strings.Contains(resumed, "/tmp/p") {
		t.Fatalf("a resumed Codex tab replayed its first prompt: %s", resumed)
	}
}

func TestNoPromptMeansNoSeparator(t *testing.T) {
	got := buildAgentCommand(AgentClaude, "claude", "medusa-ws-1", "/cfg", AgentOptions{ClaudeSessionID: "s"})
	if strings.Contains(got, " -- ") {
		t.Fatalf("command has a prompt separator with no prompt: %s", got)
	}
}

// The prompt must survive a real shell: run the command with a stand-in
// agent that prints its last argument. The file is gone afterwards, so the
// prompt does not linger on disk.
func TestInitialPromptSurvivesTheShellVerbatim(t *testing.T) {
	file := promptFile(t, awkwardPrompt)
	agent := `sh -c 'eval "last=\${$#}"; printf %s "$last"' agent`
	cmd := buildAgentCommand(AgentCodex, agent, "s", "", AgentOptions{InitialPromptFile: file})
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		t.Fatalf("running %s: %v", cmd, err)
	}
	if string(out) != awkwardPrompt {
		t.Fatalf("agent received %q, want %q", out, awkwardPrompt)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("prompt file left behind: %v", err)
	}
}
