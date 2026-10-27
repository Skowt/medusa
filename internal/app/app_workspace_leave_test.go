package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/center"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/ui/dashboard"
	"github.com/Skowt/medusa/internal/ui/layout"
	"github.com/Skowt/medusa/internal/ui/sidebar"
)

// newArchiveTestApp builds an App showing wss on the dashboard, with the
// given groups collapsed and active open.
func newArchiveTestApp(t *testing.T, collapsed map[string]bool, active *data.Workspace, wss ...*data.Workspace) *App {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		Paths: &config.Paths{
			ConfigPath:   filepath.Join(tmp, "config.json"),
			RegistryPath: filepath.Join(tmp, "workspaces.json"),
			MetadataRoot: filepath.Join(tmp, "meta"),
		},
		UI: config.UISettings{CollapsedGroups: collapsed},
	}
	lm := layout.NewManager()
	lm.SetSidebarHidden(true)
	a := &App{
		config:          cfg,
		registry:        data.NewRegistry(cfg.Paths.RegistryPath),
		workspaces:      data.NewWorkspaceStore(cfg.Paths.MetadataRoot),
		center:          center.New(cfg),
		dashboard:       dashboard.New(),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		toast:           common.NewToastModel(),
		layout:          lm,
		allWorkspaces:   wss,
		dirtyWorkspaces: map[string]bool{},
	}
	a.dashboard.SetCollapsedGroups(collapsed)
	a.dashboard.SetWorkspaces(wss)
	a.activeWorkspace = active
	return a
}

func archiveTestWorkspace(name, group string, created int64) *data.Workspace {
	ws := data.NewWorkspace(name, name, "main", "/repo", "/wt/"+name)
	ws.Group = group
	ws.Created = time.Unix(created, 0)
	return ws
}

// Archiving the open workspace moves to the next one on screen, not to the
// first workspace in the list, which here sits in a collapsed group.
func TestArchiveActiveSwitchesToNextVisibleWorkspace(t *testing.T) {
	hidden := archiveTestWorkspace("hidden", "shut", 1)
	leaving := archiveTestWorkspace("leaving", "", 2)
	shown := archiveTestWorkspace("shown", "", 3)
	a := newArchiveTestApp(t, map[string]bool{"shut": true}, leaving, hidden, leaving, shown)

	_ = a.handleArchiveWorkspace(messages.ArchiveWorkspace{Workspace: leaving})

	if a.activeWorkspace != shown {
		t.Fatalf("active after archive = %v, want shown", workspaceName(a.activeWorkspace))
	}
	if a.showWelcome || a.stayHome {
		t.Fatal("a workspace was activated but the home screen is still up")
	}
}

// With nothing visible left, archive lands on the home screen and the reload
// that follows must not open a workspace from inside a collapsed group.
func TestArchiveActiveGoesHomeWhenNothingIsVisible(t *testing.T) {
	hidden := archiveTestWorkspace("hidden", "shut", 1)
	leaving := archiveTestWorkspace("leaving", "", 2)
	a := newArchiveTestApp(t, map[string]bool{"shut": true}, leaving, hidden, leaving)

	_ = a.handleArchiveWorkspace(messages.ArchiveWorkspace{Workspace: leaving})

	if a.activeWorkspace != nil || !a.showWelcome {
		t.Fatalf("active after archive = %v, want the home screen", workspaceName(a.activeWorkspace))
	}

	if got := reloadActivates(a, hidden, leaving); got != nil {
		t.Fatalf("reload auto-activated %v", workspaceName(got))
	}
}

// Archiving a workspace that is not open leaves the open one alone.
func TestArchiveInactiveKeepsTheActiveWorkspace(t *testing.T) {
	open := archiveTestWorkspace("open", "", 1)
	other := archiveTestWorkspace("other", "", 2)
	a := newArchiveTestApp(t, nil, open, open, other)

	_ = a.handleArchiveWorkspace(messages.ArchiveWorkspace{Workspace: other})

	if a.activeWorkspace != open {
		t.Fatalf("active after archive = %v, want open", workspaceName(a.activeWorkspace))
	}
}

