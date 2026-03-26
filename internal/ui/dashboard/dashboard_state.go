package dashboard

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/ui/common"
)

// tickSpinner returns a command that ticks the spinner
func (m *Model) tickSpinner() tea.Cmd {
	return common.SafeTick(spinnerInterval, func(t time.Time) tea.Msg {
		return SpinnerTickMsg{}
	})
}

// startSpinnerIfNeeded starts spinner ticks if we have pending activity or running agents.
func (m *Model) startSpinnerIfNeeded() tea.Cmd {
	if m.spinnerActive {
		return nil
	}
	if len(m.creatingWorkspaces) == 0 && len(m.deletingWorkspaces) == 0 && !m.hasActiveAgents() {
		return nil
	}
	m.spinnerActive = true
	return m.tickSpinner()
}

// hasActiveAgents returns true if any workspace has an actively processing agent
// (detected via hook lifecycle events).
func (m *Model) hasActiveAgents() bool {
	for _, state := range m.hookStates {
		if state == "PreToolUse" || state == "PostToolUse" || state == "UserPromptSubmit" {
			return true
		}
	}
	return false
}

// StartSpinnerIfNeeded is the public version for external callers.
func (m *Model) StartSpinnerIfNeeded() tea.Cmd {
	return m.startSpinnerIfNeeded()
}

// SetWorkspaceCreating marks a workspace as creating (or clears it).
func (m *Model) SetWorkspaceCreating(ws *data.Workspace, creating bool) tea.Cmd {
	if ws == nil {
		return nil
	}
	if creating {
		m.creatingWorkspaces[ws.Root()] = ws
		m.rebuildRows()
		for i, row := range m.rows {
			if row.Type == RowWorkspace && row.Workspace != nil && row.Workspace.Root() == ws.Root() {
				m.cursor = i
				break
			}
		}
		return m.startSpinnerIfNeeded()
	}
	delete(m.creatingWorkspaces, ws.Root())
	m.rebuildRows()
	return nil
}

// SetWorkspaceDeleting marks a workspace as deleting (or clears it).
func (m *Model) SetWorkspaceDeleting(root string, deleting bool) tea.Cmd {
	if deleting {
		m.deletingWorkspaces[root] = true
		return m.startSpinnerIfNeeded()
	}
	delete(m.deletingWorkspaces, root)
	return nil
}

