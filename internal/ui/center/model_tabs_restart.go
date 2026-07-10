package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/messages"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/tmux"
)

// RestartActiveTab restarts the active tab. Agent tabs keep their Claude
// session ID via `claude --resume`; script tabs re-run their saved command.
func (m *Model) RestartActiveTab() tea.Cmd {
	return m.restartTab(m.getActiveTabIdx())
}

// RestartTabAtIndex restarts a specific tab by index. Used by the close-tab
// dialog when launched from a tab-bar click, which may target a tab other
// than the active one.
func (m *Model) RestartTabAtIndex(index int) tea.Cmd {
	return m.restartTab(index)
}

// restartTab tears down the tmux session (and, for agents, the Agent
// process) for a tab and spawns a fresh one. Agent tabs resume via
// ClaudeSessionID so the conversation continues; script tabs re-run the
// same shell command stored on the tab. Diff tabs and tabs whose assistant
// isn't in the config (aside from script) are rejected.
func (m *Model) restartTab(index int) tea.Cmd {
	tabs := m.getTabs()
	if index < 0 || index >= len(tabs) {
		return nil
	}
	tab := tabs[index]
	if tab == nil || tab.Workspace == nil {
		return nil
	}
	if tab.DiffViewer != nil {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Diff tabs cannot be restarted",
				Level:   messages.ToastInfo,
			}
		}
	}
	isScript := tab.Assistant == "script"
	if !isScript {
		if m.config == nil || m.config.Assistants == nil {
			return nil
		}
		if _, ok := m.config.Assistants[tab.Assistant]; !ok {
			return nil
		}
	}

	tab.mu.Lock()
	sessionName := tab.SessionName
	if sessionName == "" && tab.Agent != nil {
		sessionName = tab.Agent.Session
	}
	claudeSessionID := tab.ClaudeSessionID
	tabIsolated := tab.Isolated
	tabAllowUnsandboxed := tab.AllowUnsandboxedCommands
	tabPermissionMode := tab.PermissionMode
	tabFullscreen := tab.Fullscreen
	scriptFullCmd := tab.ScriptFullCmd
	tab.mu.Unlock()

	if isScript && scriptFullCmd == "" {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Cannot restart script tab: command unknown (restored from previous session)",
				Level:   messages.ToastWarning,
			}
		}
	}

	ws := tab.Workspace
	tabID := tab.ID
	if sessionName == "" {
		sessionName = tmux.SessionName("medusa", ws.Name, "1")
	}

	// Tear down the existing agent (if any) before spawning a new one.
	m.stopPTYReader(tab)
	tab.mu.Lock()
	existingAgent := tab.Agent
	tab.Agent = nil
	tab.Running = false
	tab.autoRestartAttempt = 0
	tab.mu.Unlock()
	if existingAgent != nil {
		_ = m.agentManager.CloseAgent(existingAgent)
	}
	tmuxOpts := m.getTmuxOptions()

	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	assistant := tab.Assistant
	fullscreen := tabFullscreen && appPty.AgentType(assistant) == appPty.AgentClaude

	return func() tea.Msg {
		_ = tmux.KillSession(sessionName, tmuxOpts)

		tags := tmux.SessionTags{
			WorkspaceID: string(ws.ID()),
			TabID:       string(tabID),
			CreatedAt:   time.Now().Unix(),
		}
		var agent *appPty.Agent
		var err error
		if isScript {
			tags.Type = "script"
			tags.Assistant = "script"
			agent, err = m.agentManager.CreateViewerWithTags(ws, scriptFullCmd, sessionName, uint16(termHeight), uint16(termWidth), tags)
		} else {
			tags.Type = "agent"
			tags.Assistant = assistant
			// Build agent options: resume the Claude conversation if we have
			// a session ID, and use the tab's per-tab settings.
			agentOpts := appPty.AgentOptions{
				Isolated:                 tabIsolated,
				AllowUnsandboxedCommands: tabAllowUnsandboxed,
				PermissionMode:           tabPermissionMode,
				Fullscreen:               fullscreen,
			}
			if claudeSessionID != "" {
				agentOpts.ClaudeSessionID = claudeSessionID
				agentOpts.Resume = true
			}
			agent, err = m.agentManager.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags, agentOpts)
		}
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Stopped:     true,
				Action:      "restart",
			}
		}
		// Best-effort capture of scrollback (empty for fresh sessions, which is fine).
		scrollback, _ := tmux.CapturePane(sessionName, tmuxOpts)
		return ptyTabReattachResult{
			WorkspaceID:       string(ws.ID()),
			TabID:             tabID,
			Agent:             agent,
			Rows:              termHeight,
			Cols:              termWidth,
			ScrollbackCapture: scrollback,
			ClaudeSessionID:   claudeSessionID,
			Fullscreen:        fullscreen,
		}
	}
}
