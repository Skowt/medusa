package center

import (
	"github.com/Skowt/medusa/internal/logging"
	appPty "github.com/Skowt/medusa/internal/pty"
)

// Terminal focus, reported to every agent.
//
// Claude Code enables focus reporting (DEC mode 1004) and acts on it: it
// generates the "while you were away" session recap only once the terminal has
// been unfocused for a while, and it skips PushNotification's desktop and
// mobile alerts while the terminal is focused. tmux forwards focus to a pane
// from its client's reports, and a center tab's client is the tab's own PTY --
// which never sent one. tmux treats a client that never reports as focused, so
// every agent in every tab, hidden or not, believed it was being watched: no
// recaps, and no push notifications at all.
//
// An agent is focused when medusa's terminal has focus and the agent's tab is
// the one on screen. Monitor mode shows many tabs at once and the selection
// there indexes a list the app filters, so every agent counts as unfocused
// while it is up.

// FocusIn and FocusOut are the xterm focus reports, which tmux reads from a
// client that it enabled mode 1004 on.
const (
	focusInReport  = "\x1b[I"
	focusOutReport = "\x1b[O"
)

// SyncAgentFocus reports focus changes to every tab's tmux client. It runs
// after every update, so it has to be cheap in the steady state: a tab whose
// client already has the right report costs two comparisons and no lock. Only
// a tab whose state changed, or whose client is not ready yet, is looked at
// more closely -- and a restart or reattach gives a tab a new terminal, which
// counts as a change, so the new client is told where it stands.
func (m *Model) SyncAgentFocus(terminalFocused bool) {
	for wsID, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab == nil || tab.isClosed() || tab.Agent == nil || tab.Agent.Terminal == nil {
				continue
			}
			want := terminalFocused && !m.monitorMode && m.isActiveTab(wsID, tab.ID)
			m.reportAgentFocus(tab, want)
		}
	}
}

// reportAgentFocus sends tab's client a focus report if its last one said
// otherwise, and leaves it to the next call when it cannot be sent yet.
//
// A report goes out only once the vterm has seen the client enable focus
// reporting, which tmux does at attach. Before that the PTY may still belong to
// the shell running the client command, which would take the bytes as typed
// input. A new terminal (a restart or reattach) starts with nothing reported.
func (m *Model) reportAgentFocus(tab *Tab, focused bool) {
	term := tab.Agent.Terminal
	if tab.focusTerm == term && tab.focusSent == focused {
		return
	}
	tab.mu.Lock()
	ready := tab.Terminal != nil && tab.Terminal.FocusReporting()
	tab.mu.Unlock()
	if !ready {
		return
	}
	if err := writeFocusReport(term, focused); err != nil {
		logging.Debug("focus report to tab %s failed: %v", tab.ID, err)
		return
	}
	tab.focusTerm, tab.focusSent = term, focused
}

func writeFocusReport(term *appPty.Terminal, focused bool) error {
	report := focusOutReport
	if focused {
		report = focusInReport
	}
	_, err := term.Write([]byte(report))
	return err
}
