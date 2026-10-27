package dashboard

import (
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/data"
)

func nextVisibleModel(collapsed map[string]bool, wss ...*data.Workspace) *Model {
	m := New()
	m.SetCollapsedGroups(collapsed)
	m.SetWorkspaces(wss)
	return m
}

func nameOf(ws *data.Workspace) string {
	if ws == nil {
		return "<home>"
	}
	return ws.Name
}

// The workspace below the one leaving is the next one, in display order.
func TestNextVisibleWorkspace_PrefersTheRowBelow(t *testing.T) {
	a := mkWS("a", "", []string{"r"}, time.Unix(1, 0))
	b := mkWS("b", "", []string{"r"}, time.Unix(2, 0))
	c := mkWS("c", "", []string{"r"}, time.Unix(3, 0))
	m := nextVisibleModel(nil, a, b, c)

	if got := m.NextVisibleWorkspace(string(b.ID())); got != c {
		t.Fatalf("next after b = %s, want c", nameOf(got))
	}
}

// The last visible workspace falls back to the one above it.
func TestNextVisibleWorkspace_FallsBackToTheRowAbove(t *testing.T) {
	a := mkWS("a", "", []string{"r"}, time.Unix(1, 0))
	b := mkWS("b", "", []string{"r"}, time.Unix(2, 0))
	m := nextVisibleModel(nil, a, b)

	if got := m.NextVisibleWorkspace(string(b.ID())); got != a {
		t.Fatalf("next after b = %s, want a", nameOf(got))
	}
}

// A member of a collapsed group is not on screen, so it is never chosen.
func TestNextVisibleWorkspace_SkipsCollapsedGroups(t *testing.T) {
	hidden := mkWS("hidden", "shut", []string{"r"}, time.Unix(1, 0))
	leaving := mkWS("leaving", "open", []string{"r"}, time.Unix(2, 0))
	shown := mkWS("shown", "", []string{"r"}, time.Unix(3, 0))
	m := nextVisibleModel(map[string]bool{"shut": true}, hidden, leaving, shown)

	if got := m.NextVisibleWorkspace(string(leaving.ID())); got != shown {
		t.Fatalf("next after leaving = %s, want shown", nameOf(got))
	}
}

// With every other workspace collapsed away, archived or orphaned, nothing is
// visible and the caller goes home.
func TestNextVisibleWorkspace_NilWhenNothingIsVisible(t *testing.T) {
	hidden := mkWS("hidden", "shut", []string{"r"}, time.Unix(1, 0))
	leaving := mkWS("leaving", "", []string{"r"}, time.Unix(2, 0))
	archived := mkWS("archived", "", []string{"r"}, time.Unix(3, 0))
	archived.Status = data.StatusArchived
	m := nextVisibleModel(map[string]bool{"shut": true}, hidden, leaving, archived)

	if got := m.NextVisibleWorkspace(string(leaving.ID())); got != nil {
		t.Fatalf("next after leaving = %s, want home", nameOf(got))
	}
}

// A workspace whose own group is collapsed has no row to measure from, so the
// first visible workspace is used.
func TestNextVisibleWorkspace_FromACollapsedGroup(t *testing.T) {
	shown := mkWS("shown", "", []string{"r"}, time.Unix(1, 0))
	leaving := mkWS("leaving", "shut", []string{"r"}, time.Unix(2, 0))
	m := nextVisibleModel(map[string]bool{"shut": true}, shown, leaving)

	if got := m.NextVisibleWorkspace(string(leaving.ID())); got != shown {
		t.Fatalf("next after leaving = %s, want shown", nameOf(got))
	}
}
