package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/tmux"
)

type tabSessionInfo struct {
	Status      string
	WorkspaceID string
	Assistant   string
	IsChat      bool
}

// tabSessionInfoByName builds a map from tmux session name to workspace info.
// Concurrency safety: builds the map synchronously in the Update loop.
// Goroutine closures capture only the returned map, never accessing
// a.allWorkspaces or ws.OpenTabs directly.
func (a *App) tabSessionInfoByName() map[string]tabSessionInfo {
	infoBySession := make(map[string]tabSessionInfo)
	assistants := map[string]struct{}{}
	if a.config != nil {
		for name := range a.config.Assistants {
			assistants[name] = struct{}{}
		}
	}
	for _, ws := range a.allWorkspaces {
		for _, tab := range ws.OpenTabs {
			name := strings.TrimSpace(tab.SessionName)
			if name == "" {
				continue
			}
			status := strings.ToLower(strings.TrimSpace(tab.Status))
			if status == "" {
				status = "running"
			}
			assistant := strings.TrimSpace(tab.Assistant)
			_, isChat := assistants[assistant]
			infoBySession[name] = tabSessionInfo{
				Status:      status,
				WorkspaceID: string(ws.ID()),
				Assistant:   assistant,
				IsChat:      isChat,
			}
		}
	}
	return infoBySession
}

func (a *App) handleTmuxAvailableResult(msg tmuxAvailableResult) []tea.Cmd {
	a.tmuxCheckDone = true
	a.tmuxAvailable = msg.available
	a.tmuxInstallHint = msg.installHint
	if !msg.available {
		return []tea.Cmd{a.toast.ShowError("tmux not installed. " + msg.installHint)}
	}
	_ = tmux.SetMonitorActivityOn(a.tmuxOptions)
	_ = tmux.SetStatusOff(a.tmuxOptions)
	return nil
}
