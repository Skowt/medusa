package dashboard

import (
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/ui/common"
)

func TestHover_HandleShownOnDraggableRows(t *testing.T) {
	loose := mkWS("loose", "", []string{"medusa"}, time.Unix(1, 0))
	m := dragModel(t, loose)

	idx := workspaceRowIndex(t, m, "loose")
	m.Update(tea.MouseMotionMsg{X: 2, Y: screenYForRow(t, m, idx), Button: tea.MouseNone})
	if !m.isHoveredWorkspace(loose) {
		t.Fatal("hovering a workspace row must arm its handle")
	}
	if rendered := m.renderWorkspaceRow(m.rows[idx], false); !strings.Contains(rendered, dragHandle) {
		t.Error("a hovered workspace row must render the drag handle")
	}

	// Off the rows entirely.
	m.Update(tea.MouseMotionMsg{X: 2, Y: 1, Button: tea.MouseNone})
	if m.isHoveredWorkspace(loose) {
		t.Error("leaving the row must clear its handle")
	}
}

func TestHover_NoHandleOnPinnedUngroupedHeader(t *testing.T) {
	m := dragModel(t, mkWS("loose", "", []string{"medusa"}, time.Unix(1, 0)))

	idx := groupRowIndex(t, m, "Ungrouped")
	m.Update(tea.MouseMotionMsg{X: 2, Y: screenYForRow(t, m, idx), Button: tea.MouseNone})
	if m.isHoveredGroup("") {
		t.Fatal("Ungrouped cannot be dragged, so it must not advertise a handle")
	}
	if rendered := m.renderRow(m.rows[idx], false); strings.Contains(rendered, dragHandle) {
		t.Error("the Ungrouped header must not render a drag handle")
	}
}

func TestHover_HandleShownOnGroupHeader(t *testing.T) {
	m := dragModel(t, mkWS("one", "alpha", []string{"medusa"}, time.Unix(1, 0)))

	idx := groupRowIndex(t, m, "alpha")
	m.Update(tea.MouseMotionMsg{X: 2, Y: screenYForRow(t, m, idx), Button: tea.MouseNone})
	if !m.isHoveredGroup("alpha") {
		t.Fatal("hovering a named group header must arm its handle")
	}
	if rendered := m.renderRow(m.rows[idx], false); !strings.Contains(rendered, dragHandle) {
		t.Error("a hovered group header must render the drag handle")
	}
}

func TestHover_ClearedWhenDragPromotes(t *testing.T) {
	m := dragModel(t,
		mkWS("a", "", []string{"medusa"}, time.Unix(1, 0)),
		mkWS("b", "", []string{"medusa"}, time.Unix(2, 0)),
	)

	idx := workspaceRowIndex(t, m, "a")
	m.Update(tea.MouseMotionMsg{X: 2, Y: screenYForRow(t, m, idx), Button: tea.MouseNone})
	press(t, m, idx)
	motionTo(t, m, workspaceRowIndex(t, m, "b"))

	if m.hover.kind != dragNone {
		t.Error("a live drag replaces the hover affordance; both at once is noise")
	}
}

// hoverRow hovers a row and returns what it renders.
func hoverRow(t *testing.T, m *Model, idx int, selected bool) string {
	t.Helper()
	m.Update(tea.MouseMotionMsg{X: 2, Y: screenYForRow(t, m, idx), Button: tea.MouseNone})
	return m.renderRow(m.rows[idx], selected)
}

func TestHover_HandleShownOnSelectedRow(t *testing.T) {
	m := dragModel(t, mkWS("exploration", "", []string{"medusa"}, time.Unix(1, 0)))
	idx := workspaceRowIndex(t, m, "exploration")
	m.cursor = idx

	if !strings.Contains(hoverRow(t, m, idx, true), dragHandle) {
		t.Error("a selected row must still show its handle: selection fills the row to full width, which used to squeeze the handle out")
	}
}

func TestHover_HandleShownOnLongName(t *testing.T) {
	long := "TE-11189-import-provider-not-found-on-rate-import-retry"
	m := New()
	m.SetSize(34, 40) // narrow enough that the name is ellipsized
	m.Focus()
	m.SetWorkspaces([]*data.Workspace{mkWS(long, "", []string{"medusa"}, time.Unix(1, 0))})

	idx := workspaceRowIndex(t, m, long)
	for _, selected := range []bool{false, true} {
		if selected {
			m.cursor = idx
		}
		if !strings.Contains(hoverRow(t, m, idx, selected), dragHandle) {
			t.Errorf("selected=%v: a name that fills the width must still leave room for the handle", selected)
		}
	}
}

