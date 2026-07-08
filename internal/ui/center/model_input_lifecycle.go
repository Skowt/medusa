package center

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/vterm"
)

// updateLaunchAgent handles messages.LaunchAgent.
func (m *Model) updateLaunchAgent(msg messages.LaunchAgent) (*Model, tea.Cmd) {
	return m, m.createAgentTab(msg.Assistant, msg.Workspace, msg.Isolated, msg.AllowUnsandboxedCommands, msg.PermissionMode)
}

// updateLaunchScript handles messages.LaunchScript.
func (m *Model) updateLaunchScript(msg messages.LaunchScript) (*Model, tea.Cmd) {
	return m, m.createScriptTab(msg.Command, msg.DisplayName, msg.Env, msg.Workspace)
}

// updateOpenFileInVim handles messages.OpenFileInVim.
func (m *Model) updateOpenFileInVim(msg messages.OpenFileInVim) (*Model, tea.Cmd) {
	return m, m.createVimTab(msg.Path, msg.Workspace)
}

// updatePtyTabCreateResult handles ptyTabCreateResult.
func (m *Model) updatePtyTabCreateResult(msg ptyTabCreateResult) (*Model, tea.Cmd) {
	return m, m.handlePtyTabCreated(msg)
}

// updatePtyTabReattachResult handles ptyTabReattachResult.
func (m *Model) updatePtyTabReattachResult(msg ptyTabReattachResult) (*Model, tea.Cmd) {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil || msg.Agent == nil {
		return m, nil
	}
	rows := msg.Rows
	cols := msg.Cols
	if rows <= 0 || cols <= 0 {
		tm := m.terminalMetrics()
		rows = tm.Height
		cols = tm.Width
	}
	tab.mu.Lock()
	createdTerminal := false
	if tab.Terminal == nil {
		tab.Terminal = vterm.New(cols, rows)
		createdTerminal = true
	}
	if tab.Terminal != nil {
		// Claude fullscreen agents own their alt-screen scrollback; medusa
		// keeps none for them so PgUp/drag never scrolls a vterm the app
		// can't see.
		tab.Terminal.AllowAltScreenScrollback = !msg.Fullscreen
		if createdTerminal {
			tab.Terminal.PrependScrollback(msg.ScrollbackCapture)
		}
	}
	tab.Agent = msg.Agent
	tab.SessionName = msg.Agent.Session
	if msg.ClaudeSessionID != "" {
		tab.ClaudeSessionID = msg.ClaudeSessionID
	}
	tab.Detached = false
	tab.Running = true
	tab.Fullscreen = msg.Fullscreen
	tab.monitorDirty = true
	tab.autoRestartAttempt = 0
	tab.WorkspaceRenamed = false
	tab.mu.Unlock()

	if tab.Terminal != nil && msg.Agent.Terminal != nil {
		agentTerm := msg.Agent.Terminal
		workspaceID := msg.WorkspaceID
		tabID := tab.ID
		tab.Terminal.SetResponseWriter(func(data []byte) {
			if len(data) == 0 || agentTerm == nil {
				return
			}
			if err := agentTerm.SendString(string(data)); err != nil {
				logging.Warn("Response write failed for tab %s: %v", tabID, err)
				if m.msgSink != nil {
					m.msgSink(TabInputFailed{TabID: tabID, WorkspaceID: workspaceID, Err: err})
				}
			}
		})
	}

	m.resizePTY(tab, rows, cols)

	cmd := m.startPTYReader(msg.WorkspaceID, tab)
	return m, common.SafeBatch(cmd, func() tea.Msg {
		return messages.TabReattached{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
	})
}

// updatePtyTabReattachFailed handles ptyTabReattachFailed.
func (m *Model) updatePtyTabReattachFailed(msg ptyTabReattachFailed) (*Model, tea.Cmd) {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return m, nil
	}
	tab.mu.Lock()
	tab.Running = false
	if msg.Stopped {
		tab.Detached = false
	}
	tab.mu.Unlock()
	logging.Warn("Reattach failed for tab %s: %v", msg.TabID, msg.Err)
	action := msg.Action
	if action == "" {
		action = "reattach"
	}
	label := "Reattach"
	switch action {
	case "restart":
		label = "Restart"
	case "reattach":
		label = "Reattach"
	}
	return m, common.SafeBatch(func() tea.Msg {
		return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
	}, func() tea.Msg {
		return messages.Toast{
			Message: fmt.Sprintf("%s failed: %v", label, msg.Err),
			Level:   messages.ToastWarning,
		}
	})
}

