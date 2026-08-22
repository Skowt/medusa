package center

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/ui/common"
)

// hasActiveAgent returns whether there's an active agent
func (m *Model) hasActiveAgent() bool {
	tabs := m.getTabs()
	return len(tabs) > 0 && m.getActiveTabIdx() < len(tabs)
}

// nextTab switches to the next tab
func (m *Model) nextTab() {
	tabs := m.getTabs()
	if m.infoTabActive {
		// From Info tab, go to first agent tab (or stay if none)
		if len(tabs) > 0 {
			m.infoTabActive = false
			m.setActiveTabIdx(0)
		}
		return
	}
	if len(tabs) > 0 {
		next := m.getActiveTabIdx() + 1
		if next >= len(tabs) {
			// Wrap to Info tab
			m.infoTabActive = true
		} else {
			m.setActiveTabIdx(next)
		}
	}
}

// prevTab switches to the previous tab
func (m *Model) prevTab() {
	tabs := m.getTabs()
	if m.infoTabActive {
		// From Info tab, go to last agent tab (or stay if none)
		if len(tabs) > 0 {
			m.infoTabActive = false
			m.setActiveTabIdx(len(tabs) - 1)
		}
		return
	}
	if len(tabs) > 0 {
		idx := m.getActiveTabIdx() - 1
		if idx < 0 {
			// Wrap to Info tab
			m.infoTabActive = true
		} else {
			m.setActiveTabIdx(idx)
		}
	}
}

// Public wrappers for prefix mode commands

// NextTab switches to the next tab (public wrapper)
func (m *Model) NextTab() {
	m.nextTab()
}

// PrevTab switches to the previous tab (public wrapper)
func (m *Model) PrevTab() {
	m.prevTab()
}

// CloseActiveTab closes the current tab (public wrapper)
func (m *Model) CloseActiveTab() tea.Cmd {
	return m.closeCurrentTab()
}

// CloseTabAtIndex closes a specific tab by index (public wrapper)
func (m *Model) CloseTabAtIndex(index int) tea.Cmd {
	return m.closeTabAt(index)
}

// CloseScriptTabs closes all script tabs for the given workspace and returns
// a batched command that kills their tmux sessions.
func (m *Model) CloseScriptTabs(wsID string) tea.Cmd {
	tabs := m.tabsByWorkspace[wsID]
	var cmds []tea.Cmd
	// Collect indices in reverse so removals don't shift later indices.
	for i := len(tabs) - 1; i >= 0; i-- {
		if tabs[i] != nil && tabs[i].Assistant == "script" {
			if cmd := m.closeTabAt(i); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return common.SafeBatch(cmds...)
}

// SelectTab switches to a specific tab by index (0-indexed)
func (m *Model) SelectTab(index int) {
	tabs := m.getTabs()
	if index >= 0 && index < len(tabs) {
		m.infoTabActive = false
		m.setActiveTabIdx(index)
	}
}

// SendToTerminal sends a string directly to the active terminal
func (m *Model) SendToTerminal(s string) {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return
	}
	tab := tabs[activeIdx]
	if tab.isClosed() {
		return
	}
	tab.mu.Lock()
	agent := tab.Agent
	tab.mu.Unlock()
	if agent != nil && agent.Terminal != nil {
		if err := agent.Terminal.SendString(s); err != nil {
			logging.Warn("SendToTerminal failed for tab %s: %v", tab.ID, err)
			tab.mu.Lock()
			tab.Running = false
			tab.Detached = true
			tab.mu.Unlock()
		}
	}
}

// GetTabsInfo returns information about current tabs for persistence
func (m *Model) GetTabsInfo() ([]data.TabInfo, int) {
	var result []data.TabInfo
	tabs := m.getTabs()
	for _, tab := range tabs {
		if tab == nil {
			continue
		}
		tab.mu.Lock()
		running := tab.Running
		detached := tab.Detached
		sessionName := tab.SessionName
		if sessionName == "" && tab.Agent != nil {
			sessionName = tab.Agent.Session
		}
		claudeSessionID := tab.ClaudeSessionID
		isolated := tab.Isolated
		allowUnsandboxed := tab.AllowUnsandboxedCommands
		permMode := tab.PermissionMode
		fullscreen := tab.Fullscreen
		codexSandbox := tab.CodexSandbox
		codexApproval := tab.CodexApproval
		codexSearch := tab.CodexSearch
		scriptFullCmd := tab.ScriptFullCmd
		tab.mu.Unlock()
		status := "stopped"
		if detached {
			status = "detached"
		} else if running {
			status = "running"
		}
		result = append(result, data.TabInfo{
			Assistant:                tab.Assistant,
			Name:                     tab.Name,
			SessionName:              sessionName,
			Status:                   status,
			ClaudeSessionID:          claudeSessionID,
			Isolated:                 isolated,
			AllowUnsandboxedCommands: allowUnsandboxed,
			PermissionMode:           permMode,
			Fullscreen:               fullscreen,
			CodexSandbox:             codexSandbox,
			CodexApproval:            codexApproval,
			CodexSearch:              codexSearch,
			ScriptFullCmd:            scriptFullCmd,
		})
	}
	return result, m.getActiveTabIdx()
}

// GetTabsInfoForWorkspace returns tab information for a specific workspace ID.
func (m *Model) GetTabsInfoForWorkspace(wsID string) ([]data.TabInfo, int) {
	var result []data.TabInfo
	tabs := m.tabsByWorkspace[wsID]
	for _, tab := range tabs {
		if tab == nil {
			continue
		}
		tab.mu.Lock()
		running := tab.Running
		detached := tab.Detached
		sessionName := tab.SessionName
		if sessionName == "" && tab.Agent != nil {
			sessionName = tab.Agent.Session
		}
		claudeSessionID := tab.ClaudeSessionID
		isolated := tab.Isolated
		allowUnsandboxed := tab.AllowUnsandboxedCommands
		permMode := tab.PermissionMode
		fullscreen := tab.Fullscreen
		codexSandbox := tab.CodexSandbox
		codexApproval := tab.CodexApproval
		codexSearch := tab.CodexSearch
		scriptFullCmd := tab.ScriptFullCmd
		tab.mu.Unlock()
		status := "stopped"
		if detached {
			status = "detached"
		} else if running {
			status = "running"
		}
		result = append(result, data.TabInfo{
			Assistant:                tab.Assistant,
			Name:                     tab.Name,
			SessionName:              sessionName,
			Status:                   status,
			ClaudeSessionID:          claudeSessionID,
			Isolated:                 isolated,
			AllowUnsandboxedCommands: allowUnsandboxed,
			PermissionMode:           permMode,
			Fullscreen:               fullscreen,
			CodexSandbox:             codexSandbox,
			CodexApproval:            codexApproval,
			CodexSearch:              codexSearch,
			ScriptFullCmd:            scriptFullCmd,
		})
	}
	return result, m.activeTabByWorkspace[wsID]
}

// HasDiffViewer returns true if the active tab has a diff viewer.
func (m *Model) HasDiffViewer() bool {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return false
	}
	tab := tabs[activeIdx]
	if tab.isClosed() {
		return false
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return tab.DiffViewer != nil
}
