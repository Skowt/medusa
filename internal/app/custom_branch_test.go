package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// branchTestRepo builds a repo on main with one commit, plus the given extra
// branches. No remote — GetFreshRemoteBase then degrades to the local default
// branch, which is all the fallback path needs here.
func branchTestRepo(t *testing.T, name string, branches ...string) data.RepoRef {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	runGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "init")
	for _, b := range branches {
		runGit(t, root, "branch", b)
	}
	return data.RepoRef{Path: root, Name: name}
}

func fetchCustom(t *testing.T, repos []data.RepoRef, branch string) any {
	t.Helper()
	cmd := (&App{}).fetchCustomBase(repos, "ws", "Default", "", branch, false)
	if cmd == nil {
		t.Fatal("fetchCustomBase returned a nil cmd")
	}
	return cmd()
}

func TestFetchCustomBase_SingleRepo(t *testing.T) {
	skipIfNoGit(t)

	t.Run("branch found", func(t *testing.T) {
		repos := []data.RepoRef{branchTestRepo(t, "solo", "feature/foo")}

		got := fetchCustom(t, repos, "feature/foo")
		msg, ok := got.(messages.WorkspaceFetchDone)
		if !ok {
			t.Fatalf("got %T, want WorkspaceFetchDone", got)
		}
		if msg.Bases[0] != "feature/foo" {
			t.Errorf("base = %q, want %q", msg.Bases[0], "feature/foo")
		}
		if len(msg.FallbackRepos) != 0 {
			t.Errorf("FallbackRepos = %v, want none", msg.FallbackRepos)
		}
	})

	// The old behavior: silently base off main. A typo has to be an error.
	t.Run("branch missing is an error, not a fallback to main", func(t *testing.T) {
		repos := []data.RepoRef{branchTestRepo(t, "solo")}

		msg := fetchCustom(t, repos, "feature/typo")
		failed, ok := msg.(messages.WorkspaceCreateFailed)
		if !ok {
			t.Fatalf("got %T, want WorkspaceCreateFailed (a missing branch must not silently base off main)", msg)
		}
		if failed.Err == nil {
			t.Fatal("WorkspaceCreateFailed carries no error")
		}
	})
}

func TestFetchCustomBase_MultiRepo(t *testing.T) {
	skipIfNoGit(t)

	t.Run("partial match succeeds and names the fallback repos", func(t *testing.T) {
		repos := []data.RepoRef{
			branchTestRepo(t, "has-it", "feature/foo"),
			branchTestRepo(t, "missing-a"),
			branchTestRepo(t, "missing-b"),
		}

		msg, ok := fetchCustom(t, repos, "feature/foo").(messages.WorkspaceFetchDone)
		if !ok {
			t.Fatal("a branch present in one repo must still create the workspace")
		}
		if msg.Bases[0] != "feature/foo" {
			t.Errorf("has-it base = %q, want %q", msg.Bases[0], "feature/foo")
		}
		for i, name := range []string{"missing-a", "missing-b"} {
			if msg.Bases[i+1] != "main" {
				t.Errorf("%s base = %q, want its default branch %q", name, msg.Bases[i+1], "main")
			}
		}
		if got, want := msg.FallbackRepos, []string{"missing-a", "missing-b"}; !equalStrings(got, want) {
			t.Errorf("FallbackRepos = %v, want %v", got, want)
		}
		if msg.CustomBranch != "feature/foo" {
			t.Errorf("CustomBranch = %q, want %q (the toast needs it)", msg.CustomBranch, "feature/foo")
		}
	})

	t.Run("no repo has the branch is an error", func(t *testing.T) {
		repos := []data.RepoRef{
			branchTestRepo(t, "one"),
			branchTestRepo(t, "two"),
		}

		if msg := fetchCustom(t, repos, "feature/nope"); !isCreateFailed(msg) {
			t.Fatalf("got %T, want WorkspaceCreateFailed when no repo has the branch", msg)
		}
	})

	t.Run("every repo has the branch warns about nothing", func(t *testing.T) {
		repos := []data.RepoRef{
			branchTestRepo(t, "one", "feature/foo"),
			branchTestRepo(t, "two", "feature/foo"),
		}

		msg, ok := fetchCustom(t, repos, "feature/foo").(messages.WorkspaceFetchDone)
		if !ok {
			t.Fatal("want WorkspaceFetchDone")
		}
		if len(msg.FallbackRepos) != 0 {
			t.Errorf("FallbackRepos = %v, want none — nothing fell back", msg.FallbackRepos)
		}
	})
}

func isCreateFailed(msg any) bool {
	_, ok := msg.(messages.WorkspaceCreateFailed)
	return ok
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
