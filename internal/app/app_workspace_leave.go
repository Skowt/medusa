package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// leaveWorkspace moves off ws when it is the open workspace and is going away
// (archived, or deleted from here or from outside). It opens the next
// workspace visible on the dashboard, or goes home when none is visible.
//
// The next workspace is opened here rather than by a queued message: the
// reload that follows a removal must never see an empty selection, or it would
// open the first workspace in the list, which may sit in a collapsed group.
// alsoGone names workspaces leaving at the same time, so none is picked.
func (a *App) leaveWorkspace(ws *data.Workspace, alsoGone ...*data.Workspace) []tea.Cmd {
	if ws == nil || a.activeWorkspace == nil || a.activeWorkspace.ID() != ws.ID() {
		return nil
	}

	var next *data.Workspace
	if a.dashboard != nil {
		goneIDs := make([]string, 0, len(alsoGone))
		for _, other := range alsoGone {
			goneIDs = append(goneIDs, string(other.ID()))
		}
		next = a.dashboard.NextVisibleWorkspace(string(ws.ID()), goneIDs...)
	}

	a.goHome()
	if next == nil {
		return nil
	}
	return a.handleWorkspaceActivated(messages.WorkspaceActivated{Workspace: next})
}
