package center

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/hooks"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/ui/compositor"
)

// HasRunningAgents returns whether any tab has an active agent across workspaces.
func (m *Model) HasRunningAgents() bool {
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab.isClosed() {
				continue
			}
			if !m.isChatTab(tab) {
				continue
			}
			if tab.Running {
				return true
			}
		}
	}
	return false
}

// HasActiveAgents returns whether any tab has emitted output recently.
// This is used to drive UI activity indicators without relying on process liveness alone.
func (m *Model) HasActiveAgents() bool {
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if m.IsTabActive(tab) {
				return true
			}
		}
	}
	return false
}

// IsTabActive reports hook-confirmed processing activity. PTY bytes are not a
// lifecycle signal: tmux repaint traffic can be emitted for unrelated tabs.
func (m *Model) IsTabActive(tab *Tab) bool {
	if tab == nil {
		return false
	}
	if tab.isClosed() {
		return false
	}
	if !m.isChatTab(tab) {
		return false
	}
	if tab.Detached || !tab.Running {
		return false
	}
	return hooks.IsActiveEvent(hooks.EventType(tab.HookState))
}

// HasActiveAgentsInWorkspace returns whether any tab in a workspace is actively outputting.
func (m *Model) HasActiveAgentsInWorkspace(wsID string) bool {
	for _, tab := range m.tabsByWorkspace[wsID] {
		if m.IsTabActive(tab) {
			return true
		}
	}
	return false
}

// HasRecentPTYOutput reports transport activity for stale-hook reconciliation
// only. It deliberately does not drive tab or workspace UI state.
func (m *Model) HasRecentPTYOutput(sessionName string, within time.Duration) bool {
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab != nil && tab.SessionName == sessionName && !tab.lastOutputAt.IsZero() && time.Since(tab.lastOutputAt) < within {
				return true
			}
		}
	}
	return false
}

// HasRunningTabsInWorkspace returns whether any tab in a workspace is running or detached.
// This is used to check if it's safe to modify the workspace's profile.
func (m *Model) HasRunningTabsInWorkspace(wsID string) bool {
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab == nil || tab.isClosed() {
			continue
		}
		// Any tab that is running OR detached means there's an active session
		// (detached tabs have a tmux session that's still alive)
		if tab.Running || tab.Detached {
			return true
		}
	}
	return false
}

// GetActiveWorkspaceIDs returns all workspace IDs with active agents.
func (m *Model) GetActiveWorkspaceIDs() []string {
	var active []string
	for wsID := range m.tabsByWorkspace {
		if m.HasActiveAgentsInWorkspace(wsID) {
			active = append(active, wsID)
		}
	}
	return active
}

// GetRunningWorkspaceIDs returns the IDs of all workspaces with running agents.
// This includes agents that are running but idle (waiting at prompt).
func (m *Model) GetRunningWorkspaceIDs() []string {
	var running []string
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if !m.isChatTab(tab) {
				continue
			}
			if tab.Running && tab.Workspace != nil {
				running = append(running, string(tab.Workspace.ID()))
				break // Only need one per workspace
			}
		}
	}
	return running
}

// Workspace agent state constants for GetWorkspaceAgentStates.
const (
	AgentStateIdle    = 0 // Has agent tabs but none running
	AgentStateRunning = 1 // At least one agent running but not actively processing
	AgentStateActive  = 2 // At least one agent actively processing (recent output)
)

// GetWorkspaceAgentStates returns a map of workspace ID → agent state.
// A key being present means the workspace has at least one agent tab.
// Values: AgentStateIdle (0), AgentStateRunning (1), AgentStateActive (2).
func (m *Model) GetWorkspaceAgentStates() map[string]int {
	states := make(map[string]int)
	for wsID, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab.isClosed() || !m.isChatTab(tab) {
				continue
			}
			// Workspace has at least one agent tab
			if _, exists := states[wsID]; !exists {
				states[wsID] = AgentStateIdle
			}
			if m.IsTabActive(tab) {
				states[wsID] = AgentStateActive
			} else if tab.Running && !tab.Detached && states[wsID] < AgentStateRunning {
				states[wsID] = AgentStateRunning
			}
		}
	}
	return states
}

