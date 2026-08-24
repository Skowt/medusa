package dashboard

import (
	"sort"
	"strings"

	"github.com/Skowt/medusa/internal/data"
)

// sortWorkspacesForDisplay orders one section's members the way the dashboard
// shows them: workspaces the user has placed by hand first, in SortKey order,
// then everything never placed by hand, oldest first.
//
// Treating SortKey 0 as "unplaced" rather than "position zero" is what makes
// manual ordering additive: a registry that predates it sorts exactly as it did
// before, and a workspace created into an already-ordered group lands at the
// bottom instead of jumping to the top.
func sortWorkspacesForDisplay(members []*data.Workspace) {
	sort.SliceStable(members, func(i, j int) bool {
		a, b := members[i], members[j]
		if (a.SortKey == 0) != (b.SortKey == 0) {
			return b.SortKey == 0 // placed sorts before unplaced
		}
		if a.SortKey != b.SortKey {
			return a.SortKey < b.SortKey
		}
		if a.Created.Equal(b.Created) {
			return a.Name < b.Name
		}
		return a.Created.Before(b.Created)
	})
}

// SetGroupOrder replaces the manual group order (called from App when config
// loads and after a group is dragged). Keys are group labels, with "" standing
// for the Ungrouped section.
func (m *Model) SetGroupOrder(order []string) {
	m.groupOrder = order
	m.rebuildRows()
}

// sectionOrder returns the section keys to emit, in display order: keys the
// user has ordered by hand first, in that order, then the named groups they have
// never dragged, alphabetically. Ungrouped is always last and never
// participates — it is not a real group, so a manual position for it would only
// hide where the ungrouped workspaces are.
//
// Ungrouped is emitted even with no members, as long as there is something that
// could be dragged into it: an empty section still has to be a drop target, and
// a header that vanishes the moment its last workspace leaves cannot be one.
//
// The alphabetical fallback is deliberately a function of the label alone.
// Ordering by member timestamps made a group's position depend on which of its
// members were live, so archiving a group's oldest workspace reshuffled the
// pane. A group the user has never dragged keeps that property; one they have
// dragged holds the position they gave it, and a group created later appends
// after both.
func (m *Model) sectionOrder(groupMembers map[string][]*data.Workspace) []string {
	present := make(map[string]bool, len(groupMembers))
	hasUngrouped := len(groupMembers[""]) > 0
	for key, members := range groupMembers {
		if len(members) == 0 || key == "" {
			continue
		}
		present[key] = true
	}
	if len(present) > 0 {
		hasUngrouped = true
	}

	ordered := make([]string, 0, len(present)+1)
	seen := make(map[string]bool, len(present))
	for _, key := range m.groupOrder {
		if present[key] && !seen[key] {
			ordered = append(ordered, key)
			seen[key] = true
		}
	}

	rest := make([]string, 0, len(present))
	for key := range present {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		li, lj := strings.ToLower(rest[i]), strings.ToLower(rest[j])
		if li == lj {
			return rest[i] < rest[j]
		}
		return li < lj
	})

	ordered = append(ordered, rest...)
	if hasUngrouped {
		ordered = append(ordered, "")
	}
	return ordered
}

// currentSectionKeys returns every visible section key in display order,
// derived from the persisted workspaces alone. A group drag needs the same list
// rebuildRows emits, but must not depend on the rows: a collapsed group
// contributes no rows at all.
func (m *Model) currentSectionKeys() []string {
	groupMembers := make(map[string][]*data.Workspace)
	for _, ws := range m.workspaces {
		if !orderable(ws) {
			continue
		}
		groupMembers[ws.Group] = append(groupMembers[ws.Group], ws)
	}
	return m.sectionOrder(groupMembers)
}

// orderedGroupMembers returns the roots of one group's members in display
// order. Like currentSectionKeys it reads m.workspaces rather than m.rows, so a
// collapsed group is a valid drop target.
func (m *Model) orderedGroupMembers(group string) []string {
	var members []*data.Workspace
	for _, ws := range m.workspaces {
		if !orderable(ws) || ws.Group != group {
			continue
		}
		members = append(members, ws)
	}
	sortWorkspacesForDisplay(members)

	roots := make([]string, 0, len(members))
	for _, ws := range members {
		roots = append(roots, ws.Root())
	}
	return roots
}

