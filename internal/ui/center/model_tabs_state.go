package center

import (
	"path/filepath"
	"strings"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
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

// UpdateTabClaudeSessionID refreshes a tab's persisted Claude session id when a
// SessionStart hook reports a different live id (e.g. after /clear or an
// in-session /resume mints a new session). The tab is located by tmux session
// name; wsID follows the rename redirect map. Returns true if the id actually
// changed, so the caller can persist. Runs in the Update loop; the mutex write
// mirrors the tab's other ClaudeSessionID setter.
//
// cwd is the SessionStart payload's working directory. An id is adopted only
// when it was reported from inside the tab's own workspace: any process the
// tab spawns inherits MEDUSA_SESSION_NAME, so nested claude runs fire
// SessionStart under this same session name and would otherwise overwrite the
// tab's id with a session it can never resume. An empty cwd is accepted — hook
// emitters predating the field send none, and rejecting those would stop
// tracking /clear for anyone still running one.
func (m *Model) UpdateTabClaudeSessionID(wsID, sessionName, claudeSessionID, cwd string) bool {
	if claudeSessionID == "" {
		return false
	}
	tab := m.getTabBySession(wsID, sessionName)
	if tab == nil {
		return false
	}
	if !cwdWithinWorkspace(tab.Workspace, cwd) {
		logging.Info("Ignoring Claude session id %s for %s: cwd %q is outside the workspace", claudeSessionID, sessionName, cwd)
		return false
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.ClaudeSessionID == claudeSessionID {
		return false
	}
	tab.ClaudeSessionID = claudeSessionID
	return true
}

// cwdWithinWorkspace reports whether cwd is one of the workspace's worktree
// roots or a directory inside one. Both sides are resolved through symlinks
// first: Claude Code reports the cwd it resolved (/private/tmp on macOS, say)
// while the registry may hold the symlinked form, and a spurious mismatch here
// would silently stop session-id tracking.
//
// Unverifiable inputs are accepted rather than rejected: an empty cwd, a nil
// workspace, or a workspace with no worktrees carries no evidence either way,
// and refusing those would regress /clear tracking to nothing.
func cwdWithinWorkspace(ws *data.Workspace, cwd string) bool {
	if cwd == "" || ws == nil {
		return true
	}
	roots := ws.AllRoots()
	if len(roots) == 0 {
		return true
	}
	target := resolveDir(cwd)
	for _, root := range roots {
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(resolveDir(root), target)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

// resolveDir cleans path and follows symlinks through its deepest existing
// ancestor, re-appending whatever does not exist yet. Resolving only whole
// paths would compare a resolved root against an unresolved cwd (or the
// reverse) whenever either one is missing — on macOS every path under /var or
// /tmp resolves elsewhere, so that mismatch is the common case, not the edge.
func resolveDir(path string) string {
	cleaned := filepath.Clean(path)
	rest := ""
	for dir := cleaned; ; {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			if rest == "" {
				return resolved
			}
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cleaned
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
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

// appendTabOrdered adds tab to the workspace's tab list, maintaining the
// invariant that script tabs are always last so the bar always reads
// Info → agents → scripts. Non-script tabs inserted while script tabs are
// already present shift those scripts right and bump the active-tab pointer
// accordingly. Returns the index at which the new tab was placed.
func (m *Model) appendTabOrdered(wsID string, tab *Tab) int {
	tabs := m.tabsByWorkspace[wsID]
	if tab == nil || tab.Assistant == "script" {
		m.tabsByWorkspace[wsID] = append(tabs, tab)
		return len(m.tabsByWorkspace[wsID]) - 1
	}
	firstScriptIdx := -1
	for i, t := range tabs {
		if t != nil && t.Assistant == "script" {
			firstScriptIdx = i
			break
		}
	}
	if firstScriptIdx < 0 {
		m.tabsByWorkspace[wsID] = append(tabs, tab)
		return len(m.tabsByWorkspace[wsID]) - 1
	}
	if idx, ok := m.activeTabByWorkspace[wsID]; ok && idx >= firstScriptIdx {
		m.activeTabByWorkspace[wsID] = idx + 1
	}
	newTabs := make([]*Tab, len(tabs)+1)
	copy(newTabs, tabs[:firstScriptIdx])
	newTabs[firstScriptIdx] = tab
	copy(newTabs[firstScriptIdx+1:], tabs[firstScriptIdx:])
	m.tabsByWorkspace[wsID] = newTabs
	return firstScriptIdx
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
	delete(m.restoredWorkspaces, wsID)
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
