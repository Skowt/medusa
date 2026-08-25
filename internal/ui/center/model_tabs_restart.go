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
	tabOptions := agentTabOptionsFromTab(tab)
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
	wsID := string(ws.ID())
	tabID := tab.ID
	if sessionName == "" {
		sessionName = tmux.SessionName("medusa", ws.Name, "1")
	}

	// Detach the existing agent (if any) before spawning a new one. Cancelling
	// the reader is cheap, but closing the agent kills a process group and
	// waits on it, so it belongs in the command below rather than here on the
	// UI thread.
	m.stopPTYReader(tab)
	tab.mu.Lock()
	existingAgent := tab.Agent
	tab.Agent = nil
	tab.Running = false
	tab.autoRestartAttempt = 0
	tab.mu.Unlock()
	tmuxOpts := m.getTmuxOptions()

	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	assistant := tab.Assistant
	// A restart relaunches the tab as it was started, minus anything that
	// belongs to a different assistant.
	tabOptions = tabOptions.forAssistant(assistant)
	fullscreen := tabOptions.Fullscreen

	// A restart does not carry on whatever the agent was doing, so the
	// activity spinner has to go: Claude Code's Stop hook never fires for the
	// killed turn, and nothing else would clear it.
	clearActivity := func() tea.Msg {
		return messages.AgentInterrupted{WorkspaceID: wsID, SessionName: sessionName}
	}

	restart := func() tea.Msg {
		// Kill the session first: that ends the agent and makes the old tmux
		// client exit on its own, so closing its terminal rarely has to wait
		// out even the short grace period.
		_ = tmux.KillSession(sessionName, tmuxOpts)
		if existingAgent != nil {
			_ = m.agentManager.CloseAgent(existingAgent)
		}

		tags := tmux.SessionTags{
			WorkspaceID: wsID,
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
			// Build agent options: resume the conversation if we have a
			// session ID, and use the tab's per-tab settings.
			agentOpts := tabOptions.agentOptions()
			if claudeSessionID != "" {
				agentOpts.ClaudeSessionID = claudeSessionID
				agentOpts.Resume = true
			}
			agent, err = m.agentManager.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags, agentOpts)
		}
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: wsID,
				TabID:       tabID,
				Err:         err,
				Stopped:     true,
				Action:      "restart",
			}
		}
		// No scrollback capture: the session above was just created, so its
		// history is empty by construction and the capture is a wasted
		// round-trip to tmux on a path the user is waiting on.
		return ptyTabReattachResult{
			WorkspaceID:     wsID,
			TabID:           tabID,
			Agent:           agent,
			Rows:            termHeight,
			Cols:            termWidth,
			ClaudeSessionID: claudeSessionID,
			Fullscreen:      fullscreen,
		}
	}

	return tea.Batch(clearActivity, restart)
}
