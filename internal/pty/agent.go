package pty

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
}

// buildAgentCommand assembles the env-prefixed shell command for an agent.
// It is pure: all filesystem side effects (profile dir setup, settings/hook
// injection) are performed by the caller, which passes the resolved profileDir.
func buildAgentCommand(agentType AgentType, command, sessionName, profileDir string, opts AgentOptions) string {
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
	return cmd
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
	if agentType == AgentClaude && ws.Profile == "" {
		return nil, fmt.Errorf("cannot start Claude agent without a profile (workspace %q)", ws.Name)
	}
	assistantCfg, ok := m.config.Assistants[string(agentType)]
	if !ok {
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
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
		"LINES=",   // Unset to force ioctl usage
		"COLUMNS=", // Unset to force ioctl usage
		"COLORTERM=truecolor",
	}

	var profileDir string
	if agentType == AgentClaude && ws.Profile != "" {
		profileDir = filepath.Join(m.config.Paths.ProfilesRoot, ws.Profile)
		_ = os.MkdirAll(profileDir, 0755)
		if m.config.UI.SyncProfilePlugins {
			_ = config.SyncProfileSharedDirs(m.config.Paths.ProfilesRoot, ws.Profile)
		}
		// Inject global permissions into the profile if enabled
		if m.config.UI.GlobalPermissions {
			global, err := config.LoadGlobalPermissions(m.config.Paths.GlobalPermissionsPath)
			if err == nil && (len(global.Allow) > 0 || len(global.Deny) > 0) {
				_ = config.InjectGlobalPermissions(profileDir, global)
			}
		}
		// Strip any leftover Edit(**) from a previous agent run so users
		// aren't silently still operating with the (now-removed) "allow edits"
		// pre-grant. See config.StripAllowEdits.
		_ = config.StripAllowEdits(ws.Root())
		// Inject compound command approval hook if enabled
		if m.config.UI.CompoundApprove {
			if exe, err := os.Executable(); err == nil {
				hookBin := filepath.Join(filepath.Dir(exe), "medusa-approve-compound")
				if _, err := os.Stat(hookBin); err == nil {
					_ = config.InjectCompoundApproveHook(profileDir, hookBin)
				}
			}
		}
	}

	// Pre-trust the workspace directory so Claude doesn't prompt
	// Use profile config dir if set, otherwise default ~/.claude.json
	if agentType == AgentClaude {
		_ = config.InjectTrustedDirectory(ws.Root(), profileDir)
	}

	// bypassPermissions still triggers Claude's confirmation dialog the first
	// time it's used; suppress it for users who explicitly chose it from our
	// launcher dropdown.
	if agentType == AgentClaude && opts.PermissionMode == "bypassPermissions" {
		_ = config.InjectSkipPermissionPrompt(profileDir)
	}

	// The pane starts in ws.Root() (see ClientCommandWithTags below), which is
	// the cwd Claude Code encodes into its transcript directory.
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

	// Fullscreen mark and the CLAUDE_CODE_NO_FLICKER env var must agree: both
	// are gated on agentType == AgentClaude, matching buildAgentCommand's gate.
	tags.Fullscreen = agentType == AgentClaude && opts.Fullscreen
	termCommand := tmux.ClientCommandWithTags(sessionName, ws.Root(), fullCommand, tmux.DefaultOptions(), tags)
	term, err := NewWithSize(termCommand, ws.Root(), env, rows, cols)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	agent := &Agent{
		Type:      agentType,
		Terminal:  term,
		Workspace: ws,
		Config:    assistantCfg,
		Session:   sessionName,
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

// MigrateWorkspaceAgents moves agent state from oldID to newID after a workspace rename.
// It updates the workspace pointer and tmux session names on each agent without closing terminals.
// oldName/newName are workspace display names used to compute tmux session name prefixes.
func (m *AgentManager) MigrateWorkspaceAgents(oldID, newID data.WorkspaceID, ws *data.Workspace, oldName, newName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldPrefix := tmux.SessionName("medusa", oldName) + "-"
	newPrefix := tmux.SessionName("medusa", newName) + "-"
	if agents, ok := m.agents[oldID]; ok {
		for _, agent := range agents {
			agent.Workspace = ws
			if strings.HasPrefix(agent.Session, oldPrefix) {
				agent.Session = newPrefix + strings.TrimPrefix(agent.Session, oldPrefix)
			}
		}
		m.agents[newID] = agents
		delete(m.agents, oldID)
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
