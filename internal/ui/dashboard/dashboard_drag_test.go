package dashboard

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// dragModel builds a focused, sized dashboard over the given workspaces.
func dragModel(t *testing.T, workspaces ...*data.Workspace) *Model {
	t.Helper()
	m := New()
	m.SetSize(40, 40)
	m.Focus()
	m.SetWorkspaces(workspaces)
	return m
}

// screenYForRow finds a screen Y that hit-tests to the given row, so tests
// drive the same geometry the real pointer does instead of a parallel model of
// it. Returns -1 when the row is not on screen.
func screenYForRow(t *testing.T, m *Model, idx int) int {
	t.Helper()
	height, ok := m.rowAreaHeight()
	if !ok {
		t.Fatal("row area has no height")
	}
	for y := 1; y <= height; y++ {
		if got, _, ok := m.rowIndexAt(2, y); ok && got == idx {
			return y
		}
	}
	return -1
}

func workspaceRowIndex(t *testing.T, m *Model, name string) int {
	t.Helper()
	for i, row := range m.rows {
		if row.Type == RowWorkspace && row.Workspace != nil && row.Workspace.Name == name {
			return i
		}
	}
	t.Fatalf("no workspace row named %q", name)
	return -1
}

func groupRowIndex(t *testing.T, m *Model, label string) int {
	t.Helper()
	for i, row := range m.rows {
		if row.Type == RowSectionHeader && row.IsUserGroup && row.Label == label {
			return i
		}
	}
	t.Fatalf("no group header labelled %q", label)
	return -1
}

// press sends a left-button press onto a row and returns the resulting command.
func press(t *testing.T, m *Model, rowIdx int) tea.Cmd {
	t.Helper()
	y := screenYForRow(t, m, rowIdx)
	if y < 0 {
		t.Fatalf("row %d is not on screen", rowIdx)
	}
	_, cmd := m.Update(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	return cmd
}

// promoteDrag nudges the pointer just past the promotion threshold, which is
// where the rows reflow: a workspace drag inserts the New group target. A real
// pointer crosses this on its way to wherever it is going, so tests have to as
// well — one that jumped straight to a row index would be aiming at a layout
// that no longer exists by the time the event is resolved.
func promoteDrag(t *testing.T, m *Model) {
	t.Helper()
	m.Update(tea.MouseMotionMsg{X: 2, Y: m.drag.startY + dragPromoteLines, Button: tea.MouseLeft})
	if !m.drag.active {
		t.Fatal("drag did not promote")
	}
}

// rowRef names a drop target. Targets are named rather than indexed because
// promotion reflows the rows — a workspace drag inserts the New group target —
// so a row index captured before the drag points somewhere else by the time the
// pointer gets there.
type rowRef struct {
	group bool
	name  string
}

func wsRef(name string) rowRef    { return rowRef{name: name} }
func groupRef(name string) rowRef { return rowRef{group: true, name: name} }

func (r rowRef) index(t *testing.T, m *Model) int {
	t.Helper()
	if r.group {
		return groupRowIndex(t, m, r.name)
	}
	return workspaceRowIndex(t, m, r.name)
}

// dragTo presses rowFrom, moves the pointer onto the named target, and releases.
func dragTo(t *testing.T, m *Model, rowFrom int, target rowRef) tea.Cmd {
	t.Helper()
	if cmd := press(t, m, rowFrom); cmd != nil {
		t.Fatal("press on a draggable row must not act before the release")
	}
	promoteDrag(t, m)

	y := screenYForRow(t, m, target.index(t, m))
	if y < 0 {
		t.Fatalf("target %q is not on screen", target.name)
	}
	m.Update(tea.MouseMotionMsg{X: 2, Y: y, Button: tea.MouseLeft})
	_, cmd := m.Update(tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseLeft})
	return cmd
}

func msgOf(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	return cmd()
}

func TestDrag_PressDefersActivationUntilRelease(t *testing.T) {
	m := dragModel(t, mkWS("alpha", "", []string{"medusa"}, time.Unix(1, 0)))
	idx := workspaceRowIndex(t, m, "alpha")

	if cmd := press(t, m, idx); cmd != nil {
		t.Fatal("press must not activate the workspace: it may be a drag")
	}

	y := screenYForRow(t, m, idx)
	_, cmd := m.Update(tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseLeft})
	if _, ok := msgOf(t, cmd).(messages.WorkspaceActivated); !ok {
		t.Fatalf("release without motion must activate the workspace, got %T", msgOf(t, cmd))
	}
}

func TestDrag_ShortMoveStaysAClick(t *testing.T) {
	m := dragModel(t,
		mkWS("alpha", "", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("beta", "", []string{"medusa"}, time.Unix(2, 0)),
	)
	idx := workspaceRowIndex(t, m, "alpha")
	press(t, m, idx)

	y := screenYForRow(t, m, idx)
	m.Update(tea.MouseMotionMsg{X: 2, Y: y + dragPromoteLines - 1, Button: tea.MouseLeft})
	if m.drag.active {
		t.Fatalf("a move of %d line(s) must not promote to a drag", dragPromoteLines-1)
	}

	_, cmd := m.Update(tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseLeft})
	if _, ok := msgOf(t, cmd).(messages.WorkspaceActivated); !ok {
		t.Fatalf("a press that never promoted must still activate, got %T", msgOf(t, cmd))
	}
}

