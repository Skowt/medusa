package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

func TestLoadWorkspaces_LoadsFromRegistry(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	worktreeDir := normalizePath(t.TempDir())
	worktreePath := filepath.Join(worktreeDir, "feature")
	runGit(t, repo, "worktree", "add", "-b", "feature", worktreePath, "main")

	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "workspaces.json"))
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))

	createdAt := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	ws := data.NewWorkspace("feature", "feature", "main", repo, worktreePath)
	ws.Created = createdAt
	ws.Assistant = "codex"
	ws.ScriptMode = "nonconcurrent"
	ws.Runtime = data.RuntimeLocalWorktree

	if err := store.Save(ws); err != nil {
		t.Fatalf("Save workspace: %v", err)
	}
	if err := registry.AddWorkspace(ws.Name, string(ws.ID()), ""); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	app := &App{
		registry:   registry,
		workspaces: store,
	}
	msg := app.loadWorkspaces()()
	loaded, ok := msg.(messages.WorkspacesLoaded)
	if !ok {
		t.Fatalf("expected WorkspacesLoaded, got %T", msg)
	}

	if len(loaded.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(loaded.Workspaces))
	}

	got := loaded.Workspaces[0]
	if got.Name != "feature" {
		t.Fatalf("name = %q, want %q", got.Name, "feature")
	}
	if got.Assistant != "codex" {
		t.Fatalf("assistant = %q, want %q", got.Assistant, "codex")
	}
	if !got.Created.Equal(createdAt) {
		t.Fatalf("created = %v, want %v", got.Created, createdAt)
	}
}

func TestLoadWorkspaces_AppliesProfileFromRegistry(t *testing.T) {
	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "workspaces.json"))
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))

	ws := data.NewWorkspace("ws1", "", "", "/tmp/repo", "/tmp/ws1")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save workspace: %v", err)
	}
	if err := registry.AddWorkspace(ws.Name, string(ws.ID()), "my-profile"); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	app := &App{
		registry:   registry,
		workspaces: store,
	}
	msg := app.loadWorkspaces()()
	loaded, ok := msg.(messages.WorkspacesLoaded)
	if !ok {
		t.Fatalf("expected WorkspacesLoaded, got %T", msg)
	}

	if len(loaded.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(loaded.Workspaces))
	}
	if loaded.Workspaces[0].Profile != "my-profile" {
		t.Fatalf("profile = %q, want %q", loaded.Workspaces[0].Profile, "my-profile")
	}
}

func TestLoadWorkspaces_DetectsMetadataOrphans(t *testing.T) {
	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "workspaces.json"))
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))

	// Create a workspace whose worktree directory does NOT exist on disk
	missingRoot := filepath.Join(tmp, "nonexistent-worktree")
	ws := data.NewWorkspace("orphan-meta", "branch", "main", "/tmp/repo", missingRoot)
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save workspace: %v", err)
	}
	if err := registry.AddWorkspace(ws.Name, string(ws.ID()), ""); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	app := &App{
		registry:   registry,
		workspaces: store,
	}
	msg := app.loadWorkspaces()()
	loaded, ok := msg.(messages.WorkspacesLoaded)
	if !ok {
		t.Fatalf("expected WorkspacesLoaded, got %T", msg)
	}

	if len(loaded.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(loaded.Workspaces))
	}
	got := loaded.Workspaces[0]
	if got.Orphan != data.OrphanMetadata {
		t.Fatalf("expected OrphanMetadata, got %d", got.Orphan)
	}
	if !got.IsOrphaned() {
		t.Fatal("expected IsOrphaned() to return true")
	}
}

func TestLoadWorkspaces_DetectsDirectoryOrphans(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	if err := os.MkdirAll(workspacesRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	registry := data.NewRegistry(filepath.Join(tmp, "workspaces.json"))
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))

	// Create a stray directory that no workspace metadata references
	orphanDir := filepath.Join(workspacesRoot, "stray-orphan")
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	app := &App{
		registry:   registry,
		workspaces: store,
		config: &config.Config{
			Paths: &config.Paths{
				WorkspacesRoot: workspacesRoot,
			},
		},
	}
	msg := app.loadWorkspaces()()
	loaded, ok := msg.(messages.WorkspacesLoaded)
	if !ok {
		t.Fatalf("expected WorkspacesLoaded, got %T", msg)
	}

	if len(loaded.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace (directory orphan), got %d", len(loaded.Workspaces))
	}
	got := loaded.Workspaces[0]
	if got.Orphan != data.OrphanDirectory {
		t.Fatalf("expected OrphanDirectory, got %d", got.Orphan)
	}
	if got.Name != "stray-orphan" {
		t.Fatalf("name = %q, want %q", got.Name, "stray-orphan")
	}
	if got.OrphanPath != orphanDir {
		t.Fatalf("OrphanPath = %q, want %q", got.OrphanPath, orphanDir)
	}
}

func normalizePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

// setupHealFixture builds a store, a registry, and a workspaces root with a
// worktree directory already on disk, which is the state every heal test starts
// from.
func setupHealFixture(t *testing.T, name string) (tmp, repo, root string, registry *data.Registry, store *data.WorkspaceStore) {
	t.Helper()
	tmp = t.TempDir()
	root = filepath.Join(tmp, "workspaces", name)
	repo = filepath.Join(tmp, "repo")
	for _, dir := range []string{root, repo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
	}
	registry = data.NewRegistry(filepath.Join(tmp, "workspaces.json"))
	store = data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	return tmp, repo, root, registry, store
}

