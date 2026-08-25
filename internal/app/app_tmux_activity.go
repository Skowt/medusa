package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/tmux"
)

type tabSessionInfo struct {
	Status      string
	WorkspaceID string
	Assistant   string
	IsChat      bool
	// AutoReviewer reports that the tab's approval requests are resolved by
	// an automatic reviewer rather than by the user. Codex's --approve-for-me
	// is that reviewer; Claude Code has no equivalent, since it fires
	// PermissionRequest only when it is about to prompt a human.
	AutoReviewer bool
}

// tabAutoReviewer reports whether a tab's approval requests are answered by an
// automatic reviewer instead of by the user, which is what decides whether a
// PermissionRequest hook means a human is waiting (see applyHookTransition).
//
// Codex's --approve-for-me is that reviewer, and Codex runs its PermissionRequest
// hooks before it picks one, so the flag is the only thing separating "the agent
// is blocked on you" from "its reviewer is thinking". Claude Code needs no
// equivalent: it fires PermissionRequest only when it is about to prompt a human.
func tabAutoReviewer(tab data.TabInfo) bool {
	return strings.TrimSpace(tab.Assistant) == assistantCodex && tab.CodexAuto
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
				Status:       status,
				WorkspaceID:  string(ws.ID()),
				Assistant:    assistant,
				IsChat:       isChat,
				AutoReviewer: tabAutoReviewer(tab),
			}
		}
	}
	// The center model is authoritative for newly-created tabs. Persistence is
	// debounced, so ws.OpenTabs can lag behind the first lifecycle hook.
	if a.center != nil {
		for _, ws := range a.allWorkspaces {
			wsID := string(ws.ID())
			tabs, _ := a.center.GetTabsInfoForWorkspace(wsID)
			for _, tab := range tabs {
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
					Status: status, WorkspaceID: wsID, Assistant: assistant, IsChat: isChat,
					AutoReviewer: tabAutoReviewer(tab),
				}
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
