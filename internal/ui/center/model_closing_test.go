package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
)

func closingTestModel(t *testing.T) (*Model, *data.Workspace) {
	t.Helper()
	m := New(&config.Config{Assistants: map[string]config.AssistantConfig{"claude": {Command: "claude"}}})
	ws := data.NewCheckoutWorkspace("victim", "main", t.TempDir())
	ws.OpenTabs = []data.TabInfo{{Assistant: "claude", Name: "claude", SessionName: "s", Status: "stopped"}}
	return m, ws
}

// A workspace being deleted from outside must not get its tabs back: a tab
// attaches with new-session -A, which recreates a killed session.
func TestAClosingWorkspaceIsNotRestored(t *testing.T) {
	m, ws := closingTestModel(t)
	m.SetWorkspaceClosing(string(ws.ID()), true)

	_ = m.RestoreTabsFromWorkspace(ws)

	if m.HasAgentTabsForWorkspace(string(ws.ID())) {
		t.Fatal("a closing workspace had its tabs restored")
	}
	m.SetWorkspaceClosing(string(ws.ID()), false)
	_ = m.RestoreTabsFromWorkspace(ws)
	if !m.HasAgentTabsForWorkspace(string(ws.ID())) {
		t.Fatal("clearing the closing mark did not let the workspace restore again")
	}
}

// A create that started before the closing mark lands after it, and must be
// dropped rather than become a tab.
func TestALateTabForAClosingWorkspaceIsDropped(t *testing.T) {
	m, ws := closingTestModel(t)
	m.SetWorkspaceClosing(string(ws.ID()), true)

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: ws,
		Assistant: "claude",
		Agent:     &appPty.Agent{Type: appPty.AgentClaude, Workspace: ws, Session: "s"},
		TabID:     "tab-late",
	})

	if m.HasAgentTabsForWorkspace(string(ws.ID())) {
		t.Fatal("a late tab was added to a closing workspace")
	}
}