func TestDrag_GroupHeaderPressTogglesCollapseOnRelease(t *testing.T) {
	m := dragModel(t, mkWS("alpha", "shipping", []string{"medusa"}, time.Unix(1, 0)))
	idx := groupRowIndex(t, m, "shipping")

	if cmd := press(t, m, idx); cmd != nil {
		t.Fatal("press on a group header must not toggle before the release")
	}
	y := screenYForRow(t, m, idx)
	_, cmd := m.Update(tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseLeft})
	toggle, ok := msgOf(t, cmd).(messages.ToggleGroupCollapse)
	if !ok {
		t.Fatalf("release without motion must toggle the group, got %T", msgOf(t, cmd))
	}
	if toggle.Label != "shipping" {
		t.Errorf("toggled %q, want shipping", toggle.Label)
	}
}

func TestDrag_WorkspaceDownward_TakesTargetSlot(t *testing.T) {
	a := mkWS("a", "", []string{"medusa"}, time.Unix(1, 0))
	b := mkWS("b", "", []string{"medusa"}, time.Unix(2, 0))
	c := mkWS("c", "", []string{"medusa"}, time.Unix(3, 0))
	m := dragModel(t, a, b, c)

	cmd := dragTo(t, m, workspaceRowIndex(t, m, "a"), wsRef("c"))
	reorder, ok := msgOf(t, cmd).(messages.ReorderWorkspaces)
	if !ok {
		t.Fatalf("expected ReorderWorkspaces, got %T", msgOf(t, cmd))
	}
	if reorder.Group != "" {
		t.Errorf("group = %q, want the Ungrouped key", reorder.Group)
	}
	want := []string{b.Root(), c.Root(), a.Root()}
	if !sameOrder(reorder.OrderedRoots, want) {
		t.Errorf("order = %v, want %v (the dragged row lands where the target was)", reorder.OrderedRoots, want)
	}
}

func TestDrag_WorkspaceUpward_TakesTargetSlot(t *testing.T) {
	a := mkWS("a", "", []string{"medusa"}, time.Unix(1, 0))
	b := mkWS("b", "", []string{"medusa"}, time.Unix(2, 0))
	c := mkWS("c", "", []string{"medusa"}, time.Unix(3, 0))
	m := dragModel(t, a, b, c)

	cmd := dragTo(t, m, workspaceRowIndex(t, m, "c"), wsRef("a"))
	reorder, ok := msgOf(t, cmd).(messages.ReorderWorkspaces)
	if !ok {
		t.Fatalf("expected ReorderWorkspaces, got %T", msgOf(t, cmd))
	}
	want := []string{c.Root(), a.Root(), b.Root()}
	if !sameOrder(reorder.OrderedRoots, want) {
		t.Errorf("order = %v, want %v", reorder.OrderedRoots, want)
	}
}

func TestDrag_WorkspaceOntoGroupHeader_MovesIntoGroupAtTop(t *testing.T) {
	member := mkWS("member", "shipping", []string{"medusa"}, time.Unix(1, 0))
	loose := mkWS("loose", "", []string{"medusa"}, time.Unix(2, 0))
	m := dragModel(t, member, loose)

	cmd := dragTo(t, m, workspaceRowIndex(t, m, "loose"), groupRef("shipping"))
	reorder, ok := msgOf(t, cmd).(messages.ReorderWorkspaces)
	if !ok {
		t.Fatalf("expected ReorderWorkspaces, got %T", msgOf(t, cmd))
	}
	if reorder.Group != "shipping" {
		t.Errorf("group = %q, want shipping", reorder.Group)
	}
	want := []string{loose.Root(), member.Root()}
	if !sameOrder(reorder.OrderedRoots, want) {
		t.Errorf("order = %v, want %v (a header drop lands at the top)", reorder.OrderedRoots, want)
	}
}

func TestDrag_WorkspaceOntoCollapsedGroupHeader(t *testing.T) {
	member := mkWS("member", "shipping", []string{"medusa"}, time.Unix(1, 0))
	loose := mkWS("loose", "", []string{"medusa"}, time.Unix(2, 0))
	m := dragModel(t, member, loose)
	m.SetCollapsedGroups(map[string]bool{"shipping": true})

	cmd := dragTo(t, m, workspaceRowIndex(t, m, "loose"), groupRef("shipping"))
	reorder, ok := msgOf(t, cmd).(messages.ReorderWorkspaces)
	if !ok {
		t.Fatalf("expected ReorderWorkspaces, got %T", msgOf(t, cmd))
	}
	want := []string{loose.Root(), member.Root()}
	if !sameOrder(reorder.OrderedRoots, want) {
		t.Errorf("order = %v, want %v: a collapsed group shows no member rows, so its order must come from the workspaces", reorder.OrderedRoots, want)
	}
}

