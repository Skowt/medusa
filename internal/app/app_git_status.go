package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/messages"
)

// joinStrings joins strings with a separator.
func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// requestGitStatus requests git status for a workspace (always fetches fresh)
func (a *App) requestGitStatus(root string) tea.Cmd {
	return func() tea.Msg {
		status, err := git.GetStatus(root)
		// Update cache directly (no async refresh needed, we just fetched)
		if a.statusManager != nil && err == nil {
			a.statusManager.UpdateCache(root, status)
		}
		return messages.GitStatusResult{
			Root:   root,
			Status: status,
			Err:    err,
		}
	}
}

// requestGitStatusCached requests git status using cache if available
func (a *App) requestGitStatusCached(root string) tea.Cmd {
	// Check cache first
	if a.statusManager != nil {
		if cached := a.statusManager.GetCached(root); cached != nil {
			return func() tea.Msg {
				return messages.GitStatusResult{
					Root:   root,
					Status: cached,
					Err:    nil,
				}
			}
		}
	}
	// Cache miss, fetch fresh
	return a.requestGitStatus(root)
}

// unwatchAllWorkspaces removes file watches for all known workspaces.
func (a *App) unwatchAllWorkspaces() {
	if a.fileWatcher == nil {
		return
	}
	for _, ws := range a.allWorkspaces {
		a.fileWatcher.Unwatch(ws.PrimaryWorktreeRoot())
	}
}
