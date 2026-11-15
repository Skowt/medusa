package app

import tea "charm.land/bubbletea/v2"

// recordTerminalFocus notes medusa's own terminal gaining or losing focus and
// reports whether msg was such an event. Focus is state, not input, so it is
// recorded before any overlay can consume the message -- a blur while a
// dialog was open would otherwise be lost.
func (a *App) recordTerminalFocus(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.FocusMsg:
		a.terminalBlurred = false
		return true
	case tea.BlurMsg:
		a.terminalBlurred = true
		return true
	}
	return false
}

// syncAgentFocus settles each agent tab's focus report after an update. Every
// update can change which agent is on screen, so it is done here, once,
// rather than at each of the many places that can move it.
func (a *App) syncAgentFocus() {
	if a.center != nil {
		a.center.SyncAgentFocus(!a.terminalBlurred)
	}
}
