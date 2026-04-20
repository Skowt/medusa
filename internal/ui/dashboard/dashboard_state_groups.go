package dashboard

import (
	"github.com/Skowt/medusa/internal/data"
)

// appendRepoSection emits rows for a single repo-scope section:
// repo header, user-defined groups (each collapsible), then ungrouped workspaces,
// followed by a quick-duplicate button and a spacer.
// Registry-declared groups appear first in their stored order, even when empty.
// Groups that exist only on workspaces (orphaned references) are appended after.
func (m *Model) appendRepoSection(repoKey, repoLabel string, workspaces []*data.Workspace) {
	m.rows = append(m.rows, Row{Type: RowSectionHeader, Label: repoLabel})

	grouped, ungrouped, orphanGroupOrder := partitionByGroup(workspaces)

	declared := m.groupsForScope(repoKey)
	declaredSeen := make(map[string]bool, len(declared))

	// 1. Declared groups in their stored order.
	for _, g := range declared {
		declaredSeen[g.Name] = true
		members := grouped[g.Name]
		m.rows = append(m.rows, Row{
			Type:          RowGroupHeader,
			GroupName:     g.Name,
			GroupRepoKey:  repoKey,
			GroupExpanded: g.Expanded,
			GroupCount:    len(members),
		})
		if g.Expanded {
			for _, ws := range members {
				m.rows = append(m.rows, Row{
					Type:         RowWorkspace,
					Workspace:    ws,
					GroupName:    g.Name,
					GroupRepoKey: repoKey,
				})
			}
		}
	}

	// 2. Orphan group references (workspace.Group set to a name not in the registry).
	// Surface them so the user can see their workspaces and assign/remove the group.
	for _, name := range orphanGroupOrder {
		if declaredSeen[name] {
			continue
		}
		members := grouped[name]
		m.rows = append(m.rows, Row{
			Type:          RowGroupHeader,
			GroupName:     name,
			GroupRepoKey:  repoKey,
			GroupExpanded: true,
			GroupCount:    len(members),
		})
		for _, ws := range members {
			m.rows = append(m.rows, Row{
				Type:         RowWorkspace,
				Workspace:    ws,
				GroupName:    name,
				GroupRepoKey: repoKey,
			})
		}
	}

	// 3. Ungrouped workspaces directly under the repo header.
	for _, ws := range ungrouped {
		m.rows = append(m.rows, Row{
			Type:      RowWorkspace,
			Workspace: ws,
		})
	}

	// 4. Quick-duplicate based on the last visible workspace in the section.
	if last := lastVisibleWorkspace(workspaces); last != nil {
		m.rows = append(m.rows, Row{
			Type:             RowQuickDuplicate,
			GroupRepos:       last.Repos,
			GroupProfile:     last.Profile,
			GroupCopyIgnored: last.CopyIgnored,
		})
	}
	// 5. "+ New Group" CTA for this repo scope.
	m.rows = append(m.rows, Row{
		Type:         RowCreateGroup,
		GroupRepoKey: repoKey,
	})
	m.rows = append(m.rows, Row{Type: RowSpacer})
}

// partitionByGroup splits workspaces into grouped (by Group field) and ungrouped.
// orphanGroupOrder returns group names in first-seen order — used to render
// workspace.Group references that don't exist in the registry.
func partitionByGroup(workspaces []*data.Workspace) (grouped map[string][]*data.Workspace, ungrouped []*data.Workspace, orphanGroupOrder []string) {
	grouped = make(map[string][]*data.Workspace)
	seen := make(map[string]bool)
	for _, ws := range workspaces {
		if ws.Group == "" {
			ungrouped = append(ungrouped, ws)
			continue
		}
		if !seen[ws.Group] {
			seen[ws.Group] = true
			orphanGroupOrder = append(orphanGroupOrder, ws.Group)
		}
		grouped[ws.Group] = append(grouped[ws.Group], ws)
	}
	return grouped, ungrouped, orphanGroupOrder
}

// lastVisibleWorkspace returns the workspace whose config seeds "Quick Duplicate".
// Prefer the last ungrouped workspace (matches pre-groups behavior); fall back to
// the last workspace in the slice.
func lastVisibleWorkspace(workspaces []*data.Workspace) *data.Workspace {
	for i := len(workspaces) - 1; i >= 0; i-- {
		if workspaces[i].Group == "" {
			return workspaces[i]
		}
	}
	if len(workspaces) == 0 {
		return nil
	}
	return workspaces[len(workspaces)-1]
}
