package app

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
)

func monitorNames(wss []*data.Workspace) []string {
	names := make([]string, 0, len(wss))
	for _, ws := range wss {
		names = append(names, ws.Name)
	}
	return names
}

// The monitor filter's key is the source repo path, which is what the grid
// itself resolves through monitorProjectKeyLabel. Comparing it to a workspace
// root matched nothing for a workspace with a worktree of its own, so picking
// any project chip stopped the tmux tick polling anything and the grid froze on
// whatever tab state it last had.
func TestTmuxSyncWorkspaces_MonitorFilterMatchesTheProjectKey(t *testing.T) {
	medusaA := data.NewWorkspace("medusa-a", "medusa-a", "main", "/src/medusa", "/wt/medusa-a")
	medusaB := data.NewWorkspace("medusa-b", "medusa-b", "main", "/src/medusa", "/wt/medusa-b")
	other := data.NewWorkspace("other", "other", "main", "/src/other", "/wt/other")

	a := &App{
		monitorMode:   true,
		allWorkspaces: []*data.Workspace{medusaA, medusaB, other},
	}

	key, _ := a.monitorProjectKeyLabel(medusaA)
	if key != "/src/medusa" {
		t.Fatalf("project key = %q, want the source repo path", key)
	}
	a.monitorFilter = key

	got := monitorNames(a.tmuxSyncWorkspaces())
	want := []string{"medusa-a", "medusa-b"}
	if len(got) != len(want) {
		t.Fatalf("synced %v, want every workspace on the filtered repo %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("synced %v, want %v", got, want)
		}
	}
}

// A checkout workspace has the repo as its root, so the old root comparison
// happened to succeed for it. It must keep working now the key is resolved
// properly, alongside the worktree workspaces on the same repo.
func TestTmuxSyncWorkspaces_MonitorFilterCoversCheckoutWorkspaces(t *testing.T) {
	checkout := data.NewCheckoutWorkspace("in-place", "main", "/src/medusa")
	worktree := data.NewWorkspace("feature", "feature", "main", "/src/medusa", "/wt/feature")

	a := &App{
		monitorMode:   true,
		monitorFilter: "/src/medusa",
		allWorkspaces: []*data.Workspace{checkout, worktree},
	}

	if got := monitorNames(a.tmuxSyncWorkspaces()); len(got) != 2 {
		t.Fatalf("synced %v, want both workspaces on the repo", got)
	}
}

// No filter means the whole grid, and the grid is every workspace.
func TestTmuxSyncWorkspaces_MonitorWithoutAFilterSyncsEverything(t *testing.T) {
	a := &App{
		monitorMode: true,
		allWorkspaces: []*data.Workspace{
			data.NewWorkspace("one", "one", "main", "/src/medusa", "/wt/one"),
			data.NewWorkspace("two", "two", "main", "/src/other", "/wt/two"),
		},
	}

	if got := monitorNames(a.tmuxSyncWorkspaces()); len(got) != 2 {
		t.Fatalf("synced %v, want every workspace", got)
	}
}

// Outside monitor mode only the active workspace is polled — the filter is a
// monitor-only control and must not reach this path.
func TestTmuxSyncWorkspaces_OutsideMonitorSyncsTheActiveWorkspaceOnly(t *testing.T) {
	active := data.NewWorkspace("active", "active", "main", "/src/medusa", "/wt/active")
	a := &App{
		monitorFilter:   "/src/medusa",
		activeWorkspace: active,
		allWorkspaces: []*data.Workspace{
			active,
			data.NewWorkspace("idle", "idle", "main", "/src/medusa", "/wt/idle"),
		},
	}

	got := monitorNames(a.tmuxSyncWorkspaces())
	if len(got) != 1 || got[0] != "active" {
		t.Fatalf("synced %v, want only the active workspace", got)
	}
}