// projectedGroupRoots returns the roots of group as a drag in progress would
// leave them: the dragged workspace removed from wherever it sits and inserted
// at its projected index. The commit reads this rather than the rendered rows so
// a drop into a collapsed group — which renders no member rows at all — commits
// the same order an expanded one would.
func (m *Model) projectedGroupRoots(group string, dragged string, idx int) []string {
	return insertAt(without(m.orderedGroupMembers(group), dragged), idx, dragged)
}

// projectDraggedWorkspace applies an in-progress workspace drag to the
// partition rebuildRows is about to emit, so the pane shows the order the
// release will commit rather than a hint about it. Rendering the projection is
// also what keeps the drag stable: the pointer resolves to the index the dragged
// row already occupies, which is a fixed point, so holding still holds still.
//
// The dragged workspace is returned when it is projected into a group that does
// not exist yet — a drop on "New group" — since there is no key to file it
// under until the release names one.
func (m *Model) projectDraggedWorkspace(groupMembers map[string][]*data.Workspace) *data.Workspace {
	if !m.drag.active || m.drag.kind != dragWorkspace || !m.drag.placed {
		return nil
	}

	var dragged *data.Workspace
	for key, members := range groupMembers {
		for i, ws := range members {
			if ws == nil || ws.Root() != m.drag.srcRoot {
				continue
			}
			dragged = ws
			groupMembers[key] = append(members[:i:i], members[i+1:]...)
			break
		}
		if dragged != nil {
			break
		}
	}
	if dragged == nil {
		return nil
	}
	if m.drag.placeNewGroup {
		return dragged
	}

	target := groupMembers[m.drag.placeGroup]
	idx := m.drag.placeIndex
	if idx < 0 {
		idx = 0
	}
	if idx > len(target) {
		idx = len(target)
	}
	out := make([]*data.Workspace, 0, len(target)+1)
	out = append(out, target[:idx]...)
	out = append(out, dragged)
	groupMembers[m.drag.placeGroup] = append(out, target[idx:]...)
	return nil
}

// projectDraggedGroup applies an in-progress group drag to the section order,
// clamped so Ungrouped keeps the bottom.
func (m *Model) projectDraggedGroup(keys []string) []string {
	if !m.drag.active || m.drag.kind != dragGroup || !m.drag.placed {
		return keys
	}
	last := len(keys) - 1
	if indexOf(keys, "") >= 0 {
		last--
	}
	idx := m.drag.placeIndex
	if idx > last {
		idx = last
	}
	if idx < 0 {
		idx = 0
	}
	return moveToIndex(keys, m.drag.srcGroup, idx)
}

// orderable reports whether a workspace participates in manual ordering.
// Archived and orphaned workspaces live in sections with their own sort, so
// they are neither drag sources nor drop targets.
func orderable(ws *data.Workspace) bool {
	return ws != nil && !ws.Archived() && !ws.IsOrphaned()
}

// workspaceByRoot finds a workspace by its root, or nil.
func (m *Model) workspaceByRoot(root string) *data.Workspace {
	if root == "" {
		return nil
	}
	for _, ws := range m.workspaces {
		if ws != nil && ws.Root() == root {
			return ws
		}
	}
	return nil
}

// moveToIndex returns items with move relocated to index idx. The index is read
// against the list that still contains move — the displayed order — so an index
// taken from what the user sees is the index the item ends up at, and re-reading
// the same position resolves to the same index. That fixed point is what keeps a
// live drag preview from oscillating between two placements.
func moveToIndex(items []string, move string, idx int) []string {
	if indexOf(items, move) < 0 {
		return items
	}
	return insertAt(without(items, move), idx, move)
}

func indexOf(items []string, want string) int {
	for i, item := range items {
		if item == want {
			return i
		}
	}
	return -1
}

func without(items []string, drop string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item != drop {
			out = append(out, item)
		}
	}
	return out
}

func insertAt(items []string, idx int, value string) []string {
	if idx < 0 {
		idx = 0
	}
	if idx > len(items) {
		idx = len(items)
	}
	out := make([]string, 0, len(items)+1)
	out = append(out, items[:idx]...)
	out = append(out, value)
	return append(out, items[idx:]...)
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
