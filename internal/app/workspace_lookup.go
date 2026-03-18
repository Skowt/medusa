package app

import (
	"strings"

	"github.com/Skowt/medusa/internal/data"
)

// workspaceNameExists returns true if any workspace in allWorkspaces has the
// given name (case-insensitive). An optional excludeID allows the rename flow
// to skip the workspace being renamed.
func (a *App) workspaceNameExists(name string, excludeID ...data.WorkspaceID) bool {
	for _, ws := range a.allWorkspaces {
		if len(excludeID) > 0 && ws.ID() == excludeID[0] {
			continue
		}
		if strings.EqualFold(ws.Name, name) {
			return true
		}
	}
	return false
}

func (a *App) findWorkspaceByID(id string) *data.Workspace {
	if id == "" {
		return nil
	}
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == id {
		return a.activeWorkspace
	}
	for _, ws := range a.allWorkspaces {
		if string(ws.ID()) == id {
			return ws
		}
	}
	return nil
}
