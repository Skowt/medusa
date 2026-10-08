package pty

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/shellutil"
	"github.com/Skowt/medusa/internal/tmux"
)

// AgentOptions holds optional flags for agent creation.
type AgentOptions struct {
	ClaudeSessionID          string // UUID to pass as --session-id or --resume
	Resume                   bool   // If true, use --resume instead of --session-id
	Isolated                 bool   // Enable Claude's built-in sandbox via --settings
	AllowUnsandboxedCommands bool   // sandbox.allowUnsandboxedCommands; only used when Isolated is true
	PermissionMode           string // claude --permission-mode value (acceptEdits, plan, auto, bypassPermissions)
	// Fullscreen selects Claude's fullscreen TUI renderer via
	// CLAUDE_CODE_NO_FLICKER (1 on, 0 off) and marks the tmux session
	// accordingly. It is always passed explicitly: see buildAgentCommand.
	Fullscreen bool
	// Cwd is the directory the agent runs in (the tmux pane's start
	// directory). claudeSessionArgs needs it to tell whether the worktree has
	// any conversation to continue when the recorded session id has none;
	// CreateAgentWithTags fills it in.
	Cwd string
	// CodexSandbox is codex's --sandbox policy. CodexAuto enables automatic
	// approval review. Web search is always enabled for Codex launches.
	CodexSandbox string
	CodexAuto    bool
	// InitialPromptFile names a file holding the agent's first prompt. The
	// command reads it into the positional argument after "--" (so text
	// starting with a dash is not read as a flag) and deletes it. The prompt
	// travels by file because tmux refuses a command over ~16 KB, and the
	// agent command is part of a tmux new-session command line. It applies to
	// a fresh launch only and is never written to TabInfo, so a restore,
	// reattach or restart cannot replay it. Command substitution drops
	// trailing newlines, which a prompt does not miss.
	InitialPromptFile string
}

// buildAgentCommand assembles the env-prefixed shell command for an agent.
// It is pure: all filesystem side effects (profile dir setup, settings/hook
// injection) are performed by the caller, which passes the resolved profileDir.
func buildAgentCommand(agentType AgentType, command, sessionName, profileDir string, opts AgentOptions) string {
	if agentType == AgentCodex {
		// profileDir is the profile's CODEX_HOME for a Codex tab: it holds the
		// same role CLAUDE_CONFIG_DIR does, and every Codex flag differs from
		// Claude's, so the two command lines share nothing below.
		return buildCodexCommand(command, sessionName, profileDir, opts)
	}
	cmd := fmt.Sprintf("MEDUSA_SESSION_NAME=%s %s", shellutil.Quote(sessionName), command)
	if agentType == AgentClaude && profileDir != "" {
		cmd = fmt.Sprintf("CLAUDE_CONFIG_DIR=%s %s", shellutil.Quote(profileDir), cmd)
	}
	// claudeSessionArgs handles --session-id vs --resume, falling back to a
	// fresh --session-id when a resume has no conversation file yet.
	if agentType == AgentClaude {
		cmd += claudeSessionArgs(profileDir, opts)
	}
	if agentType == AgentClaude && opts.PermissionMode != "" {
		cmd += " --permission-mode " + shellutil.Quote(opts.PermissionMode)
	}
	if agentType == AgentClaude {
		cmd += " --enable-auto-mode"
	}
	if opts.Isolated && agentType == AgentClaude {
		cmd += " --settings " + shellutil.Quote(config.ClaudeSandboxSettingsJSON(opts.AllowUnsandboxedCommands))
	}
	if agentType == AgentClaude {
		// State the renderer explicitly in both directions. Claude's /tui command
		// persists the user's choice as "tui" in the profile's settings.json, and
		// that setting wins whenever the env var is absent — so merely omitting
		// the var for a default-mode tab would still launch fullscreen once any
		// session in that profile had ever run /tui fullscreen.
		cmd = "CLAUDE_CODE_NO_FLICKER=" + noFlickerValue(opts.Fullscreen) + " " + cmd
	}
	return cmd + initialPromptArg(opts)
}

// initialPromptArg returns the trailing "-- <prompt>" for a fresh launch, and
// nothing on a resume: a resumed conversation already had its first prompt.
func initialPromptArg(opts AgentOptions) string {
	if opts.InitialPromptFile == "" || opts.Resume {
		return ""
	}
	file := shellutil.Quote(opts.InitialPromptFile)
	return ` -- "$(cat ` + file + `; rm -f ` + file + `)"`
}

func noFlickerValue(fullscreen bool) string {
	if fullscreen {
		return "1"
	}
	return "0"
}

// GenerateSessionID returns a new random UUID v4 string.
func GenerateSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}

// agentCloseGrace is how long an agent's terminal gets to exit on SIGTERM
// before it is killed. Every agent terminal's process group is just a tmux
// client (see tmux.ClientCommandWithTags) — the agent itself runs under the
// tmux server and outlives this kill — so there is nothing to shut down
// gracefully, and the default grace only stalls the caller.
const agentCloseGrace = 20 * time.Millisecond

// AgentType represents the type of AI agent
type AgentType string

const (
	AgentClaude AgentType = "claude"
	AgentCodex  AgentType = "codex"
)

// Agent represents a running AI agent instance
type Agent struct {
	Type      AgentType
	Terminal  *Terminal
	Workspace *data.Workspace
	Config    config.AssistantConfig
	Session   string
}