// rehomeWorkspace reproduces what a build predating Workspace.StableID does to
// a store: it cannot see the stored id, recomputes the old repo-plus-root hash,
// saves the metadata under that instead, and deletes the directory it moved
// off — leaving the registry entry pointing at nothing.
func rehomeWorkspace(t *testing.T, store *data.WorkspaceStore, ws *data.Workspace) (oldID, newID string) {
	t.Helper()
	oldID = string(ws.ID())
	ws.StableID = ""
	if err := store.Save(ws); err != nil {
		t.Fatalf("rehome Save: %v", err)
	}
	newID = string(ws.ID())
	if newID == oldID {
		t.Fatalf("rehome did not move the workspace off %s", oldID)
	}
	if _, err := store.Load(data.WorkspaceID(oldID)); err == nil {
		t.Fatalf("expected store entry %s to be gone after the rehome", oldID)
	}
	return oldID, newID
}

func registryIDFor(t *testing.T, registry *data.Registry, name string) string {
	t.Helper()
	entries, err := registry.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry.ID
		}
	}
	return ""
}

func TestLoadWorkspaces_HealsRehomedMetadata(t *testing.T) {
	tmp, repo, root, registry, store := setupHealFixture(t, "git-reviews")

	ws := data.NewWorkspace("git-reviews", "git-reviews", "main", repo, root)
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := registry.AddWorkspace(ws.Name, string(ws.ID()), "Work"); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	staleID, liveID := rehomeWorkspace(t, store, ws)

	app := &App{
		registry:   registry,
		workspaces: store,
		config: &config.Config{
			Paths: &config.Paths{WorkspacesRoot: filepath.Join(tmp, "workspaces")},
		},
	}
	loaded, ok := app.loadWorkspaces()().(messages.WorkspacesLoaded)
	if !ok {
		t.Fatalf("expected WorkspacesLoaded")
	}

	if len(loaded.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(loaded.Workspaces))
	}
	got := loaded.Workspaces[0]
	if got.Name != "git-reviews" {
		t.Fatalf("name = %q, want %q", got.Name, "git-reviews")
	}
	// The symptom of the stranded entry was the worktree resurfacing as a
	// directory orphan offering to delete itself.
	if got.IsOrphaned() {
		t.Fatalf("workspace is orphaned (%d), want a healthy workspace", got.Orphan)
	}
	if got.Profile != "Work" {
		t.Fatalf("profile = %q, want %q", got.Profile, "Work")
	}
	if healed := registryIDFor(t, registry, "git-reviews"); healed != liveID {
		t.Fatalf("registry id = %q, want %q (was stranded at %q)", healed, liveID, staleID)
	}
}

func TestLoadWorkspaces_HealRefusesAnAmbiguousName(t *testing.T) {
	tmp, repo, root, registry, store := setupHealFixture(t, "dup")

	// Two store entries share the name and neither worktree is on disk, so
	// nothing separates them and adopting either would be a coin flip.
	for _, wtRoot := range []string{filepath.Join(tmp, "gone-a"), filepath.Join(tmp, "gone-b")} {
		dead := data.NewWorkspace("dup", "dup", "main", repo, wtRoot)
		if err := store.Save(dead); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	ws := data.NewWorkspace("dup", "dup", "main", repo, root)
	if err := registry.AddWorkspace(ws.Name, string(ws.ID()), ""); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	staleID := string(ws.ID())

	app := &App{registry: registry, workspaces: store}
	loaded, ok := app.loadWorkspaces()().(messages.WorkspacesLoaded)
	if !ok {
		t.Fatalf("expected WorkspacesLoaded")
	}
	if len(loaded.Workspaces) != 0 {
		t.Fatalf("expected no workspace to be adopted, got %d", len(loaded.Workspaces))
	}
	if id := registryIDFor(t, registry, "dup"); id != staleID {
		t.Fatalf("registry id = %q, want it left at %q", id, staleID)
	}
}

func TestLoadWorkspaces_HealDoesNotStealClaimedMetadata(t *testing.T) {
	_, repo, root, registry, store := setupHealFixture(t, "keeper")

	keeper := data.NewWorkspace("keeper", "keeper", "main", repo, root)
	if err := store.Save(keeper); err != nil {
		t.Fatalf("Save: %v", err)
	}
	keeperID := string(keeper.ID())
	if err := registry.AddWorkspace(keeper.Name, keeperID, ""); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	// A second, stranded entry by the same name. The only store entry it could
	// match is one another entry already accounts for.
	strandedID := "0000000000000000"
	if err := registry.AddWorkspace("keeper", strandedID, ""); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	app := &App{registry: registry, workspaces: store}
	loaded, ok := app.loadWorkspaces()().(messages.WorkspacesLoaded)
	if !ok {
		t.Fatalf("expected WorkspacesLoaded")
	}
	if len(loaded.Workspaces) != 1 {
		t.Fatalf("expected only the claimed workspace, got %d", len(loaded.Workspaces))
	}

	entries, err := registry.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	for _, entry := range entries {
		if entry.ID != keeperID && entry.ID != strandedID {
			t.Fatalf("registry entry %q was repointed to %q", entry.Name, entry.ID)
		}
	}
}