// rebuildRows rebuilds the row list from workspaces
func (m *Model) rebuildRows() {
	// Remember the workspace at the current cursor so we can re-anchor after rebuild.
	var prevCursorRoot string
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		if row := m.rows[m.cursor]; row.Type == RowWorkspace && row.Workspace != nil {
			prevCursorRoot = row.Workspace.Root()
		}
	}

	m.rows = []Row{
		{Type: RowHome},
	}

	// Separate orphaned and archived workspaces from normal ones
	var orphans []*data.Workspace
	var archived []*data.Workspace
	all := make([]*data.Workspace, 0, len(m.workspaces)+len(m.creatingWorkspaces))
	existingRoots := make(map[string]bool)
	for _, ws := range m.workspaces {
		if ws.Archived() {
			archived = append(archived, ws)
			continue
		}
		if ws.IsOrphaned() {
			orphans = append(orphans, ws)
			continue
		}
		existingRoots[ws.Root()] = true
		all = append(all, ws)
	}

	// Add creating workspaces that aren't in the list yet
	for _, ws := range m.creatingWorkspaces {
		if ws == nil || existingRoots[ws.Root()] {
			continue
		}
		all = append(all, ws)
	}

	// Sort by Created ascending (oldest first, newest at bottom)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Created.Equal(all[j].Created) {
			return all[i].Name < all[j].Name
		}
		return all[i].Created.Before(all[j].Created)
	})

	// Group workspaces by repo name(s): single-repo by repo name,
	// multi-repo by sorted comma-joined repo names (truncated to 15 chars).
	repoGroups := make(map[string][]*data.Workspace) // group key -> workspaces
	groupLabels := make(map[string]string)            // group key -> display label
	var groupOrder []string                           // first-seen order of keys

	for _, ws := range all {
		var key, label string
		if len(ws.Repos) == 0 {
			key = "other"
			label = "other"
		} else {
			names := make([]string, len(ws.Repos))
			for i, r := range ws.Repos {
				names[i] = r.Name
			}
			sort.Strings(names)
			label = strings.Join(names, ", ")
			if len(label) > 15 {
				label = label[:15] + "..."
			}
			key = strings.Join(names, ",") // stable key (no truncation)
		}
		if _, seen := repoGroups[key]; !seen {
			groupOrder = append(groupOrder, key)
			groupLabels[key] = label
		}
		repoGroups[key] = append(repoGroups[key], ws)
	}

	sort.Strings(groupOrder)

	for _, key := range groupOrder {
		groupWs := repoGroups[key]
		m.rows = append(m.rows, Row{Type: RowSectionHeader, Label: groupLabels[key]})
		for _, ws := range groupWs {
			m.rows = append(m.rows, Row{
				Type:      RowWorkspace,
				Workspace: ws,
			})
		}
		lastWs := groupWs[len(groupWs)-1]
		m.rows = append(m.rows, Row{
			Type:             RowQuickDuplicate,
			GroupRepos:       lastWs.Repos,
			GroupProfile:     lastWs.Profile,
			GroupCopyIgnored: lastWs.CopyIgnored,
		})
		m.rows = append(m.rows, Row{Type: RowSpacer})
	}

	// Orphans section
	if len(orphans) > 0 {
		m.rows = append(m.rows, Row{Type: RowSectionHeader, Label: "orphans"})
		for _, ws := range orphans {
			m.rows = append(m.rows, Row{
				Type:      RowWorkspace,
				Workspace: ws,
			})
		}
		m.rows = append(m.rows, Row{Type: RowSpacer})
	}

	m.rows = append(m.rows, Row{Type: RowCreate})

	// Archived section (below "+ New Workspace" with a divider)
	if len(archived) > 0 {
		// Sort by ArchivedAt descending (newest first)
		sort.Slice(archived, func(i, j int) bool {
			return archived[i].ArchivedAt.After(archived[j].ArchivedAt)
		})
		m.rows = append(m.rows, Row{Type: RowSectionHeader, Label: "archived"})
		for _, ws := range archived {
			m.rows = append(m.rows, Row{
				Type:      RowWorkspace,
				Workspace: ws,
			})
		}
		m.rows = append(m.rows, Row{Type: RowSectionHeader, Label: "archived-footer"})
	}

	// Try to re-anchor cursor to the previously selected workspace.
	if prevCursorRoot != "" {
		found := false
		for i, row := range m.rows {
			if row.Type == RowWorkspace && row.Workspace != nil && row.Workspace.Root() == prevCursorRoot {
				m.cursor = i
				found = true
				break
			}
		}
		if !found {
			// Workspace was removed — clamp index so it lands on a neighbor.
			if m.cursor >= len(m.rows) {
				m.cursor = len(m.rows) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
			if len(m.rows) > 0 && !isSelectable(m.rows[m.cursor].Type) {
				if next := m.findSelectableRow(m.cursor, -1); next != -1 {
					m.cursor = next
				} else if next := m.findSelectableRow(m.cursor, 1); next != -1 {
					m.cursor = next
				}
			}
		}
	} else {
		// No previous workspace — just clamp.
		if m.cursor >= len(m.rows) {
			m.cursor = len(m.rows) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		if len(m.rows) > 0 && !isSelectable(m.rows[m.cursor].Type) {
			if next := m.findSelectableRow(m.cursor, 1); next != -1 {
				m.cursor = next
			} else if prev := m.findSelectableRow(m.cursor, -1); prev != -1 {
				m.cursor = prev
			}
		}
	}

	m.clampScrollOffset()
}

// clampScrollOffset ensures scrollOffset stays within valid bounds.
// archivedSectionStart returns the row index where the archived section begins,
// or -1 if there is no archived section.
func (m *Model) archivedSectionStart() int {
	for i, row := range m.rows {
		if row.Type == RowSectionHeader && row.Label == "archived" {
			return i
		}
	}
	return -1
}

// archivedSectionLineCount returns total display lines for the archived section.
func (m *Model) archivedSectionLineCount() int {
	start := m.archivedSectionStart()
	if start < 0 {
		return 0
	}
	total := 0
	for i := start; i < len(m.rows); i++ {
		total += m.rowLineCount(i)
	}
	return total
}

func (m *Model) clampScrollOffset() {
	archivedStart := m.archivedSectionStart()
	mainRowEnd := len(m.rows)
	if archivedStart >= 0 {
		mainRowEnd = archivedStart
	}
	mainLines := 0
	for i := 0; i < mainRowEnd; i++ {
		mainLines += m.rowLineCount(i)
	}
	archivedLines := m.archivedSectionLineCount()
	scrollableHeight := m.visibleHeight() - archivedLines
	if scrollableHeight < 1 {
		scrollableHeight = 1
	}
	maxOffset := mainLines - scrollableHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}
