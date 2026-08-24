package dashboard

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/messages"
)

// motionTo moves the pointer onto a row without releasing.
func motionTo(t *testing.T, m *Model, rowIdx int) {
	t.Helper()
	y := screenYForRow(t, m, rowIdx)
	if y < 0 {
		t.Fatalf("row %d is not on screen", rowIdx)
	}
	m.Update(tea.MouseMotionMsg{X: 2, Y: y, Button: tea.MouseLeft})
}

func TestDrag_PreviewMovesRowsBeforeRelease(t *testing.T) {
	m := dragModel(t,
		mkWS("a", "", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("b", "", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("c", "", []string{"medusa"}, time.Unix(3, 0)),
	)

	press(t, m, workspaceRowIndex(t, m, "a"))
	promoteDrag(t, m)
	motionTo(t, m, workspaceRowIndex(t, m, "c"))

	got := groupedRowNames(m)["Ungrouped"]
	want := []string{"b", "c", "a"}
	if !sameOrder(got, want) {
		t.Errorf("rows during the drag = %v, want %v: the preview is the order the release commits", got, want)
	}
}

func TestDrag_PreviewIsStableWhilePointerHolds(t *testing.T) {
	m := dragModel(t,
		mkWS("a", "", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("b", "", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("c", "", []string{"medusa"}, time.Unix(3, 0)),
	)

	press(t, m, workspaceRowIndex(t, m, "a"))
	promoteDrag(t, m)
	y := screenYForRow(t, m, workspaceRowIndex(t, m, "c"))
	m.Update(tea.MouseMotionMsg{X: 2, Y: y, Button: tea.MouseLeft})
	first := groupedRowNames(m)["Ungrouped"]

	// The dragged row now sits under the pointer. Re-reading the same position
	// must resolve to the same placement, or the preview would oscillate.
	for i := 0; i < 3; i++ {
		m.Update(tea.MouseMotionMsg{X: 2, Y: y, Button: tea.MouseLeft})
		if got := groupedRowNames(m)["Ungrouped"]; !sameOrder(got, first) {
			t.Fatalf("event %d moved the preview from %v to %v", i, first, got)
		}
	}
}

func TestDrag_PreviewMovesAcrossGroups(t *testing.T) {
	m := dragModel(t,
		mkWS("kept", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("loose", "", []string{"medusa"}, time.Unix(2, 0)),
	)

	press(t, m, workspaceRowIndex(t, m, "loose"))
	promoteDrag(t, m)
	motionTo(t, m, groupRowIndex(t, m, "alpha"))

	rows := groupedRowNames(m)
	if !sameOrder(rows["alpha"], []string{"loose", "kept"}) {
		t.Errorf("alpha = %v, want [loose kept]", rows["alpha"])
	}
	if len(rows["Ungrouped"]) != 0 {
		t.Errorf("Ungrouped = %v, want empty: the row is previewed in its new group", rows["Ungrouped"])
	}
	if _, ok := rows["Ungrouped"]; !ok {
		t.Error("the emptied section must keep its header, or there is nothing to drop back onto")
	}
}

func TestDrag_GroupPreviewKeepsItsMembers(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "alpha", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("three", "beta", []string{"medusa"}, time.Unix(3, 0)),
	)

	press(t, m, groupRowIndex(t, m, "beta"))
	dragSectionOnto(t, m, "beta", "alpha")

	if got := userGroupLabels(m); !sameOrder(got, []string{"beta", "alpha", "Ungrouped"}) {
		t.Fatalf("labels during the drag = %v, want [beta alpha Ungrouped]", got)
	}
	lifted := m.rows[groupRowIndex(t, m, "beta")]
	if !lifted.DragLifted {
		t.Error("the dragged section must be marked so the user can see what they are carrying")
	}
	if lifted.Collapsed {
		t.Error("dragging a section must not collapse it")
	}
	if rows := groupedRowNames(m); !sameOrder(rows["beta"], []string{"three"}) {
		t.Errorf("beta = %v, want its members travelling with it", rows["beta"])
	}
	if rows := groupedRowNames(m); !sameOrder(rows["alpha"], []string{"one", "two"}) {
		t.Errorf("alpha = %v, want its own members untouched", rows["alpha"])
	}
}

// A section only moves once the pointer is past the middle of the neighbour it
// would displace, so nudging just over the boundary must not move it yet.
func TestDrag_GroupIgnoresShallowOverlap(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "alpha", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("three", "beta", []string{"medusa"}, time.Unix(3, 0)),
	)
	before := userGroupLabels(m)

	press(t, m, groupRowIndex(t, m, "beta"))
	end := sectionEnd(t, m, "beta")
	m.Update(tea.MouseMotionMsg{X: 2, Y: end + dragPromoteLines, Button: tea.MouseLeft})

	if got := userGroupLabels(m); !sameOrder(got, before) {
		t.Errorf("labels = %v, want %v: a shallow overlap must not reorder", got, before)
	}
}

func TestDrag_GroupIsStableWhilePointerHolds(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "alpha", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("three", "beta", []string{"medusa"}, time.Unix(3, 0)),
		mkWS("four", "gamma", []string{"medusa"}, time.Unix(4, 0)),
	)

	press(t, m, groupRowIndex(t, m, "gamma"))
	dragSectionOnto(t, m, "gamma", "alpha")
	settled := userGroupLabels(m)

	// The section has moved under the pointer. Every further event at the same
	// position must leave it where it is — this is the jitter that resolving by
	// hovered section produced, where it flipped above and below its neighbour.
	y := sectionStart(t, m, "alpha")
	for i := 0; i < 6; i++ {
		m.Update(tea.MouseMotionMsg{X: 2, Y: y, Button: tea.MouseLeft})
		if got := userGroupLabels(m); !sameOrder(got, settled) {
			t.Fatalf("event %d moved the order from %v to %v", i, settled, got)
		}
	}
}

// Sweeping the pointer across the pane and back must land the section where the
// pointer is, without ever having overshot past a neighbour it did not reach.
func TestDrag_GroupSweepIsMonotone(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "beta", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("three", "beta", []string{"medusa"}, time.Unix(3, 0)),
		mkWS("four", "gamma", []string{"medusa"}, time.Unix(4, 0)),
	)

	press(t, m, groupRowIndex(t, m, "alpha"))
	height, _ := m.rowAreaHeight()

	// Down the pane one line at a time. The dragged section's index must never
	// go backwards: that is the jitter, in the form the reporter saw it — the
	// section jumping above and below the neighbour under the pointer.
	last := 0
	for y := 1; y <= height; y++ {
		m.Update(tea.MouseMotionMsg{X: 2, Y: y, Button: tea.MouseLeft})
		assertSectionsIntact(t, m)
		at := indexOf(m.displayedSectionKeys(), "alpha")
		if at < last {
			t.Fatalf("at y=%d alpha moved back from index %d to %d", y, last, at)
		}
		last = at
	}
	if got := userGroupLabels(m); got[len(got)-2] != "alpha" {
		t.Errorf("labels = %v, want alpha last of the named groups after sweeping to the bottom", got)
	}

	for y := height; y >= 1; y-- {
		m.Update(tea.MouseMotionMsg{X: 2, Y: y, Button: tea.MouseLeft})
		assertSectionsIntact(t, m)
		at := indexOf(m.displayedSectionKeys(), "alpha")
		if at > last {
			t.Fatalf("at y=%d alpha moved back from index %d to %d", y, last, at)
		}
		last = at
	}
	if got := userGroupLabels(m); got[0] != "alpha" {
		t.Errorf("labels = %v, want alpha first after sweeping back to the top", got)
	}
}

// assertSectionsIntact checks that no section lost or gained members mid-drag:
// only the section order may change while a group is being carried.
func assertSectionsIntact(t *testing.T, m *Model) {
	t.Helper()
	rows := groupedRowNames(m)
	want := map[string][]string{
		"alpha":     {"one"},
		"beta":      {"two", "three"},
		"gamma":     {"four"},
		"Ungrouped": nil,
	}
	for key, members := range want {
		if !sameOrder(rows[key], members) {
			t.Fatalf("%s = %v, want %v", key, rows[key], members)
		}
	}
}

func TestDrag_UngroupedHeaderIsNotDraggable(t *testing.T) {
	m := dragModel(t, mkWS("loose", "", []string{"medusa"}, time.Unix(1, 0)))

	cmd := press(t, m, groupRowIndex(t, m, "Ungrouped"))
	if _, ok := msgOf(t, cmd).(messages.ToggleGroupCollapse); !ok {
		t.Fatalf("the pinned Ungrouped header is not draggable, so it acts on press, got %T", msgOf(t, cmd))
	}
	if m.drag.kind != dragNone {
		t.Error("Ungrouped must not arm a group drag")
	}
}

func TestDrag_GroupOntoUngroupedLandsAboveIt(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "beta", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("loose", "", []string{"medusa"}, time.Unix(3, 0)),
	)

	cmd := dragTo(t, m, groupRowIndex(t, m, "alpha"), groupRef("Ungrouped"))
	reorder, ok := msgOf(t, cmd).(messages.ReorderGroups)
	if !ok {
		t.Fatalf("expected ReorderGroups, got %T", msgOf(t, cmd))
	}
	if !sameOrder(reorder.Labels, []string{"beta", "alpha"}) {
		t.Errorf("labels = %v, want [beta alpha]: reaching for Ungrouped means last, not below it", reorder.Labels)
	}
}

