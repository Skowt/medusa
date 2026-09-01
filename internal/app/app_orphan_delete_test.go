package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// writeLegacyMetadata writes a workspace file with no "id" field under storeID,
// the shape a workspace stored before Workspace.StableID existed still has.
func writeLegacyMetadata(t *testing.T, storeRoot, storeID, name, repo, root string) {
	t.Helper()
	dir := filepath.Join(storeRoot, storeID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	raw := `{
  "name": "` + name + `",
  "repos": [{"path": "` + repo + `", "name": "repo"}],
  "worktrees": [{"branch": "` + name + `", "base": "main", "root": "` + root + `"}],
  "runtime": "local-worktree"
}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write workspace.json: %v", err)
	}
}

// A metadata orphan stored under an ID its paths no longer hash to used to be
// undeletable: the store delete and the registry delete both addressed the
// derived ID, both hit nothing, and both reported success — so the toast said
// "Orphan cleaned up" and the orphan came back on the next load and every
// restart after it.
func TestDeleteOrphanWorkspace_RemovesAnEntryStoredUnderADriftedID(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	storeRoot := filepath.Join(tmp, "workspaces-metadata")
	if err := os.MkdirAll(workspacesRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	repo := filepath.Join(tmp, "repo")

	// The worktree is gone, which is what makes this a metadata orphan, and the
	// directory the metadata sits in is not what repo-plus-root hashes to.
	const storeID = "cfa63cbc4b27244c"
	missingRoot := filepath.Join(workspacesRoot, "terr")
	writeLegacyMetadata(t, storeRoot, storeID, "terr", repo, missingRoot)

	registry := data.NewRegistry(filepath.Join(tmp, "workspaces.json"))
	store := data.NewWorkspaceStore(storeRoot)
	if err := registry.AddWorkspace("terr", storeID, "Default"); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	app := &App{
		registry:   registry,
		workspaces: store,
		config: &config.Config{
			Paths: &config.Paths{WorkspacesRoot: workspacesRoot},
		},
	}

	loaded, ok := app.loadWorkspaces()().(messages.WorkspacesLoaded)
	if !ok {
		t.Fatalf("expected WorkspacesLoaded")
	}
	if len(loaded.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(loaded.Workspaces))
	}
	ws := loaded.Workspaces[0]
	if ws.Orphan != data.OrphanMetadata {
		t.Fatalf("orphan = %d, want OrphanMetadata", ws.Orphan)
	}

	deleted, ok := app.deleteOrphanWorkspace(ws)().(messages.OrphanWorkspaceDeleted)
	if !ok {
		t.Fatalf("expected OrphanWorkspaceDeleted")
	}
	if deleted.Err != nil {
		t.Fatalf("cleanup reported an error: %v", deleted.Err)
	}

	entries, err := registry.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("registry still lists %d entries, want none", len(entries))
	}
	if _, err := os.Stat(filepath.Join(storeRoot, storeID)); !os.IsNotExist(err) {
		t.Fatalf("store directory %s survived the cleanup", storeID)
	}

	again, ok := app.loadWorkspaces()().(messages.WorkspacesLoaded)
	if !ok {
		t.Fatalf("expected WorkspacesLoaded")
	}
	if len(again.Workspaces) != 0 {
		t.Fatalf("the orphan came back after cleanup: %d workspaces", len(again.Workspaces))
	}
}

// An orphan cleanup that removed nothing must not report success, whatever the
// reason the IDs stopped matching.
func TestDeleteOrphanWorkspace_ReportsAnEntryItCouldNotRemove(t *testing.T) {
	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "workspaces.json"))
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	if err := registry.AddWorkspace("terr", "cfa63cbc4b27244c", "Default"); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	app := &App{registry: registry, workspaces: store}
	ws := &data.Workspace{
		Name:      "terr",
		StableID:  "f122ab4888910c7d", // not the ID the registry holds
		Orphan:    data.OrphanMetadata,
		Worktrees: []data.WorktreeRef{{Root: filepath.Join(tmp, "gone")}},
	}

	deleted, ok := app.deleteOrphanWorkspace(ws)().(messages.OrphanWorkspaceDeleted)
	if !ok {
		t.Fatalf("expected OrphanWorkspaceDeleted")
	}
	if deleted.Err == nil {
		t.Fatalf("cleanup reported success while the registry entry is still there")
	}
}
