package center

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/vterm"
)

func nextAssistantName(assistant string, tabs []*Tab) string {
	assistant = strings.TrimSpace(assistant)
	if assistant == "" {
		return ""
	}

	used := make(map[string]struct{})
	for _, tab := range tabs {
		if tab == nil || tab.Assistant != assistant {
			continue
		}
		name := strings.TrimSpace(tab.Name)
		if name == "" {
			name = assistant
		}
		used[name] = struct{}{}
	}

	if _, ok := used[assistant]; !ok {
		return assistant
	}

	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s %d", assistant, i)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
}

// agentTabOptions carries the per-tab launch settings from the New Tab dialog
// (or a restore) down to the agent. Claude reads the sandbox/permission/
// fullscreen fields and Codex the three codex ones; each ignores the other's.
type agentTabOptions struct {
	Isolated                 bool
	AllowUnsandboxedCommands bool
	PermissionMode           string
	Fullscreen               bool
	CodexSandbox             string
	CodexApproval            string
	CodexSearch              bool
}

// agentTabOptionsFromTab rebuilds the launch settings of an existing tab, for
// the restore and restart paths.
func agentTabOptionsFromTab(tab *Tab) agentTabOptions {
	return agentTabOptions{
		Isolated:                 tab.Isolated,
		AllowUnsandboxedCommands: tab.AllowUnsandboxedCommands,
		PermissionMode:           tab.PermissionMode,
		Fullscreen:               tab.Fullscreen,
		CodexSandbox:             tab.CodexSandbox,
		CodexApproval:            tab.CodexApproval,
		CodexSearch:              tab.CodexSearch,
	}
}

// agentTabOptionsFromTabInfo rebuilds the launch settings of a persisted tab.
func agentTabOptionsFromTabInfo(info data.TabInfo) agentTabOptions {
	return agentTabOptions{
		Isolated:                 info.Isolated,
		AllowUnsandboxedCommands: info.AllowUnsandboxedCommands,
		PermissionMode:           info.PermissionMode,
		Fullscreen:               info.Fullscreen,
		CodexSandbox:             info.CodexSandbox,
		CodexApproval:            info.CodexApproval,
		CodexSearch:              info.CodexSearch,
	}
}

// forAssistant drops the settings that belong to another assistant, so a tab
// can never be launched with flags its agent does not take. Fullscreen is the
// one that matters most: it is Claude's renderer, and Codex neither reports
// mouse nor takes the pane's alternate screen, so marking a Codex tab
// fullscreen would forward its mouse into an app that ignores it and disable
// medusa's own scrollback.
func (o agentTabOptions) forAssistant(assistant string) agentTabOptions {
	if appPty.AgentType(assistant) == appPty.AgentCodex {
		return agentTabOptions{
			CodexSandbox:  o.CodexSandbox,
			CodexApproval: o.CodexApproval,
			CodexSearch:   o.CodexSearch,
		}
	}
	return agentTabOptions{
		Isolated:                 o.Isolated,
		AllowUnsandboxedCommands: o.AllowUnsandboxedCommands,
		PermissionMode:           o.PermissionMode,
		Fullscreen:               o.Fullscreen && appPty.AgentType(assistant) == appPty.AgentClaude,
	}
}

// agentOptions converts the tab settings into the launch options the pty layer
// takes.
func (o agentTabOptions) agentOptions() appPty.AgentOptions {
	return appPty.AgentOptions{
		Isolated:                 o.Isolated,
		AllowUnsandboxedCommands: o.AllowUnsandboxedCommands,
		PermissionMode:           o.PermissionMode,
		Fullscreen:               o.Fullscreen,
		CodexSandbox:             o.CodexSandbox,
		CodexApproval:            o.CodexApproval,
		CodexSearch:              o.CodexSearch,
	}
}

type ptyTabCreateResult struct {
	Workspace         *data.Workspace
	Assistant         string
	DisplayName       string
	Agent             *appPty.Agent
	TabID             TabID
	Activate          bool
	Rows              int
	Cols              int
	ScrollbackCapture []byte
	ClaudeSessionID   string
	Options           agentTabOptions
	ScriptFullCmd     string // Only set for script tabs; enables in-place Restart.
}

type ptyTabReattachResult struct {
	WorkspaceID       string
	TabID             TabID
	Agent             *appPty.Agent
	Rows              int
	Cols              int
	ScrollbackCapture []byte
	ClaudeSessionID   string
	Fullscreen        bool
}

