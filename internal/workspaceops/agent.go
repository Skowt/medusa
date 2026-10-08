package workspaceops

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/tmux"
)

// StartedAgent describes an agent started without the TUI.
type StartedAgent struct {
	Assistant   string
	SessionName string
	SessionID   string // Claude's pre-assigned session id; empty for Codex
	Tab         data.TabInfo
}

// StartAgent starts an agent for ws in a detached tmux session and records it
// in ws.OpenTabs, which is how a running or later-started TUI adopts it: on
// its next load it attaches to the session as an ordinary tab.
//
// prompt is passed to the agent as its first prompt and deliberately not
// stored, so no restore path can send it again.
func (d Deps) StartAgent(ws *data.Workspace, l Launch, prompt string) (StartedAgent, error) {
	promptHandedOver := false
	agentType := pty.AgentType(l.Assistant)
	if agentType == pty.AgentClaude && ws.Profile != "" {
		// The TUI injects the activity hooks into every profile at startup.
		// A headless launch may come first, and without the hooks the tab
		// would show no busy/ready state once the TUI adopts it.
		profileDir := filepath.Join(d.Config.Paths.ProfilesRoot, ws.Profile)
		if err := config.InjectHooks(profileDir, d.Config.Paths.HooksDir, config.ResolveHookEmitBinary()); err != nil {
			logging.Warn("Could not inject hooks into %s: %v", profileDir, err)
		}
	}

	opts := pty.AgentOptions{}
	if prompt != "" {
		file, err := writePromptFile(prompt)
		if err != nil {
			return StartedAgent{}, err
		}
		// The agent command deletes the file once it has read it; this only
		// covers a launch that never got that far.
		defer func() {
			if !promptHandedOver {
				_ = os.Remove(file)
			}
		}()
		opts.InitialPromptFile = file
	}
	switch agentType {
	case pty.AgentClaude:
		opts.ClaudeSessionID = pty.GenerateSessionID()
		opts.Isolated = l.Isolated
		opts.AllowUnsandboxedCommands = l.AllowUnsandboxedCommands
		opts.PermissionMode = l.PermissionMode
		opts.Fullscreen = l.Fullscreen
	case pty.AgentCodex:
		// Codex mints its own session id and reports it on SessionStart.
		opts.CodexSandbox = l.CodexSandbox
		opts.CodexAuto = l.CodexAuto
	}

	tmuxOpts := tmux.DefaultOptions()
	sessionName, err := tmux.NextUniqueSessionName(ws.Name, tmuxOpts)
	if err != nil {
		return StartedAgent{}, err
	}
	prepared, err := pty.PrepareAgent(d.Config, ws, agentType, sessionName, opts)
	if err != nil {
		return StartedAgent{}, err
	}
	tags := tmux.SessionTags{
		WorkspaceID: string(ws.ID()),
		TabID:       newTabID(),
		Type:        "agent",
		Assistant:   l.Assistant,
		CreatedAt:   time.Now().Unix(),
		Fullscreen:  prepared.Fullscreen,
	}
	if err := tmux.StartDetachedSession(prepared.SessionName, ws.Root(), prepared.Command, prepared.Env, tmuxOpts, tags); err != nil {
		return StartedAgent{}, err
	}
	promptHandedOver = true

	tab := data.TabInfo{
		Assistant:                l.Assistant,
		Name:                     l.Assistant,
		SessionName:              prepared.SessionName,
		Status:                   "running",
		ClaudeSessionID:          opts.ClaudeSessionID,
		Isolated:                 opts.Isolated,
		AllowUnsandboxedCommands: opts.AllowUnsandboxedCommands,
		PermissionMode:           opts.PermissionMode,
		Fullscreen:               prepared.Fullscreen,
		CodexSandbox:             opts.CodexSandbox,
		CodexAuto:                opts.CodexAuto,
	}
	ws.OpenTabs = append(ws.OpenTabs, tab)
	if err := d.Store.Save(ws); err != nil {
		ws.OpenTabs = ws.OpenTabs[:len(ws.OpenTabs)-1]
		_ = tmux.KillSession(prepared.SessionName, tmuxOpts)
		return StartedAgent{}, fmt.Errorf("recording the agent tab: %w", err)
	}
	return StartedAgent{
		Assistant:   l.Assistant,
		SessionName: prepared.SessionName,
		SessionID:   opts.ClaudeSessionID,
		Tab:         tab,
	}, nil
}

// writePromptFile stores a first prompt where only this user can read it. It
// travels by file because tmux refuses a command line over ~16 KB.
func writePromptFile(prompt string) (string, error) {
	f, err := os.CreateTemp("", "medusa-prompt-*")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(prompt); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func newTabID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "cli-" + hex.EncodeToString(b[:])
}
