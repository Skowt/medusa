package app

import (
	"fmt"

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
			CustomBranch:  customBranch,
			FallbackRepos: fallbackRepos,
		}
	}
}
