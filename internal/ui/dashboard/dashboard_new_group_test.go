package dashboard

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// dropOnNewGroup drags the named workspace onto the New group target and returns
// the message the drop emits.
func dropOnNewGroup(t *testing.T, m *Model, workspace string) messages.CreateGroupForWorkspace {
	t.Helper()
	press(t, m, workspaceRowIndex(t, m, workspace))
	promoteDrag(t, m)

	y := screenYForRow(t, m, newGroupRowIndex(m))
	m.Update(tea.MouseMotionMsg{X: 2, Y: y, Button: tea.MouseLeft})
	_, cmd := m.Update(tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseLeft})

	// One message, not a batch: the rename dialog has to open on a group that
	// already has its member, and batched commands arrive in no order.
	create, ok := msgOf(t, cmd).(messages.CreateGroupForWorkspace)
	if !ok {
		t.Fatalf("expected CreateGroupForWorkspace, got %T", msgOf(t, cmd))
	}
	if create.Label == "" {
		t.Fatal("the drop created no group")
	}
	return create
}

func newGroupRowIndex(m *Model) int {
	for i, row := range m.rows {
		if row.Type == RowNewGroup {
			return i
		}
	}
	return -1
}

func TestNewGroup_TargetOnlyExistsDuringAWorkspaceDrag(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("loose", "", []string{"medusa"}, time.Unix(2, 0)),
	)

	if newGroupRowIndex(m) != -1 {
		t.Error("at rest the New group target must not be shown: it is a drop target, not a button")
	}

	press(t, m, groupRowIndex(t, m, "alpha"))
	promoteDrag(t, m)
	if newGroupRowIndex(m) != -1 {
		t.Error("a group drag cannot use the New group target, so it must not show one")
	}
	m.cancelDrag()

	press(t, m, workspaceRowIndex(t, m, "loose"))
	promoteDrag(t, m)
	if newGroupRowIndex(m) == -1 {
		t.Fatal("a workspace drag must offer the New group target")
	}

	m.cancelDrag()
	if newGroupRowIndex(m) != -1 {
		t.Error("cancelling the drag must take the target away again")
	}
}

func TestNewGroup_TargetSitsAboveUngrouped(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("loose", "", []string{"medusa"}, time.Unix(2, 0)),
	)

	press(t, m, workspaceRowIndex(t, m, "one"))
	promoteDrag(t, m)

	newGroup := newGroupRowIndex(m)
	ungrouped := groupRowIndex(t, m, "Ungrouped")
	if newGroup > ungrouped {
		t.Fatalf("New group at row %d, Ungrouped at %d: the target belongs above it", newGroup, ungrouped)
	}
	for i := newGroup + 1; i < ungrouped; i++ {
		if m.rows[i].Type == RowSectionHeader {
			t.Errorf("row %d sits between New group and Ungrouped", i)
		}
	}
}

func TestNewGroup_HoverPreviewsTheWorkspaceUnderIt(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "alpha", []string{"medusa"}, time.Unix(2, 0)),
	)

	press(t, m, workspaceRowIndex(t, m, "two"))
	promoteDrag(t, m)
	motionTo(t, m, newGroupRowIndex(m))

	idx := newGroupRowIndex(m)
	if idx == -1 || idx+1 >= len(m.rows) {
		t.Fatal("New group target missing")
	}
	next := m.rows[idx+1]
	if next.Type != RowWorkspace || next.Workspace == nil || next.Workspace.Name != "two" {
		t.Fatalf("row after the target is %+v, want the dragged workspace previewed under it", next)
	}
	rows := groupedRowNames(m)
	if !sameOrder(rows["alpha"], []string{"one"}) {
		t.Errorf("alpha = %v, want [one]: the dragged workspace has left it", rows["alpha"])
	}
	if !sameOrder(rows[newGroupLabel], []string{"two"}) {
		t.Errorf("New group = %v, want [two]", rows[newGroupLabel])
	}
}

func TestNewGroup_DropCreatesAGroupWithAGeneratedName(t *testing.T) {
	loose := mkWS("loose", "", []string{"medusa"}, time.Unix(1, 0))
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(2, 0)),
		loose,
	)

	create := dropOnNewGroup(t, m, "loose")

	if create.Root != loose.Root() {
		t.Errorf("root = %q, want the dropped workspace %q", create.Root, loose.Root())
	}
	if strings.Count(create.Label, "-") != 1 {
		t.Errorf("label = %q, want a two-word hyphenated name", create.Label)
	}
	if create.Label == "alpha" {
		t.Errorf("label = %q, want a fresh name", create.Label)
	}
}

