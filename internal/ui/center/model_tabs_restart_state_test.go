package center

import (
	"strings"
	"testing"
	"time"

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

// TestRestartingSurvivesEmptyRepaint covers the window a restart actually
// spans: the replacement's tmux client repaints an empty pane long before the
// agent boots, so the tab must still read as restarting until something is
// painted. Clearing on first output instead drops the tab back to a blank
// pane and a STOPPED status for the rest of the agent's startup.
func TestRestartingSurvivesEmptyRepaint(t *testing.T) {
	m, tab := restartingTestTab(t)
	tab.restarting = true
	tab.restartingSince = time.Now()

	// tmux's post-attach repaint of an empty pane.
	writeTabOutput(tab, []byte("\x1b[2J\x1b[H"))
	tab.mu.Lock()
	restarting := m.restartingLocked(tab)
	tab.mu.Unlock()
	if !restarting {
		t.Fatal("tab stopped reading as restarting after an empty repaint")
	}
	if m.TerminalLayer() != nil {
		t.Fatal("TerminalLayer must return nil while restarting so the placeholder renders")
	}

	// The agent's first real frame.
	writeTabOutput(tab, []byte("Claude"))
	tab.mu.Lock()
	restarting = m.restartingLocked(tab)
	tab.mu.Unlock()
	if restarting {
		t.Fatal("tab still reads as restarting after the agent painted")
	}
	if m.TerminalLayer() == nil {
		t.Fatal("TerminalLayer must resume once the agent has painted")
	}
}

// TestRestartingExpires keeps a tab that never paints from claiming a restart
// is under way forever; it has to fall back to its real (stopped) state.
func TestRestartingExpires(t *testing.T) {
	m, tab := restartingTestTab(t)
	tab.restarting = true
	tab.restartingSince = time.Now().Add(-restartingMaxDisplay - time.Second)

	tab.mu.Lock()
	restarting := m.restartingLocked(tab)
	stillSet := tab.restarting
	tab.mu.Unlock()
	if restarting || stillSet {
		t.Fatal("restarting state outlived its display bound")
	}
}

// TestRestartingStatusLine guards the status shown mid-restart: tab.Running is
// false during the window, which otherwise renders as STOPPED with a "restart
// it yourself" hint while a restart is in fact already running.
func TestRestartingStatusLine(t *testing.T) {
	m, tab := restartingTestTab(t)
	tab.restarting = true
	tab.restartingSince = time.Now()

	tab.mu.Lock()
	status := m.terminalStatusLineLocked(tab)
	tab.mu.Unlock()
	if !strings.Contains(status, "RESTARTING") {
		t.Fatalf("status line during restart = %q, want it to say RESTARTING", status)
	}
	if strings.Contains(status, "STOPPED") {
		t.Fatalf("status line during restart = %q, want no STOPPED", status)
	}
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
