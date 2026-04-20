package center

import (
	"github.com/Skowt/medusa/internal/data"
)

// workspaceID returns the ID of the current workspace, or empty string
func (m *Model) workspaceID() string {
	if m.workspace == nil {
		return ""
	}
	return string(m.workspace.ID())
}

// getTabs returns the tabs for the current workspace
func (m *Model) getTabs() []*Tab {
	return m.tabsByWorkspace[m.workspaceID()]
}

// resolveWSID follows the redirect map for renamed workspaces.
// PTY reader goroutines capture the workspace ID at start and embed it in
// every message; after a rename the old ID must resolve to the new one.
func (m *Model) resolveWSID(wsID string) string {
	if redirect, ok := m.wsIDRedirects[wsID]; ok {
		return redirect
	}
	return wsID
}

// getTabByID returns the tab with the given ID, or nil if not found
func (m *Model) getTabByID(wsID string, tabID TabID) *Tab {
	wsID = m.resolveWSID(wsID)
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab.ID == tabID && !tab.isClosed() {
			return tab
		}
	}
	return nil
}

// getTabBySession returns the tab with the given tmux session name.
func (m *Model) getTabBySession(wsID, sessionName string) *Tab {
	if sessionName == "" {
		return nil
	}
	wsID = m.resolveWSID(wsID)
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab == nil || tab.isClosed() {
			continue
		}
		if tab.SessionName == sessionName {
			return tab
		}
		if tab.Agent != nil && tab.Agent.Session == sessionName {
			return tab
		}
	}
	return nil
}

// getActiveTabIdx returns the active tab index for the current workspace
func (m *Model) getActiveTabIdx() int {
	return m.activeTabByWorkspace[m.workspaceID()]
}

// setActiveTabIdx sets the active tab index for the current workspace
func (m *Model) setActiveTabIdx(idx int) {
	m.activeTabByWorkspace[m.workspaceID()] = idx
}

func (m *Model) noteTabsChanged() {
	m.tabsRevision++
}

func (m *Model) isActiveTab(wsID string, tabID TabID) bool {
	if m.workspace == nil || m.resolveWSID(wsID) != m.workspaceID() {
		return false
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx < 0 || activeIdx >= len(tabs) {
		return false
	}
	return tabs[activeIdx].ID == tabID
}

// removeTab removes a tab at index from the current workspace
func (m *Model) removeTab(idx int) {
	wsID := m.workspaceID()
	tabs := m.tabsByWorkspace[wsID]
	if idx >= 0 && idx < len(tabs) {
		m.tabsByWorkspace[wsID] = append(tabs[:idx], tabs[idx+1:]...)
		m.noteTabsChanged()
	}
}

// CleanupWorkspace removes all tabs and state for a deleted workspace
func (m *Model) CleanupWorkspace(ws *data.Workspace) {
	if ws == nil {
		return
	}
	wsID := string(ws.ID())

	// Close resources for each tab before removing
	for _, tab := range m.tabsByWorkspace[wsID] {
		tab.markClosing()
		m.stopPTYReader(tab)
		tab.mu.Lock()
		if tab.ptyTraceFile != nil {
			_ = tab.ptyTraceFile.Close()
			tab.ptyTraceFile = nil
			tab.ptyTraceClosed = true
		}
		tab.pendingOutput = nil
		tab.DiffViewer = nil
		tab.Terminal = nil
		tab.cachedSnap = nil
		tab.Workspace = nil
		tab.Running = false
		tab.mu.Unlock()
		tab.markClosed()
	}

	delete(m.tabsByWorkspace, wsID)
	delete(m.activeTabByWorkspace, wsID)
	// Clean up any rename redirects pointing to this workspace.
	for oldID, newID := range m.wsIDRedirects {
		if newID == wsID {
			delete(m.wsIDRedirects, oldID)
		}
	}
	m.noteTabsChanged()

	// Also cleanup agents for this workspace
	if m.agentManager != nil {
		m.agentManager.CloseWorkspaceAgents(ws)
	}
}
