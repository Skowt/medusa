package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/ui/common"
)

func (a *App) handleCenterPaneClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	if a.layout == nil || !a.layout.ShowCenter() || a.center.HasTabs() || a.center.IsInfoTabActive() {
		return nil
	}
	dashWidth := a.layout.DashboardWidth()
	centerWidth := a.layout.CenterWidth()
	gapX := a.layout.GapX()
	if centerWidth <= 0 {
		return nil
	}
	centerStart := a.layout.LeftGutter() + dashWidth + gapX
	centerEnd := centerStart + centerWidth
	if msg.X < centerStart || msg.X >= centerEnd {
		return nil
	}
	contentX, contentY := a.centerPaneContentOrigin()
	localX := msg.X - contentX
	localY := msg.Y - contentY
	if localX < 0 || localY < 0 {
		return nil
	}

	if a.showWelcome {
		return a.handleWelcomeClick(localX, localY)
	}
	return nil
}

func (a *App) handleWelcomeClick(localX, localY int) tea.Cmd {
	content := a.welcomeContent()
	lines := strings.Split(content, "\n")
	_, contentHeight := viewDimensions(content)

	placeWidth := a.layout.CenterWidth() - 4
	placeHeight := a.layout.Height() - 2
	if placeWidth <= 0 || placeHeight <= 0 {
		return nil
	}

	offsetY := centerOffset(placeHeight, contentHeight)

	for i, line := range lines {
		strippedLine := ansi.Strip(line)
		lineWidth := lipgloss.Width(line)
		lineOffsetX := centerOffset(placeWidth, lineWidth)

		settingsText := "[Settings]"
		if idx := strings.Index(strippedLine, settingsText); idx >= 0 {
			region := common.HitRegion{
				X:      idx + lineOffsetX,
				Y:      i + offsetY,
				Width:  len(settingsText),
				Height: 1,
			}
			if region.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowSettingsDialog{} }
			}
		}

		addWorkspaceText := "[Add workspace]"
		if idx := strings.Index(strippedLine, addWorkspaceText); idx >= 0 {
			region := common.HitRegion{
				X:      idx + lineOffsetX,
				Y:      i + offsetY,
				Width:  len(addWorkspaceText),
				Height: 1,
			}
			if region.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowCreateWorkspaceDialog{} }
			}
		}
	}

	return nil
}
