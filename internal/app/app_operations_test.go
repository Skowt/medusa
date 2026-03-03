package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/messages"
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
