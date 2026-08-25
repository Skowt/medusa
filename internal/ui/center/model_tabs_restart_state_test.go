package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/vterm"
)

func restartingTestTab(t *testing.T) (*Model, *Tab) {
	t.Helper()
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	ws := data.NewWorkspace("wt", "", "", "/tmp/repo", "/tmp/repo")
	m.SetWorkspace(ws)
	tab := &Tab{
		ID:        TabID("tab-1"),
		Assistant: "claude",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
	}
	m.tabsByWorkspace[string(ws.ID())] = []*Tab{tab}
	m.activeTabByWorkspace[string(ws.ID())] = 0
	return m, tab
}

// TestRestartClearsActivityIndicator covers the workspace spinner: a restart
// kills the turn the agent was running and Claude Code's Stop hook never fires
// for it, so restarting has to clear the activity state itself or the sidebar
// keeps spinning for a workspace that is doing nothing.
func TestRestartClearsActivityIndicator(t *testing.T) {
	m, tab := restartingTestTab(t)

	cmd := m.restartTab(0)
	if cmd == nil {
		t.Fatal("restartTab returned no command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("restartTab command produced %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("restart batch has %d commands, want 2 (clear activity + restart)", len(batch))
	}

	// The clear must run on its own command rather than after the tmux
	// teardown, so the spinner stops the moment the user asks for a restart.
	interrupted, ok := batch[0]().(messages.AgentInterrupted)
	if !ok {
		t.Fatalf("first restart command produced %T, want messages.AgentInterrupted", batch[0]())
	}
	if want := string(tab.Workspace.ID()); interrupted.WorkspaceID != want {
		t.Fatalf("AgentInterrupted.WorkspaceID = %q, want %q", interrupted.WorkspaceID, want)
	}
}
