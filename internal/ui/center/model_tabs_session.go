package center

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/vterm"
)

// detachTab is the core implementation for detaching a tab (closes PTY, keeps tmux session).
func (m *Model) detachTab(tab *Tab, index int) tea.Cmd {
	if tab == nil {
		return nil
	}
	if tab.DiffViewer != nil {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Diff tabs cannot be detached",
				Level:   messages.ToastInfo,
			}
		}
	}
	if m.config == nil || m.config.Assistants == nil {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Tab cannot be detached",
				Level:   messages.ToastInfo,
			}
		}
	}
	if _, ok := m.config.Assistants[tab.Assistant]; !ok {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Only assistant tabs can be detached",
				Level:   messages.ToastInfo,
			}
		}
	}
	tab.mu.Lock()
	alreadyDetached := tab.Detached
	hasAgent := tab.Agent != nil
	tab.mu.Unlock()
	if alreadyDetached && !hasAgent {
		return nil
	}
	m.stopPTYReader(tab)
	tab.mu.Lock()
	tab.Running = false
	tab.Detached = true
	tab.pendingOutput = nil
	if tab.Agent != nil && tab.SessionName == "" {
		tab.SessionName = tab.Agent.Session
	}
	agent := tab.Agent
	tab.Agent = nil
	tab.mu.Unlock()
	if agent != nil {
		_ = m.agentManager.CloseAgent(agent)
	}
	return func() tea.Msg {
		return messages.TabDetached{Index: index}
	}
}

// DetachTabByID closes the PTY client for a specific tab and keeps the tmux session alive.
func (m *Model) DetachTabByID(wsID string, tabID TabID) tea.Cmd {
	if wsID == "" {
		return nil
	}
	tabs := m.tabsByWorkspace[wsID]
	for idx, tab := range tabs {
		if tab == nil || tab.isClosed() || tab.ID != tabID {
			continue
		}
		return m.detachTab(tab, idx)
	}
	return nil
}

// ReattachTabByID reattaches to a detached tmux session by workspace ID and
// tab ID. Works for both agent tabs (resumed via CreateAgentWithTags) and
// script tabs (attached via CreateViewerWithTags).
func (m *Model) ReattachTabByID(wsID string, tabID TabID) tea.Cmd {
	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.Workspace == nil {
		return nil
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
	detached := tab.Detached
	sessionName := tab.SessionName
	claudeSessionID := tab.ClaudeSessionID
	scriptFullCmd := tab.ScriptFullCmd
	fullscreen := tab.Fullscreen
	tab.mu.Unlock()
	if !detached {
		return nil
	}
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	if sessionName == "" {
		sessionName = tmux.SessionName("medusa", tab.Workspace.Name, "1")
	}
	assistant := tab.Assistant
	ws := tab.Workspace
	opts := m.getTmuxOptions()
	return func() tea.Msg {
		state, err := tmux.SessionStateFor(sessionName, opts)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Action:      "reattach",
			}
		}
		if !state.Exists || !state.HasLivePane {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         fmt.Errorf("tmux session ended"),
				Stopped:     true,
				Action:      "reattach",
			}
		}
		tags := tmux.SessionTags{
			WorkspaceID: string(ws.ID()),
			TabID:       string(tabID),
		}
		var agent *appPty.Agent
		if isScript {
			tags.Type = "script"
			tags.Assistant = "script"
			// The tmux session already exists (state.Exists checked above), so
			// tmux's `new-session -A` flag makes this an attach — the command
			// here is only used for re-creation, not re-attach.
			agent, err = m.agentManager.CreateViewerWithTags(ws, scriptFullCmd, sessionName, uint16(termHeight), uint16(termWidth), tags)
		} else {
			tags.Type = "agent"
			tags.Assistant = assistant
			agent, err = m.agentManager.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags, appPty.AgentOptions{Fullscreen: fullscreen})
		}
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Action:      "reattach",
			}
		}
		// Best-effort capture of existing scrollback from the tmux pane.
		scrollback, _ := tmux.CapturePane(sessionName, opts)
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

