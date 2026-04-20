package dashboard

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// currentRow returns a pointer to the row at the cursor, or nil if out of range.
func (m *Model) currentRow() *Row {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return &m.rows[m.cursor]
}

// findParentGroupHeader walks upward from idx to find the RowGroupHeader whose
// GroupName/GroupRepoKey matches the row at idx. Returns -1 if not found.
func (m *Model) findParentGroupHeader(idx int) int {
	if idx < 0 || idx >= len(m.rows) {
		return -1
	}
	target := m.rows[idx]
	if target.GroupName == "" {
		return -1
	}
	for i := idx - 1; i >= 0; i-- {
		r := m.rows[i]
		if r.Type == RowGroupHeader && r.GroupName == target.GroupName && r.GroupRepoKey == target.GroupRepoKey {
			return i
		}
	}
	return -1
}

// handleToggleGroup toggles the expanded state of the group at the cursor.
// Returns nil if the cursor is not on a group header.
func (m *Model) handleToggleGroup() tea.Cmd {
	row := m.currentRow()
	if row == nil || row.Type != RowGroupHeader {
		return nil
	}
	name := row.GroupName
	repoKey := row.GroupRepoKey
	return func() tea.Msg {
		return messages.ToggleGroupExpanded{Name: name, RepoKey: repoKey}
	}
}

// handleRenameGroup requests the rename dialog for the group at the cursor.
func (m *Model) handleRenameGroup() tea.Cmd {
	row := m.currentRow()
	if row == nil || row.Type != RowGroupHeader {
		return nil
	}
	name := row.GroupName
	repoKey := row.GroupRepoKey
	return func() tea.Msg {
		return messages.ShowRenameGroupDialog{Name: name, RepoKey: repoKey}
	}
}

// handleDeleteGroup requests the delete-confirm dialog for the group at the cursor.
func (m *Model) handleDeleteGroup() tea.Cmd {
	row := m.currentRow()
	if row == nil || row.Type != RowGroupHeader {
		return nil
	}
	name := row.GroupName
	repoKey := row.GroupRepoKey
	return func() tea.Msg {
		return messages.ShowDeleteGroupDialog{Name: name, RepoKey: repoKey}
	}
}

// handleAssignWorkspaceGroup requests the assign-to-group select dialog for the workspace at the cursor.
func (m *Model) handleAssignWorkspaceGroup() tea.Cmd {
	row := m.currentRow()
	if row == nil || row.Type != RowWorkspace || row.Workspace == nil {
		return nil
	}
	ws := row.Workspace
	if ws.Archived() || ws.IsOrphaned() {
		return nil
	}
	return func() tea.Msg {
		return messages.ShowAssignGroupDialog{Workspace: ws}
	}
}

// handleCreateGroup requests the create-group dialog. Scope is derived from the
// cursor: on a workspace or group header row, use that scope; otherwise fall
// back to the first repo section present, since a group without any repo
// context can't be rendered meaningfully.
func (m *Model) handleCreateGroup() tea.Cmd {
	repoKey := m.repoKeyAtCursor()
	if repoKey == "" {
		return nil
	}
	return func() tea.Msg {
		return messages.ShowCreateGroupDialog{RepoKey: repoKey}
	}
}

// repoKeyAtCursor returns the repo scope key associated with the current cursor
// row, or the first repo scope in the row list if the cursor is not on a
// repo-scoped row.
func (m *Model) repoKeyAtCursor() string {
	if row := m.currentRow(); row != nil {
		switch row.Type {
		case RowWorkspace:
			if row.Workspace != nil {
				return data.RepoKeyFor(row.Workspace)
			}
		case RowGroupHeader:
			return row.GroupRepoKey
		case RowQuickDuplicate:
			if len(row.GroupRepos) > 0 {
				// Build a synthetic workspace just to reuse RepoKeyFor semantics.
				ws := &data.Workspace{Repos: row.GroupRepos}
				return data.RepoKeyFor(ws)
			}
		}
	}
	// Walk rows for any workspace to infer a default scope.
	for _, r := range m.rows {
		if r.Type == RowWorkspace && r.Workspace != nil {
			return data.RepoKeyFor(r.Workspace)
		}
		if r.Type == RowGroupHeader {
			return r.GroupRepoKey
		}
	}
	return ""
}

// groupsForScope returns the registry groups that belong to repoKey, in Order.
func (m *Model) groupsForScope(repoKey string) []data.RegistryGroup {
	var out []data.RegistryGroup
	for _, g := range m.groups {
		if g.RepoKey == repoKey {
			out = append(out, g)
		}
	}
	// Sort by Order ascending. The slice is typically very small, so use a simple insertion sort.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Order > out[j].Order; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// groupExpanded reports whether a group is expanded. Groups unknown to the registry default to expanded.
func (m *Model) groupExpanded(name, repoKey string) bool {
	for _, g := range m.groups {
		if g.Name == name && g.RepoKey == repoKey {
			return g.Expanded
		}
	}
	return true
}
