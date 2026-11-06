package dashboard

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// openWorkspaceModel builds a focused, rendered dashboard with the second of
// two workspaces open.
func openWorkspaceModel(t *testing.T) (*Model, *data.Workspace) {
	t.Helper()
	a := mkWS("a", "", []string{"r"}, time.Unix(1, 0))
	b := mkWS("b", "", []string{"r"}, time.Unix(2, 0))
	m := New()
	m.SetCaffeinate(true, false)
	m.SetSize(40, 30)
	m.Focus()
	m.SetWorkspaces([]*data.Workspace{a, b})
	m.Update(messages.WorkspaceActivated{Workspace: b})
	m.View() // lays out the toolbar and its hit regions
	return m, b
}

func assertHighlighted(t *testing.T, m *Model, ws *data.Workspace) {
	t.Helper()
	for i, row := range m.rows {
		if m.rowSelected(i) {
			if row.Workspace != ws {
				t.Fatalf("highlighted row is %v, want workspace %s", row, ws.Name)
			}
			return
		}
	}
	t.Fatalf("no row is highlighted, want workspace %s", ws.Name)
}

// Clicking a toolbar button with the mouse runs it and nothing else. It used
// to move keyboard focus onto the toolbar, which took the highlight off the
// open workspace until a row was clicked. Pressing [C] to let the Mac sleep is
// where that showed: after waking up, no workspace looked selected.
func TestToolbarClickKeepsTheOpenWorkspaceHighlighted(t *testing.T) {
	probe, _ := openWorkspaceModel(t)
	for i := range probe.toolbarHits {
		m, open := openWorkspaceModel(t)
		hit := m.toolbarHits[i]
		click := tea.MouseClickMsg{
			X:      hit.region.X + 1,
			Y:      hit.region.Y + m.toolbarY + 1,
			Button: tea.MouseLeft,
		}
		if _, cmd := m.Update(click); cmd == nil {
			t.Fatalf("button %v: click produced no command", hit.kind)
		}
		assertHighlighted(t, m, open)
	}
}

// A workspace opened from anywhere else puts the cursor on its row, and the
// row must show it, even if the keyboard had last moved onto the toolbar.
func TestActivatingAWorkspaceLeavesTheToolbar(t *testing.T) {
	m, open := openWorkspaceModel(t)
	m.cursor = m.findSelectableRow(len(m.rows)-1, -1)
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // past the last row, onto the toolbar
	if !m.toolbarFocused {
		t.Fatal("down from the last row did not focus the toolbar")
	}

	m.Update(messages.WorkspaceActivated{Workspace: open})
	assertHighlighted(t, m, open)
}
