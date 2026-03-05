package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/hooks"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/supervisor"
)

// hookActivityEvent is a Bubble Tea message carrying a parsed hook event.
type hookActivityEvent struct {
	SessionName string
	Event       hooks.EventType
	Timestamp   time.Time
}

// initHooksWatcher creates and registers the hooks directory watcher.
func (a *App) initHooksWatcher() {
	hooksDir := a.config.Paths.HooksDir
	if hooksDir == "" {
		return
	}

	// Clean up stale files from previous sessions
	hooks.CleanStaleFiles(hooksDir, 24*time.Hour)

	w, err := hooks.NewWatcher(hooksDir, func(he hooks.HookEvent) {
		a.enqueueExternalMsg(hookActivityEvent{
			SessionName: he.SessionName,
			Event:       he.Event,
			Timestamp:   he.Timestamp,
		})
	})
	if err != nil {
		logging.Warn("Hooks watcher disabled: %v", err)
		return
	}
	a.hooksWatcher = w
	a.supervisor.Start("hooks.watcher", w.Run, supervisor.WithBackoff(500*time.Millisecond))
}

// handleHookActivityEvent processes a hook activity event, resolves the session
// name to a workspace ID via tabSessionInfoByName, and updates hookWorkspaceStates.
func (a *App) handleHookActivityEvent(msg hookActivityEvent) []tea.Cmd {
	// Resolve session name → workspace ID
	wsID := ""
	if info, ok := a.tabSessionInfoByName()[msg.SessionName]; ok {
		wsID = info.WorkspaceID
	}
	if wsID == "" {
		return nil
	}

	// NotificationIdle and Stop are "clear" signals: the agent finished
	// responding and went idle, so any prior notification (permission/question)
	// has been resolved. Delete the state so the '!' indicator disappears.
	switch msg.Event {
	case hooks.EventStop, hooks.EventNotificationIdle:
		delete(a.hookWorkspaceStates, wsID)
	default:
		a.hookWorkspaceStates[wsID] = msg.Event
	}

	var cmds []tea.Cmd
	// Persist the hook state change to the workspace JSON.
	if cmd := a.persistHookState(wsID); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := a.syncActiveWorkspacesToDashboard(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// persistHookState updates the workspace's HookState field and triggers a debounced save.
func (a *App) persistHookState(wsID string) tea.Cmd {
	ws := a.findWorkspaceByID(wsID)
	if ws == nil {
		return nil
	}
	if evt, ok := a.hookWorkspaceStates[wsID]; ok {
		ws.ActivityState = string(evt)
	} else {
		ws.ActivityState = ""
	}
	return a.persistWorkspaceTabs(wsID)
}

// restoreHookStatesFromWorkspaces populates hookWorkspaceStates from persisted
// workspace HookState fields so that indicators (like '!') survive app restarts.
func (a *App) restoreHookStatesFromWorkspaces() {
	for _, ws := range a.allWorkspaces {
		if ws.ActivityState == "" {
			continue
		}
		a.hookWorkspaceStates[string(ws.ID())] = hooks.EventType(ws.ActivityState)
	}
}

// hookActiveIDs returns the set of workspace IDs that are currently active
// based on hook state (PreToolUse or UserPromptSubmit = agent actively working).
func (a *App) hookActiveIDs() map[string]bool {
	active := make(map[string]bool)
	for wsID, evt := range a.hookWorkspaceStates {
		if evt == hooks.EventPreToolUse || evt == hooks.EventUserPromptSubmit {
			active[wsID] = true
		}
	}
	return active
}
