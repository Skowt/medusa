package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		var pending *data.Workspace
		if len(msg.Repos) == 1 {
			workspacePath := filepath.Join(a.config.Paths.WorkspacesRoot, msg.Name)
			pending = data.NewWorkspace(msg.Name, msg.Name, msg.Bases[0], msg.Repos[0].Path, workspacePath)
		} else {
			worktrees := make([]data.WorktreeRef, len(msg.Repos))
			for i, repo := range msg.Repos {
				worktrees[i] = data.WorktreeRef{
					Branch: msg.Name,
					Base:   msg.Bases[i],
					Root:   filepath.Join(a.config.Paths.WorkspacesRoot, msg.Name, repo.Name),
				}
			}
			pending = data.NewMultiRepoWorkspace(msg.Name, msg.Repos, worktrees)
		}
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
	newBranch := newName
	oldRoot := ws.Root()
	oldPrimaryRoot := ws.PrimaryWorktreeRoot()
	newRoot := filepath.Join(filepath.Dir(oldRoot), newName)
	opts := a.tmuxOptions
	oldWsID := string(ws.ID())

	// 1. Validate: workspace name and branch must not already exist.
	if a.workspaceNameExists(newName, ws.ID()) {
		return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Workspace '%s' already exists", newName))}
	}
	for _, repo := range ws.Repos {
		if git.BranchExists(repo.Path, newBranch) {
			return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Branch '%s' already exists in %s", newBranch, repo.Name))}
		}
	}
	if _, err := os.Stat(newRoot); err == nil {
		return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Directory '%s' already exists", filepath.Base(newRoot)))}
	}

	// Resolve the actual branch in each worktree so we rename the right thing,
	// even when the stored branch name has drifted from reality.
	actualBranches := make([]string, len(ws.Worktrees))
	for i, wt := range ws.Worktrees {
		branch, err := git.GetCurrentBranch(wt.Root)
		if err != nil {
			return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Rename failed: cannot determine branch in %s: %s", ws.Repos[i].Name, err.Error()))}
		}
		actualBranches[i] = strings.TrimSpace(branch)
	}

	// 2. Rename branches in all repos.
	for i, repo := range ws.Repos {
		if err := git.RenameBranch(repo.Path, actualBranches[i], newBranch); err != nil {
			for j := 0; j < i; j++ {
				_ = git.RenameBranch(ws.Repos[j].Path, newBranch, actualBranches[j])
			}
			return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Rename failed in %s: %s", repo.Name, err.Error()))}
		}
	}

	// rollbackBranches undoes all branch renames.
	rollbackBranches := func() {
		for i, repo := range ws.Repos {
			_ = git.RenameBranch(repo.Path, newBranch, actualBranches[i])
		}
	}

	// 3. Move worktrees.
	if ws.IsMultiRepo() {
		if err := os.MkdirAll(newRoot, 0o755); err != nil {
			rollbackBranches()
			return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
		}
		for i, wt := range ws.Worktrees {
			newWtPath := filepath.Join(newRoot, ws.Repos[i].Name)
			if err := git.MoveWorkspace(ws.Repos[i].Path, wt.Root, newWtPath); err != nil {
				for j := 0; j < i; j++ {
					_ = git.MoveWorkspace(ws.Repos[j].Path, filepath.Join(newRoot, ws.Repos[j].Name), ws.Worktrees[j].Root)
				}
				_ = os.Remove(newRoot)
				rollbackBranches()
				return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Rename failed moving %s: %s", ws.Repos[i].Name, err.Error()))}
			}
		}
		_ = os.Remove(oldRoot)
	} else {
		if err := git.MoveWorkspace(ws.Repos[0].Path, oldRoot, newRoot); err != nil {
			rollbackBranches()
			return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
		}
	}

	// rollbackMoves undoes all worktree moves.
	rollbackMoves := func() {
		if ws.IsMultiRepo() {
			_ = os.MkdirAll(oldRoot, 0o755)
			for i := range ws.Worktrees {
				_ = git.MoveWorkspace(ws.Repos[i].Path, filepath.Join(newRoot, ws.Repos[i].Name), ws.Worktrees[i].Root)
			}
			_ = os.Remove(newRoot)
		} else {
			_ = git.MoveWorkspace(ws.Repos[0].Path, newRoot, oldRoot)
		}
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
		if ws.IsMultiRepo() {
			stored.Worktrees[i].Root = filepath.Join(newRoot, stored.Repos[i].Name)
		} else {
			stored.Worktrees[i].Root = newRoot
		}
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
				if ws.IsMultiRepo() {
					ws.Worktrees[i].Root = filepath.Join(newRoot, ws.Repos[i].Name)
				} else {
					ws.Worktrees[i].Root = newRoot
				}
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

// handleArchiveWorkspace handles the ArchiveWorkspace message.
func (a *App) handleArchiveWorkspace(msg messages.ArchiveWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	ws := msg.Workspace
	if ws == nil {
		return nil
	}
	wsID := string(ws.ID())

	// 1. Snapshot tab state synchronously (before cleanup removes the tabs).
	//    This preserves ClaudeSessionIDs so that unarchiving can resume sessions.
	tabs, activeIdx := a.center.GetTabsInfoForWorkspace(wsID)
	if len(tabs) > 0 {
		ws.OpenTabs = tabs
		ws.ActiveTabIndex = activeIdx
	}

	// 2. Stop PTY readers, then kill tmux sessions.
	a.center.CleanupWorkspace(ws)
	if cleanup := a.cleanupWorkspaceTmuxSessions(ws); cleanup != nil {
		cmds = append(cmds, cleanup)
	}

	// 3. Update workspace status and save (including the snapshotted tabs).
	ws.Status = data.StatusArchived
	ws.StatusChanged = time.Now()
	ws.ArchivedAt = time.Now()
	if err := a.workspaces.Save(ws); err != nil {
		logging.Error("Failed to archive workspace: %v", err)
		cmds = append(cmds, a.toast.ShowError("Failed to archive workspace"))
		return cmds
	}

	// 4. If active, go home
	if a.activeWorkspace != nil && a.activeWorkspace.Root() == ws.Root() {
		a.goHome()
	}

	// 5. Prune excess archived workspaces
	cmds = append(cmds, a.pruneArchivedWorkspaces()...)

	// 6. Reload + toast
	cmds = append(cmds, a.loadWorkspaces())
	cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Archived '%s'", ws.Name)))
	return cmds
}

// handleUnarchiveWorkspace handles the UnarchiveWorkspace message.
func (a *App) handleUnarchiveWorkspace(msg messages.UnarchiveWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	ws := msg.Workspace
	if ws == nil {
		return nil
	}

	// 1. Update workspace status
	ws.Status = data.StatusStarted
	ws.StatusChanged = time.Now()
	ws.ArchivedAt = time.Time{}
	if err := a.workspaces.Save(ws); err != nil {
		logging.Error("Failed to unarchive workspace: %v", err)
		cmds = append(cmds, a.toast.ShowError("Failed to unarchive workspace"))
		return cmds
	}

	// 2. Reload workspaces
	cmds = append(cmds, a.loadWorkspaces())

	// 3. Activate workspace to trigger tab restore + tmux session restart
	w := ws
	cmds = append(cmds, func() tea.Msg {
		return messages.WorkspaceActivated{Workspace: w}
	})

	// 4. Toast
	cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Unarchived '%s'", ws.Name)))
	return cmds
}

// pruneArchivedWorkspaces enforces a maximum of 5 archived workspaces.
func (a *App) pruneArchivedWorkspaces() []tea.Cmd {
	var archived []*data.Workspace
	for _, ws := range a.allWorkspaces {
		if ws.Archived() {
			archived = append(archived, ws)
		}
	}
	if len(archived) <= 5 {
		return nil
	}

	// Sort by ArchivedAt ascending (oldest first)
	sort.Slice(archived, func(i, j int) bool {
		return archived[i].ArchivedAt.Before(archived[j].ArchivedAt)
	})

	var cmds []tea.Cmd
	excess := len(archived) - 5
	for i := 0; i < excess; i++ {
		ws := archived[i]
		a.center.CleanupWorkspace(ws)
		cmds = append(cmds, a.deleteWorkspace(ws))
	}
	return cmds
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
	if !ws.IsMultiRepo() {
		return func() tea.Msg {
			return messages.ReposAddFailed{Err: fmt.Errorf("cannot add repos to a single-repo workspace")}
		}
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
