package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// mouseModeSettledMsg fires once shortly after startup and ends the mouse-mode
// nudge (see mouseMode).
type mouseModeSettledMsg struct{}

// mouseModeNudgeDelay is long enough for the alt screen to be established and
// the first frames to have gone out.
const mouseModeNudgeDelay = 120 * time.Millisecond

// mouseMode returns the mouse mode the view should request.
//
// It reports cell-motion for one short phase right after startup and all-motion
// otherwise, purely so that the mode *changes*. Bubbletea only writes the
// mouse-enable sequence when the requested mode differs from the last frame's,
// and it writes that sequence before it enters the alternate screen — so on a
// terminal that scopes DEC private modes to the screen buffer, the only enable
// medusa ever asks for lands on the primary screen and is dropped on the way
// into the alternate one. Nothing changes the mode after that, so motion
// reporting stays off for the rest of the run and every hover affordance
// silently does nothing.
//
// Asking for a different mode once, after the alt screen is up, makes bubbletea
// emit a fresh all-motion enable where it can take effect. The cost is one frame
// in which only button events are reported.
func (a *App) mouseMode() tea.MouseMode {
	if a.mouseModePhase == 1 {
		return tea.MouseModeCellMotion
	}
	return tea.MouseModeAllMotion
}

// beginMouseModeNudge starts the nudge, once, on the first window size we see.
func (a *App) beginMouseModeNudge() tea.Cmd {
	if a.mouseModePhase != 0 {
		return nil
	}
	a.mouseModePhase = 1
	return common.SafeTick(mouseModeNudgeDelay, func(time.Time) tea.Msg {
		return mouseModeSettledMsg{}
	})
}

// routeMouseClick routes mouse click events to the appropriate pane.
func (a *App) routeMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	leftGutter := a.layout.LeftGutter()
	topGutter := a.layout.TopGutter()
	dashWidth := a.layout.DashboardWidth()
	centerWidth := a.layout.CenterWidth()
	gapX := 0
	if a.layout.ShowCenter() {
		gapX = a.layout.GapX()
	}
	centerStart := leftGutter + dashWidth + gapX
	centerEnd := centerStart + centerWidth
	sidebarStart := centerEnd
	sidebarEnd := centerEnd
	if a.layout.ShowSidebar() {
		sidebarStart = centerEnd + gapX
		sidebarEnd = sidebarStart + a.layout.SidebarWidth()
	}

	// Terminal pane is below center, spanning center's width
	terminalStart := centerStart
	terminalEnd := centerEnd
	terminalTopY := topGutter + a.layout.CenterContentHeight()
	terminalBottomY := terminalTopY + a.layout.TerminalHeight()

	inSidebarX := a.layout.ShowSidebar() && msg.X >= sidebarStart && msg.X < sidebarEnd
	inTerminalArea := a.layout.ShowTerminal() && msg.X >= terminalStart && msg.X < terminalEnd && msg.Y >= terminalTopY && msg.Y < terminalBottomY
	inCenterArea := a.layout.ShowCenter() && msg.X >= centerStart && msg.X < centerEnd && msg.Y >= topGutter && msg.Y < terminalTopY

	// Focus pane on left-click press
	var focusCmd tea.Cmd
	if msg.Button == tea.MouseLeft {
		// Check for terminal toggle button click
		if inTerminalArea && msg.X == a.terminalToggleX && msg.Y == a.terminalToggleY {
			return func() tea.Msg { return messages.ToggleTerminalCollapse{} }
		}

		if msg.X < leftGutter {
			a.focusPane(messages.PaneDashboard)
		} else if msg.X < leftGutter+dashWidth {
			// Clicked on dashboard (left bar)
			a.focusPane(messages.PaneDashboard)
		} else if inTerminalArea {
			// Clicked on terminal pane (below center)
			focusCmd = a.focusPane(messages.PaneTerminal)
		} else if inCenterArea {
			// Clicked on center pane
			a.focusPane(messages.PaneCenter)
		} else if inSidebarX {
			// Clicked on sidebar (git changes)
			a.focusPane(messages.PaneSidebar)
		}
	}

	if cmd := a.handleCenterPaneClick(msg); cmd != nil {
		return cmd
	}

	// Forward mouse events to the focused pane
	// This ensures drag events are received even if the mouse leaves the pane bounds
	switch a.focusedPane {
	case messages.PaneDashboard:
		adjusted := msg
		if a.layout != nil {
			adjusted.X -= a.layout.LeftGutter()
			adjusted.Y -= a.layout.TopGutter()
		}
		newDashboard, cmd := a.dashboard.Update(adjusted)
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		adjusted := msg
		if a.layout != nil {
			adjusted.Y -= a.layout.TopGutter()
		}
		newCenter, cmd := a.center.Update(adjusted)
		a.center = newCenter
		return cmd
	case messages.PaneTerminal:
		// Forward clicks to terminal pane
		if inTerminalArea {
			newTerm, cmd := a.sidebarTerminal.Update(msg)
			a.sidebarTerminal = newTerm
			// If the click returned a command (e.g., CreateNewTab from "+ New" button),
			// skip focusCmd to avoid double terminal creation
			if cmd != nil {
				return cmd
			}
			return focusCmd
		}
	case messages.PaneSidebar:
		adjusted := msg
		if a.layout != nil {
			adjusted.X, adjusted.Y = a.adjustSidebarMouseXY(adjusted.X, adjusted.Y)
		}
		// Ignore clicks in the gap/right gutter so they don't trigger sidebar actions.
		if inSidebarX {
			newSidebar, cmd := a.sidebar.Update(adjusted)
			a.sidebar = newSidebar
			return cmd
		}
	}
	return focusCmd
}

