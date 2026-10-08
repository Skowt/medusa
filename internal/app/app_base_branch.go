package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/workspaceops"
)

// Base-branch resolution — step 1 of workspace creation. Each function below
// backs one option of the Base Branch dialog and hands the result to
// createWorkspace via messages.WorkspaceFetchDone. The resolution itself lives
// in workspaceops, which the headless CLI shares.

// fetchRemoteBase resolves each repo's default branch (main/master/develop),
// fetching origin first if the last fetch is stale.
func (a *App) fetchRemoteBase(repos []data.RepoRef, name, profile, group string, copyIgnored bool) tea.Cmd {
	reposCopy := copyRepos(repos)
	return func() tea.Msg {
		bases, err := workspaceops.RemoteBases(reposCopy)
		if err != nil {
			return messages.WorkspaceCreateFailed{Err: err}
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
	reposCopy := copyRepos(repos)
	return func() tea.Msg {
		bases, err := workspaceops.CheckedOutBases(reposCopy)
		if err != nil {
			return messages.WorkspaceCreateFailed{Err: err}
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

// fetchCustomBase fetches if stale, then resolves a named branch in each repo;
// repos without it fall back to their default branch and are reported in
// FallbackRepos so the caller can warn.
func (a *App) fetchCustomBase(repos []data.RepoRef, name, profile, group, customBranch string, copyIgnored bool) tea.Cmd {
	reposCopy := copyRepos(repos)
	return func() tea.Msg {
		bases, fallbackRepos, err := workspaceops.CustomBases(reposCopy, customBranch)
		if err != nil {
			return messages.WorkspaceCreateFailed{Err: err}
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
// above: nothing is branched, the repo is pulled (see
// workspaceops.PullBeforeOpen) and the branch it is on is recorded.
func (a *App) resolveCheckoutBase(repos []data.RepoRef, name, profile, group string) tea.Cmd {
	reposCopy := copyRepos(repos)
	return func() tea.Msg {
		if len(reposCopy) == 0 {
			return messages.WorkspaceCreateFailed{Err: fmt.Errorf("missing repo")}
		}
		branch, notice := workspaceops.CheckoutBranch(reposCopy[0].Path)
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

func copyRepos(repos []data.RepoRef) []data.RepoRef {
	out := make([]data.RepoRef, len(repos))
	copy(out, repos)
	return out
}
