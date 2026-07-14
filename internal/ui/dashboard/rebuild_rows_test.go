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

// A workspace still being created belongs in the group it was designated for,
// not in Ungrouped — the row must not move once creation finishes.
func TestRebuildRows_CreatingWorkspace_SitsInItsGroup(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("existing", "shipping", []string{"medusa"}, time.Unix(1, 0)),
	}
	pending := mkWS("pending", "shipping", []string{"medusa"}, time.Unix(2, 0))
	m.creatingWorkspaces[pending.Root()] = pending
	m.rebuildRows()

	var section string
	var members []string
	for _, r := range m.rows {
		switch r.Type {
		case RowSectionHeader:
			if r.IsUserGroup {
				section = r.Label
			}
		case RowWorkspace:
			if r.Workspace != nil && r.Workspace.Name == "pending" {
				members = append(members, section)
			}
		}
	}
	if len(members) != 1 || members[0] != "shipping" {
		t.Fatalf("creating workspace rendered under %v, want [shipping]", members)
	}
}

func groupHeaderOrder(m *Model) []string {
	var order []string
	for _, r := range m.rows {
		if r.Type == RowSectionHeader && r.IsUserGroup {
			order = append(order, r.Label)
		}
	}
	return order
}

func TestRebuildRows_MultipleGroups_AlphabeticalOrder(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("a", "zebra", []string{"medusa"}, time.Unix(10, 0)),
		mkWS("b", "apple", []string{"medusa"}, time.Unix(20, 0)),
	}
	m.rebuildRows()

	order := groupHeaderOrder(m)
	if len(order) != 2 || order[0] != "apple" || order[1] != "zebra" {
		t.Fatalf("expected alphabetical order [apple, zebra], got %v", order)
	}
}

func TestRebuildRows_GroupOrder_CaseInsensitive(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("a", "Zebra", []string{"medusa"}, time.Unix(10, 0)),
		mkWS("b", "apple", []string{"medusa"}, time.Unix(20, 0)),
	}
	m.rebuildRows()

	order := groupHeaderOrder(m)
	if len(order) != 2 || order[0] != "apple" || order[1] != "Zebra" {
		t.Fatalf("expected case-insensitive order [apple, Zebra], got %v", order)
	}
}

// Archiving a group's oldest member must not move the group: order is a
// function of the label alone, never of member creation times.
func TestRebuildRows_GroupOrder_StableAcrossArchive(t *testing.T) {
	oldest := mkWS("a", "zebra", []string{"medusa"}, time.Unix(10, 0))
	m := New()
	m.workspaces = []*data.Workspace{
		oldest,
		mkWS("b", "zebra", []string{"medusa"}, time.Unix(40, 0)),
		mkWS("c", "apple", []string{"medusa"}, time.Unix(20, 0)),
	}
	m.rebuildRows()
	before := groupHeaderOrder(m)

	oldest.Status = data.StatusArchived
	oldest.ArchivedAt = time.Unix(50, 0)
	m.rebuildRows()
	after := groupHeaderOrder(m)

	if len(before) != 2 || before[0] != "apple" || before[1] != "zebra" {
		t.Fatalf("expected [apple, zebra] before archive, got %v", before)
	}
	if len(after) != len(before) || after[0] != before[0] || after[1] != before[1] {
		t.Fatalf("group order shifted on archive: %v -> %v", before, after)
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

func workspaceRowNames(m *Model) []string {
	var names []string
	for _, r := range m.rows {
		if r.Type == RowWorkspace {
			names = append(names, r.Workspace.Name)
		}
	}
	return names
}

// Within a group, oldest workspace first — repo names must not influence order.
func TestRebuildRows_WithinGroup_SortedByCreatedOldestFirst(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("z", "g", []string{"zulu"}, time.Unix(1, 0)),
		mkWS("a", "g", []string{"alpha"}, time.Unix(2, 0)),
		mkWS("m", "g", []string{"alpha", "mike"}, time.Unix(3, 0)),
	}
	m.rebuildRows()

	names := workspaceRowNames(m)
	if len(names) != 3 || names[0] != "z" || names[1] != "a" || names[2] != "m" {
		t.Fatalf("expected oldest-first order [z, a, m], got %v", names)
	}
}

// Ungrouped uses the same comparator as a named group.
func TestRebuildRows_Ungrouped_SortedByCreatedOldestFirst(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("newest", "", []string{"alpha"}, time.Unix(30, 0)),
		mkWS("oldest", "", []string{"zulu"}, time.Unix(10, 0)),
		mkWS("middle", "", []string{"mike"}, time.Unix(20, 0)),
	}
	m.rebuildRows()

	names := workspaceRowNames(m)
	if len(names) != 3 || names[0] != "oldest" || names[1] != "middle" || names[2] != "newest" {
		t.Fatalf("expected oldest-first order [oldest, middle, newest], got %v", names)
	}
}

// Equal timestamps fall back to name so the order is deterministic.
func TestRebuildRows_WithinGroup_EqualCreated_TiebreaksByName(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("beta", "g", []string{"zulu"}, time.Unix(5, 0)),
		mkWS("alpha", "g", []string{"zulu"}, time.Unix(5, 0)),
	}
	m.rebuildRows()

	names := workspaceRowNames(m)
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Fatalf("expected name tiebreak [alpha, beta], got %v", names)
	}
}

func TestRenderWorkspaceRow_MultiRepoSelected_ShowsFourLines(t *testing.T) {
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
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines for selected multi-repo (name+meta+repos+footer), got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[2], "billing") {
		t.Errorf("line 3 missing repo: %q", lines[2])
	}
	if !strings.Contains(lines[3], "[archive]") {
		t.Errorf("line 4 missing action footer: %q", lines[3])
	}
}

func TestRenderWorkspaceRow_SingleRepoSelected_StaysThreeLines(t *testing.T) {
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
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines for selected single-repo (name+meta+footer), got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[2], "[dupe]") {
		t.Errorf("line 3 missing action footer: %q", lines[2])
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
	m.width = 40 // wide enough that a short name fits one line
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
	// Selected multi-repo: name(1) + metadata(1) + repo list(1) + footer(1).
	m.cursor = wsIdx
	if got := m.rowLineCount(wsIdx); got != 4 {
		t.Errorf("rowLineCount selected multi-repo = %d, want 4", got)
	}

	m.cursor = -1
	if got := m.rowLineCount(wsIdx); got != 2 {
		t.Errorf("rowLineCount non-selected multi-repo = %d, want 2", got)
	}
}

func TestActiveRowLineCount_MatchesRender(t *testing.T) {
	cases := []struct {
		name     string
		wsName   string
		repos    []string
		selected bool
	}{
		{"short-unselected", "alpha", []string{"medusa"}, false},
		{"short-selected", "alpha", []string{"medusa"}, true},
		{"long-selected-wraps", "no-ticket-prompt-injection-hardening-pass", []string{"medusa"}, true},
		{"multirepo-selected", "PE-37895-place-to-place-migration-spike", []string{"a", "b", "c"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := New()
			m.width = 35
			m.workspaces = []*data.Workspace{mkWS(c.wsName, "", c.repos, time.Unix(1, 0))}
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
			if c.selected {
				m.cursor = wsIdx
			} else {
				m.cursor = -1
			}
			rendered := m.renderRow(m.rows[wsIdx], c.selected)
			wantLines := strings.Count(rendered, "\n") + 1
			if got := m.rowLineCount(wsIdx); got != wantLines {
				t.Errorf("rowLineCount=%d but render produced %d lines:\n%s", got, wantLines, rendered)
			}
		})
	}
}
