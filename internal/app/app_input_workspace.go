package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/tmux"
)

// handleWorkspaceFetchDone handles the WorkspaceFetchDone message (step 1 of creation).
func (a *App) handleWorkspaceFetchDone(msg messages.WorkspaceFetchDone) []tea.Cmd {
	var cmds []tea.Cmd
	// Advance overlay to step 1 ("Creating worktree")
	if a.creationOverlay != nil {
		a.creationOverlay.AdvanceStep()
	}
	// Show the "creating" indicator in the dashboard
	if msg.Name != "" && len(msg.Repos) > 0 {
		workspacePath := filepath.Join(a.config.Paths.WorkspacesRoot, msg.Name, msg.Repos[0].Name)
		pending := data.NewWorkspace(msg.Name, msg.Name, msg.Bases[0], msg.Repos[0].Path, workspacePath)
		if cmd := a.dashboard.SetWorkspaceCreating(pending, true); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.createWorkspace(msg.Name, msg.Repos, msg.Bases, msg.Profile))
	return cmds
}

// handleRenameWorkspace handles the RenameWorkspace message.
func (a *App) handleRenameWorkspace(msg messages.RenameWorkspace) []tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}

	ws := msg.Workspace
	newName := msg.NewName
	oldBranch := ws.Branch()
	newBranch := newName
	oldRoot := ws.Root()
	oldPrimaryRoot := ws.PrimaryWorktreeRoot()
	newRoot := filepath.Join(filepath.Dir(oldRoot), newName)
	opts := a.tmuxOptions
	oldWsID := string(ws.ID())

	// 1. Validate: branch must not exist in any repo, target dir must not exist.
	for _, repo := range ws.Repos {
		if git.BranchExists(repo.Path, newBranch) {
			return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Branch '%s' already exists in %s", newBranch, repo.Name))}
		}
	}
	if _, err := os.Stat(newRoot); err == nil {
		return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Directory '%s' already exists", filepath.Base(newRoot)))}
	}

	// 2. Rename branches in all repos.
	for i, repo := range ws.Repos {
		if err := git.RenameBranch(repo.Path, oldBranch, newBranch); err != nil {
			for j := 0; j < i; j++ {
				_ = git.RenameBranch(ws.Repos[j].Path, newBranch, oldBranch)
			}
			return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Rename failed in %s: %s", repo.Name, err.Error()))}
		}
	}

	// rollbackBranches undoes all branch renames.
	rollbackBranches := func() {
		for _, repo := range ws.Repos {
			_ = git.RenameBranch(repo.Path, newBranch, oldBranch)
		}
	}

	// 3. Move worktrees (uniform layout: always iterate).
	if err := os.MkdirAll(newRoot, 0o755); err != nil {
		rollbackBranches()
		return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
	}
	for i, wt := range ws.Worktrees {
		newWtPath := filepath.Join(newRoot, ws.Repos[i].Name)
		if err := git.MoveWorkspace(ws.Repos[i].Path, wt.Root, newWtPath); err != nil {
			// Rollback previously moved worktrees
			for j := 0; j < i; j++ {
				_ = git.MoveWorkspace(ws.Repos[j].Path, filepath.Join(newRoot, ws.Repos[j].Name), ws.Worktrees[j].Root)
			}
			_ = os.Remove(newRoot)
			rollbackBranches()
			return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Rename failed moving %s: %s", ws.Repos[i].Name, err.Error()))}
		}
	}
	// Remove old parent directory (should be empty now).
	_ = os.Remove(oldRoot)

	// rollbackMoves undoes all worktree moves.
	rollbackMoves := func() {
		_ = os.MkdirAll(oldRoot, 0o755)
		for i := range ws.Worktrees {
			_ = git.MoveWorkspace(ws.Repos[i].Path, filepath.Join(newRoot, ws.Repos[i].Name), ws.Worktrees[i].Root)
		}
		_ = os.Remove(newRoot)
	}

	// 4. Update store.
	stored, err := a.workspaces.Load(ws.ID())
	if err != nil {
		rollbackMoves()
		rollbackBranches()
		return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
	}
	stored.Name = newName
	for i := range stored.Worktrees {
		stored.Worktrees[i].Branch = newBranch
		stored.Worktrees[i].Root = filepath.Join(newRoot, stored.Repos[i].Name)
	}
	if err := a.workspaces.Save(stored); err != nil {
		rollbackMoves()
		rollbackBranches()
		return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
	}
	newWs := stored

	// 4b. Update registry entry (ID changed because root changed).
	if err := a.registry.UpdateWorkspace(oldWsID, newName, string(newWs.ID())); err != nil {
		logging.Warn("Failed to update registry after rename: %v", err)
	}

	// 5. Migrate running agents in-place (no kill/restart).
	newID := string(newWs.ID())

	// 5a. Rename tmux sessions so they match the new workspace name.
	tabsInfo, _ := a.center.GetTabsInfoForWorkspace(oldWsID)
	oldPrefix := tmux.SessionName("medusa", ws.Name) + "-"
	newPrefix := tmux.SessionName("medusa", newName) + "-"
	for _, t := range tabsInfo {
		if strings.HasPrefix(t.SessionName, oldPrefix) {
			newSessionName := newPrefix + strings.TrimPrefix(t.SessionName, oldPrefix)
			if err := tmux.RenameSession(t.SessionName, newSessionName, opts); err != nil {
				logging.Warn("Failed to rename tmux session %s: %v", t.SessionName, err)
			}
		}
	}

	// 5b. Migrate center tabs (re-keys under new workspace ID, sets up PTY reader redirects).
	a.center.MigrateWorkspaceTabs(oldWsID, newID, newWs, ws.Name, newName)

	// 5c. Migrate agent manager state.
	a.center.AgentManager().MigrateWorkspaceAgents(data.WorkspaceID(oldWsID), data.WorkspaceID(newID), newWs, ws.Name, newName)

	// 5d. Migrate sidebar terminal tabs.
	a.sidebarTerminal.MigrateWorkspaceTabs(oldWsID, newID, newWs)

	// 6. Update in-memory UI state.
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == oldWsID {
		newWs.Profile = a.activeWorkspace.Profile
		a.activeWorkspace = newWs
		a.center.SetWorkspace(newWs)
	}
	for _, ws := range a.allWorkspaces {
		if string(ws.ID()) == oldWsID {
			ws.Name = newWs.Name
			for i := range ws.Worktrees {
				ws.Worktrees[i].Root = filepath.Join(newRoot, ws.Repos[i].Name)
				ws.Worktrees[i].Branch = newBranch
			}
		}
	}
	if a.dirtyWorkspaces[oldWsID] {
		delete(a.dirtyWorkspaces, oldWsID)
		a.dirtyWorkspaces[newID] = true
	}
	if a.fileWatcher != nil {
		a.fileWatcher.Unwatch(oldPrimaryRoot)
		_ = a.fileWatcher.Watch(newWs.PrimaryWorktreeRoot())
	}
	if a.permissionWatcher != nil {
		a.permissionWatcher.Unwatch(oldRoot)
		_ = a.permissionWatcher.Watch(newWs.Root())
	}
	if a.statusManager != nil {
		a.statusManager.Invalidate(oldPrimaryRoot)
	}
	if a.dashboard != nil {
		a.dashboard.InvalidateStatus(oldPrimaryRoot)
	}

	// 7. Persist tab state, toast, and reload.
	var cmds []tea.Cmd
	if cmd := a.persistWorkspaceTabs(newID); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds,
		a.toast.ShowSuccess(fmt.Sprintf("Renamed to '%s'. Restart agents to use new directory.", newWs.Name)),
		a.loadWorkspaces(),
	)
	return cmds
}