func (m *Model) tabSelectionChangedCmd() tea.Cmd {
	wsID := m.workspaceID()
	if wsID == "" {
		return nil
	}
	return func() tea.Msg {
		return messages.TabSelectionChanged{
			WorkspaceID: wsID,
			ActiveIndex: m.getActiveTabIdx(),
		}
	}
}

// RestoreTabsFromWorkspace recreates tabs from persisted workspace metadata.
// Only agent tabs with known assistants are restored.
func (m *Model) RestoreTabsFromWorkspace(ws *data.Workspace) tea.Cmd {
	if ws == nil || len(ws.OpenTabs) == 0 {
		return nil
	}
	wsID := string(ws.ID())
	if len(m.tabsByWorkspace[wsID]) > 0 {
		return nil
	}
	if _, ok := m.restoredWorkspaces[wsID]; ok {
		return nil
	}

	activeIdx := ws.ActiveTabIndex
	// Pre-scan to pick the persisted index of the tab that should receive
	// initial focus: the persisted-active tab if it's a non-script, otherwise
	// the first non-script tab at or after activeIdx, otherwise the first
	// non-script tab overall. Script tabs are never initially focused.
	focusPersistedIdx := -1
	firstNonScriptIdx := -1
	for i, tab := range ws.OpenTabs {
		if tab.Assistant == "" || tab.Assistant == "script" {
			continue
		}
		if m.config == nil || m.config.Assistants == nil {
			continue
		}
		if _, ok := m.config.Assistants[tab.Assistant]; !ok {
			continue
		}
		if firstNonScriptIdx == -1 {
			firstNonScriptIdx = i
		}
		if i >= activeIdx {
			focusPersistedIdx = i
			break
		}
	}
	if focusPersistedIdx == -1 {
		focusPersistedIdx = firstNonScriptIdx
	}

	var cmds []tea.Cmd
	restoreCount := 0
	setFocus := func(tab *Tab) {
		if tab == nil {
			return
		}
		for idx, t := range m.tabsByWorkspace[wsID] {
			if t == tab {
				m.activeTabByWorkspace[wsID] = idx
				m.infoTabActive = false
				return
			}
		}
	}
	for i, tab := range ws.OpenTabs {
		if tab.Assistant == "" {
			continue
		}
		isScript := tab.Assistant == "script"
		if !isScript {
			if m.config == nil || m.config.Assistants == nil {
				continue
			}
			if _, ok := m.config.Assistants[tab.Assistant]; !ok {
				continue
			}
		}
		status := strings.ToLower(strings.TrimSpace(tab.Status))
		activate := !isScript && i == focusPersistedIdx
		// Script tabs always reattach to their existing tmux session.
		if isScript {
			info := tab
			placeholder := m.addPlaceholderTab(ws, info, true)
			restoreCount++
			if placeholder != nil {
				cmds = append(cmds, m.ReattachTabByID(wsID, placeholder.ID))
			}
			continue
		}
		if status == "stopped" {
			placeholder := m.addPlaceholderTab(ws, tab, false)
			restoreCount++
			if activate {
				setFocus(placeholder)
			}
			continue
		}
		if status == "detached" {
			placeholder := m.addPlaceholderTab(ws, tab, true)
			restoreCount++
			if activate {
				setFocus(placeholder)
			}
			if placeholder != nil {
				cmds = append(cmds, m.ReattachTabByID(wsID, placeholder.ID))
			}
			continue
		}
		restoreCount++
		cmds = append(cmds, m.createAgentTabWithSession(tab.Assistant, ws, tab.SessionName, tab.Name, activate, tab.ClaudeSessionID, tab.Isolated, tab.AllowUnsandboxedCommands, tab.PermissionMode, tab.Fullscreen))
	}
	if restoreCount > 0 {
		m.restoredWorkspaces[wsID] = struct{}{}
	}
	if restoreCount > 0 && focusPersistedIdx == -1 {
		// Only scripts were restored — focus the Info tab instead.
		m.activeTabByWorkspace[wsID] = 0
		m.infoTabActive = true
	}
	return common.SafeBatch(cmds...)
}

