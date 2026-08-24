package dashboard

import (
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/data"
)

// newGroupLabel is the key groupedRowNames files rows under the New group drop
// target, which is a section boundary for attribution purposes even though it is
// not a section header.
const newGroupLabel = "New group"

// groupedRowNames returns the workspace names under each user group header, in
// display order.
func groupedRowNames(m *Model) map[string][]string {
	out := map[string][]string{}
	current := ""
	for _, row := range m.rows {
		switch {
		case row.Type == RowSectionHeader && row.IsUserGroup:
			current = row.Label
			if _, ok := out[current]; !ok {
				out[current] = nil
			}
		case row.Type == RowNewGroup:
			current = newGroupLabel
			if _, ok := out[current]; !ok {
				out[current] = nil
			}
		case row.Type == RowWorkspace && row.Workspace != nil && current != "":
			out[current] = append(out[current], row.Workspace.Name)
		}
	}
	return out
}

func userGroupLabels(m *Model) []string {
	var labels []string
	for _, row := range m.rows {
		if row.Type == RowSectionHeader && row.IsUserGroup {
			labels = append(labels, row.Label)
		}
	}
	return labels
}

func withSortKey(ws *data.Workspace, key int) *data.Workspace {
	ws.SortKey = key
	return ws
}

func TestSortKey_PlacedWorkspacesLeadUnplacedOnes(t *testing.T) {
	m := New()
	m.SetWorkspaces([]*data.Workspace{
		mkWS("oldest", "", []string{"medusa"}, time.Unix(1, 0)),
		withSortKey(mkWS("placed-second", "", []string{"medusa"}, time.Unix(2, 0)), 20),
		withSortKey(mkWS("placed-first", "", []string{"medusa"}, time.Unix(3, 0)), 10),
		mkWS("newest", "", []string{"medusa"}, time.Unix(4, 0)),
	})

	got := groupedRowNames(m)["Ungrouped"]
	want := []string{"placed-first", "placed-second", "oldest", "newest"}
	if !sameOrder(got, want) {
		t.Errorf("order = %v, want %v: hand-placed workspaces lead, then the rest oldest-first", got, want)
	}
}

