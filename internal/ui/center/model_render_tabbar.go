package center

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// renderTabBar renders the tab bar with activity indicators
func (m *Model) renderTabBar() string {
	m.tabHits = m.tabHits[:0]
	currentTabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()

	var renderedTabs []string
	x := 0

	// Info tab (virtual, always first)
	{
		infoLabel := "Info"
		infoIndicator := common.Icons.Running + " "
		var infoRendered string
		if m.infoTabActive {
			bg := common.ColorSurface2
			pad := lipgloss.NewStyle().Background(bg).Render(" ")
			indicatorPart := lipgloss.NewStyle().Foreground(common.ColorInfo).Background(bg).Render(infoIndicator)
			namePart := lipgloss.NewStyle().Foreground(common.ColorForeground).Background(bg).Render(infoLabel)
			infoRendered = pad + indicatorPart + namePart + pad
		} else {
			indicatorStyled := lipgloss.NewStyle().Foreground(common.ColorInfo).Render(infoIndicator)
			infoRendered = m.styles.Tab.Render(indicatorStyled + m.styles.Muted.Render(infoLabel))
		}
		infoWidth := lipgloss.Width(infoRendered)
		if infoWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitInfo,
				index: -1,
				region: common.HitRegion{
					X:      x,
					Y:      0,
					Width:  infoWidth,
					Height: 1,
				},
			})
		}
		renderedTabs = append(renderedTabs, infoRendered)
		x += infoWidth
	}

	for i, tab := range currentTabs {
		name := tab.Name
		if name == "" {
			name = tab.Assistant
		}

		// Read tab state under lock
		tab.mu.Lock()
		tabDisconnected := tab.Detached || !tab.Running
		tab.mu.Unlock()

		// Brand-color indicator for agent tabs (running = solid dot, idle = ring).
		var indicator string
		var tabActive bool
		isChat := m.isChatTab(tab)
		if isChat {
			if tabDisconnected {
				indicator = common.Icons.Idle + " "
			} else {
				indicator = common.Icons.Running + " "
			}
			tabActive = m.IsTabActive(tab)
		}

		agentStyle := m.styles.AgentClaude
		if tab.Assistant != "claude" {
			agentStyle = m.styles.AgentTerm
		}

		// Build tab content with close affordance
		closeLabel := m.styles.Muted.Render("×")
		var rendered string
		var style lipgloss.Style
		if i == activeIdx && !m.infoTabActive {
			// Active tab - each part styled with same background
			bg := common.ColorSurface2
			pad := lipgloss.NewStyle().Background(bg).Render(" ")
			indicatorFg := agentStyle.GetForeground()
			if tabDisconnected {
				indicatorFg = common.ColorMuted
			}
			indicatorPart := lipgloss.NewStyle().Foreground(indicatorFg).Background(bg).Render(indicator)
			nameStyle := lipgloss.NewStyle().Foreground(common.ColorForeground).Background(bg)
			if tabDisconnected {
				nameStyle = nameStyle.Foreground(common.ColorMuted)
			}
			namePart := nameStyle.Render(name)
			space := lipgloss.NewStyle().Background(bg).Render(" ")
			closePart := lipgloss.NewStyle().Foreground(common.ColorMuted).Background(bg).Render("×")
			rendered = pad + indicatorPart + namePart + space + closePart + pad
			style = m.styles.ActiveTab
		} else {
			// Inactive tab
			var nameStyled string
			if tabDisconnected {
				nameStyled = m.styles.Muted.Render(name)
			} else if tabActive {
				nameStyled = lipgloss.NewStyle().Foreground(common.ColorPrimary).Bold(true).Render(name)
			} else {
				nameStyled = m.styles.Muted.Render(name)
			}
			var indicatorStyled string
			if tabDisconnected {
				indicatorStyled = m.styles.Muted.Render(indicator)
			} else {
				indicatorStyled = agentStyle.Render(indicator)
			}
			content := indicatorStyled + nameStyled + " " + closeLabel
			rendered = m.styles.Tab.Render(content)
			style = m.styles.Tab
		}
		renderedWidth := lipgloss.Width(rendered)
		if renderedWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitTab,
				index: i,
				region: common.HitRegion{
					X:      x,
					Y:      0,
					Width:  renderedWidth,
					Height: 1,
				},
			})

			frameX, _ := style.GetFrameSize()
			leftFrame := frameX / 2

			// Close button: anchor from the right edge of the rendered tab.
			// The close label (×) plus right padding occupies the rightmost cells.
			closeWidth := lipgloss.Width(closeLabel) + 1 // +1 for pad/space before ×
			closeX := x + renderedWidth - leftFrame - closeWidth
			if closeX > x {
				m.tabHits = append(m.tabHits, tabHit{
					kind:  tabHitClose,
					index: i,
					region: common.HitRegion{
						X:      closeX,
						Y:      0,
						Width:  renderedWidth - (closeX - x),
						Height: 1,
					},
				})
			}
		}
		x += renderedWidth
		renderedTabs = append(renderedTabs, rendered)
	}

	// Add control button (hidden for archived workspaces). Clicking always
	// opens the customize dialog so settings can be chosen per tab.
	if m.workspace == nil || !m.workspace.Archived() {
		btn := m.styles.TabPlus.Render("+ New Agent")
		btnWidth := lipgloss.Width(btn)
		if btnWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitPlus,
				index: -1,
				region: common.HitRegion{
					X:      x,
					Y:      0,
					Width:  btnWidth,
					Height: 1,
				},
			})
		}
		renderedTabs = append(renderedTabs, btn)
	}

	// Join tabs horizontally at the bottom so borders align
	tabLine := lipgloss.JoinHorizontal(lipgloss.Bottom, renderedTabs...)

	// Add a subtle separator line below the tabs
	separatorStyle := lipgloss.NewStyle().Foreground(common.ColorSurface2)
	separatorLine := separatorStyle.Render(strings.Repeat("─", m.contentWidth()))

	return tabLine + "\n" + separatorLine
}

