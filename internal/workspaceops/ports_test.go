package workspaceops

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/process"
)

// Each medusa process (the TUI, every CLI run) has its own allocator, so a
// workspace created by one must not be handed a port another already stored
// on disk; and a deleted workspace's port must be free again.
func TestCreateAndDeleteManagePersistedPorts(t *testing.T) {
	repo := newRepo(t)
	d := newDeps(t)
	d.Ports = process.NewPortAllocator(6200, 10)
	first, err := d.Create(CreateRequest{Name: "first", Repos: []data.RepoRef{repo}, Bases: []string{"main"}, Profile: "Work"})
	if err != nil {
		t.Fatal(err)
	}

	other := d
	other.Ports = process.NewPortAllocator(6200, 10)
	second, err := other.Create(CreateRequest{Name: "second", Repos: []data.RepoRef{repo}, Bases: []string{"main"}, Profile: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := d.Store.Load(second.ID())
	if err != nil {
		t.Fatal(err)
	}
	if first.Port != 6200 || stored.Port != 6210 {
		t.Fatalf("stored ports = %d, %d; want 6200, 6210", first.Port, stored.Port)
	}

	other.Delete(second)
	if _, ok := other.Ports.GetPort(string(second.ID())); ok {
		t.Fatal("Delete kept the workspace's port allocated")
	}
}
