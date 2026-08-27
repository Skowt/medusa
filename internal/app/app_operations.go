package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
)

// loadWorkspaces loads all workspaces from the store and applies profiles from the registry.
func (a *App) loadWorkspaces() tea.Cmd {
	return func() tea.Msg {
		entries, err := a.registry.ListWorkspaces()
		if err != nil {
			logging.Warn("Failed to load registry: %v", err)
			return messages.WorkspacesLoaded{Workspaces: nil}
		}

		var workspaces []*data.Workspace
		for i, entry := range entries {
			ws, err := a.workspaces.Load(data.WorkspaceID(entry.ID))
			if err != nil {
				logging.Warn("Failed to load workspace %s: %v", entry.Name, err)
				continue
			}
			if ws == nil {
				continue
			}

			// Migrate uniform-layout single-repo workspaces to flat {ws_name}/ layout.
			if a.config != nil && len(ws.Repos) == 1 && len(ws.Worktrees) == 1 {
				wtRoot := ws.Worktrees[0].Root
				expectedRoot := filepath.Join(a.config.Paths.WorkspacesRoot, ws.Name)
				if wtRoot == filepath.Join(expectedRoot, ws.Repos[0].Name) {
					// Old uniform layout detected — migrate to flat.
					tmpRoot := expectedRoot + ".migrating"
					if mErr := git.MoveWorkspace(ws.Repos[0].Path, wtRoot, tmpRoot); mErr != nil {
						logging.Warn("Migration: failed to move workspace %s: %v", ws.Name, mErr)
					} else {
						_ = os.Remove(expectedRoot) // remove now-empty parent
						if rErr := os.Rename(tmpRoot, expectedRoot); rErr != nil {
							logging.Warn("Migration: failed to rename workspace %s: %v", ws.Name, rErr)
							_ = git.MoveWorkspace(ws.Repos[0].Path, tmpRoot, wtRoot)
						} else {
							oldID := ws.ID()
							ws.Worktrees[0].Root = expectedRoot
							if sErr := a.workspaces.Save(ws); sErr != nil {
								logging.Warn("Migration: failed to save workspace %s: %v", ws.Name, sErr)
								// Rollback: move back to uniform layout
								_ = os.Rename(expectedRoot, tmpRoot)
								_ = os.MkdirAll(filepath.Dir(wtRoot), 0o755)
								_ = git.MoveWorkspace(ws.Repos[0].Path, tmpRoot, wtRoot)
								ws.Worktrees[0].Root = wtRoot
							} else {
								newID := ws.ID()
								if oldID != newID {
									_ = a.workspaces.Delete(oldID)
									_ = a.registry.UpdateWorkspace(string(oldID), ws.Name, string(newID))
									entries[i].ID = string(newID)
								}
								logging.Info("Migration: moved workspace %s to flat layout", ws.Name)
							}
						}
					}
				}
			}

			// Apply profile from registry
			ws.Profile = entry.Profile
			workspaces = append(workspaces, ws)
		}

		// Phase A: detect metadata orphans (worktree directory missing on disk)
		for _, ws := range workspaces {
			root := ws.PrimaryWorktreeRoot()
			if root == "" {
				continue
			}
			if _, err := os.Stat(root); os.IsNotExist(err) {
				ws.Orphan = data.OrphanMetadata
			}
		}

		// Phase B: detect directory orphans (directory exists but no metadata)
		if a.config != nil && a.config.Paths.WorkspacesRoot != "" {
			knownRoots := make(map[string]bool)
			for _, ws := range workspaces {
				for _, root := range ws.AllRoots() {
					knownRoots[root] = true
				}
				if ws.Root() != "" {
					knownRoots[ws.Root()] = true
				}
			}

			dirEntries, err := os.ReadDir(a.config.Paths.WorkspacesRoot)
			if err == nil {
				for _, de := range dirEntries {
					if !de.IsDir() {
						continue
					}
					// Skip hidden directories (e.g. .claude)
					if strings.HasPrefix(de.Name(), ".") {
						continue
					}
					dirPath := filepath.Join(a.config.Paths.WorkspacesRoot, de.Name())
					if knownRoots[dirPath] {
						continue
					}
					orphan := &data.Workspace{
						Name: de.Name(),
						Worktrees: []data.WorktreeRef{
							{Root: dirPath},
						},
						Orphan:     data.OrphanDirectory,
						OrphanPath: dirPath,
					}
					workspaces = append(workspaces, orphan)
				}
			}
		}

		return messages.WorkspacesLoaded{Workspaces: workspaces}
	}
}