// handleWorkspaceRenameFailed handles a failed workspace rename.
func (a *App) handleWorkspaceRenameFailed(msg messages.WorkspaceRenameFailed) tea.Cmd {
	logging.Error("Failed to rename workspace %s: %v", msg.Workspace.Name, msg.Err)
	return a.toast.ShowError("Rename failed: " + msg.Err.Error())
}

// handleDeleteWorkspace handles the DeleteWorkspace message.
func (a *App) handleDeleteWorkspace(msg messages.DeleteWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace == nil {
		logging.Warn("DeleteWorkspace received with nil workspace")
		return nil
	}
	// Clean up tabs first so that killing tmux sessions doesn't trigger
	// auto-reattach logic in the now-removed PTY readers.
	a.center.CleanupWorkspace(msg.Workspace)
	if cleanup := a.cleanupWorkspaceTmuxSessions(msg.Workspace); cleanup != nil {
		cmds = append(cmds, cleanup)
	}
	if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root(), true); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, a.deleteWorkspace(msg.Workspace))
	return cmds
}

// handleWorkspaceCreatedWithWarning handles the WorkspaceCreatedWithWarning message.
func (a *App) handleWorkspaceCreatedWithWarning(msg messages.WorkspaceCreatedWithWarning) []tea.Cmd {
	var cmds []tea.Cmd
	a.err = fmt.Errorf("workspace created with warning: %s", msg.Warning)
	if msg.Workspace != nil {
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.loadWorkspaces())
	return cmds
}

// handleWorkspaceCreated handles the WorkspaceCreated message.
func (a *App) handleWorkspaceCreated(msg messages.WorkspaceCreated) []tea.Cmd {
	a.creationOverlay = nil
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, a.runSetupAsync(msg.Workspace))
		// Mark for auto-launch after workspaces reload
		if a.config.UI.AutoStartAgent {
			a.pendingAutoLaunch = msg.Workspace.Root()
		}
	}
	cmds = append(cmds, a.loadWorkspaces())
	return cmds
}

// handleWorkspaceSetupComplete handles the WorkspaceSetupComplete message.
func (a *App) handleWorkspaceSetupComplete(msg messages.WorkspaceSetupComplete) tea.Cmd {
	if msg.Err != nil {
		return a.toast.ShowWarning(fmt.Sprintf("Setup failed for %s: %v", msg.Workspace.Name, msg.Err))
	}
	return nil
}

