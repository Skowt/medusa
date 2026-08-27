package center

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
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
	// The workspace opens directly onto its remembered agent tab. That tab is
	// visible immediately, so acknowledge any completion that happened while
	// another workspace was selected just as an explicit tab selection would.
	if ws != nil {
		tabs := m.getTabs()
		idx := m.getActiveTabIdx()
		if idx >= 0 && idx < len(tabs) && tabs[idx] != nil {
			tabs[idx].Unread = false
		}
	}
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

// RenameTabSessions rewrites the tmux session names recorded for a workspace's
// tabs, given the old-to-new mapping the rename actually performed. Only
// sessions tmux confirmed are passed in, so what the tab believes it is attached
// to and what exists on the server stay the same thing.
func (m *Model) RenameTabSessions(wsID string, renamed map[string]string) {
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab == nil {
			continue
		}
		if newName, ok := renamed[tab.SessionName]; ok {
			tab.SessionName = newName
		}
		if tab.Agent != nil {
			if newName, ok := renamed[tab.Agent.Session]; ok {
				tab.Agent.Session = newName
			}
		}
	}
	m.recordTabRestoreOrderFromTabs(wsID, m.tabsByWorkspace[wsID])
}