// deleteOrphanWorkspace removes an orphaned workspace.
// Metadata orphans: remove store + registry entries.
// Directory orphans: remove the directory from disk.
func (a *App) deleteOrphanWorkspace(ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.OrphanWorkspaceDeleted{Workspace: ws}
		}
	}
	return func() tea.Msg {
		var err error
		switch ws.Orphan {
		case data.OrphanMetadata:
			if storeErr := a.workspaces.Delete(ws.ID()); storeErr != nil {
				err = fmt.Errorf("remove workspace store: %w", storeErr)
			}
			if regErr := a.registry.RemoveWorkspace(string(ws.ID())); regErr != nil && err == nil {
				err = fmt.Errorf("remove registry entry: %w", regErr)
			}
		case data.OrphanDirectory:
			if ws.OrphanPath != "" {
				if rmErr := forceRemoveAll(ws.OrphanPath); rmErr != nil {
					err = fmt.Errorf("remove %s: %w", ws.OrphanPath, rmErr)
				}
			}
		}
		return messages.OrphanWorkspaceDeleted{Workspace: ws, Err: err}
	}
}

// forceRemoveAll removes path, first chmod'ing any directories under it to
// 0o700 so os.RemoveAll can unlink their contents. Needed for Go module
// cache trees (e.g. .gotools/pkg/mod/) which are created mode 0o555.
func forceRemoveAll(path string) error {
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(path)
}

