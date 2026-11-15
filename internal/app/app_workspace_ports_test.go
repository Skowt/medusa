package app

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/process"
)

// Every loaded workspace must end up with a stored port, and a restarted
// medusa (a fresh allocator, possibly touched before the load) must hand each
// one the same port again.
func TestLoadWorkspaces_PersistsPortsThatSurviveRestart(t *testing.T) {
	tests := []struct {
		name           string
		storedPorts    map[string]int
		expectedOutput map[string]int
	}{
		{name: "records without ports get distinct ones", storedPorts: map[string]int{"alpha": 0, "beta": 0}, expectedOutput: map[string]int{"alpha": 6200, "beta": 6210}},
		{name: "a new port skips a stored one", storedPorts: map[string]int{"alpha": 6200, "beta": 0}, expectedOutput: map[string]int{"alpha": 6200, "beta": 6210}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			registry := data.NewRegistry(filepath.Join(tmp, "workspaces.json"))
			store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
			for _, name := range []string{"alpha", "beta"} {
				ws := data.NewWorkspace(name, name, "main", "/tmp/repo-"+name, t.TempDir())
				ws.Port = tt.storedPorts[name]
				if err := store.Save(ws); err != nil {
					t.Fatal(err)
				}
				if err := registry.AddWorkspace(ws.Name, string(ws.ID()), ""); err != nil {
					t.Fatal(err)
				}
			}

			first := &App{registry: registry, workspaces: store, scripts: process.NewScriptRunner(6200, 10)}
			if got := loadedPorts(t, first); !reflect.DeepEqual(got, tt.expectedOutput) {
				t.Fatalf("first load ports = %v, want %v", got, tt.expectedOutput)
			}
			restarted := &App{registry: registry, workspaces: store, scripts: process.NewScriptRunner(6200, 10)}
			restarted.scripts.Ports().AllocatePort("touched-before-load")
			if got := loadedPorts(t, restarted); !reflect.DeepEqual(got, tt.expectedOutput) {
				t.Fatalf("ports after restart = %v, want %v", got, tt.expectedOutput)
			}
		})
	}
}

// loadedPorts loads the workspaces and returns, per name, the port stored on
// the record, failing if the allocator disagrees with it.
func loadedPorts(t *testing.T, a *App) map[string]int {
	t.Helper()
	loaded, ok := a.loadWorkspaces()().(messages.WorkspacesLoaded)
	if !ok {
		t.Fatal("expected WorkspacesLoaded")
	}
	ports := make(map[string]int)
	for _, ws := range loaded.Workspaces {
		stored, err := a.workspaces.Load(ws.ID())
		if err != nil {
			t.Fatal(err)
		}
		if allocated, _ := a.scripts.Ports().GetPort(string(ws.ID())); allocated != stored.Port {
			t.Errorf("%s allocator port %d, stored port %d", ws.Name, allocated, stored.Port)
		}
		ports[ws.Name] = stored.Port
	}
	return ports
}