func TestSortKey_AbsentEverywhereKeepsCreatedOrder(t *testing.T) {
	m := New()
	m.SetWorkspaces([]*data.Workspace{
		mkWS("third", "", []string{"medusa"}, time.Unix(3, 0)),
		mkWS("first", "", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("second", "", []string{"medusa"}, time.Unix(2, 0)),
	})

	got := groupedRowNames(m)["Ungrouped"]
	want := []string{"first", "second", "third"}
	if !sameOrder(got, want) {
		t.Errorf("order = %v, want %v: a registry with no keys must order as it did before manual ordering existed", got, want)
	}
}

func TestSortKey_EqualKeysTiebreakByCreated(t *testing.T) {
	m := New()
	m.SetWorkspaces([]*data.Workspace{
		withSortKey(mkWS("newer", "", []string{"medusa"}, time.Unix(2, 0)), 10),
		withSortKey(mkWS("older", "", []string{"medusa"}, time.Unix(1, 0)), 10),
	})

	got := groupedRowNames(m)["Ungrouped"]
	want := []string{"older", "newer"}
	if !sameOrder(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestGroupOrder_ManualOrderWins(t *testing.T) {
	m := New()
	m.SetWorkspaces([]*data.Workspace{
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "beta", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("three", "gamma", []string{"medusa"}, time.Unix(3, 0)),
	})
	m.SetGroupOrder([]string{"gamma", "alpha", "beta"})

	got := userGroupLabels(m)
	want := []string{"gamma", "alpha", "beta", "Ungrouped"}
	if !sameOrder(got, want) {
		t.Errorf("labels = %v, want %v", got, want)
	}
}

func TestGroupOrder_UngroupedStaysLast(t *testing.T) {
	m := New()
	m.SetWorkspaces([]*data.Workspace{
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("loose", "", []string{"medusa"}, time.Unix(2, 0)),
	})
	// Even asked for first, explicitly.
	m.SetGroupOrder([]string{"", "alpha"})

	got := userGroupLabels(m)
	want := []string{"alpha", "Ungrouped"}
	if !sameOrder(got, want) {
		t.Errorf("labels = %v, want %v: Ungrouped is pinned to the bottom", got, want)
	}
}

func TestGroupOrder_UnknownGroupsTrailAlphabetically(t *testing.T) {
	m := New()
	m.SetWorkspaces([]*data.Workspace{
		mkWS("one", "zeta", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "alpha", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("three", "beta", []string{"medusa"}, time.Unix(3, 0)),
		mkWS("loose", "", []string{"medusa"}, time.Unix(4, 0)),
	})
	m.SetGroupOrder([]string{"zeta"})

	got := userGroupLabels(m)
	want := []string{"zeta", "alpha", "beta", "Ungrouped"}
	if !sameOrder(got, want) {
		t.Errorf("labels = %v, want %v: a group never dragged keeps the alphabetical fallback and appends after the ordered ones", got, want)
	}
}

func TestGroupOrder_StaleLabelsAreIgnored(t *testing.T) {
	m := New()
	m.SetWorkspaces([]*data.Workspace{
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
	})
	m.SetGroupOrder([]string{"deleted-group", "alpha", "alpha"})

	got := userGroupLabels(m)
	want := []string{"alpha", "Ungrouped"}
	if !sameOrder(got, want) {
		t.Errorf("labels = %v, want %v: a named label with no members must not emit a section", got, want)
	}
}

// Ungrouped is the one section that shows with no members: an empty section
// still has to be a drop target, and a header that vanishes when its last
// workspace leaves cannot be one.
func TestGroupOrder_UngroupedShownWhenEmpty(t *testing.T) {
	m := New()
	m.SetWorkspaces([]*data.Workspace{
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
	})

	if got := userGroupLabels(m); !sameOrder(got, []string{"alpha", "Ungrouped"}) {
		t.Errorf("labels = %v, want [alpha Ungrouped]", got)
	}
	if members := groupedRowNames(m)["Ungrouped"]; len(members) != 0 {
		t.Errorf("Ungrouped members = %v, want none", members)
	}
}

// With nothing to drag there is nothing to drop, so an empty dashboard stays
// empty rather than showing a lone Ungrouped header.
func TestGroupOrder_NoSectionsWithoutWorkspaces(t *testing.T) {
	m := New()
	m.SetWorkspaces(nil)

	if got := userGroupLabels(m); len(got) != 0 {
		t.Errorf("labels = %v, want none", got)
	}
}

func TestMoveToIndex(t *testing.T) {
	cases := []struct {
		name  string
		items []string
		move  string
		idx   int
		want  []string
	}{
		{"downward", []string{"a", "b", "c"}, "a", 2, []string{"b", "c", "a"}},
		{"upward", []string{"a", "b", "c"}, "c", 0, []string{"c", "a", "b"}},
		{"in place", []string{"a", "b", "c"}, "b", 1, []string{"a", "b", "c"}},
		{"past the end clamps", []string{"a", "b"}, "a", 9, []string{"b", "a"}},
		{"absent item is a no-op", []string{"a", "b"}, "zz", 0, []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := moveToIndex(tc.items, tc.move, tc.idx); !sameOrder(got, tc.want) {
				t.Errorf("moveToIndex(%v, %q, %d) = %v, want %v", tc.items, tc.move, tc.idx, got, tc.want)
			}
		})
	}
}

// TestMoveToIndex_IsAFixedPoint is the property the live drag preview depends
// on: re-reading the index an item already sits at must not move it, or the
// preview would oscillate between two placements while the pointer holds still.
func TestMoveToIndex_IsAFixedPoint(t *testing.T) {
	items := []string{"a", "b", "c", "d"}
	for idx := range items {
		moved := moveToIndex(items, "a", idx)
		again := moveToIndex(moved, "a", indexOf(moved, "a"))
		if !sameOrder(moved, again) {
			t.Errorf("index %d: %v then re-read gave %v", idx, moved, again)
		}
	}
}

func TestOrderedGroupMembers_SkipsArchivedAndOrphans(t *testing.T) {
	live := mkWS("live", "shipping", []string{"medusa"}, time.Unix(1, 0))
	archived := mkWS("archived", "shipping", []string{"medusa"}, time.Unix(2, 0))
	archived.Status = data.StatusArchived
	orphan := mkWS("orphan", "shipping", []string{"medusa"}, time.Unix(3, 0))
	orphan.Orphan = data.OrphanMetadata

	m := New()
	m.SetWorkspaces([]*data.Workspace{live, archived, orphan})

	got := m.orderedGroupMembers("shipping")
	want := []string{live.Root()}
	if !sameOrder(got, want) {
		t.Errorf("members = %v, want %v: sections with their own sort must not join manual ordering", got, want)
	}
}
