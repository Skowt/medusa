package center

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/tmux"
)

// Init initializes the center pane
func (m *Model) Init() tea.Cmd {
	return nil
}

// Focus sets the focus state
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the center pane is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetWorkspace sets the active workspace.
func (m *Model) SetWorkspace(ws *data.Workspace) {
	m.workspace = ws
	m.infoCursor = 0
	m.infoTabActive = false
	m.tabScrollOffset = 0
	m.lastRenderedActiveID = ""
	// Activating a workspace shows the info bar, which shrinks the terminal
	// paint height. Reconcile tab sizes so a tab sized under the previous
	// (info-bar-absent) state isn't painted — and clipped — at the new height.
	// Only matters when a workspace is active (tabs are painted then); monitor
	// mode sizes tabs via its own grid path, so skip it there.
	if ws != nil && !m.monitorMode && m.height > 0 {
		m.reconcileTerminalSizes()
	}
}

// InfoCursor returns the current cursor position on the Info tab.
func (m *Model) InfoCursor() int {
	return m.infoCursor
}

// SetInfoContent sets the content displayed when the Info tab is active.
func (m *Model) SetInfoContent(content string) {
	m.infoContent = content
}

// IsInfoTabActive returns whether the Info tab is currently selected.
// Also returns true when a workspace is active but has no agent tabs,
// since the Info tab is auto-selected in that case.
func (m *Model) IsInfoTabActive() bool {
	if m.infoTabActive {
		return true
	}
	return m.workspace != nil && len(m.getTabs()) == 0
}

// SelectInfoTab activates the Info tab.
func (m *Model) SelectInfoTab() {
	m.infoTabActive = true
}

// HasTabs returns whether there are any tabs for the current workspace
func (m *Model) HasTabs() bool {
	return len(m.getTabs()) > 0
}

// HasTabsForWorkspace returns whether there are any tabs for a given workspace ID
func (m *Model) HasTabsForWorkspace(wsID string) bool {
	return len(m.tabsByWorkspace[wsID]) > 0
}

// HasAgentTabsForWorkspace returns whether there are any non-script tabs for a
// given workspace ID. Used by the agent auto-launch gate, which must not count
// dev-server script tabs created by run commands.
func (m *Model) HasAgentTabsForWorkspace(wsID string) bool {
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab != nil && tab.Assistant != "script" {
			return true
		}
	}
	return false
}

// TabAssistantAt returns the Assistant field of the tab at the given index in
// the active workspace, or "" if the index is out of range. Pass -1 to look
// up the currently active tab.
func (m *Model) TabAssistantAt(index int) string {
	tabs := m.getTabs()
	if index < 0 {
		index = m.getActiveTabIdx()
	}
	if index < 0 || index >= len(tabs) || tabs[index] == nil {
		return ""
	}
	return tabs[index].Assistant
}

// AgentManager returns the agent manager instance.
func (m *Model) AgentManager() *appPty.AgentManager {
	return m.agentManager
}

// MigrateWorkspaceTabs moves tab state from oldID to newID after a workspace rename.
// It updates the workspace pointer, tmux session names, and adjusts the current workspace if needed.
// oldName/newName are the workspace display names used to compute tmux session name prefixes.
//
// Running PTY readers are NOT restarted — they continue emitting messages with the old
// workspace ID. The wsIDRedirects map ensures those messages are routed to the migrated
// tabs. Restarting readers would race with the still-blocked inner read goroutine and
// corrupt output.
func (m *Model) MigrateWorkspaceTabs(oldID, newID string, ws *data.Workspace, oldName, newName string) {
	oldPrefix := tmux.SessionName("medusa", oldName) + "-"
	newPrefix := tmux.SessionName("medusa", newName) + "-"

	if tabs, ok := m.tabsByWorkspace[oldID]; ok {
		for _, tab := range tabs {
			if tab != nil {
				tab.Workspace = ws
				// Mark tab as needing restart — the agent's shell still uses the old directory.
				tab.WorkspaceRenamed = true
				// Update tmux session name to match the renamed session.
				if strings.HasPrefix(tab.SessionName, oldPrefix) {
					tab.SessionName = newPrefix + strings.TrimPrefix(tab.SessionName, oldPrefix)
				}
				if tab.Agent != nil {
					if strings.HasPrefix(tab.Agent.Session, oldPrefix) {
						tab.Agent.Session = newPrefix + strings.TrimPrefix(tab.Agent.Session, oldPrefix)
					}
				}
			}
		}
		m.tabsByWorkspace[newID] = tabs
		delete(m.tabsByWorkspace, oldID)
	}
	if idx, ok := m.activeTabByWorkspace[oldID]; ok {
		m.activeTabByWorkspace[newID] = idx
		delete(m.activeTabByWorkspace, oldID)
	}
	if _, ok := m.restoredWorkspaces[oldID]; ok {
		m.restoredWorkspaces[newID] = struct{}{}
		delete(m.restoredWorkspaces, oldID)
	}
	// Redirect old workspace ID → new so PTY reader messages are routed correctly.
	// Also update any existing redirects that pointed to oldID (handles chained renames).
	for k, v := range m.wsIDRedirects {
		if v == oldID {
			m.wsIDRedirects[k] = newID
		}
	}
	m.wsIDRedirects[oldID] = newID
	if m.workspace != nil && string(m.workspace.ID()) == oldID {
		m.workspace = ws
	}
	m.noteTabsChanged()
}
