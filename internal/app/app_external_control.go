package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
)

// Workspaces can change underneath the TUI: `medusa workspace` creates and
// deletes them from the shell and tells the TUI over the hooks socket. A
// creation arrives as messages.RefreshDashboard, which reloads and adopts the
// new workspace's agent session. A deletion needs one step more, below.

// workspaceClosingMsg reports that a workspace is about to be deleted from
// outside the TUI.
type workspaceClosingMsg struct {
	WorkspaceID string
}

// handleWorkspaceClosing drops a workspace's tabs before anything kills its
// tmux sessions. It is the first half of the TUI's own delete
// (handleDeleteWorkspace) and exists for the same reason: a tab whose session
// dies is auto-reattached or auto-restarted, and the restart's
// `new-session -A` would start a fresh agent in a worktree being deleted. The
// CLI waits for the tabs' clients to detach before it kills anything.
func (a *App) handleWorkspaceClosing(msg workspaceClosingMsg) tea.Cmd {
	ws := a.findWorkspaceByID(msg.WorkspaceID)
	if ws == nil {
		return nil
	}
	logging.Info("Workspace %s is being deleted from outside; dropping its tabs", ws.Name)
	if a.center != nil {
		a.center.SetWorkspaceClosing(msg.WorkspaceID, true)
	}
	a.dropWorkspaceTabs(ws)
	if a.externalClosing == nil {
		a.externalClosing = make(map[string]time.Time)
	}
	a.externalClosing[msg.WorkspaceID] = time.Now()
	if a.dashboard == nil {
		return nil
	}
	return a.dashboard.SetWorkspaceDeleting(msg.WorkspaceID, true)
}

// dropWorkspaceTabs closes a workspace's tabs without touching its sessions,
// leaves it if it is active, and stops it being persisted again.
func (a *App) dropWorkspaceTabs(ws *data.Workspace) {
	id := string(ws.ID())
	if a.center != nil {
		a.center.CleanupWorkspace(ws)
	}
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == id {
		a.goHome()
	}
	delete(a.dirtyWorkspaces, id)
}

// dropVanishedWorkspaces is the backstop for a delete the TUI was not warned
// about: any live workspace in the current list that the reload no longer
// has loses its tabs. A workspace whose name is still present is left alone,
// since an ID migration looks the same as a delete by ID alone.
func (a *App) dropVanishedWorkspaces(loaded []*data.Workspace) {
	ids := make(map[data.WorkspaceID]bool, len(loaded))
	names := make(map[string]bool, len(loaded))
	for _, ws := range loaded {
		ids[ws.ID()] = true
		names[ws.Name] = true
	}
	for _, ws := range a.allWorkspaces {
		if ws == nil || ws.IsOrphaned() || ids[ws.ID()] || names[ws.Name] {
			continue
		}
		logging.Info("Workspace %s disappeared on reload; dropping its tabs", ws.Name)
		a.dropWorkspaceTabs(ws)
	}
}

// externalClosingGrace is how long a workspace an outside delete announced
// stays closed while reloads still list it. A delete removes the registry
// entry within moments of announcing itself, so a workspace still listed
// after this is one whose delete never finished, and it gets its tabs back.
const externalClosingGrace = 30 * time.Second

// clearExternalClosing lifts the closing mark from workspaces an outside
// delete announced, once a reload no longer lists them. A reload that does
// still list one may have read the registry before the delete removed it, so
// the mark stays — lifting it there would let the eager restore reattach.
func (a *App) clearExternalClosing(loaded []*data.Workspace) []tea.Cmd {
	listed := make(map[string]bool, len(loaded))
	for _, ws := range loaded {
		listed[string(ws.ID())] = true
	}
	var cmds []tea.Cmd
	for id, since := range a.externalClosing {
		if listed[id] && time.Since(since) < externalClosingGrace {
			continue
		}
		if a.center != nil {
			a.center.SetWorkspaceClosing(id, false)
		}
		if a.dashboard != nil {
			if cmd := a.dashboard.SetWorkspaceDeleting(id, false); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		delete(a.externalClosing, id)
	}
	return cmds
}

// persistSnapshotAllowed reports whether a workspace may be written back to the
// store. A workspace the registry no longer lists was deleted — possibly from
// outside, with a persist already in flight — and saving it would recreate its
// metadata with nothing pointing at it. An unreadable registry allows the
// save: losing tab state to a read error is the worse outcome.
func persistSnapshotAllowed(registry *data.Registry, id data.WorkspaceID) bool {
	if registry == nil {
		return true
	}
	ok, err := registry.Has(string(id))
	if err != nil {
		return true
	}
	return ok
}
