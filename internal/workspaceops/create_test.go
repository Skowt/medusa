package workspaceops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
)

// gitRun runs git in dir with the user's global config ignored, so a test
// does not depend on whoever runs it.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo makes a repository with one commit on main.
func newRepo(t *testing.T) data.RepoRef {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "init")
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return data.RepoRef{Name: "repo", Path: resolved}
}

func newDeps(t *testing.T) Deps {
	t.Helper()
	home := t.TempDir()
	cfg := &config.Config{Paths: &config.Paths{
		Home:           home,
		WorkspacesRoot: filepath.Join(home, "workspaces"),
		RegistryPath:   filepath.Join(home, "workspaces.json"),
		MetadataRoot:   filepath.Join(home, "workspaces-metadata"),
		RecentsPath:    filepath.Join(home, "recents.json"),
		ProfilesRoot:   filepath.Join(home, "profiles"),
		HooksDir:       filepath.Join(home, "hooks"),
	}}
	return Deps{
		Config:   cfg,
		Registry: data.NewRegistry(cfg.Paths.RegistryPath),
		Store:    data.NewWorkspaceStore(cfg.Paths.MetadataRoot),
		Recents:  data.NewRecentsStore(cfg.Paths.RecentsPath),
	}
}

func TestCreateWorktreeWorkspace(t *testing.T) {
	d := newDeps(t)
	repo := newRepo(t)

	ws, err := d.Create(CreateRequest{
		Name: "feature", Repos: []data.RepoRef{repo}, Bases: []string{"main"},
		Profile: "Work", Group: "team", Worktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if want := filepath.Join(d.Config.Paths.WorkspacesRoot, "feature"); ws.Root() != want {
		t.Fatalf("root = %q, want %q", ws.Root(), want)
	}
	if !git.IsWorktree(ws.Root()) {
		t.Fatalf("%s is not a worktree", ws.Root())
	}
	if !git.BranchExists(repo.Path, "feature") {
		t.Fatal("branch feature was not created")
	}
	entries, err := d.Registry.ListWorkspaces()
	if err != nil || len(entries) != 1 || entries[0].ID != string(ws.ID()) || entries[0].Profile != "Work" {
		t.Fatalf("registry = %+v, %v; want one entry for the workspace under Work", entries, err)
	}
	loaded, err := d.Store.Load(ws.ID())
	if err != nil || loaded.Group != "team" || loaded.Profile != "Work" {
		t.Fatalf("stored workspace = %+v, %v", loaded, err)
	}
	if exists, _ := d.NameExists("FEATURE"); !exists {
		t.Fatal("NameExists is not case-insensitive")
	}
}

func TestCreateCheckoutWorkspaceMakesNothingOnDisk(t *testing.T) {
	d := newDeps(t)
	repo := newRepo(t)

	ws, err := d.Create(CreateRequest{
		Name: "co", Repos: []data.RepoRef{repo}, Bases: []string{"main"}, Profile: "Default",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.Root() != repo.Path || ws.UsesWorktree() {
		t.Fatalf("checkout workspace root = %q (worktree %v), want the repo itself", ws.Root(), ws.UsesWorktree())
	}
	if git.BranchExists(repo.Path, "co") {
		t.Fatal("a checkout workspace must not cut a branch")
	}
	if _, err := os.Stat(filepath.Join(d.Config.Paths.WorkspacesRoot, "co")); !os.IsNotExist(err) {
		t.Fatalf("a checkout workspace made a directory: %v", err)
	}
}

func TestCreateRefusesATakenName(t *testing.T) {
	d := newDeps(t)
	repo := newRepo(t)
	req := CreateRequest{Name: "dup", Repos: []data.RepoRef{repo}, Bases: []string{"main"}, Profile: "Default", Worktree: true}
	if _, err := d.Create(req); err != nil {
		t.Fatal(err)
	}
	req.Name = "Dup"
	_, err := d.Create(req)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second Create err = %v, want an 'already exists' error", err)
	}
	if entries, _ := d.Registry.ListWorkspaces(); len(entries) != 1 {
		t.Fatalf("registry has %d entries after a refused create, want 1", len(entries))
	}
}

func TestCreateRefusesAnExistingBranch(t *testing.T) {
	d := newDeps(t)
	repo := newRepo(t)
	gitRun(t, repo.Path, "branch", "taken")
	_, err := d.Create(CreateRequest{Name: "taken", Repos: []data.RepoRef{repo}, Bases: []string{"main"}, Profile: "Default", Worktree: true})
	if err == nil || !strings.Contains(err.Error(), "branch 'taken' already exists") {
		t.Fatalf("Create err = %v, want a branch-exists error", err)
	}
	if _, statErr := os.Stat(filepath.Join(d.Config.Paths.WorkspacesRoot, "taken")); !os.IsNotExist(statErr) {
		t.Fatal("a refused create left a directory behind")
	}
}

func TestDeleteWorktreeWorkspaceRemovesWhatItMade(t *testing.T) {
	d := newDeps(t)
	repo := newRepo(t)
	ws, err := d.Create(CreateRequest{Name: "gone", Repos: []data.RepoRef{repo}, Bases: []string{"main"}, Profile: "Default", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}

	if warning := d.Delete(ws); warning != "" {
		t.Fatalf("Delete warning: %s", warning)
	}
	if _, err := os.Stat(ws.Root()); !os.IsNotExist(err) {
		t.Fatalf("worktree still on disk: %v", err)
	}
	if git.BranchExists(repo.Path, "gone") {
		t.Fatal("branch survived delete")
	}
	if ok, _ := d.Registry.Has(string(ws.ID())); ok {
		t.Fatal("registry entry survived delete")
	}
	if _, err := d.Store.Load(ws.ID()); !os.IsNotExist(err) {
		t.Fatalf("metadata survived delete: %v", err)
	}
}

func TestDeleteCheckoutWorkspaceLeavesTheRepo(t *testing.T) {
	d := newDeps(t)
	repo := newRepo(t)
	ws, err := d.Create(CreateRequest{Name: "co", Repos: []data.RepoRef{repo}, Bases: []string{"main"}, Profile: "Default"})
	if err != nil {
		t.Fatal(err)
	}

	d.Delete(ws)

	if info, err := os.Stat(filepath.Join(repo.Path, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("source repo's .git is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Path, "README")); err != nil {
		t.Fatalf("source repo's files are gone: %v", err)
	}
	if ok, _ := d.Registry.Has(string(ws.ID())); ok {
		t.Fatal("registry entry survived delete")
	}
}