// sectionStart returns the screen Y of a section's first line.
func sectionStart(t *testing.T, m *Model, key string) int {
	t.Helper()
	for _, ext := range m.sectionExtents() {
		if ext.key == key {
			return ext.start - m.scrollOffset + 1
		}
	}
	t.Fatalf("no section %q", key)
	return -1
}

// sectionEnd returns the screen Y one line past a section's last line.
func sectionEnd(t *testing.T, m *Model, key string) int {
	t.Helper()
	for _, ext := range m.sectionExtents() {
		if ext.key == key {
			return ext.end - m.scrollOffset + 1
		}
	}
	t.Fatalf("no section %q", key)
	return -1
}

// dragSectionOnto walks a dragged section past a target section, one motion
// event at a time. Each event aims deep into the target rather than at its edge,
// since a section only moves once the pointer is past the midpoint of what it
// would displace.
func dragSectionOnto(t *testing.T, m *Model, src, target string) {
	t.Helper()
	keys := m.displayedSectionKeys()
	up := indexOf(keys, src) > indexOf(keys, target)

	for range m.rows {
		keys = m.displayedSectionKeys()
		from, to := indexOf(keys, src), indexOf(keys, target)
		if from < 0 || to < 0 {
			t.Fatalf("sections %q / %q not both present in %v", src, target, keys)
		}
		if (up && from < to) || (!up && from > to) {
			return // src is now past target
		}
		ext, ok := sectionExtentOf(m, target)
		if !ok {
			t.Fatalf("no extent for %q", target)
		}
		line := ext.end - 1
		if up {
			line = ext.start
		}
		m.Update(tea.MouseMotionMsg{X: 2, Y: line - m.scrollOffset + 1, Button: tea.MouseLeft})
	}
	t.Fatalf("section %q never moved past %q", src, target)
}

func sectionExtentOf(m *Model, key string) (sectionExtent, bool) {
	for _, ext := range m.sectionExtents() {
		if ext.key == key {
			return ext, true
		}
	}
	return sectionExtent{}, false
}