// updateTabSessionStatus handles messages.TabSessionStatus.
func (m *Model) updateTabSessionStatus(msg messages.TabSessionStatus) (*Model, tea.Cmd) {
	if msg.Status != "stopped" {
		return m, nil
	}
	tab := m.getTabBySession(msg.WorkspaceID, msg.SessionName)
	if tab == nil {
		return m, nil
	}
	m.stopPTYReader(tab)
	tab.mu.Lock()
	agent := tab.Agent
	tab.Agent = nil
	tab.mu.Unlock()
	if agent != nil {
		_ = m.agentManager.CloseAgent(agent)
	}
	tab.mu.Lock()
	tab.Running = false
	tab.Detached = false
	tab.autoRestartAttempt = 0
	tabID := tab.ID
	tab.mu.Unlock()

	// Schedule automatic restart after a brief delay.
	wsID := msg.WorkspaceID
	restartCmd := common.SafeTick(tabAutoRestartInitial, func(time.Time) tea.Msg {
		return tabAutoRestart{WorkspaceID: wsID, TabID: tabID, Attempt: 1}
	})

	return m, common.SafeBatch(func() tea.Msg {
		return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(tab.ID)}
	}, func() tea.Msg {
		return messages.Toast{
			Message: "Detected crash, attempting auto-restart...",
			Level:   messages.ToastWarning,
		}
	}, restartCmd)
}

// updateTabAutoRestart handles automatic restart attempts for stopped tabs.
func (m *Model) updateTabAutoRestart(msg tabAutoRestart) (*Model, tea.Cmd) {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return m, nil
	}

	// If the tab was already restarted (manually or by a previous attempt), skip.
	tab.mu.Lock()
	running := tab.Running
	detached := tab.Detached
	tab.mu.Unlock()
	if running || detached {
		return m, nil
	}

	// Check assistant is still valid.
	if m.config == nil || m.config.Assistants == nil {
		return m, nil
	}
	if _, ok := m.config.Assistants[tab.Assistant]; !ok {
		return m, nil
	}

	tab.mu.Lock()
	sessionName := tab.SessionName
	claudeSessionID := tab.ClaudeSessionID
	tabIsolated := tab.Isolated
	tabAllowUnsandboxed := tab.AllowUnsandboxedCommands
	tabPermissionMode := tab.PermissionMode
	tab.autoRestartAttempt = msg.Attempt
	tab.mu.Unlock()

	ws := tab.Workspace
	if ws == nil {
		return m, nil
	}
	tabID := tab.ID
	if sessionName == "" {
		sessionName = tmux.SessionName("medusa", ws.Name, "1")
	}

	// Clean up any leftover agent.
	m.stopPTYReader(tab)
	tab.mu.Lock()
	existingAgent := tab.Agent
	tab.Agent = nil
	tab.mu.Unlock()
	if existingAgent != nil {
		_ = m.agentManager.CloseAgent(existingAgent)
	}

	tmuxOpts := m.getTmuxOptions()
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	assistant := tab.Assistant
	attempt := msg.Attempt
	fullscreen := appPty.AgentType(assistant) == appPty.AgentClaude

	logging.Info("Auto-restart attempt %d/%d for tab %s", attempt, tabAutoRestartMax, tabID)

	return m, func() tea.Msg {
		_ = tmux.KillSession(sessionName, tmuxOpts)

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

		tags := tmux.SessionTags{
			WorkspaceID: string(ws.ID()),
			TabID:       string(tabID),
			Type:        "agent",
			Assistant:   assistant,
			CreatedAt:   time.Now().Unix(),
		}
		agent, err := m.agentManager.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags, agentOpts)
		if err != nil {
			logging.Warn("Auto-restart attempt %d failed for tab %s: %v", attempt, tabID, err)
			return tabAutoRestartFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Attempt:     attempt,
				Err:         err,
			}
		}

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