// AgentManager manages agent instances
type AgentManager struct {
	config *config.Config
	mu     sync.Mutex
	agents map[data.WorkspaceID][]*Agent
}

// NewAgentManager creates a new agent manager
func NewAgentManager(cfg *config.Config) *AgentManager {
	return &AgentManager{
		config: cfg,
		agents: make(map[data.WorkspaceID][]*Agent),
	}
}

// CreateAgentWithTags creates a new agent for the given workspace with tmux tags.
func (m *AgentManager) CreateAgentWithTags(ws *data.Workspace, agentType AgentType, sessionName string, rows, cols uint16, tags tmux.SessionTags, opts AgentOptions) (*Agent, error) {
	p, err := PrepareAgent(m.config, ws, agentType, sessionName, opts)
	if err != nil {
		return nil, err
	}
	// Fullscreen mark and the CLAUDE_CODE_NO_FLICKER env var must agree: both
	// are gated on agentType == AgentClaude, matching buildAgentCommand's gate.
	tags.Fullscreen = p.Fullscreen
	termCommand := tmux.ClientCommandWithTags(p.SessionName, ws.Root(), p.Command, tmux.DefaultOptions(), tags)
	term, err := NewWithSize(termCommand, ws.Root(), p.Env, rows, cols)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}
	agent := &Agent{
		Type:      agentType,
		Terminal:  term,
		Workspace: ws,
		Config:    p.Config,
		Session:   p.SessionName,
	}

	m.mu.Lock()
	m.agents[ws.ID()] = append(m.agents[ws.ID()], agent)
	m.mu.Unlock()

	return agent, nil
}

// CreateViewerWithTags creates a new viewer for the given workspace with tmux tags.
func (m *AgentManager) CreateViewerWithTags(ws *data.Workspace, command string, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*Agent, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is required")
	}
	if sessionName == "" {
		sessionName, _ = tmux.NextUniqueSessionName(ws.Name, tmux.DefaultOptions())
	}
	if err := tmux.EnsureAvailable(); err != nil {
		return nil, err
	}
	// Build environment
	env := []string{
		fmt.Sprintf("WORKSPACE_ROOT=%s", ws.Root()),
		fmt.Sprintf("WORKSPACE_NAME=%s", ws.Name),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	}

	termCommand := tmux.ClientCommandWithTags(sessionName, ws.Root(), command, tmux.DefaultOptions(), tags)
	logging.Info("CreateViewerWithTags: termCommand=%s", termCommand)
	term, err := NewWithSize(termCommand, ws.Root(), env, rows, cols)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	agent := &Agent{
		Type:      AgentType("viewer"),
		Terminal:  term,
		Workspace: ws,
		Config:    config.AssistantConfig{}, // No specific config
		Session:   sessionName,
	}

	m.mu.Lock()
	m.agents[ws.ID()] = append(m.agents[ws.ID()], agent)
	m.mu.Unlock()

	return agent, nil
}

// CloseAgent closes an agent
func (m *AgentManager) CloseAgent(agent *Agent) error {
	if agent.Terminal != nil {
		_ = agent.Terminal.CloseWithGrace(agentCloseGrace)
	}

	// Remove from list
	if agent.Workspace != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		agents := m.agents[agent.Workspace.ID()]
		for i, a := range agents {
			if a == agent {
				m.agents[agent.Workspace.ID()] = append(agents[:i], agents[i+1:]...)
				break
			}
		}
	}

	return nil
}

// CloseAll closes all agents
func (m *AgentManager) CloseAll() {
	m.mu.Lock()
	agentsByWorkspace := m.agents
	m.agents = make(map[data.WorkspaceID][]*Agent)
	m.mu.Unlock()

	for _, agents := range agentsByWorkspace {
		for _, agent := range agents {
			if agent.Terminal != nil {
				_ = agent.Terminal.CloseWithGrace(agentCloseGrace)
			}
		}
	}
}

// RenameAgentSessions rewrites the tmux session names recorded for a
// workspace's agents, given the old-to-new mapping the rename performed.
func (m *AgentManager) RenameAgentSessions(wsID data.WorkspaceID, renamed map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, agent := range m.agents[wsID] {
		if newName, ok := renamed[agent.Session]; ok {
			agent.Session = newName
		}
	}
}

// CloseWorkspaceAgents closes and removes all agents for a specific workspace
func (m *AgentManager) CloseWorkspaceAgents(ws *data.Workspace) {
	if ws == nil {
		return
	}
	wsID := ws.ID()
	m.mu.Lock()
	agents := m.agents[wsID]
	delete(m.agents, wsID)
	m.mu.Unlock()
	for _, agent := range agents {
		if agent.Terminal != nil {
			_ = agent.Terminal.CloseWithGrace(agentCloseGrace)
		}
	}
}

// SendInterrupt sends an interrupt to an agent
func (m *AgentManager) SendInterrupt(agent *Agent) error {
	if agent.Terminal == nil {
		return nil
	}

	// Send multiple interrupts if configured (e.g., for Claude)
	for i := 0; i < agent.Config.InterruptCount; i++ {
		if err := agent.Terminal.SendInterrupt(); err != nil {
			return err
		}
		// Add delay between interrupts if configured
		if i < agent.Config.InterruptCount-1 && agent.Config.InterruptDelayMs > 0 {
			time.Sleep(time.Duration(agent.Config.InterruptDelayMs) * time.Millisecond)
		}
	}

	return nil
}
