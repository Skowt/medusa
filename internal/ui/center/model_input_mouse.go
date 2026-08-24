package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/ui/diff"
)

const doubleClickTimeout = 400 * time.Millisecond

// updateMouseClick handles tea.MouseClickMsg in the Update switch.
func (m *Model) updateMouseClick(msg tea.MouseClickMsg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle tab bar clicks (e.g., the plus button) even without an active agent.
	if msg.Button == tea.MouseLeft {
		if cmd := m.handleTabBarClick(msg); cmd != nil {
			return m, cmd
		}
		// Handle action bar clicks
		if cmd := m.handleActionBarClickFromMsg(msg); cmd != nil {
			return m, cmd
		}
	}

	// Handle info tab content clicks
	if msg.Button == tea.MouseLeft && m.infoTabActive && m.infoContent != "" {
		if cmd := m.handleInfoContentClick(msg); cmd != nil {
			return m, cmd
		}
	}

	// Handle mouse events for text selection
	if !m.focused || !m.hasActiveAgent() {
		return m, nil
	}

	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx >= len(tabs) {
		return m, nil
	}
	tab := tabs[activeIdx]
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	if tab2, ok := m.activeTabForwardsMouse(); ok {
		if code, cok := sgrMouseButton(msg.Button, false); cok {
			m.forwardMouse(tab2, code, msg.X, msg.Y, false)
		}
		return m, nil
	}
	if msg.Button != tea.MouseLeft {
		return m, nil
	}

	// Convert screen coordinates to terminal coordinates
	termX, termY, inBounds := m.screenToTerminal(msg.X, msg.Y)

	// Detect double-click: same position within timeout
	now := time.Now()
	var absLine int
	tab.mu.Lock()
	if inBounds && tab.Terminal != nil {
		absLine = tab.Terminal.ScreenYToAbsoluteLine(termY)
	}
	isDoubleClick := inBounds &&
		now.Sub(tab.lastClickTime) < doubleClickTimeout &&
		tab.lastClickX == termX &&
		tab.lastClickLine == absLine
	tab.lastClickTime = now
	tab.lastClickX = termX
	tab.lastClickLine = absLine
	tab.mu.Unlock()

	if isDoubleClick {
		// Double-click: select word at cursor
		if m.isTabActorReady() {
			if m.sendTabEvent(tabEvent{
				tab:         tab,
				workspaceID: m.workspaceID(),
				tabID:       tab.ID,
				kind:        tabEventSelectionWord,
				termX:       termX,
				termY:       termY,
				inBounds:    inBounds,
			}) {
				return m, common.SafeBatch(cmds...)
			}
		}
		tab.mu.Lock()
		if tab.Terminal != nil {
			wordStart, wordEnd := tab.Terminal.WordBoundsAt(termX, absLine)
			tab.Selection = SelectionState{
				Active:    false,
				StartX:    wordStart,
				StartLine: absLine,
				EndX:      wordEnd,
				EndLine:   absLine,
			}
			tab.Terminal.SetSelection(wordStart, absLine, wordEnd, absLine, true, false)
			// Copy the selected word
			text := tab.Terminal.GetSelectedText(wordStart, absLine, wordEnd, absLine)
			if text != "" {
				if err := common.CopyToClipboard(text); err != nil {
					logging.Error("Failed to copy word to clipboard: %v", err)
				}
			}
		}
		tab.mu.Unlock()
		return m, common.SafeBatch(cmds...)
	}

	if m.isTabActorReady() {
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventSelectionStart,
			termX:       termX,
			termY:       termY,
			inBounds:    inBounds,
		}) {
			return m, common.SafeBatch(cmds...)
		}
	}
	tab.mu.Lock()
	if tab.Terminal != nil {
		tab.Terminal.ClearSelection()
	}
	tab.Selection = SelectionState{}
	if inBounds && tab.Terminal != nil {
		// Store anchor for potential drag, but don't set VTerm selection yet.
		// Visual highlighting only appears once the user drags (motion event).
		tab.Selection = SelectionState{
			Active:    true,
			StartX:    termX,
			StartLine: absLine,
			EndX:      termX,
			EndLine:   absLine,
		}
	}
	tab.mu.Unlock()
	return m, common.SafeBatch(cmds...)
}