type ptyTabReattachFailed struct {
	WorkspaceID string
	TabID       TabID
	Err         error
	Stopped     bool
	Action      string
}

func truncateDisplayName(name string) string {
	if len(name) > 20 {
		return "..." + name[len(name)-17:]
	}
	return name
}

// createAgentTab creates a new agent tab with per-tab settings, keeping only
// the ones the chosen assistant understands.
func (m *Model) createAgentTab(assistant string, ws *data.Workspace, opts agentTabOptions) tea.Cmd {
	return m.createAgentTabWithSession(assistant, ws, "", "", true, "", opts.forAssistant(assistant))
}

func (m *Model) createAgentTabWithSession(assistant string, ws *data.Workspace, sessionName string, displayName string, activate bool, claudeSessionID string, opts agentTabOptions) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no workspace selected"), Context: "creating agent"}
		}
	}

	// Calculate terminal dimensions using the same metrics as render/layout.
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	tabID := generateTabID()

	return func() tea.Msg {
		logging.Info("Creating agent tab: assistant=%s workspace=%s", assistant, ws.Name)

		if sessionName == "" {
			sessionName, _ = tmux.NextUniqueSessionName(ws.Name, tmux.DefaultOptions())
		}

		// Build agent options for session resumption and per-tab settings.
		agentOpts := opts.agentOptions()
		switch appPty.AgentType(assistant) {
		case appPty.AgentClaude:
			if claudeSessionID != "" {
				// Restoring from persisted state — resume existing conversation.
				agentOpts.ClaudeSessionID = claudeSessionID
				agentOpts.Resume = true
			} else {
				// New tab — generate a fresh session ID.
				claudeSessionID = appPty.GenerateSessionID()
				agentOpts.ClaudeSessionID = claudeSessionID
			}
		case appPty.AgentCodex:
			// Codex mints its own session ids, so there is nothing to
			// pre-assign: a restore resumes the id the SessionStart hook
			// reported, and a fresh tab has none until it does.
			if claudeSessionID != "" {
				agentOpts.ClaudeSessionID = claudeSessionID
				agentOpts.Resume = true
			}
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
			logging.Error("Failed to create agent: %v", err)
			return messages.Error{Err: err, Context: "creating agent"}
		}

		logging.Info("Agent created, Terminal=%v", agent.Terminal != nil)

		// Best-effort capture of existing scrollback from the tmux pane.
		// For newly created sessions this returns empty content (harmless no-op).
		scrollback, _ := tmux.CapturePane(agent.Session, m.getTmuxOptions())

		return ptyTabCreateResult{
			Workspace:         ws,
			Assistant:         assistant,
			Agent:             agent,
			TabID:             tabID,
			DisplayName:       displayName,
			Activate:          activate,
			Rows:              termHeight,
			Cols:              termWidth,
			ScrollbackCapture: scrollback,
			ClaudeSessionID:   claudeSessionID,
			Options:           opts,
		}
	}
}

