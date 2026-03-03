package dashboard

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/ui/common"
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

// hasActiveAgents returns true if any workspace has an actively processing agent.
func (m *Model) hasActiveAgents() bool {
	for _, active := range m.tmuxConfirmedActive {
		if active {
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
	m.rows = []Row{
		{Type: RowHome},
		{Type: RowSpacer},
	}

	// Collect all visible workspaces
	all := make([]*data.Workspace, 0, len(m.workspaces)+len(m.creatingWorkspaces))
	existingRoots := make(map[string]bool)
	for _, ws := range m.workspaces {
		if ws.Archived() {
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

	// Sort by StatusChanged (zero means never changed, sorts first), then by creation time
	sort.SliceStable(all, func(i, j int) bool {
		ti, tj := all[i].StatusChanged, all[j].StatusChanged
		if !ti.Equal(tj) {
			// Zero values (never changed) sort before non-zero (changed more recently = later)
			if ti.IsZero() != tj.IsZero() {
				return ti.IsZero()
			}
			return ti.Before(tj)
		}
		if all[i].Created.Equal(all[j].Created) {
			return all[i].Name < all[j].Name
		}
		return all[i].Created.Before(all[j].Created)
	})

	// Group by status: In Progress, Blocked, Merged
	type statusGroup struct {
		label string
		match func(data.WorkspaceStatus) bool
	}
	groups := []statusGroup{
		{"In Progress", func(s data.WorkspaceStatus) bool {
			return s == data.StatusNone || s == data.StatusStarted
		}},
		{"Blocked", func(s data.WorkspaceStatus) bool { return s == data.StatusBlocked }},
		{"Merged", func(s data.WorkspaceStatus) bool { return s == data.StatusMerged }},
	}

	for _, g := range groups {
		var groupWs []*data.Workspace
		for _, ws := range all {
			if g.match(ws.Status) {
				groupWs = append(groupWs, ws)
			}
		}
		if len(groupWs) == 0 {
			continue
		}
		m.rows = append(m.rows, Row{Type: RowSectionHeader, Label: g.label})
		m.rows = append(m.rows, Row{Type: RowSpacer})
		for _, ws := range groupWs {
			m.rows = append(m.rows, Row{
				Type:      RowWorkspace,
				Workspace: ws,
			})
		}
		m.rows = append(m.rows, Row{Type: RowSpacer})
	}

	m.rows = append(m.rows, Row{Type: RowCreate})

	// Clamp cursor
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

	m.clampScrollOffset()
}

// clampScrollOffset ensures scrollOffset stays within valid bounds.
func (m *Model) clampScrollOffset() {
	totalLines := 0
	for i := range m.rows {
		totalLines += m.rowLineCount(i)
	}
	maxOffset := totalLines - m.visibleHeight()
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
