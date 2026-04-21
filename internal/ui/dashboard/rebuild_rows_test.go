package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/data"
)

func mkWS(name, group string, repos []string, created time.Time) *data.Workspace {
	refs := make([]data.RepoRef, len(repos))
	worktrees := make([]data.WorktreeRef, len(repos))
	for i, r := range repos {
		refs[i] = data.RepoRef{Name: r, Path: "/src/" + r}
		worktrees[i] = data.WorktreeRef{Root: "/wt/" + name + "/" + r, Branch: name, Base: "main"}
	}
	return &data.Workspace{
		Name:      name,
		Group:     group,
		Repos:     refs,
		Worktrees: worktrees,
		Created:   created,
	}
}

func TestRebuildRows_NoGroups_UngroupedHeaderAlwaysVisible(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("alpha", "", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("beta", "", []string{"medusa"}, time.Unix(2, 0)),
	}
	m.rebuildRows()

	var gotLabels []string
	for _, r := range m.rows {
		if r.Type == RowSectionHeader && r.IsUserGroup {
			gotLabels = append(gotLabels, r.Label)
		}
	}
	if len(gotLabels) != 1 || gotLabels[0] != "Ungrouped" {
		t.Fatalf("expected a single [Ungrouped] header when no named groups exist, got %v", gotLabels)
	}
}

func TestRebuildRows_OneGroup_UngroupedTrails(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("tagged", "shipping", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("untagged", "", []string{"medusa"}, time.Unix(2, 0)),
	}
	m.rebuildRows()

	var gotLabels []string
	for _, r := range m.rows {
		if r.Type == RowSectionHeader && r.IsUserGroup {
			gotLabels = append(gotLabels, r.Label)
		}
	}
	if len(gotLabels) != 2 || gotLabels[0] != "shipping" || gotLabels[1] != "Ungrouped" {
		t.Fatalf("expected [shipping, Ungrouped], got %v", gotLabels)
	}
}

func TestRebuildRows_MultipleGroups_FirstUseOrder(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("a", "zebra", []string{"medusa"}, time.Unix(10, 0)),
		mkWS("b", "apple", []string{"medusa"}, time.Unix(20, 0)),
	}
	m.rebuildRows()

	var order []string
	for _, r := range m.rows {
		if r.Type == RowSectionHeader && r.IsUserGroup {
			order = append(order, r.Label)
		}
	}
	if len(order) != 2 || order[0] != "zebra" || order[1] != "apple" {
		t.Fatalf("expected first-use order [zebra, apple], got %v", order)
	}
}

func TestRebuildRows_Collapsed_EmitsHeaderOnlyWithCount(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("x", "shipping", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("y", "shipping", []string{"medusa"}, time.Unix(2, 0)),
	}
	m.collapsedGroups = map[string]bool{"shipping": true}
	m.rebuildRows()

	var header Row
	var wsCount int
	for _, r := range m.rows {
		if r.Type == RowSectionHeader && r.Label == "shipping" {
			header = r
		}
		if r.Type == RowWorkspace {
			wsCount++
		}
	}
	if !header.Collapsed || header.MemberCount != 2 {
		t.Errorf("expected Collapsed=true MemberCount=2, got %+v", header)
	}
	if wsCount != 0 {
		t.Errorf("expected no workspace rows when group collapsed, got %d", wsCount)
	}
}

func TestRebuildRows_WithinGroup_SortedByRepoNames(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("z", "g", []string{"zulu"}, time.Unix(1, 0)),
		mkWS("a", "g", []string{"alpha"}, time.Unix(2, 0)),
		mkWS("m", "g", []string{"alpha", "mike"}, time.Unix(3, 0)),
	}
	m.rebuildRows()

	var names []string
	for _, r := range m.rows {
		if r.Type == RowWorkspace {
			names = append(names, r.Workspace.Name)
		}
	}
	if len(names) != 3 || names[0] != "a" || names[1] != "m" || names[2] != "z" {
		t.Fatalf("expected within-group sort [a, m, z], got %v", names)
	}
}

