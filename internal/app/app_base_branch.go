package app

import (
	"fmt"

	"github.com/Skowt/medusa/internal/logging"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/messages"
)

// Base-branch resolution — step 1 of workspace creation. Each function below
// backs one option of the Base Branch dialog, resolves a base ref per repo, and
// hands the result to createWorkspace via messages.WorkspaceFetchDone. The base
// is the commit the new worktree's branch starts from; see git.CreateWorkspace.

// fetchRemoteBase resolves each repo's default branch (main/master/develop),
// fetching origin first if the last fetch is stale.
func (a *App) fetchRemoteBase(repos []data.RepoRef, name, profile, group string, copyIgnored bool) tea.Cmd {
	reposCopy := make([]data.RepoRef, len(repos))
	copy(reposCopy, repos)
	return func() tea.Msg {
		bases := make([]string, len(reposCopy))
		for i, repo := range reposCopy {
			base, err := git.GetFreshRemoteBase(repo.Path)
			if err != nil {
				base = "HEAD"
			}
			// Verify the base ref resolves to a commit
			if err := git.ValidateRef(repo.Path, base); err != nil {
				return messages.WorkspaceCreateFailed{
					Err: fmt.Errorf("%s: repo has no commits on %q", repo.Name, base),
				}
			}
			bases[i] = base
		}
		return messages.WorkspaceFetchDone{
			Name:        name,
			Repos:       reposCopy,
			Bases:       bases,
			Profile:     profile,
			CopyIgnored: copyIgnored,
			Group:       group,
			Worktree:    true,
		}
	}
}

// fetchCheckedOutBase resolves the currently checked-out branch as the base (no fetch).
func (a *App) fetchCheckedOutBase(repos []data.RepoRef, name, profile, group string, copyIgnored bool) tea.Cmd {
	reposCopy := make([]data.RepoRef, len(repos))
	copy(reposCopy, repos)
	return func() tea.Msg {
		bases := make([]string, len(reposCopy))
		for i, repo := range reposCopy {
			base, err := git.GetCheckedOutBase(repo.Path)
			if err != nil {
				base = "HEAD"
			}
			// Verify the base ref resolves to a commit
			if err := git.ValidateRef(repo.Path, base); err != nil {
				return messages.WorkspaceCreateFailed{
					Err: fmt.Errorf("%s: repo has no commits — make an initial commit first", repo.Name),
				}
			}
			bases[i] = base
		}
		return messages.WorkspaceFetchDone{
			Name:        name,
			Repos:       reposCopy,
			Bases:       bases,
			Profile:     profile,
			CopyIgnored: copyIgnored,
			Group:       group,
			Worktree:    true,
		}
	}
}

// fetchCustomBase fetches if stale, then resolves a named branch in each repo,
// looking locally first and then on origin.
//
// A repo that doesn't have the branch is only fatal if *no* repo has it — in a
// multi-repo workspace a feature branch commonly lives in one repo while the
// others stay on their default branch, and that should still create. Repos
// without the branch fall back to their default branch, and the caller warns
// about them (FallbackRepos) so the fallback is never silent.
func (a *App) fetchCustomBase(repos []data.RepoRef, name, profile, group, customBranch string, copyIgnored bool) tea.Cmd {
	reposCopy := make([]data.RepoRef, len(repos))
	copy(reposCopy, repos)
	return func() tea.Msg {
		bases := make([]string, len(reposCopy))
		var fallbackRepos []string
		for i, repo := range reposCopy {
			_ = git.FetchIfStale(repo.Path)

			base, err := git.ResolveCustomBranch(repo.Path, customBranch)
			if err == nil {
				bases[i] = base
				continue
			}

			base, fbErr := git.GetFreshRemoteBase(repo.Path)
			if fbErr != nil {
				return messages.WorkspaceCreateFailed{
					Err: fmt.Errorf("%s: %w", repo.Name, err),
				}
			}
			if vErr := git.ValidateRef(repo.Path, base); vErr != nil {
				return messages.WorkspaceCreateFailed{
					Err: fmt.Errorf("%s: repo has no commits on %q", repo.Name, base),
				}
			}
			bases[i] = base
			fallbackRepos = append(fallbackRepos, repo.Name)
		}

		if len(fallbackRepos) == len(reposCopy) {
			return messages.WorkspaceCreateFailed{
				Err: fmt.Errorf("branch %q not found locally or on origin", customBranch),
			}
		}

		return messages.WorkspaceFetchDone{
			Name:          name,
			Repos:         reposCopy,
			Bases:         bases,
			Profile:       profile,
			CopyIgnored:   copyIgnored,
			Group:         group,
			Worktree:      true,
			CustomBranch:  customBranch,
			FallbackRepos: fallbackRepos,
		}
	}
}

// resolveCheckoutBase is the no-worktree counterpart of the three functions
// above. Nothing is branched: the workspace opens on the repo exactly as it
// stands, so the only thing to resolve is the branch it is already on, recorded
// so the UI has something to show.
//
// It does pull first, which the name understates. Opening an agent on a repo
// that is a week behind is the common way to start work on stale code, and the
// worktree path already fetches for exactly that reason. See pullBeforeOpen for
// the conditions.
func (a *App) resolveCheckoutBase(repos []data.RepoRef, name, profile, group string) tea.Cmd {
	reposCopy := make([]data.RepoRef, len(repos))
	copy(reposCopy, repos)
	return func() tea.Msg {
		if len(reposCopy) == 0 {
			return messages.WorkspaceCreateFailed{Err: fmt.Errorf("missing repo")}
		}
		repoPath := reposCopy[0].Path
		notice := pullBeforeOpen(repoPath)
		branch, err := git.GetCurrentBranch(repoPath)
		if err != nil {
			branch = ""
		}
		return messages.WorkspaceFetchDone{
			Name:       name,
			Repos:      reposCopy,
			Bases:      []string{branch},
			Profile:    profile,
			Group:      group,
			Worktree:   false,
			PullNotice: notice,
		}
	}
}

// pullBeforeOpen brings a repo up to date before an agent starts working in it,
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
func pullBeforeOpen(repoPath string) string {
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