// createWorkspace creates a new workspace. With worktree false nothing is
// created on disk at all: the workspace is a label over the source repo's own
// checkout, so no branch is cut and no directory is made.
func (a *App) createWorkspace(name string, repos []data.RepoRef, bases []string, profile, group string, copyIgnored, worktree bool) tea.Cmd {
	return func() (msg tea.Msg) {
		var ws *data.Workspace
		defer func() {
			if r := recover(); r != nil {
				logging.Error("panic in createWorkspace: %v", r)
				msg = messages.WorkspaceCreateFailed{
					Workspace: ws,
					Err:       fmt.Errorf("create workspace panicked: %v", r),
				}
			}
		}()

		if len(repos) == 0 || name == "" {
			return messages.WorkspaceCreateFailed{
				Err: fmt.Errorf("missing repos or workspace name"),
			}
		}

		// Validate workspace name doesn't already exist
		if a.workspaceNameExists(name) {
			return messages.WorkspaceCreateFailed{
				Err: fmt.Errorf("workspace '%s' already exists", name),
			}
		}

		if !worktree {
			// The repo is the root, and the root is half of what identifies a
			// workspace — so a second one over the same repo hashes to the same
			// ID and would overwrite the first.
			ws = data.NewCheckoutWorkspace(name, bases[0], repos[0].Path)
			if existing, lErr := a.workspaces.Load(ws.ID()); lErr == nil && existing != nil {
				return messages.WorkspaceCreateFailed{
					Err: fmt.Errorf("workspace '%s' already uses this repo directly", existing.Name),
				}
			}
			return a.registerWorkspace(ws, repos, profile, group, false, nil)
		}

		// Validate branch doesn't exist in any repo
		for _, repo := range repos {
			if git.BranchExists(repo.Path, name) {
				return messages.WorkspaceCreateFailed{
					Err: fmt.Errorf("branch '%s' already exists in %s", name, repo.Name),
				}
			}
		}

		if len(repos) == 1 {
			// Single-repo workspace — flat {ws_name}/ layout
			repo := repos[0]
			base := bases[0]
			workspacePath := filepath.Join(
				a.config.Paths.WorkspacesRoot,
				name,
			)

			ws = data.NewWorkspace(name, name, base, repo.Path, workspacePath)

			if err := git.CreateWorkspace(repo.Path, workspacePath, name, base); err != nil {
				return messages.WorkspaceCreateFailed{Workspace: ws, Err: err}
			}

			// Wait for .git file to exist
			waitForGitFile(workspacePath)
		} else {
			// Multi-repo workspace
			specs := make([]git.RepoSpec, len(repos))
			for i, repo := range repos {
				wsPath := filepath.Join(
					a.config.Paths.WorkspacesRoot,
					name,
					repo.Name,
				)
				specs[i] = git.RepoSpec{
					RepoPath:      repo.Path,
					RepoName:      repo.Name,
					WorkspacePath: wsPath,
					Branch:        name,
					Base:          bases[i],
				}
			}

			if err := git.CreateGroupWorkspace(specs); err != nil {
				return messages.WorkspaceCreateFailed{Err: err}
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

		return a.registerWorkspace(ws, repos, profile, group, copyIgnored, rollback)
	}
}

// registerWorkspace saves a freshly built workspace, records it in the registry
// and the recents list, and returns the message that ends creation. rollback
// undoes whatever was made on disk if the save fails; it is nil for a workspace
// that made nothing.
func (a *App) registerWorkspace(ws *data.Workspace, repos []data.RepoRef, profile, group string, copyIgnored bool, rollback func()) tea.Msg {
	ws.CopyIgnored = copyIgnored

	// Set profile and group on the workspace before saving so they persist.
	if profile != "" {
		ws.Profile = profile
	}
	if group != "" {
		ws.Group = group
	}

	if err := a.workspaces.Save(ws); err != nil {
		if rollback != nil {
			rollback()
		}
		return messages.WorkspaceCreateFailed{Workspace: ws, Err: err}
	}

	if err := a.registry.AddWorkspace(ws.Name, string(ws.ID()), profile); err != nil {
		logging.Warn("Failed to register workspace: %v", err)
	}

	if err := a.recents.Add(repos); err != nil {
		logging.Warn("Failed to update recents: %v", err)
	}

	// If gitignored files need copying, signal the overlay to advance first.
	if copyIgnored {
		return messages.WorkspaceWorktreeDone{Workspace: ws, Repos: repos}
	}
	return messages.WorkspaceCreated{Workspace: ws}
}

// copyIgnoredFilesCmd copies gitignored files from source repos into the workspace worktrees.
func copyIgnoredFilesCmd(ws *data.Workspace, repos []data.RepoRef) tea.Cmd {
	return func() tea.Msg {
		if len(repos) == 1 {
			copyIgnoredFiles(repos[0].Path, ws.PrimaryWorktreeRoot())
		} else {
			for i, repo := range repos {
				if i < len(ws.Worktrees) {
					copyIgnoredFiles(repo.Path, ws.Worktrees[i].Root)
				}
			}
		}
		return messages.WorkspaceCreated{Workspace: ws}
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

// runSetupAsync runs setup scripts asynchronously and returns a
// WorkspaceSetupComplete message.
//
// A workspace over the repo's own checkout is skipped: setup-workspace commands
// exist to make a *fresh* worktree usable — installing dependencies, copying env
// files in — and the repo the user already works in is already set up. Running
// them there would at best repeat work and at worst overwrite files they have.
// The message is still emitted, since the run scripts hang off it.
func (a *App) runSetupAsync(ws *data.Workspace) tea.Cmd {
	return func() tea.Msg {
		if ws != nil && !ws.UsesWorktree() {
			return messages.WorkspaceSetupComplete{Workspace: ws}
		}
		if err := a.scripts.RunSetup(ws); err != nil {
			return messages.WorkspaceSetupComplete{Workspace: ws, Err: err}
		}
		return messages.WorkspaceSetupComplete{Workspace: ws}
	}
}

// deleteWorkspace deletes a workspace.
// If silent is true, branch warnings are suppressed (used for auto-pruned archived workspaces).
func (a *App) deleteWorkspace(ws *data.Workspace, silent ...bool) tea.Cmd {
	isSilent := len(silent) > 0 && silent[0]
	if ws == nil {
		return func() tea.Msg {
			return messages.WorkspaceDeleteFailed{
				Workspace: ws,
				Err:       fmt.Errorf("missing workspace"),
			}
		}
	}

	// Clear UI components if deleting the active workspace
	if a.activeWorkspace != nil && a.activeWorkspace.Root() == ws.Root() {
		a.goHome()
	}

	return func() tea.Msg {
		var branchWarning string

		// Only worktrees medusa made are removed. A workspace pointed straight
		// at a repo's own checkout owns neither the directory nor the branch, so
		// deleting it here would delete the user's repo — see removableWorktrees,
		// which is the guard rather than a convenience.
		specs := removableWorktrees(ws)
		if len(specs) > 0 {
			_, branchErrs := git.RemoveGroupWorkspace(specs)
			if len(branchErrs) > 0 {
				msgs := make([]string, len(branchErrs))
				for i, e := range branchErrs {
					msgs[i] = e.Error()
				}
				branchWarning = "Failed to delete branches: " + joinStrings(msgs, "; ")
			}
		}
		// Clean up the workspace root directory, unless the root is a repo.
		if root := ws.Root(); root != "" && ws.UsesWorktree() {
			_ = os.RemoveAll(root)
		}

		_ = a.workspaces.Delete(ws.ID())
		_ = a.registry.RemoveWorkspace(string(ws.ID()))

		return messages.WorkspaceDeleted{
			Workspace:     ws,
			BranchWarning: branchWarning,
			Silent:        isSilent,
		}
	}
}

// removableWorktrees returns the specs for the worktrees of ws that git may
// remove, dropping any whose directory is the source repo itself or is not a
// worktree on disk. Both checks matter: the first catches a workspace created
// with the worktree box unticked, and the second is the backstop for anything
// whose stored path has since come to mean something else. Deleting a branch is
// bound to the same test, since a checkout workspace records the branch the repo
// happened to be on rather than one of its own.
func removableWorktrees(ws *data.Workspace) []git.RepoSpec {
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