func TestRenderWorkspaceRow_MultiRepoSelected_ShowsThreeLines(t *testing.T) {
	m := New()
	m.width = 40
	m.workspaces = []*data.Workspace{
		mkWS("alpha", "", []string{"medusa", "billing"}, time.Unix(1, 0)),
	}
	m.rebuildRows()
	wsIdx := -1
	for i, r := range m.rows {
		if r.Type == RowWorkspace {
			wsIdx = i
			break
		}
	}
	if wsIdx == -1 {
		t.Fatal("expected a workspace row")
	}
	m.cursor = wsIdx

	out := m.renderWorkspaceRow(m.rows[wsIdx], true)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines for selected multi-repo, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[2], "billing") {
		t.Errorf("line 3 missing repo: %q", lines[2])
	}
}

func TestRenderWorkspaceRow_SingleRepoSelected_StaysTwoLines(t *testing.T) {
	m := New()
	m.width = 40
	m.workspaces = []*data.Workspace{
		mkWS("alpha", "", []string{"medusa"}, time.Unix(1, 0)),
	}
	m.rebuildRows()
	wsIdx := -1
	for i, r := range m.rows {
		if r.Type == RowWorkspace {
			wsIdx = i
			break
		}
	}
	m.cursor = wsIdx

	out := m.renderWorkspaceRow(m.rows[wsIdx], true)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines for selected single-repo, got %d:\n%s", len(lines), out)
	}
}

func TestRebuildRows_GroupsWithArchivedAndOrphanSections(t *testing.T) {
	m := New()
	active := mkWS("a", "shipping", []string{"medusa"}, time.Unix(1, 0))
	archived := mkWS("old", "", []string{"medusa"}, time.Unix(2, 0))
	archived.Status = data.StatusArchived
	archived.ArchivedAt = time.Unix(100, 0)
	orphan := mkWS("ghost", "", []string{"medusa"}, time.Unix(3, 0))
	orphan.Orphan = data.OrphanMetadata

	m.workspaces = []*data.Workspace{active, archived, orphan}
	m.rebuildRows()

	// Find indices of key rows.
	idx := map[string]int{}
	for i, r := range m.rows {
		switch {
		case r.Type == RowSectionHeader && r.Label == "shipping":
			idx["shipping"] = i
		case r.Type == RowSectionHeader && r.Label == "orphans":
			idx["orphans"] = i
		case r.Type == RowCreate:
			idx["create"] = i
		case r.Type == RowSectionHeader && r.Label == "archived":
			idx["archived"] = i
		}
	}
	if _, ok := idx["shipping"]; !ok {
		t.Fatalf("missing shipping group header: %v", idx)
	}
	if _, ok := idx["create"]; !ok {
		t.Fatalf("missing create row: %v", idx)
	}
	// Ordering: user group -> orphans -> create -> archived.
	if orph, ok := idx["orphans"]; ok && orph <= idx["shipping"] {
		t.Errorf("orphans must come after user group (orphans=%d shipping=%d)", orph, idx["shipping"])
	}
	if orph, ok := idx["orphans"]; ok && orph >= idx["create"] {
		t.Errorf("orphans must come before create (orphans=%d create=%d)", orph, idx["create"])
	}
	if arch, ok := idx["archived"]; ok && arch <= idx["create"] {
		t.Errorf("archived must come after create (archived=%d create=%d)", arch, idx["create"])
	}
}

func TestRowLineCount_SelectedMultiRepo(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("alpha", "", []string{"medusa", "billing"}, time.Unix(1, 0)),
	}
	m.rebuildRows()
	wsIdx := -1
	for i, r := range m.rows {
		if r.Type == RowWorkspace {
			wsIdx = i
			break
		}
	}
	m.cursor = wsIdx
	if got := m.rowLineCount(wsIdx); got != 3 {
		t.Errorf("rowLineCount selected multi-repo = %d, want 3", got)
	}

	m.cursor = -1
	if got := m.rowLineCount(wsIdx); got != 2 {
		t.Errorf("rowLineCount non-selected multi-repo = %d, want 2", got)
	}
}