func (m *Model) handleTabBarClick(msg tea.MouseClickMsg) tea.Cmd {
	// Tab bar is below the info bar (if present).
	// Layout: border (Y=0) → info bar (Y=1..infoBarHeight) → tab bar
	// Account for border (1) and padding (1) on the left side when converting X coordinates
	const (
		borderTop   = 1
		borderLeft  = 1
		paddingLeft = 1
	)

	// Tab bar Y position depends on info bar height
	infoBarHeight := m.infoBarHeight()
	tabBarY := borderTop + infoBarHeight

	if msg.Y != tabBarY {
		return nil
	}
	// Convert screen X to content X (subtract pane offset, border, and padding)
	localX := msg.X - m.offsetX - borderLeft - paddingLeft
	if localX < 0 {
		return nil
	}
	// All tab hits are at Y=0 relative to the tab bar
	localY := 0
	// Check close buttons (they overlap with tab regions)
	for _, hit := range m.tabHits {
		if hit.kind == tabHitClose && hit.region.Contains(localX, localY) {
			idx := hit.index
			return func() tea.Msg {
				return messages.CloseTabAt{Index: idx}
			}
		}
	}
	// Then check tabs and other buttons
	for _, hit := range m.tabHits {
		if hit.region.Contains(localX, localY) {
			switch hit.kind {
			case tabHitPlus:
				if m.workspace != nil && m.workspace.Archived() {
					ws := m.workspace
					return func() tea.Msg {
						return messages.ShowUnarchiveWorkspaceDialog{Workspace: ws}
					}
				}
				return func() tea.Msg { return messages.ShowCustomizeTabDialog{} }
			case tabHitInfo:
				m.infoTabActive = true
				return nil
			case tabHitTab:
				if m.workspace != nil && m.workspace.Archived() {
					ws := m.workspace
					return func() tea.Msg {
						return messages.ShowUnarchiveWorkspaceDialog{Workspace: ws}
					}
				}
				m.infoTabActive = false
				m.setActiveTabIdx(hit.index)
				return m.tabSelectionChangedCmd()
			}
		}
	}
	return nil
}
