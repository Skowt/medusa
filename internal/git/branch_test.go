package git

import (
	"os"
	"path/filepath"
	"testing"
)

// cloneOf clones src into a temp dir, giving the clone an "origin" remote whose
// remote-tracking branches cover every branch in src. Branches other than the
// default are present as origin/<name> but not as local branches — which is
// exactly the remote-only case ResolveCustomBranch has to handle.
func cloneOf(t *testing.T, src string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "clone")
	runGit(t, t.TempDir(), "clone", src, dst)
	return dst
}

// initRepoOn is initRepo with a caller-chosen default branch.
func initRepoOn(t *testing.T, branch string) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-b", branch)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("init"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "init")
	return root
}

func TestGetBaseBranch(t *testing.T) {
	skipIfNoGit(t)

	assertBase := func(t *testing.T, repo, want string) {
		t.Helper()
		got, err := GetBaseBranch(repo)
		if err != nil {
			t.Fatalf("GetBaseBranch: %v", err)
		}
		if got != want {
			t.Errorf("base branch = %q, want %q", got, want)
		}
	}

	t.Run("plain main repo", func(t *testing.T) {
		assertBase(t, initRepo(t), "main")
	})

	t.Run("no remote falls back to the name guesses", func(t *testing.T) {
		assertBase(t, initRepoOn(t, "master"), "master")
	})

	// A tag named "main" is not a branch. "rev-parse --verify main" resolves it
	// anyway, which used to make a master repo report "main" and branch new
	// worktrees off a frozen tag.
	t.Run("a tag named main does not shadow the real branch", func(t *testing.T) {
		repo := initRepoOn(t, "master")
		runGit(t, repo, "tag", "main")

		assertBase(t, repo, "master")
	})

	// origin/HEAD is authoritative. Guessing names first meant a stale local
	// "main" in a master repo won and origin/HEAD was never consulted.
	t.Run("origin/HEAD beats a stale local main", func(t *testing.T) {
		upstream := initRepoOn(t, "master")
		repo := cloneOf(t, upstream)
		runGit(t, repo, "branch", "main") // stale leftover

		if !BranchExists(repo, "main") {
			t.Fatal("fixture: local main should exist, or this proves nothing")
		}
		assertBase(t, repo, "master")
	})

	t.Run("origin/HEAD is used even when it is not a guessed name", func(t *testing.T) {
		upstream := initRepoOn(t, "trunk")
		assertBase(t, cloneOf(t, upstream), "trunk")
	})

	t.Run("nothing to go on assumes main", func(t *testing.T) {
		assertBase(t, initRepoOn(t, "trunk"), "main")
	})
}

func TestResolveCustomBranch(t *testing.T) {
	skipIfNoGit(t)

	t.Run("local branch wins", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "branch", "feature/foo")

		got, err := ResolveCustomBranch(repo, "feature/foo")
		if err != nil {
			t.Fatalf("ResolveCustomBranch: %v", err)
		}
		if got != "feature/foo" {
			t.Errorf("base = %q, want %q", got, "feature/foo")
		}
	})

	t.Run("remote-only branch resolves to origin/<branch>", func(t *testing.T) {
		upstream := initRepo(t)
		runGit(t, upstream, "branch", "feature/foo")
		repo := cloneOf(t, upstream)

		// Precondition: the branch must not exist locally, or this test would
		// pass via the local lookup and prove nothing.
		if BranchExists(repo, "feature/foo") {
			t.Fatal("feature/foo exists locally in the clone; fixture is wrong")
		}

		got, err := ResolveCustomBranch(repo, "feature/foo")
		if err != nil {
			t.Fatalf("ResolveCustomBranch: %v", err)
		}
		if got != "origin/feature/foo" {
			t.Errorf("base = %q, want %q", got, "origin/feature/foo")
		}
	})

	t.Run("missing branch errors", func(t *testing.T) {
		repo := initRepo(t)

		if _, err := ResolveCustomBranch(repo, "nope"); err == nil {
			t.Fatal("expected an error for a branch that exists neither locally nor on origin")
		}
	})

	t.Run("a commit SHA is not a branch", func(t *testing.T) {
		repo := initRepo(t)
		sha := runGit(t, repo, "rev-parse", "HEAD")

		if _, err := ResolveCustomBranch(repo, sha); err == nil {
			t.Fatal("expected an error: this resolves branches, not arbitrary refs")
		}
	})
}
