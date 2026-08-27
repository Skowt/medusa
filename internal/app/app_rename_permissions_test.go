package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/center"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/ui/sidebar"
)

// newTestApp builds a minimal App suitable for rename tests. It wires up only
// the fields that the rename handlers touch (config, workspaces store, center,
// toast and dirtyWorkspaces). Everything else is left nil/zero,
// which is safe because the rename handlers nil-check optional components.
func newTestApp(t *testing.T) (*App, *config.Config) {
	t.Helper()
	// Resolve symlinks so that paths are stable even after directories
	// are moved/deleted (macOS /var -> /private/var).
	tmp := normalizePath(t.TempDir())
	cfg := &config.Config{
		Paths: &config.Paths{
			Home:           tmp,
			WorkspacesRoot: filepath.Join(tmp, "workspaces"),
			MetadataRoot:   filepath.Join(tmp, "workspaces-metadata"),
			RegistryPath:   filepath.Join(tmp, "workspaces.json"),
			ProfilesRoot:   filepath.Join(tmp, "profiles"),
		},
	}
	store := data.NewWorkspaceStore(cfg.Paths.MetadataRoot)
	registry := data.NewRegistry(cfg.Paths.RegistryPath)

	app := &App{
		config:          cfg,
		registry:        registry,
		workspaces:      store,
		center:          center.New(cfg),
		toast:           common.NewToastModel(),
		dirtyWorkspaces: make(map[string]bool),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
	return app, cfg
}

// TestRenameWorkspace_LeavesTheWorktreeWhereItIs is the property the rename
// exists to have: it changes a label. Moving the directory used to change the
// workspace's root, and with it its ID and every agent's working directory.
func TestRenameWorkspace_LeavesTheWorktreeWhereItIs(t *testing.T) {
	skipIfNoGit(t)

	app, _ := newTestApp(t)

	// Set up a git repo with a worktree.
	repo := normalizePath(t.TempDir())
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	worktreeDir := normalizePath(t.TempDir())
	branchName := "my-feature"
	worktreePath := filepath.Join(worktreeDir, branchName)

	runGit(t, repo, "worktree", "add", "--no-track", "-b", branchName, worktreePath, "main")

	ws := data.NewWorkspace(branchName, branchName, "main", repo, worktreePath)
	ws.Created = time.Now()
	ws.Runtime = data.RuntimeLocalWorktree
	if err := app.workspaces.Save(ws); err != nil {
		t.Fatalf("Save workspace: %v", err)
	}
	oldID := ws.ID()

	newName := "renamed-ws"
	cmds := app.handleRenameWorkspace(messages.RenameWorkspace{
		Workspace: ws,
		NewName:   newName,
	})
	if len(cmds) == 0 {
		t.Fatal("handleRenameWorkspace returned no commands (rename likely failed)")
	}

	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("expected worktree %s to stay where it was: %v", worktreePath, err)
	}
	if _, err := os.Stat(filepath.Join(worktreeDir, newName)); !os.IsNotExist(err) {
		t.Errorf("rename created a directory named after the new name")
	}

	stored, err := app.workspaces.Load(oldID)
	if err != nil || stored == nil {
		t.Fatalf("workspace should still be stored under its original ID: %v", err)
	}
	if stored.Name != newName {
		t.Errorf("stored name = %q, want %q", stored.Name, newName)
	}
	if stored.Root() != worktreePath {
		t.Errorf("stored root = %q, want %q", stored.Root(), worktreePath)
	}

	// The git branch is not renamed either.
	output, err := git.GetCurrentBranch(worktreePath)
	if err != nil {
		t.Fatalf("GetCurrentBranch: %v", err)
	}
	if got := strings.TrimSpace(output); got != branchName {
		t.Errorf("expected branch %q to be unchanged, got %q", branchName, got)
	}
}

// TestRenameWorkspace_OnACheckoutLeavesTheRepoAlone covers the case the old
// implementation would have been worst at: with the root being the user's own
// repo, moving it to match the new name moves the repo.
func TestRenameWorkspace_OnACheckoutLeavesTheRepoAlone(t *testing.T) {
	skipIfNoGit(t)

	app, _ := newTestApp(t)

	repo := normalizePath(t.TempDir())
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	ws := data.NewCheckoutWorkspace("in-place", "main", repo)
	if err := app.workspaces.Save(ws); err != nil {
		t.Fatalf("Save workspace: %v", err)
	}

	cmds := app.handleRenameWorkspace(messages.RenameWorkspace{
		Workspace: ws,
		NewName:   "renamed",
	})
	if len(cmds) == 0 {
		t.Fatal("handleRenameWorkspace returned no commands (rename likely failed)")
	}

	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("the repo should be untouched by a rename: %v", err)
	}
	stored, err := app.workspaces.Load(ws.ID())
	if err != nil || stored == nil {
		t.Fatalf("workspace should still be stored under its original ID: %v", err)
	}
	if stored.Name != "renamed" || stored.Root() != repo {
		t.Errorf("stored = (%q, %q), want (%q, %q)", stored.Name, stored.Root(), "renamed", repo)
	}
}
