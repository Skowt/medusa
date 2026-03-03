package app

import "github.com/andyrewlee/medusa/internal/data"

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