// routeMouseWheel routes mouse wheel events to the appropriate pane.
func (a *App) routeMouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	switch a.focusedPane {
	case messages.PaneDashboard:
		adjusted := msg
		if a.layout != nil {
			adjusted.X -= a.layout.LeftGutter()
			adjusted.Y -= a.layout.TopGutter()
		}
		newDashboard, cmd := a.dashboard.Update(adjusted)
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		adjusted := msg
		if a.layout != nil {
			adjusted.Y -= a.layout.TopGutter()
		}
		newCenter, cmd := a.center.Update(adjusted)
		a.center = newCenter
		return cmd
	case messages.PaneTerminal:
		newTerm, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerm
		return cmd
	case messages.PaneSidebar:
		adjusted := msg
		if a.layout != nil {
			adjusted.X, adjusted.Y = a.adjustSidebarMouseXY(adjusted.X, adjusted.Y)
		}
		newSidebar, cmd := a.sidebar.Update(adjusted)
		a.sidebar = newSidebar
		return cmd
	}
	return nil
}

// routeMouseMotion routes mouse motion events to the appropriate pane.
func (a *App) routeMouseMotion(msg tea.MouseMotionMsg) tea.Cmd {
	// Hover-capable all-motion events have no pressed button. Always let the
	// center and the dashboard observe them, regardless of which pane owns
	// keyboard focus: the center's copy affordances activate when entered and
	// clear when left, and the dashboard's drag handles advertise a row before
	// anything has been clicked. Drag motion remains routed exclusively to the
	// focused pane below.
	if !a.loggedFirstMotion {
		// Logged once. Whether the terminal reports pointer motion at all — and
		// from when — is invisible from inside the app otherwise, and hover
		// affordances silently do nothing without it.
		a.loggedFirstMotion = true
		logging.Info("First mouse motion received: button=%v at (%d,%d)", msg.Button, msg.X, msg.Y)
	}

	if msg.Button == tea.MouseNone {
		var centerCmd, dashCmd tea.Cmd
		// The dashboard goes first: its drag handles must not depend on the
		// center pane's hover handling getting through.
		if a.dashboard != nil {
			adjusted := msg
			if a.layout != nil {
				adjusted.X -= a.layout.LeftGutter()
				adjusted.Y -= a.layout.TopGutter()
			}
			newDashboard, cmd := a.dashboard.Update(adjusted)
			a.dashboard, dashCmd = newDashboard, cmd
		}
		if a.center != nil {
			adjusted := msg
			if a.layout != nil {
				adjusted.Y -= a.layout.TopGutter()
			}
			newCenter, cmd := a.center.Update(adjusted)
			a.center, centerCmd = newCenter, cmd
		}
		switch a.focusedPane {
		case messages.PaneCenter:
			return centerCmd
		case messages.PaneDashboard:
			return dashCmd
		}
		// Other panes still see hover motion through the switch below.
	}

	switch a.focusedPane {
	case messages.PaneDashboard:
		adjusted := msg
		if a.layout != nil {
			adjusted.X -= a.layout.LeftGutter()
			adjusted.Y -= a.layout.TopGutter()
		}
		newDashboard, cmd := a.dashboard.Update(adjusted)
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		adjusted := msg
		if a.layout != nil {
			adjusted.Y -= a.layout.TopGutter()
		}
		newCenter, cmd := a.center.Update(adjusted)
		a.center = newCenter
		return cmd
	case messages.PaneTerminal:
		newTerm, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerm
		return cmd
	case messages.PaneSidebar:
		adjusted := msg
		if a.layout != nil {
			adjusted.X, adjusted.Y = a.adjustSidebarMouseXY(adjusted.X, adjusted.Y)
		}
		newSidebar, cmd := a.sidebar.Update(adjusted)
		a.sidebar = newSidebar
		return cmd
	}
	return nil
}

// routeMouseRelease routes mouse release events to the appropriate pane.
func (a *App) routeMouseRelease(msg tea.MouseReleaseMsg) tea.Cmd {
	switch a.focusedPane {
	case messages.PaneDashboard:
		adjusted := msg
		if a.layout != nil {
			adjusted.X -= a.layout.LeftGutter()
			adjusted.Y -= a.layout.TopGutter()
		}
		newDashboard, cmd := a.dashboard.Update(adjusted)
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		adjusted := msg
		if a.layout != nil {
			adjusted.Y -= a.layout.TopGutter()
		}
		newCenter, cmd := a.center.Update(adjusted)
		a.center = newCenter
		return cmd
	case messages.PaneTerminal:
		newTerm, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerm
		return cmd
	case messages.PaneSidebar:
		adjusted := msg
		if a.layout != nil {
			adjusted.X, adjusted.Y = a.adjustSidebarMouseXY(adjusted.X, adjusted.Y)
		}
		newSidebar, cmd := a.sidebar.Update(adjusted)
		a.sidebar = newSidebar
		return cmd
	}
	return nil
}

// handleMonitorModeClick handles mouse clicks in monitor mode.
func (a *App) handleMonitorModeClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button == tea.MouseLeft {
		a.focusPane(messages.PaneMonitor)
		if a.monitorExitHit(msg.X, msg.Y) {
			return a.toggleMonitorMode()
		}
		if filter, ok := a.monitorFilterHit(msg.X, msg.Y); ok {
			a.monitorFilter = filter
			return nil
		}
		// Click to focus tile (just select, don't exit)
		a.selectMonitorTile(msg.X, msg.Y)
	}
	return nil
}
