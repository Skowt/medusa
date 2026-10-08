package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/center"
	"github.com/Skowt/medusa/internal/ui/dashboard"
	"github.com/Skowt/medusa/internal/ui/layout"
	"github.com/Skowt/medusa/internal/ui/sidebar"
)

// newControlTestApp builds an App with one workspace that has a (stopped,
// so PTY-less) Claude tab in the center pane.
func newControlTestApp(t *testing.T) (*App, *data.Workspace) {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		Paths:      &config.Paths{RegistryPath: filepath.Join(tmp, "workspaces.json"), MetadataRoot: filepath.Join(tmp, "meta")},
		Assistants: map[string]config.AssistantConfig{"claude": {Command: "claude"}},
	}
	ws := data.NewCheckoutWorkspace("victim", "main", tmp)
	ws.Profile = "Default"
	ws.OpenTabs = []data.TabInfo{{Assistant: "claude", Name: "claude", SessionName: "medusa-victim-1", Status: "stopped"}}

	a := &App{
		config:          cfg,
		registry:        data.NewRegistry(cfg.Paths.RegistryPath),
		workspaces:      data.NewWorkspaceStore(cfg.Paths.MetadataRoot),
		center:          center.New(cfg),
		dashboard:       dashboard.New(),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		allWorkspaces:   []*data.Workspace{ws},
		dirtyWorkspaces: map[string]bool{string(ws.ID()): true},
	}
	_ = a.center.RestoreTabsFromWorkspace(ws)
	if !a.center.HasAgentTabsForWorkspace(string(ws.ID())) {
		t.Fatal("fixture: the workspace has no tab to drop")
	}
	a.activeWorkspace = ws
	return a, ws
}

func TestWorkspaceClosingDropsItsTabsBeforeTheSessionsDie(t *testing.T) {
	a, ws := newControlTestApp(t)

	a.handleWorkspaceClosing(workspaceClosingMsg{WorkspaceID: string(ws.ID())})

	if a.center.HasAgentTabsForWorkspace(string(ws.ID())) {
		t.Fatal("tabs survived workspace-closing, so killing the sessions would trigger a reattach")
	}
	if a.activeWorkspace != nil {
		t.Fatal("the closing workspace is still active")
	}
	if a.dirtyWorkspaces[string(ws.ID())] {
		t.Fatal("the closing workspace is still queued for persist")
	}
}

func TestVanishedWorkspaceIsDroppedOnReload(t *testing.T) {
	a, ws := newControlTestApp(t)

	a.dropVanishedWorkspaces(nil)

	if a.center.HasAgentTabsForWorkspace(string(ws.ID())) {
		t.Fatal("a workspace deleted from outside kept its tabs")
	}
}

func TestAWorkspaceThatOnlyChangedIDIsNotDropped(t *testing.T) {
	a, ws := newControlTestApp(t)
	moved := data.NewCheckoutWorkspace(ws.Name, "main", t.TempDir())

	a.dropVanishedWorkspaces([]*data.Workspace{moved})

	if !a.center.HasAgentTabsForWorkspace(string(ws.ID())) {
		t.Fatal("an ID migration was mistaken for a delete")
	}
}

func TestPersistIsRefusedForAnUnregisteredWorkspace(t *testing.T) {
	dir := t.TempDir()
	reg := data.NewRegistry(filepath.Join(dir, "workspaces.json"))
	if err := reg.AddWorkspace("kept", "kept-id", ""); err != nil {
		t.Fatal(err)
	}
	if !persistSnapshotAllowed(reg, "kept-id") {
		t.Fatal("a registered workspace was refused")
	}
	if persistSnapshotAllowed(reg, "gone-id") {
		t.Fatal("a deleted workspace would be written back")
	}

	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !persistSnapshotAllowed(data.NewRegistry(broken), "any") {
		t.Fatal("an unreadable registry blocked a save, losing tab state")
	}
}

func TestControlMessagesAreCritical(t *testing.T) {
	if !isCriticalExternalMsg(messages.RefreshDashboard{}) {
		t.Error("RefreshDashboard can be dropped under load")
	}
	if !isCriticalExternalMsg(workspaceClosingMsg{}) {
		t.Error("workspaceClosingMsg can be dropped under load")
	}
}

// A reload that read the registry before the delete removed the entry still
// lists the workspace. It must not restore the tabs the closing event dropped.
func TestAStaleReloadDoesNotRestoreAClosingWorkspace(t *testing.T) {
	a, ws := newControlTestApp(t)
	a.handleWorkspaceClosing(workspaceClosingMsg{WorkspaceID: string(ws.ID())})

	a.dropVanishedWorkspaces([]*data.Workspace{ws})
	_ = a.clearExternalClosing([]*data.Workspace{ws})
	_ = a.center.RestoreTabsFromWorkspace(ws)

	if a.center.HasAgentTabsForWorkspace(string(ws.ID())) {
		t.Fatal("a stale reload restored a workspace that is being deleted")
	}

	_ = a.clearExternalClosing(nil)
	if _, ok := a.externalClosing[string(ws.ID())]; ok {
		t.Fatal("the closing mark outlived the workspace")
	}
}

// A registry that cannot be read says nothing about which workspaces exist.
// Treating it as "none" would close every tab in the TUI.
func TestAFailedLoadKeepsEveryTab(t *testing.T) {
	a, ws := newControlTestApp(t)
	a.layout = layout.NewManager()
	if err := os.WriteFile(a.config.Paths.RegistryPath, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	msg, ok := a.loadWorkspaces()().(messages.WorkspacesLoaded)
	if !ok || msg.Err == nil {
		t.Fatalf("loadWorkspaces on a broken registry = %+v, want a load error", msg)
	}
	a.handleWorkspacesLoaded(msg)

	if !a.center.HasAgentTabsForWorkspace(string(ws.ID())) {
		t.Fatal("a failed registry read closed the workspace's tabs")
	}
	if len(a.allWorkspaces) != 1 {
		t.Fatalf("a failed load replaced the workspace list with %d entries", len(a.allWorkspaces))
	}
}
