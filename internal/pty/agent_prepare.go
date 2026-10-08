package pty

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/tmux"
)

// PreparedAgent is everything needed to run an agent in a tmux session, with
// the filesystem side effects already done. The TUI runs Command inside a tmux
// client it attaches to; the headless CLI starts it in a detached session.
type PreparedAgent struct {
	SessionName string
	Command     string   // agent command, then a reset and a login shell
	Env         []string // environment for the process that starts the session
	Fullscreen  bool     // Claude with Fullscreen set; never Codex
	Config      config.AssistantConfig
}

// PrepareAgent sets up a profile for an agent launch — the profile directory,
// the pre-trusted worktree, a Codex tab's CODEX_HOME — and builds the command
// line. It is the part of starting an agent that does not need a terminal.
func PrepareAgent(cfg *config.Config, ws *data.Workspace, agentType AgentType, sessionName string, opts AgentOptions) (PreparedAgent, error) {
	if isAgentAssistant(agentType) && ws.Profile == "" {
		return PreparedAgent{}, fmt.Errorf("cannot start %s agent without a profile (workspace %q)", agentType, ws.Name)
	}
	assistantCfg, ok := cfg.Assistants[string(agentType)]
	if !ok {
		return PreparedAgent{}, fmt.Errorf("unknown agent type: %s", agentType)
	}
	if sessionName == "" {
		sessionName, _ = tmux.NextUniqueSessionName(ws.Name, tmux.DefaultOptions())
	}
	if err := tmux.EnsureAvailable(); err != nil {
		return PreparedAgent{}, err
	}

	// Build environment
	env := []string{
		fmt.Sprintf("WORKSPACE_ROOT=%s", ws.Root()),
		fmt.Sprintf("WORKSPACE_NAME=%s", ws.Name),
		"LINES=",   // Unset to force ioctl usage
		"COLUMNS=", // Unset to force ioctl usage
		"COLORTERM=truecolor",
	}

	var profileDir string
	if agentType == AgentClaude && ws.Profile != "" {
		profileDir = filepath.Join(cfg.Paths.ProfilesRoot, ws.Profile)
		_ = os.MkdirAll(profileDir, 0755)
	}

	// A Codex tab's profileDir is its CODEX_HOME, which also carries the
	// worktree trust entry and the activity hooks Codex reads.
	if agentType == AgentCodex && ws.Profile != "" {
		profileDir = config.CodexHomeDir(cfg.Paths.ProfilesRoot, ws.Profile)
		prepareCodexHome(profileDir, ws.Root(), cfg.Paths.HooksDir, config.ResolveHookEmitBinary())
	}

	// Pre-trust the workspace directory so Claude doesn't prompt
	// Use profile config dir if set, otherwise default ~/.claude.json
	if agentType == AgentClaude {
		_ = config.InjectTrustedDirectory(ws.Root(), profileDir)
	}

	// The pane starts in ws.Root(), which is the cwd Claude Code encodes into
	// its transcript directory.
	opts.Cwd = ws.Root()
	agentCommand := buildAgentCommand(agentType, assistantCfg.Command, sessionName, profileDir, opts)

	// Create terminal with agent command, falling back to shell on exit
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	// Execute agent, then reset terminal state and drop to shell
	// Reset sequence: stty sane (terminal modes), exit alt screen, show cursor, reset attrs, RIS
	// Use -l flag to start login shell so .zshrc/.bashrc are loaded
	fullCommand := fmt.Sprintf("%s; stty sane; printf '\\033[?1049l\\033[?25h\\033[0m\\033c'; echo 'Agent exited. Dropping to shell...'; export TERM=xterm-256color; exec %s -l", agentCommand, shell)

	return PreparedAgent{
		SessionName: sessionName,
		Command:     fullCommand,
		Env:         env,
		Fullscreen:  agentType == AgentClaude && opts.Fullscreen,
		Config:      assistantCfg,
	}, nil
}
