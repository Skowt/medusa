package center

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/logging"
)

// sgrMouseButton maps a Bubble Tea mouse button to an SGR (mode 1006) button
// code. motion adds the 32 "button-event" bit for drag reporting. ok is false
// for buttons we never forward (e.g. MouseNone).
func sgrMouseButton(b tea.MouseButton, motion bool) (int, bool) {
	var code int
	switch b {
	case tea.MouseLeft:
		code = 0
	case tea.MouseMiddle:
		code = 1
	case tea.MouseRight:
		code = 2
	case tea.MouseWheelUp:
		code = 64
	case tea.MouseWheelDown:
		code = 65
	default:
		return 0, false
	}
	if motion {
		code += 32
	}
	return code, true
}

// encodeSGRMouse builds an SGR mouse report. col1/row1 are 1-based. A release
// uses the final 'm'; press/motion/wheel use 'M'.
func encodeSGRMouse(code, col1, row1 int, release bool) []byte {
	final := byte('M')
	if release {
		final = 'm'
	}
	return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", code, col1, row1, final))
}

// tabAppOwnsMouse reports whether the tab's application should receive mouse
// input: either a fullscreen-launched agent, or any app that is currently in
// the alt screen with mouse reporting enabled (e.g. Claude Code switched to
// fullscreen at runtime via /tui fullscreen in a classic-launched session).
func tabAppOwnsMouse(tab *Tab) bool {
	if tab == nil {
		return false
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Fullscreen {
		return true
	}
	return tab.Terminal != nil && tab.Terminal.AltScreen && tab.Terminal.MouseReporting()
}

// tabAppOwnsScrollKeys reports whether scroll keys (PgUp/PgDown) belong to the
// tab's application rather than medusa's scrollback view. Any alt-screen app
// owns its paging keys, mouse reporting or not (vim, less, fullscreen Claude).
func tabAppOwnsScrollKeys(tab *Tab) bool {
	if tab == nil {
		return false
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Fullscreen {
		return true
	}
	return tab.Terminal != nil && tab.Terminal.AltScreen
}

// activeTabForwardsMouse reports whether mouse input for the active tab should
// be forwarded to the app instead of handled by medusa. True only for a
// focused, live agent terminal view whose app owns the mouse — never the info
// tab or diff viewer.
func (m *Model) activeTabForwardsMouse() (*Tab, bool) {
	if !m.focused || !m.hasActiveAgent() || m.infoTabActive {
		return nil, false
	}
	tabs := m.getTabs()
	idx := m.getActiveTabIdx()
	if idx < 0 || idx >= len(tabs) {
		return nil, false
	}
	tab := tabs[idx]
	if !tabAppOwnsMouse(tab) {
		return nil, false
	}
	if m.getDiffViewer(tab) != nil {
		return nil, false // diff viewer is medusa-rendered
	}
	return tab, true
}

// forwardMouse writes an SGR mouse report to the tab's PTY. It is a no-op when
// the event is outside the terminal content region or the agent terminal is
// unavailable (e.g. during tests or a dead session).
func (m *Model) forwardMouse(tab *Tab, code, screenX, screenY int, release bool) {
	termX, termY, inBounds := m.screenToTerminal(screenX, screenY)
	if !inBounds {
		return
	}
	if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
		return
	}
	seq := encodeSGRMouse(code, termX+1, termY+1, release)
	if _, err := tab.Agent.Terminal.Write(seq); err != nil {
		logging.Warn("mouse forward write failed for tab %s: %v", tab.ID, err)
	}
}
