package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// newCheckoutTestRepo builds a git repo with one commit and returns its path.
func newCheckoutTestRepo(t *testing.T) string {
	t.Helper()
	repo := normalizePath(t.TempDir())
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	return repo
}

// TestCreateWorkspace_WithoutWorktreeMakesNothing is the whole point of the
// unticked checkbox: the workspace is a label over the repo, so no branch is
// cut and no directory appears under the workspaces root.
func TestCreateWorkspace_WithoutWorktreeMakesNothing(t *testing.T) {
	skipIfNoGit(t)

	app, cfg := newTestApp(t)
	app.recents = data.NewRecentsStore(filepath.Join(cfg.Paths.Home, "recents.json"))
	repo := newCheckoutTestRepo(t)

	repos := []data.RepoRef{{Path: repo, Name: filepath.Base(repo)}}
	msg := app.createWorkspace("in-place", repos, []string{"main"}, "", "", false, false)()

	created, ok := msg.(messages.WorkspaceCreated)
	if !ok {
		t.Fatalf("createWorkspace = %#v, want WorkspaceCreated", msg)
	}
	ws := created.Workspace
	if ws.Root() != repo {
		t.Errorf("root = %q, want the repo %q", ws.Root(), repo)
	}
	if ws.UsesWorktree() {
		t.Error("workspace should not claim a worktree of its own")
	}
	if ws.Runtime != data.RuntimeLocalCheckout {
		t.Errorf("runtime = %q, want %q", ws.Runtime, data.RuntimeLocalCheckout)
	}
	if git.BranchExists(repo, "in-place") {
		t.Error("a branch was created for a workspace that asked for no worktree")
	}
	if _, err := os.Stat(filepath.Join(cfg.Paths.WorkspacesRoot, "in-place")); !os.IsNotExist(err) {
		t.Error("a directory was created under the workspaces root")
	}
}

// TestCreateWorkspace_WithoutWorktreeAllowsSeveralOverOneRepo is the property
// the minted ID exists for. Every checkout workspace over a repo has that repo
// as its root, so the old repo-plus-root hash gave them all one ID and the
// second overwrote the first in the store.
func TestCreateWorkspace_WithoutWorktreeAllowsSeveralOverOneRepo(t *testing.T) {
	skipIfNoGit(t)

	app, cfg := newTestApp(t)
	app.recents = data.NewRecentsStore(filepath.Join(cfg.Paths.Home, "recents.json"))
	repo := newCheckoutTestRepo(t)
	repos := []data.RepoRef{{Path: repo, Name: filepath.Base(repo)}}

	first, ok := app.createWorkspace("first", repos, []string{"main"}, "", "", false, false)().(messages.WorkspaceCreated)
	if !ok {
		t.Fatal("first create should have succeeded")
	}
	msg := app.createWorkspace("second", repos, []string{"main"}, "", "", false, false)()
	second, ok := msg.(messages.WorkspaceCreated)
	if !ok {
		t.Fatalf("second create = %#v, want WorkspaceCreated", msg)
	}

	if first.Workspace.Root() != second.Workspace.Root() {
		t.Fatalf("both workspaces should sit on the repo, got %q and %q",
			first.Workspace.Root(), second.Workspace.Root())
	}
	if first.Workspace.ID() == second.Workspace.ID() {
		t.Fatalf("both workspaces share the ID %q, so the second overwrote the first",
			first.Workspace.ID())
	}

	// Both must survive in the store: a shared ID showed up as the first one
	// simply being gone.
	for _, ws := range []*data.Workspace{first.Workspace, second.Workspace} {
		loaded, err := app.workspaces.Load(ws.ID())
		if err != nil {
			t.Fatalf("Load %s: %v", ws.Name, err)
		}
		if loaded.Name != ws.Name {
			t.Errorf("store entry %s holds %q, want %q", ws.ID(), loaded.Name, ws.Name)
		}
	}
}

// TestWorkspaceID_SurvivesARename pins the reason the minted ID is stored
// rather than recomputed. PTY reader goroutines capture the ID when they start
// and stamp it on every message they emit, so an ID that moved with the name
// would strand every message a running agent had in flight.
func TestWorkspaceID_SurvivesARename(t *testing.T) {
	ws := data.NewCheckoutWorkspace("before", "main", "/src/medusa")
	before := ws.ID()
	ws.Name = "after"
	if ws.ID() != before {
		t.Fatalf("rename moved the ID from %q to %q", before, ws.ID())
	}
}

