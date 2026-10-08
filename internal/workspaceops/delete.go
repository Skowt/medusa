package workspaceops

import (
	"os"
	"strings"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
)

// Delete removes what medusa made for a workspace — its worktrees, their
// branches and its root directory — and then its metadata and registry entry.
// It returns a warning when branches could not be deleted. It does not touch
// tmux; callers kill the workspace's sessions first.
func (d Deps) Delete(ws *data.Workspace) (branchWarning string) {
	// Only worktrees medusa made are removed. A workspace pointed straight
	// at a repo's own checkout owns neither the directory nor the branch, so
	// deleting it here would delete the user's repo — see RemovableWorktrees,
	// which is the guard rather than a convenience.
	specs := RemovableWorktrees(ws)
	if len(specs) > 0 {
		_, branchErrs := git.RemoveGroupWorkspace(specs)
		if len(branchErrs) > 0 {
			msgs := make([]string, len(branchErrs))
			for i, e := range branchErrs {
				msgs[i] = e.Error()
			}
			branchWarning = "Failed to delete branches: " + strings.Join(msgs, "; ")
		}
	}
	// Clean up the workspace root directory, unless the root is a repo.
	if root := ws.Root(); root != "" && ws.UsesWorktree() {
		_ = os.RemoveAll(root)
	}

	_ = d.Store.Delete(ws.ID())
	_ = d.Registry.RemoveWorkspace(string(ws.ID()))
	return branchWarning
}

// RemovableWorktrees returns the specs for the worktrees of ws that git may
// remove, dropping any whose directory is the source repo itself or is not a
// worktree on disk. Both checks matter: the first catches a workspace created
// with the worktree box unticked, and the second is the backstop for anything
// whose stored path has since come to mean something else. Deleting a branch is
// bound to the same test, since a checkout workspace records the branch the repo
// happened to be on rather than one of its own.
func RemovableWorktrees(ws *data.Workspace) []git.RepoSpec {
	var specs []git.RepoSpec
	for i, repo := range ws.Repos {
		if i >= len(ws.Worktrees) {
			break
		}
		root := ws.Worktrees[i].Root
		if root == "" || data.NormalizePath(root) == data.NormalizePath(repo.Path) {
			continue
		}
		// A path still on disk that is not a worktree is not ours to remove.
		// One already gone is fine to pass through: git prunes the registration
		// and the branch goes with it.
		if info, err := os.Stat(root); err == nil && info.IsDir() && !git.IsWorktree(root) {
			logging.Warn("Refusing to delete %s: not a git worktree", root)
			continue
		}
		specs = append(specs, git.RepoSpec{
			RepoPath:      repo.Path,
			RepoName:      repo.Name,
			WorkspacePath: root,
			Branch:        ws.Worktrees[i].Branch,
		})
	}
	return specs
}
