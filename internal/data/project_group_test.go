package data

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGroupWorkspace_AllRoots(t *testing.T) {
	gw := &GroupWorkspace{
		Name: "test-group-ws",
		Primary: Workspace{
			Name: "test-group-ws",
			Root: "/groups/test-group-ws",
		},
		Secondary: []Workspace{
			{Name: "api", Root: "/groups/test-group-ws/api"},
			{Name: "protos", Root: "/groups/test-group-ws/protos"},
			{Name: "web", Root: "/groups/test-group-ws/web"},
		},
	}

	roots := gw.AllRoots()
	if len(roots) != 3 {
		t.Fatalf("AllRoots() returned %d items, want 3", len(roots))
	}
	for i, ws := range gw.Secondary {
		if roots[i] != ws.Root {
			t.Errorf("AllRoots()[%d] = %q, want %q", i, roots[i], ws.Root)
		}
	}
}

func TestGroupWorkspace_SecondaryRootsNotPersisted(t *testing.T) {
	// SecondaryRoots on Workspace is json:"-" (transient). Verify that after
	// saving and reloading a GroupWorkspace, Primary.SecondaryRoots is empty.
	// This guards the invariant that callers must repopulate it from AllRoots().
	store := NewWorkspaceStore(t.TempDir())

	gw := &GroupWorkspace{
		Name:      "persist-test",
		GroupName: "mygroup",
		Created:   time.Now(),
		Primary: Workspace{
			Name: "persist-test",
			Repo: "/repos/primary",
			Root: "/groups/persist-test",
			Env:  map[string]string{},
		},
		Secondary: []Workspace{
			{Name: "api", Repo: "/repos/api", Root: "/groups/persist-test/api", Env: map[string]string{}},
			{Name: "protos", Repo: "/repos/protos", Root: "/groups/persist-test/protos", Env: map[string]string{}},
		},
	}

	// Populate transient field before save
	gw.Primary.SecondaryRoots = gw.AllRoots()
	if len(gw.Primary.SecondaryRoots) != 2 {
		t.Fatalf("SecondaryRoots before save = %d, want 2", len(gw.Primary.SecondaryRoots))
	}

	if err := store.SaveGroupWorkspace(gw); err != nil {
		t.Fatalf("SaveGroupWorkspace: %v", err)
	}

	loaded, err := store.LoadGroupWorkspace(gw.ID())
	if err != nil {
		t.Fatalf("LoadGroupWorkspace: %v", err)
	}

	// SecondaryRoots must be empty after load (it's json:"-")
	if len(loaded.Primary.SecondaryRoots) != 0 {
		t.Errorf("loaded Primary.SecondaryRoots = %v, want empty (field is json:\"-\")", loaded.Primary.SecondaryRoots)
	}

	// But AllRoots() still returns the correct secondary roots from the Secondary slice
	roots := loaded.AllRoots()
	if len(roots) != 2 {
		t.Fatalf("AllRoots() after load = %d, want 2", len(roots))
	}

	// Callers must repopulate: loaded.Primary.SecondaryRoots = loaded.AllRoots()
	loaded.Primary.SecondaryRoots = loaded.AllRoots()
	if len(loaded.Primary.SecondaryRoots) != 2 {
		t.Errorf("repopulated SecondaryRoots = %d, want 2", len(loaded.Primary.SecondaryRoots))
	}
}

func TestGroupWorkspace_RenamePreservesSecondaryRoots(t *testing.T) {
	// Simulates the rename flow: load from store, update paths, verify
	// SecondaryRoots can be repopulated from the updated Secondary slice.
	store := NewWorkspaceStore(t.TempDir())

	gw := &GroupWorkspace{
		Name:      "old-name",
		GroupName: "mygroup",
		Created:   time.Now(),
		Primary: Workspace{
			Name: "old-name",
			Repo: "/repos/primary",
			Root: "/groups/old-name",
			Env:  map[string]string{},
		},
		Secondary: []Workspace{
			{Name: "api", Repo: "/repos/api", Root: "/groups/old-name/api", Env: map[string]string{}},
			{Name: "protos", Repo: "/repos/protos", Root: "/groups/old-name/protos", Env: map[string]string{}},
		},
	}

	if err := store.SaveGroupWorkspace(gw); err != nil {
		t.Fatalf("SaveGroupWorkspace: %v", err)
	}

	// Simulate rename: load, update paths, save
	loaded, err := store.LoadGroupWorkspace(gw.ID())
	if err != nil {
		t.Fatalf("LoadGroupWorkspace: %v", err)
	}

	newName := "new-name"
	newGroupRoot := filepath.Join(filepath.Dir(loaded.Primary.Root), newName)
	loaded.Name = newName
	loaded.Primary.Name = newName
	loaded.Primary.Root = newGroupRoot
	for i := range loaded.Secondary {
		loaded.Secondary[i].Root = filepath.Join(newGroupRoot, filepath.Base(loaded.Secondary[i].Root))
	}

	// The critical step: repopulate SecondaryRoots after rename
	loaded.Primary.SecondaryRoots = loaded.AllRoots()

	// Verify the roots reflect the new paths
	roots := loaded.Primary.SecondaryRoots
	if len(roots) != 2 {
		t.Fatalf("SecondaryRoots after rename = %d, want 2", len(roots))
	}
	for _, root := range roots {
		if filepath.Dir(root) != newGroupRoot {
			t.Errorf("renamed root %q not under new group root %q", root, newGroupRoot)
		}
	}
}
