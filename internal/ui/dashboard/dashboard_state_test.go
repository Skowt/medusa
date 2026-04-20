package dashboard

import (
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/data"
)

func newTestWorkspace(name, group, repoName string, created time.Time) *data.Workspace {
	return &data.Workspace{
		Name:      name,
		Group:     group,
		Created:   created,
		Repos:     []data.RepoRef{{Name: repoName, Path: "/tmp/" + repoName}},
		Worktrees: []data.WorktreeRef{{Root: "/tmp/" + name, Branch: name}},
	}
}

func findRow(rows []Row, pred func(Row) bool) (int, bool) {
	for i, r := range rows {
		if pred(r) {
			return i, true
		}
	}
	return -1, false
}

func TestRebuildRows_GroupedWorkspacesNestUnderHeader(t *testing.T) {
	m := New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.workspaces = []*data.Workspace{
		newTestWorkspace("ws-a", "Billing", "medusa", base),
		newTestWorkspace("ws-b", "Billing", "medusa", base.Add(time.Minute)),
		newTestWorkspace("ws-c", "", "medusa", base.Add(2*time.Minute)),
	}
	m.groups = []data.RegistryGroup{
		{Name: "Billing", RepoKey: "medusa", Expanded: true, Order: 0},
	}
	m.rebuildRows()

	headerIdx, ok := findRow(m.rows, func(r Row) bool {
		return r.Type == RowGroupHeader && r.GroupName == "Billing"
	})
	if !ok {
		t.Fatalf("expected a RowGroupHeader for Billing, rows=%+v", m.rows)
	}
	if !m.rows[headerIdx].GroupExpanded {
		t.Error("expected group to be expanded")
	}
	if m.rows[headerIdx].GroupCount != 2 {
		t.Errorf("expected GroupCount=2, got %d", m.rows[headerIdx].GroupCount)
	}

	// Members appear right after the header and carry GroupName.
	next := m.rows[headerIdx+1]
	if next.Type != RowWorkspace || next.Workspace == nil || next.Workspace.Name != "ws-a" || next.GroupName != "Billing" {
		t.Fatalf("expected ws-a under group header, got %+v", next)
	}

	// The ungrouped workspace is present and outside any group block.
	idxC, ok := findRow(m.rows, func(r Row) bool {
		return r.Type == RowWorkspace && r.Workspace != nil && r.Workspace.Name == "ws-c"
	})
	if !ok {
		t.Fatal("ungrouped workspace ws-c missing")
	}
	if m.rows[idxC].GroupName != "" {
		t.Errorf("expected ws-c to be ungrouped, got GroupName=%q", m.rows[idxC].GroupName)
	}
}

func TestRebuildRows_CollapsedGroupHidesMembers(t *testing.T) {
	m := New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.workspaces = []*data.Workspace{
		newTestWorkspace("ws-a", "Billing", "medusa", base),
		newTestWorkspace("ws-b", "Billing", "medusa", base.Add(time.Minute)),
	}
	m.groups = []data.RegistryGroup{
		{Name: "Billing", RepoKey: "medusa", Expanded: false, Order: 0},
	}
	m.rebuildRows()

	headerIdx, ok := findRow(m.rows, func(r Row) bool {
		return r.Type == RowGroupHeader
	})
	if !ok {
		t.Fatal("expected a RowGroupHeader")
	}
	if m.rows[headerIdx].GroupCount != 2 {
		t.Errorf("collapsed group should report count=2, got %d", m.rows[headerIdx].GroupCount)
	}

	for _, r := range m.rows {
		if r.Type == RowWorkspace && r.GroupName == "Billing" {
			t.Fatalf("workspace %q is still rendered under collapsed group", r.Workspace.Name)
		}
	}
}

func TestRebuildRows_CursorAnchorPersistsAcrossCollapseToggle(t *testing.T) {
	m := New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.workspaces = []*data.Workspace{
		newTestWorkspace("ws-a", "Billing", "medusa", base),
	}
	m.groups = []data.RegistryGroup{
		{Name: "Billing", RepoKey: "medusa", Expanded: true, Order: 0},
	}
	m.rebuildRows()

	headerIdx, ok := findRow(m.rows, func(r Row) bool { return r.Type == RowGroupHeader })
	if !ok {
		t.Fatal("expected a RowGroupHeader")
	}
	m.cursor = headerIdx

	// Collapse and rebuild; cursor should still be on the same group header.
	m.groups[0].Expanded = false
	m.rebuildRows()

	newIdx, ok := findRow(m.rows, func(r Row) bool {
		return r.Type == RowGroupHeader && r.GroupName == "Billing"
	})
	if !ok {
		t.Fatal("group header disappeared after collapse")
	}
	if m.cursor != newIdx {
		t.Errorf("cursor did not anchor to group header across rebuild: cursor=%d want=%d", m.cursor, newIdx)
	}
}

func TestRebuildRows_EmptyDeclaredGroupStillRendered(t *testing.T) {
	m := New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.workspaces = []*data.Workspace{
		newTestWorkspace("ws-a", "", "medusa", base),
	}
	m.groups = []data.RegistryGroup{
		{Name: "Empty", RepoKey: "medusa", Expanded: true, Order: 0},
	}
	m.rebuildRows()

	headerIdx, ok := findRow(m.rows, func(r Row) bool {
		return r.Type == RowGroupHeader && r.GroupName == "Empty"
	})
	if !ok {
		t.Fatal("expected empty group to still render a header so users can rename/delete it")
	}
	if m.rows[headerIdx].GroupCount != 0 {
		t.Errorf("expected GroupCount=0, got %d", m.rows[headerIdx].GroupCount)
	}
}

func TestRebuildRows_OrphanGroupReferenceRendered(t *testing.T) {
	// A workspace references a group name that is not in the registry; the dashboard
	// should still surface a header so the user can move the workspace out of it.
	m := New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.workspaces = []*data.Workspace{
		newTestWorkspace("ws-a", "Ghost", "medusa", base),
	}
	m.rebuildRows()

	_, ok := findRow(m.rows, func(r Row) bool {
		return r.Type == RowGroupHeader && r.GroupName == "Ghost"
	})
	if !ok {
		t.Fatal("expected orphan group reference to still render a header")
	}
}
