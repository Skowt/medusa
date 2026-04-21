package dashboard

import (
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

func TestRebuildRows_NoGroups_FlatListNoHeaders(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("alpha", "", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("beta", "", []string{"medusa"}, time.Unix(2, 0)),
	}
	m.rebuildRows()

	var headers int
	for _, r := range m.rows {
		if r.Type == RowSectionHeader {
			headers++
		}
	}
	if headers != 0 {
		t.Fatalf("expected no section headers when no groups exist, got %d", headers)
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