// updateTabAutoRestartFailed handles a failed auto-restart attempt.
func (m *Model) updateTabAutoRestartFailed(msg tabAutoRestartFailed) (*Model, tea.Cmd) {
	if msg.Attempt < tabAutoRestartMax {
		// Retry with exponential backoff.
		delay := tabAutoRestartInitial
		for i := 1; i < msg.Attempt; i++ {
			delay *= 2
			if delay > tabAutoRestartMaxWait {
				delay = tabAutoRestartMaxWait
				break
			}
		}
		wsID := msg.WorkspaceID
		tabID := msg.TabID
		next := msg.Attempt + 1
		return m, common.SafeTick(delay, func(time.Time) tea.Msg {
			return tabAutoRestart{WorkspaceID: wsID, TabID: tabID, Attempt: next}
		})
	}
	// Max attempts exhausted — show manual restart hint.
	return m, func() tea.Msg {
		return messages.Toast{
			Message: "Auto-restart failed. Press Ctrl-a S to restart manually.",
			Level:   messages.ToastWarning,
		}
	}
}

// updateTabActorReady handles tabActorReady.
func (m *Model) updateTabActorReady(_ tabActorReady) (*Model, tea.Cmd) {
	m.setTabActorReady()
	m.noteTabActorHeartbeat()
	return m, nil
}

// updateTabActorHeartbeat handles tabActorHeartbeat.
func (m *Model) updateTabActorHeartbeat(_ tabActorHeartbeat) (*Model, tea.Cmd) {
	m.noteTabActorHeartbeat()
	return m, nil
}

// updateMonitorSnapshotTick handles monitorSnapshotTick.
func (m *Model) updateMonitorSnapshotTick(msg monitorSnapshotTick) (*Model, tea.Cmd) {
	return m, m.handleMonitorSnapshotTick(msg)
}

// updateMonitorSnapshotResult handles monitorSnapshotResult.
func (m *Model) updateMonitorSnapshotResult(msg monitorSnapshotResult) (*Model, tea.Cmd) {
	m.applyMonitorSnapshotResult(msg.snapshots)
	return m, nil
}

// updateOpenDiff handles messages.OpenDiff.
func (m *Model) updateOpenDiff(msg messages.OpenDiff) (*Model, tea.Cmd) {
	// Check if new-style Change is provided, otherwise convert from legacy fields
	if msg.Change != nil {
		return m, m.createDiffTab(msg.Change, msg.Mode, msg.Workspace)
	}
	// Legacy path: convert File/StatusCode to Change
	change := &git.Change{
		Path: msg.File,
	}
	mode := git.DiffModeUnstaged
	if msg.StatusCode == "??" {
		change.Kind = git.ChangeUntracked
	} else if len(msg.StatusCode) >= 1 && msg.StatusCode[0] != ' ' {
		// Staged change
		mode = git.DiffModeStaged
		switch msg.StatusCode[0] {
		case 'A':
			change.Kind = git.ChangeAdded
		case 'D':
			change.Kind = git.ChangeDeleted
		case 'M':
			change.Kind = git.ChangeModified
		case 'R':
			change.Kind = git.ChangeRenamed
		}
		change.Staged = true
	} else {
		// Unstaged change
		if len(msg.StatusCode) >= 2 {
			switch msg.StatusCode[1] {
			case 'A':
				change.Kind = git.ChangeAdded
			case 'D':
				change.Kind = git.ChangeDeleted
			case 'M':
				change.Kind = git.ChangeModified
			}
		}
	}
	return m, m.createDiffTab(change, mode, msg.Workspace)
}

// updateWorkspaceDeleted handles messages.WorkspaceDeleted.
func (m *Model) updateWorkspaceDeleted(msg messages.WorkspaceDeleted) (*Model, tea.Cmd) {
	m.CleanupWorkspace(msg.Workspace)
	return m, nil
}

// updateTabSelectionResult handles tabSelectionResult.
func (m *Model) updateTabSelectionResult(msg tabSelectionResult) (*Model, tea.Cmd) {
	if msg.clipboard != "" {
		if err := common.CopyToClipboard(msg.clipboard); err != nil {
			logging.Error("Failed to copy to clipboard: %v", err)
		} else {
			logging.Info("Copied %d chars to clipboard", len(msg.clipboard))
		}
	}
	return m, nil
}

// updateSelectionTickRequest handles selectionTickRequest.
func (m *Model) updateSelectionTickRequest(msg selectionTickRequest) (*Model, tea.Cmd) {
	cmd := common.SafeTick(100*time.Millisecond, func(time.Time) tea.Msg {
		return selectionScrollTick{WorkspaceID: msg.workspaceID, TabID: msg.tabID, Gen: msg.gen}
	})
	return m, cmd
}

// updateTabDiffCmd handles tabDiffCmd.
func (m *Model) updateTabDiffCmd(msg tabDiffCmd) (*Model, tea.Cmd) {
	return m, msg.cmd
}
