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

	a.hookWorkspaceStates[wsID] = msg.Event

	var cmds []tea.Cmd
	if cmd := a.syncActiveWorkspacesToDashboard(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
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
