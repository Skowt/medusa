package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

func TestWorkspaceNameExists_ExactMatch(t *testing.T) {
	app := &App{
		allWorkspaces: []*data.Workspace{
			{Name: "alpha"},
			{Name: "beta"},
		},
	}

	if !app.workspaceNameExists("alpha") {
		t.Error("expected true for existing name 'alpha'")
	}
	if !app.workspaceNameExists("beta") {
		t.Error("expected true for existing name 'beta'")
	}
	if app.workspaceNameExists("gamma") {
		t.Error("expected false for non-existing name 'gamma'")
	}
}

func TestWorkspaceNameExists_CaseInsensitive(t *testing.T) {
	app := &App{
		allWorkspaces: []*data.Workspace{
			{Name: "My-Feature"},
		},
	}

	if !app.workspaceNameExists("my-feature") {
		t.Error("expected case-insensitive match for 'my-feature'")
	}
	if !app.workspaceNameExists("MY-FEATURE") {
		t.Error("expected case-insensitive match for 'MY-FEATURE'")
	}
}

func TestWorkspaceNameExists_ExcludeID(t *testing.T) {
	ws := data.NewWorkspace("alpha", "alpha", "main", "/tmp/repo", "/tmp/ws")
	app := &App{
		allWorkspaces: []*data.Workspace{ws},
	}

	// Without exclude — should match
	if !app.workspaceNameExists("alpha") {
		t.Error("expected true without excludeID")
	}

	// With exclude — should skip
	if app.workspaceNameExists("alpha", ws.ID()) {
		t.Error("expected false when excluding the matching workspace ID")
	}
}

func TestWorkspaceNameExists_EmptyList(t *testing.T) {
	app := &App{}

	if app.workspaceNameExists("anything") {
		t.Error("expected false for empty workspace list")
	}
}

func TestCreateWorkspace_RejectsDuplicateName(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	wsRoot := normalizePath(t.TempDir())
	app := &App{
		config: &config.Config{
			Paths: &config.Paths{
				WorkspacesRoot: wsRoot,
			},
		},
		allWorkspaces: []*data.Workspace{
			{Name: "existing-ws"},
		},
	}

	repos := []data.RepoRef{{Path: repo, Name: "repo"}}
	bases := []string{"main"}

	cmd := app.createWorkspace("existing-ws", repos, bases, "", "", false)
	msg := cmd()

	fail, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if fail.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := fail.Err.Error(); got != "workspace 'existing-ws' already exists" {
		t.Errorf("error = %q, want %q", got, "workspace 'existing-ws' already exists")
	}
}

func TestCreateWorkspace_RejectsBranchExists(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	// Create a branch that will collide
	runGit(t, repo, "branch", "taken-branch")

	wsRoot := normalizePath(t.TempDir())
	app := &App{
		config: &config.Config{
			Paths: &config.Paths{
				WorkspacesRoot: wsRoot,
			},
		},
		allWorkspaces: []*data.Workspace{},
	}

	repos := []data.RepoRef{{Path: repo, Name: "myrepo"}}
	bases := []string{"main"}

	cmd := app.createWorkspace("taken-branch", repos, bases, "", "", false)
	msg := cmd()

	fail, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if fail.Err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "branch 'taken-branch' already exists in myrepo"
	if got := fail.Err.Error(); got != expected {
		t.Errorf("error = %q, want %q", got, expected)
	}
}

func TestCreateWorkspace_PassesValidationForUniqueName(t *testing.T) {
	// Verify that a unique name passes the duplicate-name check and reaches
	// the branch-exists check (which also passes), meaning validation logic
	// does not block legitimate names. We don't set up a full repo here —
	// the function will fail later in git operations, but that's expected.
	// The key assertion is that it does NOT fail with the "already exists" error.
	app := &App{
		config: &config.Config{
			Paths: &config.Paths{
				WorkspacesRoot: t.TempDir(),
			},
		},
		allWorkspaces: []*data.Workspace{
			{Name: "other-ws"},
		},
	}

	repos := []data.RepoRef{{Path: "/nonexistent", Name: "repo"}}
	bases := []string{"main"}

	cmd := app.createWorkspace("unique-ws", repos, bases, "", "", false)
	msg := cmd()

	fail, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed (from git), got %T", msg)
	}
	// Should NOT be the duplicate-name error
	if fail.Err.Error() == "workspace 'unique-ws' already exists" {
		t.Error("unique name was incorrectly rejected as duplicate")
	}
}

func TestCreateWorkspace_PersistsGroup(t *testing.T) {
	skipIfNoGit(t)

	// Set up a git repo with initial commit
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	// Set up workspace root and temporary registry/store
	wsRoot := normalizePath(t.TempDir())
	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "workspaces.json"))
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	recents := data.NewRecentsStore(filepath.Join(tmp, "recents.json"))

	// Create the app with registry, store, and recents
	app := &App{
		config: &config.Config{
			Paths: &config.Paths{
				WorkspacesRoot: wsRoot,
			},
		},
		registry:      registry,
		workspaces:    store,
		recents:       recents,
		allWorkspaces: []*data.Workspace{},
	}

	// Call createWorkspace with a group
	repos := []data.RepoRef{{Path: repo, Name: "repo"}}
	bases := []string{"main"}
	cmd := app.createWorkspace("test-group-ws", repos, bases, "", "shipping-q2", false)
	msg := cmd()

	// Check that the message is WorkspaceCreated (not failed)
	created, ok := msg.(messages.WorkspaceCreated)
	if !ok {
		t.Fatalf("expected WorkspaceCreated, got %T: %v", msg, msg)
	}

	// Verify in-memory workspace has the group set
	if created.Workspace.Group != "shipping-q2" {
		t.Errorf("in-memory Group = %q, want %q", created.Workspace.Group, "shipping-q2")
	}

	// Verify on disk: reload from store and check Group persists
	reloaded, err := store.Load(created.Workspace.ID())
	if err != nil {
		t.Fatalf("Load workspace from store: %v", err)
	}
	if reloaded.Group != "shipping-q2" {
		t.Errorf("on-disk Group = %q, want %q", reloaded.Group, "shipping-q2")
	}
}
