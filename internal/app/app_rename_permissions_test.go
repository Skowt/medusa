package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/permissions"
	"github.com/andyrewlee/medusa/internal/ui/center"
	"github.com/andyrewlee/medusa/internal/ui/common"
)

// newTestApp builds a minimal App suitable for rename tests. It wires up only
// the fields that the rename handlers touch (config, workspaces store, center,
// toast, permissionWatcher, dirtyWorkspaces). Everything else is left nil/zero,
// which is safe because the rename handlers nil-check optional components.
func newTestApp(t *testing.T) (*App, *config.Config) {
	t.Helper()
	// Resolve symlinks so that paths are stable even after directories
	// are moved/deleted (macOS /var -> /private/var).
	tmp := normalizePath(t.TempDir())
	cfg := &config.Config{
		Paths: &config.Paths{
			Home:                  tmp,
			WorkspacesRoot:        filepath.Join(tmp, "workspaces"),
			MetadataRoot:          filepath.Join(tmp, "workspaces-metadata"),
			RegistryPath:          filepath.Join(tmp, "workspaces.json"),
			ProfilesRoot:          filepath.Join(tmp, "profiles"),
			GlobalPermissionsPath: filepath.Join(tmp, "global_permissions.json"),
		},
	}
	store := data.NewWorkspaceStore(cfg.Paths.MetadataRoot)
	registry := data.NewRegistry(cfg.Paths.RegistryPath)
	pw, err := permissions.NewPermissionWatcher(func(string, []string) {})
	if err != nil {
		t.Fatalf("NewPermissionWatcher: %v", err)
	}
	t.Cleanup(func() { pw.Close() })

	app := &App{
		config:            cfg,
		registry:          registry,
		workspaces:        store,
		center:            center.New(cfg),
		toast:             common.NewToastModel(),
		permissionWatcher: pw,
		dirtyWorkspaces:   make(map[string]bool),
	}
	return app, cfg
}

func TestRenameWorkspace_UpdatesPermissionWatcher(t *testing.T) {
	skipIfNoGit(t)

	app, _ := newTestApp(t)
	pw := app.permissionWatcher

	// Set up a git repo with a worktree.
	repo := normalizePath(t.TempDir())
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	worktreeDir := normalizePath(t.TempDir())
	oldName := "old-feature"
	oldRoot := filepath.Join(worktreeDir, oldName)
	runGit(t, repo, "worktree", "add", "--no-track", "-b", oldName, oldRoot, "main")

	// Persist workspace into the store so handleRenameWorkspace can Load it.
	ws := data.NewWorkspace(oldName, oldName, "main", repo, oldRoot)
	ws.Created = time.Now()
	ws.Runtime = data.RuntimeLocalWorktree
	if err := app.workspaces.Save(ws); err != nil {
		t.Fatalf("Save workspace: %v", err)
	}

	// Register the old root with the permission watcher.
	if err := pw.Watch(oldRoot); err != nil {
		t.Fatalf("Watch old root: %v", err)
	}
	if !pw.IsWatching(oldRoot) {
		t.Fatal("expected permission watcher to be watching old root before rename")
	}

	// Rename the workspace.
	newName := "new-feature"
	cmds := app.handleRenameWorkspace(messages.RenameWorkspace{
		Workspace: ws,
		NewName:   newName,
	})
	if len(cmds) == 0 {
		t.Fatal("handleRenameWorkspace returned no commands (rename likely failed)")
	}

	newRoot := filepath.Join(worktreeDir, newName)
	if pw.IsWatching(oldRoot) {
		t.Errorf("permission watcher still watching old root %s after rename", oldRoot)
	}
	if !pw.IsWatching(newRoot) {
		t.Errorf("permission watcher not watching new root %s after rename", newRoot)
	}
}
