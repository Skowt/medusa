package app

import (
	"path/filepath"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/ui/center"
)

func archivedWorkspaceWithRunningTab(name string) *data.Workspace {
	ws := data.NewWorkspace(name, name, "main", "/tmp/repo", "/tmp/"+name)
	ws.Status = data.StatusArchived
	ws.OpenTabs = []data.TabInfo{{
		Assistant:       "claude",
		Name:            "claude",
		SessionName:     "medusa-" + name + "-1",
		Status:          "running",
		ClaudeSessionID: "11111111-2222-3333-4444-555555555555",
	}}
	return ws
}

// Archive kills the workspace's tmux sessions on purpose; the snapshot taken
// just before must stay frozen so unarchive can relaunch the agents. A tmux
// sync sweep must therefore never touch an archived workspace.
func TestSyncWorkspaceTabsFromTmuxSkipsArchived(t *testing.T) {
	ws := archivedWorkspaceWithRunningTab("ws")
	app := &App{tmuxAvailable: true}
	if cmd := app.syncWorkspaceTabsFromTmux(ws); cmd != nil {
		t.Fatalf("expected no sync command for archived workspace")
	}
}

// A sync result can arrive after the workspace was archived (the check ran
// before archive killed the sessions). It must not rewrite the frozen
// snapshot to "stopped" — that turns unarchive's restore into inert
// placeholders instead of relaunching agents.
func TestTmuxTabsSyncResultIgnoresArchivedWorkspace(t *testing.T) {
	ws := archivedWorkspaceWithRunningTab("ws")
	app := &App{allWorkspaces: []*data.Workspace{ws}}

	cmds := app.handleTmuxTabsSyncResult(tmuxTabsSyncResult{
		WorkspaceID: string(ws.ID()),
		Updates: []tmuxTabStatusUpdate{{
			SessionName:   ws.OpenTabs[0].SessionName,
			Status:        "stopped",
			NotifyStopped: true,
		}},
	})

	if got := ws.OpenTabs[0].Status; got != "running" {
		t.Fatalf("archived workspace tab status = %q, want %q", got, "running")
	}
	if len(cmds) != 0 {
		t.Fatalf("expected no commands for archived workspace, got %d", len(cmds))
	}
}

// A debounced persist scheduled before an archive fires after CleanupWorkspace
// removed the in-memory tabs. It must not overwrite the archived snapshot
// with the (now empty) live tab state.
func TestPersistDebounceSkipsArchivedWorkspace(t *testing.T) {
	ws := archivedWorkspaceWithRunningTab("ws")
	store := data.NewWorkspaceStore(filepath.Join(t.TempDir(), "workspaces-metadata"))
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save workspace: %v", err)
	}

	app := &App{
		workspaces:      store,
		allWorkspaces:   []*data.Workspace{ws},
		center:          center.New(&config.Config{}),
		dirtyWorkspaces: map[string]bool{string(ws.ID()): true},
		persistToken:    1,
	}
	if cmd := app.handlePersistDebounce(persistDebounceMsg{token: 1}); cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("unexpected message from persist command: %v", msg)
		}
	}

	if len(ws.OpenTabs) != 1 || ws.OpenTabs[0].Status != "running" {
		t.Fatalf("archived workspace open tabs were overwritten: %+v", ws.OpenTabs)
	}
}