// TestWorkspaceID_LegacyEntriesKeepTheirDerivedID guards the migration: a
// workspace written before StableID existed has no stored ID, and everything
// already refers to the one derived from its paths.
func TestWorkspaceID_LegacyEntriesKeepTheirDerivedID(t *testing.T) {
	legacy := &data.Workspace{
		Name:      "legacy",
		Repos:     []data.RepoRef{{Path: "/src/medusa", Name: "medusa"}},
		Worktrees: []data.WorktreeRef{{Root: "/wt/legacy", Branch: "legacy"}},
	}
	minted := data.NewWorkspace("legacy", "legacy", "main", "/src/medusa", "/wt/legacy")
	if legacy.ID() == minted.ID() {
		t.Fatal("a legacy entry must keep its path-derived ID, not adopt the minted one")
	}

	store := data.NewWorkspaceStore(t.TempDir())
	derived := legacy.ID()
	if err := store.Save(legacy); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if legacy.ID() != derived {
		t.Fatalf("saving moved the ID from %q to %q", derived, legacy.ID())
	}
	if legacy.StableID != derived {
		t.Fatalf("StableID = %q, want the derived ID %q pinned in place", legacy.StableID, derived)
	}
}

// TestDeleteWorkspace_WithoutWorktreeLeavesTheRepo is the safeguard that matters
// most in this change: the workspace's root is the user's repo, so deleting the
// workspace must remove only medusa's record of it.
func TestDeleteWorkspace_WithoutWorktreeLeavesTheRepo(t *testing.T) {
	skipIfNoGit(t)

	app, _ := newTestApp(t)
	repo := newCheckoutTestRepo(t)

	ws := data.NewCheckoutWorkspace("in-place", "main", repo)
	if err := app.workspaces.Save(ws); err != nil {
		t.Fatalf("Save workspace: %v", err)
	}

	msg := app.deleteWorkspace(ws)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("deleteWorkspace = %#v, want WorkspaceDeleted", msg)
	}

	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("the repo should survive deleting the workspace: %v", err)
	}
	if !git.BranchExists(repo, "main") {
		t.Error("the repo's branch was deleted")
	}
	if stored, err := app.workspaces.Load(ws.ID()); err == nil && stored != nil {
		t.Error("the workspace record should be gone")
	}
}

// TestRemovableWorktrees_SkipsAPathThatIsNotAWorktree is the backstop for a
// stored path that has come to mean something else — a plain clone sitting where
// a worktree used to be must not be handed to "git worktree remove".
func TestRemovableWorktrees_SkipsAPathThatIsNotAWorktree(t *testing.T) {
	skipIfNoGit(t)

	repo := newCheckoutTestRepo(t)
	notAWorktree := newCheckoutTestRepo(t)

	ws := data.NewWorkspace("ws", "ws", "main", repo, notAWorktree)
	if specs := removableWorktrees(ws); len(specs) != 0 {
		t.Errorf("removableWorktrees = %v, want none", specs)
	}
}

// TestRemovableWorktrees_KeepsARealWorktree is the other half: the guard must
// not refuse the case it exists to allow.
func TestRemovableWorktrees_KeepsARealWorktree(t *testing.T) {
	skipIfNoGit(t)

	repo := newCheckoutTestRepo(t)
	worktreePath := filepath.Join(normalizePath(t.TempDir()), "feature")
	runGit(t, repo, "worktree", "add", "--no-track", "-b", "feature", worktreePath, "main")

	ws := data.NewWorkspace("feature", "feature", "main", repo, worktreePath)
	specs := removableWorktrees(ws)
	if len(specs) != 1 || specs[0].WorkspacePath != worktreePath {
		t.Fatalf("removableWorktrees = %v, want one spec for %s", specs, worktreePath)
	}
}