// updateMouseMotion handles tea.MouseMotionMsg in the Update switch.
func (m *Model) updateMouseMotion(msg tea.MouseMotionMsg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd
	m.updateCopyHover(msg.X, msg.Y)

	// Handle mouse drag events for text selection
	if !m.focused || !m.hasActiveAgent() {
		return m, nil
	}
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx >= len(tabs) {
		return m, nil
	}
	tab := tabs[activeIdx]
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	if tab2, ok := m.activeTabForwardsMouse(); ok {
		if code, cok := sgrMouseButton(msg.Button, true); cok {
			m.forwardMouse(tab2, code, msg.X, msg.Y, false)
		}
		return m, nil
	}

	termX, termY, _ := m.screenToTerminal(msg.X, msg.Y)

	if m.isTabActorReady() {
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventSelectionUpdate,
			termX:       termX,
			termY:       termY,
		}) {
			return m, common.SafeBatch(cmds...)
		}
	}
	tab.mu.Lock()
	if tab.Selection.Active && tab.Terminal != nil {
		termWidth := tab.Terminal.Width
		termHeight := tab.Terminal.Height
		if termX < 0 {
			termX = 0
		}
		if termX >= termWidth {
			termX = termWidth - 1
		}
		if termY < 0 {
			if !tab.FrameRendering {
				tab.Terminal.ScrollView(1)
			}
			termY = 0
		} else if termY >= termHeight {
			if !tab.FrameRendering {
				tab.Terminal.ScrollView(-1)
			}
			termY = termHeight - 1
		}
		absLine := tab.Terminal.ScreenYToAbsoluteLine(termY)
		startX := tab.Terminal.SelStartX()
		startLine := tab.Terminal.SelStartLine()
		if !tab.Terminal.HasSelection() {
			startX = tab.Selection.StartX
			startLine = tab.Selection.StartLine
		}
		tab.Selection.EndX = termX
		tab.Selection.EndLine = absLine
		tab.Terminal.SetSelection(startX, startLine, termX, absLine, true, false)
		tab.Selection.StartX = startX
		tab.Selection.StartLine = startLine
	}
	tab.mu.Unlock()
	return m, common.SafeBatch(cmds...)
}

func (m *Model) updateCopyHover(screenX, screenY int) {
	const (
		borderTop   = 1
		borderLeft  = 1
		paddingLeft = 1
	)
	localX := screenX - m.offsetX - borderLeft - paddingLeft
	m.copyHoverActive = false
	if localX < 0 {
		return
	}

	contentY := screenY - borderTop
	if contentY == m.actionBarY {
		for _, hit := range m.actionBarHits {
			if hit.region.Contains(localX, 0) {
				switch hit.kind {
				case actionBarCopyBranch:
					m.copyHover, m.copyHoverActive = copyTargetBranch, true
				case actionBarCopyDir:
					m.copyHover, m.copyHoverActive = copyTargetWorkdir, true
				}
				return
			}
		}
	}

	tabBarY := borderTop + m.infoBarHeight()
	if screenY == tabBarY {
		for _, hit := range m.tabHits {
			if hit.kind == tabHitSessionID && hit.region.Contains(localX, 0) {
				m.copyHover, m.copyHoverActive = copyTargetSessionID, true
				return
			}
		}
		return
	}

	if !m.IsInfoTabActive() {
		return
	}
	if field, ok := m.infoCopyHit(localX, contentY-m.infoContentOriginY()); ok {
		m.copyHover, m.copyHoverActive = infoCopyRows[field].target, true
	}
}

