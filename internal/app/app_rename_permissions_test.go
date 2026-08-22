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

// TestRenameWorkspace_KeepsBranch verifies that renaming a workspace moves the
// folder but does not rename the git branch.
func TestRenameWorkspace_KeepsBranch(t *testing.T) {
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

	newName := "renamed-ws"
	cmds := app.handleRenameWorkspace(messages.RenameWorkspace{
		Workspace: ws,
		NewName:   newName,
	})
	if len(cmds) == 0 {
		t.Fatal("handleRenameWorkspace returned no commands (rename likely failed)")
	}

	// Verify the new worktree directory exists.
	newRoot := filepath.Join(worktreeDir, newName)
	if _, err := os.Stat(newRoot); os.IsNotExist(err) {
		t.Fatalf("expected new worktree root %s to exist", newRoot)
	}

	// Verify the git branch was NOT renamed — it should still be "my-feature".
	output, err := git.GetCurrentBranch(newRoot)
	if err != nil {
		t.Fatalf("GetCurrentBranch: %v", err)
	}
	if got := strings.TrimSpace(output); got != branchName {
		t.Errorf("expected branch %q to be unchanged, got %q", branchName, got)
	}
}
