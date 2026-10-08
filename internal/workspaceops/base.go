package workspaceops

import (
	"fmt"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
)

// Base-branch resolution — step 1 of workspace creation. Each function below
// backs one option of the Base Branch dialog (and one CLI flag) and resolves a
// base ref per repo. The base is the commit the new worktree's branch starts
// from; see git.CreateWorkspace.

// RemoteBases resolves each repo's default branch (main/master/develop),
// fetching origin first if the last fetch is stale.
func RemoteBases(repos []data.RepoRef) ([]string, error) {
	bases := make([]string, len(repos))
	for i, repo := range repos {
		base, err := git.GetFreshRemoteBase(repo.Path)
		if err != nil {
			base = "HEAD"
		}
		// Verify the base ref resolves to a commit
		if err := git.ValidateRef(repo.Path, base); err != nil {
			return nil, fmt.Errorf("%s: repo has no commits on %q", repo.Name, base)
		}
		bases[i] = base
	}
	return bases, nil
}

// CheckedOutBases resolves the currently checked-out branch as the base (no fetch).
func CheckedOutBases(repos []data.RepoRef) ([]string, error) {
	bases := make([]string, len(repos))
	for i, repo := range repos {
		base, err := git.GetCheckedOutBase(repo.Path)
		if err != nil {
			base = "HEAD"
		}
		// Verify the base ref resolves to a commit
		if err := git.ValidateRef(repo.Path, base); err != nil {
			return nil, fmt.Errorf("%s: repo has no commits — make an initial commit first", repo.Name)
		}
		bases[i] = base
	}
	return bases, nil
}

// CustomBases fetches if stale, then resolves a named branch in each repo,
// looking locally first and then on origin.
//
// A repo that doesn't have the branch is only fatal if *no* repo has it — in a
// multi-repo workspace a feature branch commonly lives in one repo while the
// others stay on their default branch, and that should still create. Repos
// without the branch fall back to their default branch and are returned in
// fallbackRepos, so the caller can warn and the fallback is never silent.
func CustomBases(repos []data.RepoRef, branch string) (bases, fallbackRepos []string, err error) {
	bases = make([]string, len(repos))
	for i, repo := range repos {
		_ = git.FetchIfStale(repo.Path)

		base, err := git.ResolveCustomBranch(repo.Path, branch)
		if err == nil {
			bases[i] = base
			continue
		}

		base, fbErr := git.GetFreshRemoteBase(repo.Path)
		if fbErr != nil {
			return nil, nil, fmt.Errorf("%s: %w", repo.Name, err)
		}
		if vErr := git.ValidateRef(repo.Path, base); vErr != nil {
			return nil, nil, fmt.Errorf("%s: repo has no commits on %q", repo.Name, base)
		}
		bases[i] = base
		fallbackRepos = append(fallbackRepos, repo.Name)
	}

	if len(fallbackRepos) == len(repos) {
		return nil, nil, fmt.Errorf("branch %q not found locally or on origin", branch)
	}
	return bases, fallbackRepos, nil
}

// CheckoutBranch is the no-worktree counterpart of the functions above.
// Nothing is branched: the workspace opens on the repo exactly as it stands,
// so the only thing to resolve is the branch it is already on, recorded so
// the UI has something to show.
//
// It does pull first, which the name understates. Opening an agent on a repo
// that is a week behind is the common way to start work on stale code, and the
// worktree path already fetches for exactly that reason. See PullBeforeOpen
// for the conditions.
func CheckoutBranch(repoPath string) (branch, pullNotice string) {
	pullNotice = PullBeforeOpen(repoPath)
	branch, err := git.GetCurrentBranch(repoPath)
	if err != nil {
		branch = ""
	}
	return branch, pullNotice
}

// PullBeforeOpen brings a repo up to date before an agent starts working in it,
// and returns what to tell the user when it did not. An empty string means there
// is nothing worth saying: it pulled, or there was nothing to pull.
//
// Three conditions gate it, and each one is a case where pulling would be worse
// than being out of date:
//
//  1. **A dirty working tree is left alone.** The user has work in progress
//     there; a pull that touches it is medusa moving their files around. They
//     are told, so "why is this behind origin" has an answer on screen.
//  2. **A branch with no upstream is skipped silently.** A local-only branch and
//     a detached HEAD have nothing to pull from, and warning about it every time
//     would be noise rather than information.
//  3. **Only a fast-forward counts.** git.PullFastForward refuses a diverged
//     branch rather than writing a merge commit into the user's history.
func PullBeforeOpen(repoPath string) string {
	status, err := git.GetStatus(repoPath)
	if err != nil {
		logging.Warn("Skipping pull for %s: %v", repoPath, err)
		return ""
	}
	if !status.Clean {
		return "Uncommitted changes in the repo, so it was opened without pulling"
	}
	if !git.HasUpstream(repoPath) {
		return ""
	}
	if err := git.PullFastForward(repoPath); err != nil {
		logging.Warn("Pull failed for %s: %v", repoPath, err)
		return "Could not pull the repo, so it was opened as it stands"
	}
	return ""
}