func (m *Model) handlePtyTabCreated(msg ptyTabCreateResult) tea.Cmd {
	if msg.Workspace == nil || msg.Agent == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("missing workspace or agent"), Context: "creating terminal tab"}
		}
	}

	// msg.Rows/Cols were captured when this (async) create was initiated and
	// may be stale by the time the result arrives. Size the vterm/PTY from the
	// current metrics so they match the height the tab is painted at.
	tm := m.terminalMetrics()
	rows := tm.Height
	cols := tm.Width

	displayName := strings.TrimSpace(msg.DisplayName)
	if displayName == "" {
		wsID := string(msg.Workspace.ID())
		displayName = nextAssistantName(msg.Assistant, m.tabsByWorkspace[wsID])
	}
	if displayName == "" {
		displayName = "Terminal"
	}

	// Create virtual terminal emulator with scrollback
	term := vterm.New(cols, rows)
	// The tmux client this vterm reads from enters the alt screen at attach
	// whatever the agent does, so scrollback must not be gated on AltScreen.
	// Frame-painting agents are excluded by AppFullscreen/mouse reporting.
	term.AllowAltScreenScrollback = true
	term.AppFullscreen = msg.Options.Fullscreen
	term.PrependScrollback(msg.ScrollbackCapture)

	// Create tab with unique ID (pre-generated if provided)
	tabID := msg.TabID
	if tabID == "" {
		tabID = generateTabID()
	}
	tab := &Tab{
		ID:                       tabID,
		Name:                     displayName,
		Assistant:                msg.Assistant,
		Workspace:                msg.Workspace,
		Agent:                    msg.Agent,
		SessionName:              msg.Agent.Session,
		ClaudeSessionID:          msg.ClaudeSessionID,
		Terminal:                 term,
		Running:                  true, // Agent/viewer starts running
		monitorDirty:             true,
		Isolated:                 msg.Options.Isolated,
		AllowUnsandboxedCommands: msg.Options.AllowUnsandboxedCommands,
		PermissionMode:           msg.Options.PermissionMode,
		Fullscreen:               msg.Options.Fullscreen,
		CodexSandbox:             msg.Options.CodexSandbox,
		CodexApproval:            msg.Options.CodexApproval,
		CodexSearch:              msg.Options.CodexSearch,
		ScriptFullCmd:            msg.ScriptFullCmd,
	}

	// Set up response writer for terminal queries (DSR, DA, etc.)
	if msg.Agent.Terminal != nil {
		agentTerm := msg.Agent.Terminal
		workspaceID := string(msg.Workspace.ID())
		term.SetResponseWriter(func(data []byte) {
			if len(data) == 0 || agentTerm == nil {
				return
			}
			if m.isTabActorReady() {
				response := append([]byte(nil), data...)
				if !m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: workspaceID,
					tabID:       tabID,
					kind:        tabEventSendResponse,
					response:    response,
				}) {
					if err := agentTerm.SendString(string(response)); err != nil {
						logging.Warn("Response write failed for tab %s: %v", tabID, err)
						if m.msgSink != nil {
							m.msgSink(TabInputFailed{TabID: tabID, WorkspaceID: workspaceID, Err: err})
						}
					}
				}
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

	// Set PTY size to match
	if msg.Agent.Terminal != nil {
		m.resizePTY(tab, rows, cols)
	}

	// Add tab to the workspace's tab list (script tabs are kept at the end).
	wsID := string(msg.Workspace.ID())
	createdIdx := m.appendTabOrdered(wsID, tab)
	if msg.Activate {
		m.activeTabByWorkspace[wsID] = createdIdx
		m.infoTabActive = false
	}
	m.noteTabsChanged()

	return func() tea.Msg {
		return messages.TabCreated{Index: createdIdx, Name: displayName}
	}
}

// closeCurrentTab closes the current tab
func (m *Model) closeCurrentTab() tea.Cmd {
	if m.infoTabActive {
		return nil // Info tab is not closeable
	}

	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()

	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}

	return m.closeTabAt(activeIdx)
}

func (m *Model) closeTabAt(index int) tea.Cmd {
	tabs := m.getTabs()
	if len(tabs) == 0 || index < 0 || index >= len(tabs) {
		return nil
	}

	tab := tabs[index]
	tab.markClosing()

	// Capture session info before cleanup for async kill
	sessionName := tab.SessionName
	tmuxOpts := m.getTmuxOptions()

	m.stopPTYReader(tab)

	// Close agent
	if tab.Agent != nil {
		_ = m.agentManager.CloseAgent(tab.Agent)
	}

	tab.mu.Lock()
	if tab.ptyTraceFile != nil {
		_ = tab.ptyTraceFile.Close()
		tab.ptyTraceFile = nil
		tab.ptyTraceClosed = true
	}
	// Clean up viewers and release memory
	// Note: tab.Agent is intentionally NOT niled here to avoid racing with
	// tab_actor which reads it without locking. The agent is already closed
	// via CloseAgent() above; leaving the pointer intact is safe.
	tab.DiffViewer = nil
	tab.Terminal = nil
	tab.cachedSnap = nil
	tab.Workspace = nil
	tab.Running = false
	tab.pendingOutput = nil
	tab.mu.Unlock()
	tab.markClosed()

	// Remove from tabs
	m.removeTab(index)

	// Adjust active tab
	tabs = m.getTabs() // Get updated tabs
	activeIdx := m.getActiveTabIdx()
	if index == activeIdx {
		if activeIdx >= len(tabs) && activeIdx > 0 {
			m.setActiveTabIdx(activeIdx - 1)
		}
	} else if index < activeIdx {
		m.setActiveTabIdx(activeIdx - 1)
	}

	closedCmd := func() tea.Msg {
		return messages.TabClosed{Index: index}
	}

	// Kill tmux session asynchronously to avoid blocking the UI
	if sessionName != "" {
		killCmd := func() tea.Msg {
			_ = tmux.KillSession(sessionName, tmuxOpts)
			return nil
		}
		return tea.Batch(closedCmd, killCmd)
	}

	return closedCmd
}
