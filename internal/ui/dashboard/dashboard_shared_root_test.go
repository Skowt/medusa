package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/data"
)

// checkoutWS builds a workspace that owns no worktree: its root is the source
// repo, which every checkout workspace over that repo shares.
func checkoutWS(name string, created time.Time) *data.Workspace {
	ws := data.NewCheckoutWorkspace(name, "main", "/src/medusa")
	ws.Created = created
	return ws
}

// Two workspaces over one repo are two rows. Keying the pane's pending state
// off the root instead of the ID collapsed them: the placeholder for the second
// was filtered out as one that already existed.
func TestSharedRoot_BothWorkspacesGetARow(t *testing.T) {
	first := checkoutWS("first", time.Unix(1, 0))
	second := checkoutWS("second", time.Unix(2, 0))
	if first.Root() != second.Root() {
		t.Fatalf("fixture is wrong: roots %q and %q differ", first.Root(), second.Root())
	}

	m := New()
	m.SetSize(40, 40)
	m.SetWorkspaces([]*data.Workspace{first})
	if cmd := m.SetWorkspaceCreating(second, true); cmd == nil {
		t.Error("marking a workspace as creating should start the spinner")
	}

	var names []string
	for _, row := range m.rows {
		if row.Type == RowWorkspace && row.Workspace != nil {
			names = append(names, row.Workspace.Name)
		}
	}
	if len(names) != 2 {
		t.Fatalf("rows = %v, want one per workspace", names)
	}
}

// The spinner belongs to the workspace it was raised for, not to whatever else
// happens to sit on the same directory.
func TestSharedRoot_PendingStateDoesNotLeakToTheSibling(t *testing.T) {
	first := checkoutWS("first", time.Unix(1, 0))
	second := checkoutWS("second", time.Unix(2, 0))

	m := New()
	m.SetSize(40, 40)
	m.SetWorkspaces([]*data.Workspace{first, second})

	m.SetWorkspaceDeleting(string(second.ID()), true)
	if m.workspacePending(first) {
		t.Error("deleting the second workspace marked the first as pending")
	}
	if !m.workspacePending(second) {
		t.Error("the workspace being deleted is not marked as pending")
	}

	m.SetWorkspaceDeleting(string(second.ID()), false)
	_ = m.SetWorkspaceCreating(second, true)
	if m.workspacePending(first) {
		t.Error("creating the second workspace marked the first as pending")
	}
}

// Activating one of two workspaces on a repo must not light up the other.
func TestSharedRoot_OnlyTheActiveWorkspaceRendersAsCurrent(t *testing.T) {
	first := checkoutWS("first", time.Unix(1, 0))
	second := checkoutWS("second", time.Unix(2, 0))

	m := New()
	m.SetSize(40, 40)
	m.SetWorkspaces([]*data.Workspace{first, second})
	m.activeID = string(second.ID())
	m.MarkUnread(string(first.ID()))
	m.MarkUnread(string(second.ID()))

	// Unread is suppressed for the active workspace, so it is the one signal
	// that separates "this row is current" from "this row shares its root".
	firstLines := strings.Join(m.renderWorkspaceNameLines(first, false, 30), "")
	secondLines := strings.Join(m.renderWorkspaceNameLines(second, false, 30), "")
	if firstLines == secondLines {
		t.Fatal("both rows rendered identically, so the active one is decided by root")
	}
}

// Dragging one of two workspaces on a repo must lift and commit only that one.
func TestSharedRoot_DragIdentifiesOneWorkspace(t *testing.T) {
	first := checkoutWS("first", time.Unix(1, 0))
	second := checkoutWS("second", time.Unix(2, 0))

	m := New()
	m.SetSize(40, 40)
	m.Focus()
	m.SetWorkspaces([]*data.Workspace{first, second})

	m.drag = dragState{kind: dragWorkspace, active: true, srcID: string(second.ID())}
	if m.isDragSourceWorkspace(first) {
		t.Error("dragging the second workspace lifted the first as well")
	}
	if !m.isDragSourceWorkspace(second) {
		t.Error("the dragged workspace is not marked as the source")
	}

	m.hover = hoverState{kind: dragWorkspace, workspace: string(second.ID())}
	if m.isHoveredWorkspace(first) {
		t.Error("hovering the second workspace showed the first one's handle")
	}
}