func TestHover_HandleShownOnSelectedGroupHeader(t *testing.T) {
	m := dragModel(t, mkWS("one", "Triage", []string{"medusa"}, time.Unix(1, 0)))
	idx := groupRowIndex(t, m, "Triage")
	m.cursor = idx

	if !strings.Contains(hoverRow(t, m, idx, true), dragHandle) {
		t.Error("a selected group header must still show its handle")
	}
}

func TestHover_HandleShownOnLongGroupLabel(t *testing.T) {
	label := "a-very-long-group-label-that-fills-the-whole-pane"
	m := New()
	m.SetSize(30, 40)
	m.Focus()
	m.SetWorkspaces([]*data.Workspace{mkWS("one", label, []string{"medusa"}, time.Unix(1, 0))})

	idx := groupRowIndex(t, m, label)
	if !strings.Contains(hoverRow(t, m, idx, false), dragHandle) {
		t.Error("a group label filling the width must be clipped for the handle, not lose it")
	}
}

// TestHover_DoesNotChangeRowHeight is the property the reserved gutter exists
// for: rowLineCount reads the cursor, not the pointer, so a row that grew or
// shrank on hover would move every row below it out from under the pointer.
func TestHover_DoesNotChangeRowHeight(t *testing.T) {
	long := "TE-11189-import-provider-not-found-on-rate-import-retry"
	m := New()
	m.SetSize(34, 40)
	m.Focus()
	m.SetWorkspaces([]*data.Workspace{mkWS(long, "", []string{"medusa"}, time.Unix(1, 0))})

	idx := workspaceRowIndex(t, m, long)
	for _, cursor := range []int{-1, idx} {
		m.cursor = cursor
		before := m.rowLineCount(idx)
		m.Update(tea.MouseMotionMsg{X: 2, Y: screenYForRow(t, m, idx), Button: tea.MouseNone})
		if after := m.rowLineCount(idx); after != before {
			t.Errorf("cursor=%d: hover changed the row from %d lines to %d", cursor, before, after)
		}
	}
}

// TestHover_HandleKeepsRowWidth guards the alignment: the handled line must come
// back exactly contentWidth wide, or the selection padding that follows would
// push the handle off the right edge.
func TestHover_HandleKeepsRowWidth(t *testing.T) {
	m := dragModel(t, mkWS("exploration", "", []string{"medusa"}, time.Unix(1, 0)))
	idx := workspaceRowIndex(t, m, "exploration")
	m.cursor = idx

	rendered := hoverRow(t, m, idx, true)
	first := strings.SplitN(rendered, "\n", 2)[0]
	contentWidth := m.width - 3
	if got := lipgloss.Width(first); got != contentWidth {
		t.Errorf("handled line is %d columns, want %d", got, contentWidth)
	}
	if plain := ansi.Strip(first); !strings.HasSuffix(plain, dragHandle) {
		t.Errorf("the handle must be the last visible cell, flush right; got %q", plain)
	}
}

// TestHover_HandleIsNotPaintedInASurfaceColor guards the bug that made the
// handle invisible: Surface tokens are background tiers, and Surface3 against a
// dark theme's background is a ~1.2:1 contrast ratio. The handle rendered
// perfectly and could not be seen.
func TestHover_HandleIsNotPaintedInASurfaceColor(t *testing.T) {
	m := dragModel(t, mkWS("alpha", "Triage", []string{"medusa"}, time.Unix(1, 0)))

	wsIdx := workspaceRowIndex(t, m, "alpha")
	groupIdx := groupRowIndex(t, m, "Triage")

	for _, tc := range []struct {
		name     string
		idx      int
		selected bool
	}{
		{"workspace", wsIdx, false},
		{"workspace selected", wsIdx, true},
		{"group header", groupIdx, false},
		{"group header selected", groupIdx, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.selected {
				m.cursor = tc.idx
			} else {
				m.cursor = -1
			}
			rendered := hoverRow(t, m, tc.idx, tc.selected)
			if !strings.Contains(rendered, dragHandle) {
				t.Fatal("no handle rendered at all")
			}
			for name, surface := range map[string]color.Color{
				"Surface1": common.ColorSurface1,
				"Surface2": common.ColorSurface2,
				"Surface3": common.ColorSurface3,
			} {
				painted := lipgloss.NewStyle().Foreground(surface).Render(dragHandle)
				if strings.Contains(rendered, painted) {
					t.Errorf("handle painted in %s, a background tier", name)
				}
			}
		})
	}
}