func TestNewGroup_TargetIsNotKeyboardSelectable(t *testing.T) {
	m := dragModel(t, mkWS("loose", "", []string{"medusa"}, time.Unix(1, 0)))
	press(t, m, workspaceRowIndex(t, m, "loose"))
	promoteDrag(t, m)

	idx := newGroupRowIndex(m)
	if idx == -1 {
		t.Fatal("New group target missing")
	}
	if isSelectable(m.rows[idx]) {
		t.Error("the New group target is drag-only; the cursor must skip it")
	}
}

func TestNewGroup_TargetRendersItsLabel(t *testing.T) {
	m := dragModel(t, mkWS("loose", "", []string{"medusa"}, time.Unix(1, 0)))
	press(t, m, workspaceRowIndex(t, m, "loose"))
	promoteDrag(t, m)

	idx := newGroupRowIndex(m)
	if got := ansi.Strip(m.renderRow(m.rows[idx], false)); !strings.Contains(got, "New group") {
		t.Errorf("rendered %q, want it to name itself", got)
	}
}

func TestNewGroupName_AvoidsTakenLabels(t *testing.T) {
	taken := map[string]bool{}
	for i := 0; i < 200; i++ {
		name := newGroupName(taken)
		if taken[name] {
			t.Fatalf("draw %d returned %q, which was already taken", i, name)
		}
		if !strings.Contains(name, "-") {
			t.Fatalf("name %q is not two words", name)
		}
		taken[name] = true
	}
}

// With every combination taken, the generator must still terminate with
// something unused rather than loop on a pool it can never draw from.
func TestNewGroupName_ExhaustedPoolStillTerminates(t *testing.T) {
	taken := map[string]bool{}
	for _, adjective := range groupAdjectives {
		for _, noun := range groupNouns {
			taken[adjective+"-"+noun] = true
		}
	}

	name := newGroupName(taken)
	if taken[name] {
		t.Fatalf("returned %q, which is taken", name)
	}
}

func TestGroupLabels_IncludesArchived(t *testing.T) {
	archived := mkWS("old", "retired", []string{"medusa"}, time.Unix(1, 0))
	archived.Status = data.StatusArchived
	m := New()
	m.SetWorkspaces([]*data.Workspace{
		mkWS("live", "current", []string{"medusa"}, time.Unix(2, 0)),
		archived,
	})

	labels := m.groupLabels()
	if !labels["current"] || !labels["retired"] {
		t.Errorf("labels = %v, want both: unarchiving would bring the group back", labels)
	}
}

// A generated name must not decide where the group lands: the user dropped it at
// the bottom, so it is pinned there rather than falling back to the alphabetical
// order an undragged group gets.
func TestNewGroup_DropPinsTheGroupLast(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "middle", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("two", "zzz-last", []string{"medusa"}, time.Unix(2, 0)),
		mkWS("loose", "", []string{"medusa"}, time.Unix(3, 0)),
	)

	create := dropOnNewGroup(t, m, "loose")

	if len(create.Order) == 0 || create.Order[len(create.Order)-1] != create.Label {
		t.Errorf("order = %v, want %q last", create.Order, create.Label)
	}
	if !sameOrder(create.Order[:len(create.Order)-1], []string{"middle", "zzz-last"}) {
		t.Errorf("existing groups reordered: %v", create.Order)
	}
}

// The target is separated from the Ungrouped header below it, so it does not
// read as that section's own header.
func TestNewGroup_TargetIsSeparatedFromUngrouped(t *testing.T) {
	m := dragModel(t,
		mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("loose", "", []string{"medusa"}, time.Unix(2, 0)),
	)

	press(t, m, workspaceRowIndex(t, m, "one"))
	promoteDrag(t, m)

	idx := newGroupRowIndex(m)
	if idx == -1 {
		t.Fatal("New group target missing")
	}
	if m.rows[idx+1].Type != RowSpacer {
		t.Errorf("row after the target is %v, want a spacer between it and Ungrouped", m.rows[idx+1].Type)
	}
}