// Deleting the open workspace moves to the next one on screen.
func TestDeleteActiveSwitchesToNextVisibleWorkspace(t *testing.T) {
	hidden := archiveTestWorkspace("hidden", "shut", 1)
	leaving := archiveTestWorkspace("leaving", "", 2)
	shown := archiveTestWorkspace("shown", "", 3)
	a := newArchiveTestApp(t, map[string]bool{"shut": true}, leaving, hidden, leaving, shown)

	_ = a.handleDeleteWorkspace(messages.DeleteWorkspace{Workspace: leaving})

	if a.activeWorkspace != shown {
		t.Fatalf("active after delete = %v, want shown", workspaceName(a.activeWorkspace))
	}
}

// Deleting the open workspace with nothing else visible goes home, and the
// reload after the delete lands does not open a collapsed workspace.
func TestDeleteActiveGoesHomeWhenNothingIsVisible(t *testing.T) {
	hidden := archiveTestWorkspace("hidden", "shut", 1)
	leaving := archiveTestWorkspace("leaving", "", 2)
	a := newArchiveTestApp(t, map[string]bool{"shut": true}, leaving, hidden, leaving)

	_ = a.handleDeleteWorkspace(messages.DeleteWorkspace{Workspace: leaving})
	_ = a.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: leaving})

	if a.activeWorkspace != nil || !a.showWelcome {
		t.Fatalf("active after delete = %v, want the home screen", workspaceName(a.activeWorkspace))
	}
	if got := reloadActivates(a, hidden); got != nil {
		t.Fatalf("reload auto-activated %v", workspaceName(got))
	}
}

// A delete announced from outside (the workspace CLI) leaves the same way.
func TestExternalDeleteSwitchesToNextVisibleWorkspace(t *testing.T) {
	leaving := archiveTestWorkspace("leaving", "", 1)
	shown := archiveTestWorkspace("shown", "", 2)
	a := newArchiveTestApp(t, nil, leaving, leaving, shown)

	_ = a.handleWorkspaceClosing(workspaceClosingMsg{WorkspaceID: string(leaving.ID())})

	if a.activeWorkspace != shown {
		t.Fatalf("active after outside delete = %v, want shown", workspaceName(a.activeWorkspace))
	}
}

// When a reload finds several workspaces gone at once, leaving the open one
// must not land on another that went with it. The list order makes also-gone
// dropped first, so nothing would move off it again afterwards.
func TestVanishedWorkspacesNeverLandOnAnotherVanishedOne(t *testing.T) {
	leaving := archiveTestWorkspace("leaving", "", 1)
	alsoGone := archiveTestWorkspace("also-gone", "", 2)
	shown := archiveTestWorkspace("shown", "", 3)
	a := newArchiveTestApp(t, nil, leaving, alsoGone, leaving, shown)

	_ = a.handleWorkspacesLoaded(messages.WorkspacesLoaded{Workspaces: []*data.Workspace{shown}})

	if a.activeWorkspace != shown {
		t.Fatalf("active after reload = %v, want shown", workspaceName(a.activeWorkspace))
	}
}

// Going home on purpose stays home: a later reload does not open a workspace.
func TestGoingHomeStaysHomeAcrossReloads(t *testing.T) {
	open := archiveTestWorkspace("open", "", 1)
	other := archiveTestWorkspace("other", "", 2)
	a := newArchiveTestApp(t, nil, open, open, other)

	a.goHome()

	if got := reloadActivates(a, open, other); got != nil {
		t.Fatalf("reload auto-activated %v", workspaceName(got))
	}
}

// A cold start still opens a workspace: only goHome pins the home screen.
func TestStartupStillAutoActivates(t *testing.T) {
	first := archiveTestWorkspace("first", "", 1)
	a := newArchiveTestApp(t, nil, nil, first)
	a.showWelcome = true

	if got := reloadActivates(a, first); got != first {
		t.Fatalf("startup activated %v, want first", workspaceName(got))
	}
}

// reloadActivates runs a reload listing wss and returns the workspace it
// opens on its own, or nil.
func reloadActivates(a *App, wss ...*data.Workspace) *data.Workspace {
	for _, cmd := range a.handleWorkspacesLoaded(messages.WorkspacesLoaded{Workspaces: wss}) {
		if cmd == nil {
			continue
		}
		if activated, ok := cmd().(messages.WorkspaceActivated); ok {
			return activated.Workspace
		}
	}
	return nil
}

func workspaceName(ws *data.Workspace) string {
	if ws == nil {
		return "<home>"
	}
	return ws.Name
}
