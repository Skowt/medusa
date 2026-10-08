package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/workspaceops"
)

// loadWorkspaces loads all workspaces from the store and applies profiles from the registry.
func (a *App) loadWorkspaces() tea.Cmd {
	return func() tea.Msg {
		entries, err := a.registry.ListWorkspaces()
		if err != nil {
			logging.Warn("Failed to load registry: %v", err)
			return messages.WorkspacesLoaded{Err: err}
		}

		claimed := make(map[data.WorkspaceID]bool, len(entries))
		for _, entry := range entries {
			claimed[data.WorkspaceID(entry.ID)] = true
		}
		healer := newRegistryHealer(a.workspaces, claimed)

		var workspaces []*data.Workspace
		for i, entry := range entries {
			ws, err := a.workspaces.Load(data.WorkspaceID(entry.ID))
			// A missing file is the one failure worth second-guessing: the
			// metadata may have been rehomed under a different store ID rather
			// than lost. Any other error means the file is there and unreadable,
			// where adopting a different directory would be a guess, not a fix.
			if os.IsNotExist(err) {
				if healed, healedID := a.healStrandedEntry(healer, entry.Name, entry.ID); healed != nil {
					entries[i].ID = string(healedID)
					entry.ID = string(healedID)
					ws, err = healed, nil
				}
			}
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

// createWorkspace creates a new workspace. With worktree false nothing is
// created on disk at all: the workspace is a label over the source repo's own
// checkout, so no branch is cut and no directory is made.
func (a *App) createWorkspace(name string, repos []data.RepoRef, bases []string, profile, group string, copyIgnored, worktree bool) tea.Cmd {
	return func() tea.Msg {
		if len(repos) == 0 || name == "" {
			return messages.WorkspaceCreateFailed{
				Err: fmt.Errorf("missing repos or workspace name"),
			}
		}

		// Validate workspace name doesn't already exist. The in-memory list
		// also covers placeholders still being created, which the registry
		// check inside workspaceops.Create cannot see.
		if a.workspaceNameExists(name) {
			return messages.WorkspaceCreateFailed{
				Err: fmt.Errorf("workspace '%s' already exists", name),
			}
		}

		ws, err := a.workspaceOps().Create(workspaceops.CreateRequest{
			Name:        name,
			Repos:       repos,
			Bases:       bases,
			Profile:     profile,
			Group:       group,
			CopyIgnored: copyIgnored && worktree,
			Worktree:    worktree,
		})
		if err != nil {
			return messages.WorkspaceCreateFailed{Workspace: ws, Err: err}
		}

		// If gitignored files need copying, signal the overlay to advance first.
		if ws.CopyIgnored {
			return messages.WorkspaceWorktreeDone{Workspace: ws, Repos: repos}
		}
		return messages.WorkspaceCreated{Workspace: ws}
	}
}

// workspaceOps bundles the App's stores for the shared workspace operations.
func (a *App) workspaceOps() workspaceops.Deps {
	return workspaceops.Deps{
		Config:   a.config,
		Registry: a.registry,
		Store:    a.workspaces,
		Recents:  a.recents,
	}
}

// copyIgnoredFilesCmd copies gitignored files from source repos into the workspace worktrees.
func copyIgnoredFilesCmd(ws *data.Workspace, repos []data.RepoRef) tea.Cmd {
	return func() tea.Msg {
		workspaceops.CopyIgnored(ws, repos)
		return messages.WorkspaceCreated{Workspace: ws}
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
	if a.activeWorkspace != nil && a.activeWorkspace.ID() == ws.ID() {
		a.goHome()
	}

	return func() tea.Msg {
		branchWarning := a.workspaceOps().Delete(ws)
		return messages.WorkspaceDeleted{
			Workspace:     ws,
			BranchWarning: branchWarning,
			Silent:        isSilent,
		}
	}
}
