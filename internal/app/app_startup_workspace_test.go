package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
)

func newStartupApp(t *testing.T, last string, wss ...*data.Workspace) *App {
	t.Helper()
	return &App{
		config: &config.Config{
			Paths: &config.Paths{ConfigPath: filepath.Join(t.TempDir(), "config.json")},
			UI:    config.UISettings{LastWorkspace: last},
		},
		allWorkspaces: wss,
	}
}

func startupWorkspace(name string) *data.Workspace {
	return data.NewWorkspace(name, name, "main", "/repo", "/wt/"+name)
}

// The workspace medusa was on when it exited is the one it comes back to,
// wherever it sits in the list.
func TestStartupWorkspaceIDPrefersLast(t *testing.T) {
	first, last := startupWorkspace("first"), startupWorkspace("last")
	a := newStartupApp(t, string(last.ID()), first, last)
	if got := a.startupWorkspaceID(); got != string(last.ID()) {
		t.Fatalf("startupWorkspaceID = %q, want %q", got, last.ID())
	}
}

// A remembered workspace that is gone, archived, or orphaned must not leave
// medusa on the welcome screen: it falls back to the top of the list.
func TestStartupWorkspaceIDFallsBackToFirst(t *testing.T) {
	first, second := startupWorkspace("first"), startupWorkspace("second")

	t.Run("no memory", func(t *testing.T) {
		a := newStartupApp(t, "", first, second)
		if got := a.startupWorkspaceID(); got != string(first.ID()) {
			t.Fatalf("startupWorkspaceID = %q, want %q", got, first.ID())
		}
	})

	t.Run("workspace deleted", func(t *testing.T) {
		a := newStartupApp(t, "gone", first, second)
		if got := a.startupWorkspaceID(); got != string(first.ID()) {
			t.Fatalf("startupWorkspaceID = %q, want %q", got, first.ID())
		}
	})

	t.Run("workspace archived", func(t *testing.T) {
		archived := startupWorkspace("archived")
		archived.Status = data.StatusArchived
		a := newStartupApp(t, string(archived.ID()), first, archived)
		if got := a.startupWorkspaceID(); got != string(first.ID()) {
			t.Fatalf("startupWorkspaceID = %q, want %q", got, first.ID())
		}
	})

	t.Run("no eligible workspaces", func(t *testing.T) {
		archived := startupWorkspace("archived")
		archived.Status = data.StatusArchived
		a := newStartupApp(t, string(archived.ID()), archived)
		if got := a.startupWorkspaceID(); got != "" {
			t.Fatalf("startupWorkspaceID = %q, want the welcome screen", got)
		}
	})
}

// The memory has to reach disk, and only when it actually changed — every
// workspace switch runs through here and each save rewrites the config file.
func TestRememberLastWorkspacePersists(t *testing.T) {
	ws := startupWorkspace("ws")
	a := newStartupApp(t, "", ws)

	a.rememberLastWorkspace(ws)
	if a.config.UI.LastWorkspace != string(ws.ID()) {
		t.Fatalf("LastWorkspace = %q, want %q", a.config.UI.LastWorkspace, ws.ID())
	}
	stat, err := os.Stat(a.config.Paths.ConfigPath)
	if err != nil {
		t.Fatalf("config was not written: %v", err)
	}

	a.rememberLastWorkspace(ws)
	again, err := os.Stat(a.config.Paths.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(stat.ModTime()) || again.Size() != stat.Size() {
		t.Fatal("re-activating the same workspace rewrote the config file")
	}

	a.rememberLastWorkspace(nil)
	if a.config.UI.LastWorkspace != string(ws.ID()) {
		t.Fatal("a nil workspace cleared the remembered one")
	}
}
