package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/perf"
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

// requestGitStatus requests git status for a workspace (always fetches fresh).
//
// Coalesces concurrent requests for the same root: if a fetch is already in
// flight, additional calls return nil and the in-flight fetch's result will
// update the UI once it lands. The flag is cleared inside the fetch goroutine
// (under mutex) so cache-hit results flowing through requestGitStatusCached
// don't interfere.
func (a *App) requestGitStatus(root string) tea.Cmd {
	if root == "" {
		return nil
	}
	a.gitStatusInFlightMu.Lock()
	if a.gitStatusInFlight == nil {
		a.gitStatusInFlight = make(map[string]bool)
	}
	if a.gitStatusInFlight[root] {
		a.gitStatusInFlightMu.Unlock()
		perf.Count("git_status_skip_inflight", 1)
		return nil
	}
	a.gitStatusInFlight[root] = true
	a.gitStatusInFlightMu.Unlock()

	return func() tea.Msg {
		// Clear the in-flight flag even if GetStatus panics, so the workspace
		// isn't permanently marked busy.
		defer func() {
			a.gitStatusInFlightMu.Lock()
			delete(a.gitStatusInFlight, root)
			a.gitStatusInFlightMu.Unlock()
		}()
		done := perf.Time("git_status")
		status, err := git.GetStatus(root)
		done()
		perf.Count("git_status_fetch", 1)
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