// updateMouseRelease handles tea.MouseReleaseMsg in the Update switch.
func (m *Model) updateMouseRelease(msg tea.MouseReleaseMsg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle mouse release events for text selection
	if !m.focused || !m.hasActiveAgent() {
		return m, nil
	}
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx >= len(tabs) {
		return m, nil
	}
	tab := tabs[activeIdx]
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	if tab2, ok := m.activeTabForwardsMouse(); ok {
		if code, cok := sgrMouseButton(msg.Button, false); cok {
			m.forwardMouse(tab2, code, msg.X, msg.Y, true)
		}
		return m, nil
	}

	if m.isTabActorReady() {
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventSelectionFinish,
		}) {
			return m, common.SafeBatch(cmds...)
		}
	}
	tab.mu.Lock()
	if tab.Selection.Active {
		if tab.Terminal != nil &&
			(tab.Selection.StartX != tab.Selection.EndX ||
				tab.Selection.StartLine != tab.Selection.EndLine) {
			text := tab.Terminal.GetSelectedText(
				tab.Terminal.SelStartX(), tab.Terminal.SelStartLine(),
				tab.Terminal.SelEndX(), tab.Terminal.SelEndLine(),
			)
			if text != "" {
				if err := common.CopyToClipboard(text); err != nil {
					logging.Error("Failed to copy to clipboard: %v", err)
				} else {
					logging.Info("Copied %d chars to clipboard", len(text))
				}
			}
		}
		tab.Selection.Active = false
	}
	tab.mu.Unlock()
	return m, common.SafeBatch(cmds...)
}

// updateMouseWheel handles tea.MouseWheelMsg in the Update switch.
func (m *Model) updateMouseWheel(msg tea.MouseWheelMsg) (*Model, tea.Cmd) {
	if !m.focused || !m.hasActiveAgent() {
		return m, nil
	}

	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx >= len(tabs) {
		return m, nil
	}
	tab := tabs[activeIdx]
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	if tab2, ok := m.activeTabForwardsMouse(); ok {
		code, cok := sgrMouseButton(msg.Button, false)
		if cok {
			m.forwardMouse(tab2, code, msg.X, msg.Y, false)
		}
		return m, nil
	}
	// A frame-rendering app owns its history even when it has not requested
	// mouse reporting. Do not manufacture vterm scrollback from old frames.
	if tabAppOwnsScreen(tab) {
		return m, nil
	}

	delta := 0
	tab.mu.Lock()
	if tab.Terminal != nil {
		delta = common.ScrollDeltaForHeight(tab.Terminal.Height, 8)
	}
	tab.mu.Unlock()
	if delta > 0 {
		if m.isTabActorReady() {
			sent := false
			switch msg.Button {
			case tea.MouseWheelUp:
				sent = m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: m.workspaceID(),
					tabID:       tab.ID,
					kind:        tabEventScrollBy,
					delta:       delta,
				})
			case tea.MouseWheelDown:
				sent = m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: m.workspaceID(),
					tabID:       tab.ID,
					kind:        tabEventScrollBy,
					delta:       -delta,
				})
			}
			if sent {
				return m, nil
			}
		}
		tab.mu.Lock()
		if tab.Terminal != nil {
			switch msg.Button {
			case tea.MouseWheelUp:
				tab.Terminal.ScrollView(delta)
			case tea.MouseWheelDown:
				tab.Terminal.ScrollView(-delta)
			}
		}
		tab.mu.Unlock()
	}
	return m, nil
}

func (m *Model) getDiffViewer(tab *Tab) *diff.Model {
	if tab == nil {
		return nil
	}
	tab.mu.Lock()
	dv := tab.DiffViewer
	tab.mu.Unlock()
	return dv
}

func (m *Model) dispatchDiffInput(tab *Tab, msg tea.Msg) (bool, tea.Cmd) {
	if tab == nil {
		return false, nil
	}
	dv := m.getDiffViewer(tab)
	if dv == nil {
		return false, nil
	}
	if m.isTabActorReady() {
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventDiffInput,
			diffMsg:     msg,
		}) {
			return true, nil
		}
	}
	newDV, cmd := dv.Update(msg)
	tab.mu.Lock()
	tab.DiffViewer = newDV
	tab.mu.Unlock()
	return true, cmd
}