// TestNameDialogWorktreeCheckboxDrivesRuntime is the seam between the checkbox
// and everything downstream: Runtime is how the choice rides the rest of the
// dialog chain, and the copy-gitignored box has nothing to copy into without it.
func TestNameDialogWorktreeCheckboxDrivesRuntime(t *testing.T) {
	for _, tc := range []struct {
		name        string
		worktree    bool
		copyIgnored bool
		wantRuntime string
		wantCopy    bool
	}{
		{"worktree on", true, true, data.RuntimeLocalWorktree, true},
		{"worktree off", false, true, data.RuntimeLocalCheckout, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := newTestApp(t)
			app.config.UI.LastCreateWorktree = true
			app.showNameWorkspaceDialog([]data.RepoRef{{Path: "/src/repo", Name: "repo"}})
			ws := app.dialogWorkspace

			_, handled := app.handleCreateDialogResult(common.DialogResult{
				ID:             DialogCreateWorkspace,
				Confirmed:      true,
				Value:          "feature",
				CheckboxValue:  tc.worktree,
				Checkbox2Value: tc.copyIgnored,
			}, ws, "feature")
			if !handled {
				t.Fatal("DialogCreateWorkspace should be handled by the create flow")
			}
			if ws.Runtime != tc.wantRuntime {
				t.Errorf("Runtime = %q, want %q", ws.Runtime, tc.wantRuntime)
			}
			if ws.CopyIgnored != tc.wantCopy {
				t.Errorf("CopyIgnored = %v, want %v", ws.CopyIgnored, tc.wantCopy)
			}
			if app.config.UI.LastCreateWorktree != tc.worktree {
				t.Errorf("LastCreateWorktree = %v, want the checkbox value %v",
					app.config.UI.LastCreateWorktree, tc.worktree)
			}
		})
	}
}

// TestCheckoutSkipsTheBaseBranchStep: with no worktree there is no branch to cut
// and nothing to fetch, so the step the fetch exists to serve must not be shown.
func TestCheckoutSkipsTheBaseBranchStep(t *testing.T) {
	app, _ := newTestApp(t)

	app.dialogWorkspace = &data.Workspace{
		Repos:   []data.RepoRef{{Path: "/src/repo", Name: "repo"}},
		Runtime: data.RuntimeLocalCheckout,
	}
	app.dialogDefaultName = "in-place"
	if cmd := app.advanceToBaseBranchOrCreate(); cmd == nil {
		t.Fatal("a checkout workspace should advance straight to creation")
	}
	if app.dialog != nil {
		t.Error("the base-branch dialog was shown for a workspace with no worktree")
	}

	app.dialogWorkspace = &data.Workspace{
		Repos:   []data.RepoRef{{Path: "/src/repo", Name: "repo"}},
		Runtime: data.RuntimeLocalWorktree,
	}
	app.dialogDefaultName = "feature"
	if cmd := app.advanceToBaseBranchOrCreate(); cmd != nil {
		t.Error("a worktree workspace should stop on the base-branch dialog")
	}
	if app.dialog == nil || !app.dialog.Visible() {
		t.Error("the base-branch dialog was not shown for a worktree workspace")
	}
}

// TestDeleteWorkspace_WithoutWorktreeIgnoresABranchSwitch: the branch recorded
// on a checkout workspace is whatever the repo happened to be on when it was
// created, and the user is free to move off it. Neither that branch nor the one
// they moved to is medusa's to delete.
func TestDeleteWorkspace_WithoutWorktreeIgnoresABranchSwitch(t *testing.T) {
	skipIfNoGit(t)

	app, _ := newTestApp(t)
	repo := newCheckoutTestRepo(t)

	// Created while the repo is on main.
	ws := data.NewCheckoutWorkspace("in-place", "main", repo)
	if err := app.workspaces.Save(ws); err != nil {
		t.Fatalf("Save workspace: %v", err)
	}

	// The user then cuts and checks out a branch inside the repo.
	runGit(t, repo, "checkout", "-b", "side-quest")

	if _, ok := app.deleteWorkspace(ws)().(messages.WorkspaceDeleted); !ok {
		t.Fatal("deleteWorkspace should have succeeded")
	}

	if !git.BranchExists(repo, "main") {
		t.Error("the branch recorded on the workspace was deleted")
	}
	if !git.BranchExists(repo, "side-quest") {
		t.Error("the branch checked out at delete time was deleted")
	}
	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("the repo should survive: %v", err)
	}
}