// handleWorkspaceCreateFailed handles the WorkspaceCreateFailed message.
func (a *App) handleWorkspaceCreateFailed(msg messages.WorkspaceCreateFailed) tea.Cmd {
	a.creationOverlay = nil
	if msg.Workspace != nil {
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			return cmd
		}
	}
	a.err = msg.Err
	logging.Error("Error in creating workspace: %v", msg.Err)
	return nil
}

// handleWorkspaceDeleted handles the WorkspaceDeleted message.
func (a *App) handleWorkspaceDeleted(msg messages.WorkspaceDeleted) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root(), false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if a.statusManager != nil {
			a.statusManager.Invalidate(msg.Workspace.Root())
		}
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		newTerminal, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerminal
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		// If the deleted workspace was active, clear it so the next
		// loadWorkspaces cycle auto-activates the nearest workspace.
		if a.activeWorkspace != nil && a.activeWorkspace.Root() == msg.Workspace.Root() {
			a.goHome()
		}
	}
	if msg.BranchWarning != "" {
		cmds = append(cmds, a.toast.ShowWarning(msg.BranchWarning))
	}
	cmds = append(cmds, a.loadWorkspaces())
	return cmds
}

// handleWorkspaceDeleteFailed handles the WorkspaceDeleteFailed message.
func (a *App) handleWorkspaceDeleteFailed(msg messages.WorkspaceDeleteFailed) tea.Cmd {
	if msg.Workspace != nil {
		if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root(), false); cmd != nil {
			return cmd
		}
	}
	a.err = msg.Err
	logging.Error("Error in removing workspace: %v", msg.Err)
	return nil
}

// handleSetWorkspaceStatus handles the SetWorkspaceStatus message.
func (a *App) handleSetWorkspaceStatus(msg messages.SetWorkspaceStatus) tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}
	msg.Workspace.Status = msg.Status
	msg.Workspace.StatusChanged = time.Now()
	if err := a.workspaces.Save(msg.Workspace); err != nil {
		logging.Error("Failed to save workspace: %v", err)
		return a.toast.ShowError("Failed to save setting")
	}
	if a.dashboard != nil {
		a.dashboard.SetWorkspaces(a.allWorkspaces)
	}
	return nil
}

// saveAndRefreshWorkspace saves a workspace and refreshes the dashboard in-place.
// If toastMsg is non-empty, a success toast is shown.
func (a *App) saveAndRefreshWorkspace(ws *data.Workspace, toastMsg string) tea.Cmd {
	if err := a.workspaces.Save(ws); err != nil {
		logging.Error("Failed to save workspace: %v", err)
		return a.toast.ShowError("Failed to save setting")
	}
	if a.dashboard != nil {
		a.dashboard.SetWorkspaces(a.allWorkspaces)
	}
	if toastMsg != "" {
		return a.toast.ShowSuccess(toastMsg)
	}
	return nil
}

// handleAddReposToWorkspace adds new repos to an existing workspace by creating worktrees.
func (a *App) handleAddReposToWorkspace(msg messages.AddReposToWorkspace) tea.Cmd {
	ws := msg.Workspace
	newRepos := msg.Repos
	if ws == nil || len(newRepos) == 0 {
		return nil
	}

	return func() tea.Msg {
		branch := ws.Branch()
		oldID := string(ws.ID())

		for _, repo := range newRepos {
			// Check if branch already exists in the new repo
			if git.BranchExists(repo.Path, branch) {
				return messages.ReposAddFailed{
					Err: fmt.Errorf("branch '%s' already exists in %s", branch, repo.Name),
				}
			}
		}

		// Determine workspace root for new worktrees (uniform layout: always parent)
		wsRoot := ws.Root()

		// Fetch default base and create worktree for each new repo
		for _, repo := range newRepos {
			base, err := git.GetDefaultBase(repo.Path)
			if err != nil {
				return messages.ReposAddFailed{
					Err: fmt.Errorf("failed to get default base for %s: %w", repo.Name, err),
				}
			}

			wtPath := filepath.Join(wsRoot, repo.Name)
			if err := git.CreateWorkspace(repo.Path, wtPath, branch, base); err != nil {
				return messages.ReposAddFailed{
					Err: fmt.Errorf("failed to create worktree for %s: %w", repo.Name, err),
				}
			}

			ws.Repos = append(ws.Repos, repo)
			ws.Worktrees = append(ws.Worktrees, data.WorktreeRef{
				Branch: branch,
				Base:   base,
				Root:   wtPath,
			})
		}

		// Save the updated workspace
		if err := a.workspaces.Save(ws); err != nil {
			return messages.ReposAddFailed{
				Err: fmt.Errorf("failed to save workspace: %w", err),
			}
		}

		// Update registry if workspace ID changed (e.g. single-repo → multi-repo)
		newID := string(ws.ID())
		if oldID != newID {
			if err := a.registry.UpdateWorkspace(oldID, ws.Name, newID); err != nil {
				logging.Warn("Failed to update registry after adding repos: %v", err)
			}
		}

		return messages.ReposAddedToWorkspace{Workspace: ws}
	}
}
