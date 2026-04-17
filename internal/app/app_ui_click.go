package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// Welcome-screen button labels. The render path (welcomeContent) and the
// click handler (handleWelcomeClick) share these constants so the two cannot
// drift apart — an earlier mismatch ("[Add workspace]" vs "[+ Add Workspace]")
// made the Add-Workspace button completely unclickable.
const (
	welcomeAddWorkspaceLabel = "[+ Add Workspace]"
	welcomeSettingsLabel     = "[Settings]"
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

		if r, ok := markerRegion(strippedLine, welcomeSettingsLabel, i+offsetY, lineOffsetX); ok {
			if r.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowSettingsDialog{} }
			}
		}
		if r, ok := markerRegion(strippedLine, welcomeAddWorkspaceLabel, i+offsetY, lineOffsetX); ok {
			if r.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowCreateWorkspaceDialog{} }
			}
		}
	}

	return nil
}

// markerRegion finds label inside stripped (a pre-ANSI-stripped line) and
// returns a HitRegion whose X and Width are measured in display columns —
// never in bytes. strings.Index returns a byte offset, so callers that treat
// that offset as a column miscompute hit regions whenever the prefix contains
// multi-byte runes. markerRegion shields against that by re-measuring the
// prefix width through lipgloss.
func markerRegion(stripped, label string, y, offsetX int) (common.HitRegion, bool) {
	idx := strings.Index(stripped, label)
	if idx < 0 {
		return common.HitRegion{}, false
	}
	return common.HitRegion{
		X:      lipgloss.Width(stripped[:idx]) + offsetX,
		Y:      y,
		Width:  lipgloss.Width(label),
		Height: 1,
	}, true
}