// AddTabsFromWorkspace adds new tabs without resetting existing UI state.
func (m *Model) AddTabsFromWorkspace(ws *data.Workspace, tabs []data.TabInfo) tea.Cmd {
	if ws == nil || len(tabs) == 0 {
		return nil
	}
	if m.config == nil || m.config.Assistants == nil {
		return nil
	}
	wsID := string(ws.ID())
	existing := make(map[string]struct{}, len(m.tabsByWorkspace[wsID]))
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab == nil || tab.isClosed() {
			continue
		}
		sessionName := strings.TrimSpace(tab.SessionName)
		if sessionName == "" && tab.Agent != nil {
			sessionName = strings.TrimSpace(tab.Agent.Session)
		}
		if sessionName != "" {
			existing[sessionName] = struct{}{}
		}
	}

	var cmds []tea.Cmd
	for _, tab := range tabs {
		if tab.Assistant == "" {
			continue
		}
		if _, ok := m.config.Assistants[tab.Assistant]; !ok {
			continue
		}
		sessionName := strings.TrimSpace(tab.SessionName)
		if sessionName != "" {
			if _, ok := existing[sessionName]; ok {
				continue
			}
			existing[sessionName] = struct{}{}
		}
		status := strings.ToLower(strings.TrimSpace(tab.Status))
		if status == "stopped" {
			m.addPlaceholderTab(ws, tab, false)
			continue
		}
		if status == "detached" {
			m.addPlaceholderTab(ws, tab, true)
			// Auto-reattach: find the tab we just added and trigger reattach
			wsTabs := m.tabsByWorkspace[wsID]
			if len(wsTabs) > 0 {
				lastTab := wsTabs[len(wsTabs)-1]
				cmds = append(cmds, m.ReattachTabByID(wsID, lastTab.ID))
			}
			continue
		}
		cmds = append(cmds, m.createAgentTabWithSession(tab.Assistant, ws, sessionName, tab.Name, false, tab.ClaudeSessionID, tab.Isolated, tab.AllowUnsandboxedCommands, tab.PermissionMode, tab.Fullscreen))
	}
	return common.SafeBatch(cmds...)
}

// addPlaceholderTab adds a stopped or detached tab placeholder so it remains visible
// in the UI and can be restarted or reattached. If detached is true, the tab will
// attempt reattachment to its tmux session; otherwise it will create a fresh session.
func (m *Model) addPlaceholderTab(ws *data.Workspace, info data.TabInfo, detached bool) *Tab {
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	if termWidth < 1 {
		termWidth = 80
	}
	if termHeight < 1 {
		termHeight = 24
	}
	displayName := strings.TrimSpace(info.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(info.Assistant)
	}
	if displayName == "" {
		displayName = "Terminal"
	}
	term := vterm.New(termWidth, termHeight)
	// Alt-screen apps (fullscreen Claude, vim, less) own their scrollback;
	// capturing their viewport scroll-offs would fill medusa's scrollback
	// with frame fragments no real terminal keeps.
	term.AllowAltScreenScrollback = false
	tab := &Tab{
		ID:                       generateTabID(),
		Name:                     displayName,
		Assistant:                info.Assistant,
		Workspace:                ws,
		SessionName:              info.SessionName,
		ClaudeSessionID:          info.ClaudeSessionID,
		Detached:                 detached,
		Running:                  false,
		Terminal:                 term,
		Isolated:                 info.Isolated,
		AllowUnsandboxedCommands: info.AllowUnsandboxedCommands,
		PermissionMode:           info.PermissionMode,
		Fullscreen:               info.Fullscreen,
		ScriptFullCmd:            info.ScriptFullCmd,
	}
	wsID := string(ws.ID())
	m.appendTabOrdered(wsID, tab)
	return tab
}
