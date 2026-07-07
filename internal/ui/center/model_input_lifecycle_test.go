package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/vterm"
)

// TestUpdatePtyTabReattachResultSetsFullscreen verifies that reattaching a
// tab (explicit restart, auto-restart, or plain reattach) updates the
// in-memory tab.Fullscreen flag from the message, not just at tab-creation
// time. Without this, a classic (pre-fullscreen-feature) tab that gets
// relaunched in fullscreen mode would keep tab.Fullscreen=false, so mouse
// input would never be routed to the PTY even though Claude is now running
// with mouse reporting enabled.
func TestUpdatePtyTabReattachResultSetsFullscreen(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}

	for _, want := range []bool{true, false} {
		m := New(cfg)
		ws := data.NewWorkspace("wt", "", "", "/tmp/repo", "/tmp/repo")
		m.SetWorkspace(ws)
		wsID := string(ws.ID())

		tab := &Tab{
			ID:         TabID("tab-1"),
			Workspace:  ws,
			Terminal:   vterm.New(80, 24),
			Fullscreen: !want, // start from the opposite value
		}
		m.tabsByWorkspace[wsID] = []*Tab{tab}
		m.activeTabByWorkspace[wsID] = 0

		agent := &appPty.Agent{Session: "sess-1"}

		msg := ptyTabReattachResult{
			WorkspaceID: wsID,
			TabID:       tab.ID,
			Agent:       agent,
			Rows:        24,
			Cols:        80,
			Fullscreen:  want,
		}

		_, _ = m.updatePtyTabReattachResult(msg)

		tab.mu.Lock()
		got := tab.Fullscreen
		running := tab.Running
		tab.mu.Unlock()

		if !running {
			t.Fatalf("expected tab.Running=true after reattach result, fullscreen=%v", want)
		}
		if got != want {
			t.Fatalf("expected tab.Fullscreen=%v after reattach result, got %v", want, got)
		}
	}
}