// newCheckoutTestClone builds an origin repo and a clone of it, then adds a
// commit to origin so the clone is one behind. Returns the clone.
func newCheckoutTestClone(t *testing.T) string {
	t.Helper()
	origin := newCheckoutTestRepo(t)
	clone := filepath.Join(normalizePath(t.TempDir()), "clone")
	runGit(t, ".", "clone", origin, clone)

	if err := os.WriteFile(filepath.Join(origin, "NEW.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write NEW.md: %v", err)
	}
	runGit(t, origin, "add", "NEW.md")
	runGit(t, origin, "commit", "-m", "second")
	return clone
}

// TestPullBeforeOpen_PullsACleanRepo is the quality-of-life behaviour: an agent
// opened on a repo a week behind starts work on stale code.
func TestPullBeforeOpen_PullsACleanRepo(t *testing.T) {
	skipIfNoGit(t)

	clone := newCheckoutTestClone(t)
	if _, err := os.Stat(filepath.Join(clone, "NEW.md")); !os.IsNotExist(err) {
		t.Fatal("the clone should start out behind origin")
	}

	if notice := pullBeforeOpen(clone); notice != "" {
		t.Errorf("notice = %q, want none for a clean pull", notice)
	}
	if _, err := os.Stat(filepath.Join(clone, "NEW.md")); err != nil {
		t.Fatalf("the repo was not brought up to date: %v", err)
	}
}

// TestPullBeforeOpen_LeavesADirtyRepoAlone: the user has work in progress there,
// and a pull that touches it is medusa moving their files around.
func TestPullBeforeOpen_LeavesADirtyRepoAlone(t *testing.T) {
	skipIfNoGit(t)

	clone := newCheckoutTestClone(t)
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("dirty the tree: %v", err)
	}

	notice := pullBeforeOpen(clone)
	if notice == "" {
		t.Error("a skipped pull must be explained, or being behind origin has no visible reason")
	}
	if _, err := os.Stat(filepath.Join(clone, "NEW.md")); !os.IsNotExist(err) {
		t.Error("a dirty repo was pulled")
	}
	body, err := os.ReadFile(filepath.Join(clone, "README.md"))
	if err != nil || string(body) != "mine\n" {
		t.Errorf("the user's uncommitted edit was disturbed: %q, %v", body, err)
	}
}

// TestPullBeforeOpen_SkipsARepoWithNoUpstream stays quiet: a local-only branch
// has nothing to pull from, and saying so every time is noise.
func TestPullBeforeOpen_SkipsARepoWithNoUpstream(t *testing.T) {
	skipIfNoGit(t)

	repo := newCheckoutTestRepo(t)
	if notice := pullBeforeOpen(repo); notice != "" {
		t.Errorf("notice = %q, want none for a branch with no upstream", notice)
	}
}

// TestPullBeforeOpen_RefusesToMergeADivergedBranch: --ff-only is what keeps an
// unattended pull from writing a merge commit into the user's history or leaving
// them with conflicts before they can start.
func TestPullBeforeOpen_RefusesToMergeADivergedBranch(t *testing.T) {
	skipIfNoGit(t)

	clone := newCheckoutTestClone(t)
	// A local commit on the same branch, so the two have diverged.
	if err := os.WriteFile(filepath.Join(clone, "LOCAL.md"), []byte("local\n"), 0o644); err != nil {
		t.Fatalf("write LOCAL.md: %v", err)
	}
	runGit(t, clone, "add", "LOCAL.md")
	runGit(t, clone, "commit", "-m", "local work")

	before, err := git.GetCurrentBranch(clone)
	if err != nil {
		t.Fatalf("GetCurrentBranch: %v", err)
	}

	if notice := pullBeforeOpen(clone); notice == "" {
		t.Error("a refused pull must be explained")
	}
	if _, err := os.Stat(filepath.Join(clone, "NEW.md")); !os.IsNotExist(err) {
		t.Error("a diverged branch was merged")
	}
	if after, _ := git.GetCurrentBranch(clone); after != before {
		t.Errorf("branch changed from %q to %q", before, after)
	}
}