func (m *Model) isChatTab(tab *Tab) bool {
	if tab == nil {
		return false
	}
	if tab.DiffViewer != nil {
		return false
	}
	if m != nil && m.config != nil && len(m.config.Assistants) > 0 {
		_, ok := m.config.Assistants[tab.Assistant]
		return ok
	}
	return tab.Assistant == "claude"
}

// StartPTYReaders starts reading from all PTYs across all workspaces. It runs
// on the PTY watchdog tick, so it is also where output whose signal was lost
// gets flushed, and where a tab whose reader had to be replaced is redrawn.
func (m *Model) StartPTYReaders() tea.Cmd {
	if m.isTabActorReady() {
		lastBeat := atomic.LoadInt64(&m.tabActorHeartbeat)
		if lastBeat > 0 && time.Since(time.Unix(0, lastBeat)) > tabActorStallTimeout {
			logging.Warn("tab actor stalled; clearing readiness for restart")
			atomic.StoreUint32(&m.tabActorReady, 0)
		}
	}
	var cmds []tea.Cmd
	for wtID, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab == nil || tab.isClosed() {
				continue
			}
			tab.mu.Lock()
			readerActive := tab.readerActive
			tab.mu.Unlock()
			if readerActive {
				lastBeat := atomic.LoadInt64(&tab.ptyHeartbeat)
				if lastBeat > 0 && time.Since(time.Unix(0, lastBeat)) > ptyReaderStallTimeout {
					logging.Warn("PTY reader stalled for tab %s; restarting", tab.ID)
					m.stopPTYReader(tab)
					// Whatever the stalled reader held is gone, and its
					// leftovers must not mix with the new reader's stream.
					// Start clean and have tmux redraw the whole screen.
					tab.output.Reset()
					cmds = append(cmds, m.redrawTabCmd(tab))
				}
			}
			_ = m.startPTYReader(wtID, tab)
			if tab.output.Len() > 0 && !tab.flushScheduled {
				// Output is waiting and no flush is coming: its signal was
				// dropped. Take the signal back so new output raises one again.
				tab.output.ClearSignal()
				cmds = append(cmds, m.scheduleFlush(wtID, tab))
			}
		}
	}
	return common.SafeBatch(cmds...)
}

// redrawTabCmd asks tmux to repaint the tab's whole screen, which puts a vterm
// that missed part of the stream back in step with the pane.
func (m *Model) redrawTabCmd(tab *Tab) tea.Cmd {
	tab.mu.Lock()
	sessionName := tab.SessionName
	if sessionName == "" && tab.Agent != nil {
		sessionName = tab.Agent.Session
	}
	tab.mu.Unlock()
	if sessionName == "" {
		return nil
	}
	opts := m.getTmuxOptions()
	return func() tea.Msg {
		if err := tmux.RefreshSessionClients(sessionName, opts); err != nil {
			logging.Warn("redrawing tmux clients of %s failed: %v", sessionName, err)
		}
		return nil
	}
}

// TerminalLayer returns a VTermLayer for the active terminal, if any.
// This creates a snapshot of the terminal state while holding the lock,
// so the returned layer can be safely used for rendering without locks.
// Uses snapshot caching to avoid recreating when terminal state unchanged.
func (m *Model) TerminalLayer() *compositor.VTermLayer {
	if m.infoTabActive {
		return nil
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Terminal == nil {
		return nil
	}
	// Check if we can reuse the cached snapshot
	version := tab.Terminal.Version()
	showCursor := m.focused
	if tab.cachedSnap != nil &&
		tab.cachedVersion == version &&
		tab.cachedShowCursor == showCursor {
		// Reuse cached snapshot
		return compositor.NewVTermLayer(tab.cachedSnap)
	}

	// Create new snapshot while holding the lock, reusing cached lines when possible.
	snap := compositor.NewVTermSnapshotWithCache(tab.Terminal, showCursor, tab.cachedSnap)
	if snap == nil {
		return nil
	}

	// Cache the snapshot
	tab.cachedSnap = snap
	tab.cachedVersion = version
	tab.cachedShowCursor = showCursor

	return compositor.NewVTermLayer(snap)
}
