package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/hooks"
	"github.com/Skowt/medusa/internal/logging"
)

// staleBusyTimeout is how long a busy hook state may go without any new hook
// event before the reconciler degrades it to ready. Hook delivery is lossy
// (nc/binary can fail, Claude Code drops events on interrupt); the 60s
// idle_prompt notification is the primary rescue, this is the backstop when
// that is lost too. Long enough that a single long-running tool call (which
// emits no hook events while executing) doesn't get cleared while the PTY is
// quiet between output bursts.
const staleBusyTimeout = 3 * time.Minute

// staleBusyWorkspaces clears busy hook states that have seen no event for
// staleBusyTimeout and show no PTY output, returning the workspace IDs it
// cleared. Needs-input states ('!') are never reconciled away: the dialog
// stays on screen until the user answers, however long that takes. Restored
// states with no stamp (fresh app start) get a full grace window first.
//
// The degrade is silent by design — a staleness guess must fix the spinner,
// never fire a sound.
func (a *App) staleBusyWorkspaces() []string {
	if len(a.hookTabStates) > 0 {
		return a.staleBusyTabs()
	}
	var cleared []string
	for wsID, evt := range a.hookWorkspaceStates {
		if !hooks.IsActiveEvent(evt) {
			continue
		}
		stamp, ok := a.hookLastStamp[wsID]
		if !ok {
			a.hookLastStamp[wsID] = hookEventStamp{at: time.Now()}
			continue
		}
		if time.Since(stamp.at) < staleBusyTimeout {
			continue
		}
		if a.center != nil && a.center.HasActiveAgentsInWorkspace(wsID) {
			continue
		}
		delete(a.hookWorkspaceStates, wsID)
		// Drop the background-work knowledge with the state: a dead session
		// must not leave a stale count that swallows a future idle rescue.
		delete(a.hookOutstanding, wsID)
		cleared = append(cleared, wsID)
	}
	return cleared
}

func (a *App) staleBusyTabs() []string {
	infoBySession := a.tabSessionInfoByName()
	affected := make(map[string]bool)
	now := time.Now()
	for sessionName, evt := range a.hookTabStates {
		if !hooks.IsActiveEvent(evt) {
			continue
		}
		stamp, ok := a.hookTabLastStamp[sessionName]
		if !ok {
			a.hookTabLastStamp[sessionName] = hookEventStamp{at: now}
			continue
		}
		if now.Sub(stamp.at) < staleBusyTimeout {
			continue
		}
		if a.center != nil && a.center.HasRecentPTYOutput(sessionName, staleBusyTimeout) {
			continue
		}
		info, ok := infoBySession[sessionName]
		if ok {
			affected[info.WorkspaceID] = true
			if a.center != nil {
				a.center.SetTabHookState(info.WorkspaceID, sessionName, "", false)
			}
		}
		delete(a.hookTabStates, sessionName)
		delete(a.hookTabOutstanding, sessionName)
		delete(a.hookTabLastStamp, sessionName)
	}
	cleared := make([]string, 0, len(affected))
	for wsID := range affected {
		a.recomputeWorkspaceHookState(wsID, infoBySession)
		cleared = append(cleared, wsID)
	}
	return cleared
}

// reconcileStaleHookStates runs staleBusyWorkspaces and persists/syncs any
// change. Called from the PTY watchdog tick.
func (a *App) reconcileStaleHookStates() []tea.Cmd {
	cleared := a.staleBusyWorkspaces()
	if len(cleared) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, wsID := range cleared {
		logging.Warn("Hook activity for workspace %s went stale (no event for %s); clearing busy state", wsID, staleBusyTimeout)
		if cmd := a.persistHookState(wsID); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := a.syncActiveWorkspacesToDashboard(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}
