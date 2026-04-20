package app

import (
	"fmt"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
)

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

	// 4. Stop watching git state — archived workspaces can't change on disk.
	if a.fileWatcher != nil {
		a.fileWatcher.Unwatch(ws.PrimaryWorktreeRoot())
	}

	// 5. If active, go home
	if a.activeWorkspace != nil && a.activeWorkspace.Root() == ws.Root() {
		a.goHome()
	}

	// 6. Prune excess archived workspaces
	cmds = append(cmds, a.pruneArchivedWorkspaces()...)

	// 7. Reload + toast
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
		cmds = append(cmds, a.deleteWorkspace(ws, true))
	}
	return cmds
}
