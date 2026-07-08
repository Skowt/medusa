package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/vterm"
)

// TestSetWorkspaceReconcilesTabVtermSize guards against the "input cut off"
// bug. terminalMetrics().Height depends on infoBarHeight(), which is 0 while
// no workspace is active and 2 once one is. A tab sized while m.workspace is
// nil therefore gets a vterm 2 rows taller than it will ever be painted (a tab
// is only ever painted with its workspace active). SetWorkspace must reconcile
// the tab back down to the paint height, otherwise PositionedVTermLayer.DrawAt
// clips the bottom rows — Claude's input box + the line being typed.
func TestSetWorkspaceReconcilesTabVtermSize(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	ws := data.NewWorkspace("wt", "", "", "/tmp/repo", "/tmp/repo")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:        TabID("tab-1"),
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	// Size the pane while NO workspace is active: the info bar is absent, so
	// the tab gets the taller height.
	m.SetSize(120, 47)
	tallHeight := tab.Terminal.Height
	if tallHeight != m.terminalMetrics().Height {
		t.Fatalf("precondition: tab height %d != nil-workspace metrics %d", tallHeight, m.terminalMetrics().Height)
	}

	// Activate the workspace: the info bar now reserves rows, so the paint
	// height shrinks. The tab's vterm must follow, or its bottom rows clip.
	m.SetWorkspace(ws)
	want := m.terminalMetrics().Height
	if want >= tallHeight {
		t.Fatalf("precondition: activating workspace should shrink the paint height (was %d, now %d); test can't detect the bug", tallHeight, want)
	}
	if got := tab.Terminal.Height; got != want {
		t.Fatalf("tab vterm height not reconciled after SetWorkspace: got %d, want %d (current paint height)", got, want)
	}
}

// TestReattachResultSizesFromCurrentMetrics verifies that installing a reattach
// result sizes the new vterm from the current terminal metrics, not from the
// Rows/Cols captured when the (async) reattach was initiated. The captured
// value is stale by the time the result arrives — trusting it is what left
// tabs sized taller than the paint height and got their input box clipped.
func TestReattachResultSizesFromCurrentMetrics(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	ws := data.NewWorkspace("wt", "", "", "/tmp/repo", "/tmp/repo")
	wsID := string(ws.ID())
	m.SetWorkspace(ws)
	m.SetSize(120, 47)

	want := m.terminalMetrics().Height

	tab := &Tab{
		ID:        TabID("tab-1"),
		Workspace: ws,
		// Terminal nil: the handler must create it at the current metrics.
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0

	staleRows := want + 2 // as if captured while the info bar was absent
	msg := ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       &appPty.Agent{Session: "sess-1"},
		Rows:        staleRows,
		Cols:        80,
		Fullscreen:  true,
	}

	_, _ = m.updatePtyTabReattachResult(msg)

	if tab.Terminal == nil {
		t.Fatalf("expected reattach to create a vterm")
	}
	if got := tab.Terminal.Height; got != want {
		t.Fatalf("vterm sized from stale Rows: got %d, want %d (current metrics); staleRows was %d", got, want, staleRows)
	}
}