func TestDrag_GroupOntoOtherGroup_ReordersSections(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "beta", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("three", "", []string{"medusa"}, time.Unix(3, 0)),
	)

	cmd := dragTo(t, m, groupRowIndex(t, m, "beta"), groupRef("alpha"))
	reorder, ok := msgOf(t, cmd).(messages.ReorderGroups)
	if !ok {
		t.Fatalf("expected ReorderGroups, got %T", msgOf(t, cmd))
	}
	want := []string{"beta", "alpha"}
	if !sameOrder(reorder.Labels, want) {
		t.Errorf("labels = %v, want %v (Ungrouped is pinned, so it is never persisted)", reorder.Labels, want)
	}
}

func TestDrag_GroupOntoMemberRow_TargetsThatGroup(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "beta", []string{"medusa"}, time.Unix(2, 0)),
	)

	cmd := dragTo(t, m, groupRowIndex(t, m, "beta"), wsRef("one"))
	reorder, ok := msgOf(t, cmd).(messages.ReorderGroups)
	if !ok {
		t.Fatalf("a member row must stand in for its group header, got %T", msgOf(t, cmd))
	}
	want := []string{"beta", "alpha"}
	if !sameOrder(reorder.Labels, want) {
		t.Errorf("labels = %v, want %v", reorder.Labels, want)
	}
}

func TestDrag_DropOnSelfIsNoop(t *testing.T) {
	m := dragModel(t,
		mkWS("a", "", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("b", "", []string{"medusa"}, time.Unix(2, 0)),
	)
	idx := workspaceRowIndex(t, m, "a")
	press(t, m, idx)

	// Promote by moving down, then come back to the row we started on.
	y := screenYForRow(t, m, idx)
	m.Update(tea.MouseMotionMsg{X: 2, Y: y + dragPromoteLines, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 2, Y: y, Button: tea.MouseLeft})
	_, cmd := m.Update(tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseLeft})
	if cmd != nil {
		t.Fatalf("dropping a row on itself must not reorder, got %T", cmd())
	}
}

func TestDrag_EscapeCancels(t *testing.T) {
	m := dragModel(t,
		mkWS("a", "", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("b", "", []string{"medusa"}, time.Unix(2, 0)),
	)
	from := workspaceRowIndex(t, m, "a")
	press(t, m, from)
	m.Update(tea.MouseMotionMsg{X: 2, Y: screenYForRow(t, m, workspaceRowIndex(t, m, "b")), Button: tea.MouseLeft})
	if !m.drag.active {
		t.Fatal("drag did not promote")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.drag.kind != dragNone {
		t.Fatal("esc must cancel the drag")
	}
	_, cmd := m.Update(tea.MouseReleaseMsg{X: 2, Y: 3, Button: tea.MouseLeft})
	if cmd != nil {
		t.Fatalf("a cancelled drag must not reorder or activate, got %T", cmd())
	}
}

func TestDrag_ArchivedRowActivatesOnPress(t *testing.T) {
	live := mkWS("live", "", []string{"medusa"}, time.Unix(1, 0))
	old := mkWS("old", "", []string{"medusa"}, time.Unix(2, 0))
	old.Status = data.StatusArchived
	old.ArchivedAt = time.Unix(10, 0)
	m := dragModel(t, live, old)

	idx := workspaceRowIndex(t, m, "old")
	cmd := press(t, m, idx)
	if _, ok := msgOf(t, cmd).(messages.ShowArchivedWorkspaceDialog); !ok {
		t.Fatalf("archived rows are not draggable and must act on press, got %T", msgOf(t, cmd))
	}
	if m.drag.kind != dragNone {
		t.Error("archived row must not arm a drag")
	}
}

func TestDrag_AutoScrollAtEdges(t *testing.T) {
	var workspaces []*data.Workspace
	for i := 0; i < 20; i++ {
		workspaces = append(workspaces, mkWS(string(rune('a'+i)), "", []string{"medusa"}, time.Unix(int64(i+1), 0)))
	}
	m := New()
	m.SetSize(40, 14)
	m.Focus()
	m.SetWorkspaces(workspaces)

	press(t, m, workspaceRowIndex(t, m, "a"))
	height, _ := m.rowAreaHeight()
	m.Update(tea.MouseMotionMsg{X: 2, Y: height, Button: tea.MouseLeft})
	if m.scrollOffset == 0 {
		t.Fatal("a drag held at the bottom edge must scroll the row area")
	}

	before := m.scrollOffset
	m.Update(tea.MouseMotionMsg{X: 2, Y: 1, Button: tea.MouseLeft})
	if m.scrollOffset >= before {
		t.Fatal("a drag held at the top edge must scroll back up")
	}
}

func TestDrag_MotionWithoutButtonAbandonsCandidate(t *testing.T) {
	m := dragModel(t,
		mkWS("a", "", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("b", "", []string{"medusa"}, time.Unix(2, 0)),
	)
	press(t, m, workspaceRowIndex(t, m, "a"))
	m.Update(tea.MouseMotionMsg{X: 2, Y: 8, Button: tea.MouseNone})
	if m.drag.kind != dragNone {
		t.Fatal("hover motion means the release was never seen: the candidate must be dropped")
	}
}
