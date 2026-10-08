package workspaceops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
)

// CreateRequest describes a workspace to make. Bases holds one resolved base
// per repo; for a checkout workspace (Worktree false) Bases[0] is the branch
// the repo is already on, recorded for display.
type CreateRequest struct {
	Name        string
	Repos       []data.RepoRef
	Bases       []string
	Profile     string
	Group       string
	CopyIgnored bool
	Worktree    bool
}

// NameExists reports whether the registry already holds a workspace with this
// name, compared case-insensitively as the TUI compares it.
func (d Deps) NameExists(name string) (bool, error) {
	if d.Registry == nil {
		return false, nil
	}
	entries, err := d.Registry.ListWorkspaces()
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name, name) {
			return true, nil
		}
	}
	return false, nil
}

// Create makes a workspace: with Worktree true it cuts a branch named after
// the workspace and puts a worktree under the workspaces root; with Worktree
// false nothing is created on disk at all, and the workspace is a label over
// the source repo's own checkout. Either way it is saved, registered and added
// to the recents list.
//
// Gitignored files are not copied here even when CopyIgnored is set — the TUI
// shows a progress step for that — so callers run CopyIgnored themselves.
//
// The returned workspace may be non-nil alongside an error, so a caller
// showing a placeholder for it can clear that placeholder.
func (d Deps) Create(req CreateRequest) (ws *data.Workspace, err error) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("panic in workspaceops.Create: %v", r)
			err = fmt.Errorf("create workspace panicked: %v", r)
		}
	}()

	name, repos, bases := req.Name, req.Repos, req.Bases
	if len(repos) == 0 || name == "" {
		return nil, fmt.Errorf("missing repos or workspace name")
	}
	if len(bases) < len(repos) {
		return nil, fmt.Errorf("missing base branch")
	}
	exists, err := d.NameExists(name)
	if err != nil {
		return nil, fmt.Errorf("reading registry: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("workspace '%s' already exists", name)
	}

	if !req.Worktree {
		// Nothing is created on disk: the source repo is the root. Several
		// of these may sit over one repo, which is why the ID is minted at
		// creation rather than derived from the root they all share.
		ws = data.NewCheckoutWorkspace(name, bases[0], repos[0].Path)
		if existing, lErr := d.Store.Load(ws.ID()); lErr == nil && existing != nil {
			return nil, fmt.Errorf("workspace id collides with '%s'", existing.Name)
		}
		return ws, d.register(ws, req, nil)
	}

	// Validate branch doesn't exist in any repo
	for _, repo := range repos {
		if git.BranchExists(repo.Path, name) {
			return nil, fmt.Errorf("branch '%s' already exists in %s", name, repo.Name)
		}
	}

	if len(repos) == 1 {
		// Single-repo workspace — flat {ws_name}/ layout
		repo := repos[0]
		base := bases[0]
		workspacePath := filepath.Join(d.Config.Paths.WorkspacesRoot, name)

		ws = data.NewWorkspace(name, name, base, repo.Path, workspacePath)

		if err := git.CreateWorkspace(repo.Path, workspacePath, name, base); err != nil {
			return ws, err
		}

		// Wait for .git file to exist
		waitForGitFile(workspacePath)
	} else {
		// Multi-repo workspace
		specs := make([]git.RepoSpec, len(repos))
		for i, repo := range repos {
			specs[i] = git.RepoSpec{
				RepoPath:      repo.Path,
				RepoName:      repo.Name,
				WorkspacePath: filepath.Join(d.Config.Paths.WorkspacesRoot, name, repo.Name),
				Branch:        name,
				Base:          bases[i],
			}
		}

		if err := git.CreateGroupWorkspace(specs); err != nil {
			return nil, err
		}

		// Wait for .git files
		for _, spec := range specs {
			waitForGitFile(spec.WorkspacePath)
		}

		worktrees := make([]data.WorktreeRef, len(specs))
		for i, spec := range specs {
			worktrees[i] = data.WorktreeRef{
				Branch: spec.Branch,
				Base:   spec.Base,
				Root:   spec.WorkspacePath,
			}
		}
		ws = data.NewMultiRepoWorkspace(name, repos, worktrees)
	}

	rollback := func() {
		if len(repos) == 1 {
			_ = git.RemoveWorkspace(repos[0].Path, ws.Worktrees[0].Root)
			_ = git.DeleteBranch(repos[0].Path, name)
			_ = os.RemoveAll(ws.Root())
			return
		}
		specs := make([]git.RepoSpec, len(repos))
		for i, repo := range repos {
			specs[i] = git.RepoSpec{
				RepoPath:      repo.Path,
				WorkspacePath: ws.Worktrees[i].Root,
				Branch:        name,
			}
		}
		git.RemoveGroupWorkspace(specs)
	}

	return ws, d.register(ws, req, rollback)
}

// register saves a freshly built workspace and records it in the registry and
// the recents list. rollback undoes whatever was made on disk if the save
// fails; it is nil for a workspace that made nothing.
func (d Deps) register(ws *data.Workspace, req CreateRequest, rollback func()) error {
	ws.CopyIgnored = req.CopyIgnored

	// Set profile and group on the workspace before saving so they persist.
	if req.Profile != "" {
		ws.Profile = req.Profile
	}
	if req.Group != "" {
		ws.Group = req.Group
	}

	if err := d.Store.Save(ws); err != nil {
		if rollback != nil {
			rollback()
		}
		return err
	}

	if err := d.Registry.AddWorkspace(ws.Name, string(ws.ID()), req.Profile); err != nil {
		logging.Warn("Failed to register workspace: %v", err)
	}

	if d.Recents != nil {
		if err := d.Recents.Add(req.Repos); err != nil {
			logging.Warn("Failed to update recents: %v", err)
		}
	}
	return nil
}

// CopyIgnored copies gitignored files from the source repos into the
// workspace's worktrees.
func CopyIgnored(ws *data.Workspace, repos []data.RepoRef) {
	if len(repos) == 1 {
		CopyIgnoredFiles(repos[0].Path, ws.PrimaryWorktreeRoot())
		return
	}
	for i, repo := range repos {
		if i < len(ws.Worktrees) {
			CopyIgnoredFiles(repo.Path, ws.Worktrees[i].Root)
		}
	}
}

// waitForGitFile waits for a .git file to appear (race condition from workspace creation).
func waitForGitFile(workspacePath string) {
	gitPath := filepath.Join(workspacePath, ".git")
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(gitPath); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
