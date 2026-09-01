package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeLegacyWorkspace writes a workspace file with no "id" field, the shape
// every workspace stored before StableID existed still has on disk.
func writeLegacyWorkspace(t *testing.T, root string, id WorkspaceID, ws *Workspace) {
	t.Helper()
	raw, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	delete(fields, "id")
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	dir := filepath.Join(root, string(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, workspaceFilename), raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// A legacy workspace whose root has moved since it was stored no longer hashes
// to the directory it lives in. Left to ID()'s fallback it answers to an ID
// nothing on disk holds, so store and registry deletes both hit nothing and
// report success — which is how an orphan survived being cleaned up.
func TestLoadPinsTheDirectoryAsTheIdentity(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := NewWorkspace("terr", "terr", "main", "/repos/medusa", "/workspaces/terr/medusa")
	ws.StableID = ""
	storeID := WorkspaceID("cfa63cbc4b27244c")
	// The flat-layout migration moved the root, so the derived hash no longer
	// reproduces the directory the metadata sits in.
	ws.Worktrees[0].Root = "/workspaces/terr"
	writeLegacyWorkspace(t, root, storeID, ws)

	loaded, err := store.Load(storeID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ID() != storeID {
		t.Fatalf("ID() = %q, want the directory it was loaded from (%q)", loaded.ID(), storeID)
	}
}

// The pin has to survive a save, or the next start is back where it started.
func TestSaveWritesThePinnedIdentityBack(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := NewWorkspace("terr", "terr", "main", "/repos/medusa", "/workspaces/terr/medusa")
	ws.StableID = ""
	storeID := WorkspaceID("cfa63cbc4b27244c")
	ws.Worktrees[0].Root = "/workspaces/terr"
	writeLegacyWorkspace(t, root, storeID, ws)

	loaded, err := store.Load(storeID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := store.Save(loaded); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, string(storeID), workspaceFilename)); err != nil {
		t.Fatalf("the workspace was rehomed off %s: %v", storeID, err)
	}

	again, err := store.Load(storeID)
	if err != nil {
		t.Fatalf("re-Load: %v", err)
	}
	if again.StableID != storeID {
		t.Fatalf("StableID = %q, want %q", again.StableID, storeID)
	}
}

// A workspace whose stored id already agrees with its directory must be left
// exactly as it is, moved root or not.
func TestLoadKeepsAnExistingStableID(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := NewWorkspace("git-reviews", "git-reviews", "main", "/repos/medusa", "/workspaces/git-reviews")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.StableID != ws.StableID {
		t.Fatalf("StableID = %q, want %q", loaded.StableID, ws.StableID)
	}
}
