package center

import (
	"image/color"
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
		tabAllowEdits := tab.AllowEdits
		tabIsolated := tab.Isolated
		tabSkipPerms := tab.SkipPermissions
		tabRenamed := tab.WorkspaceRenamed
		tab.mu.Unlock()

		// Add brand color indicator for agent tabs (not file viewers)
		var indicator string
		var tabActive bool
		isChat := m.isChatTab(tab)
		if isChat {
			if tabDisconnected {
				indicator = common.Icons.Idle + " " // Disconnected indicator
			} else {
				indicator = common.Icons.Running + " " // Brand color dot
			}
			tabActive = m.IsTabActive(tab)
		}

		agentStyle := m.styles.AgentClaude
		if tab.Assistant != "claude" {
			agentStyle = m.styles.AgentTerm
		}

		// Build mode indicator icons with spacing
		type modeIcon struct {
			char    string
			fg      color.Color
			tooltip string
		}
		var modeIconList []modeIcon
		if isChat {
			if tabRenamed {
				modeIconList = append(modeIconList, modeIcon{
					char:    "⚠",
					fg:      common.ColorWarning,
					tooltip: "Workspace renamed: restart agent to use new directory",
				})
			}
			if tabAllowEdits {
				modeIconList = append(modeIconList, modeIcon{
					char:    "✎",
					fg:      common.ColorSuccess,
					tooltip: "Immediately allow edits: agent can write files without asking",
				})
			}
			if tabIsolated {
				modeIconList = append(modeIconList, modeIcon{
					char:    "⛶",
					fg:      common.ColorError,
					tooltip: "Sandboxed: agent runs in an isolated environment",
				})
			}
			if tabSkipPerms {
				modeIconList = append(modeIconList, modeIcon{
					char:    "∅",
					fg:      common.ColorWarning,
					tooltip: "Bypass permissions: agent skips all permission checks",
				})
			}
		}
		renderModeIcons := func(bgColor color.Color) string {
			if len(modeIconList) == 0 {
				return ""
			}
			parenStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
			if bgColor != nil {
				parenStyle = parenStyle.Background(bgColor)
			}
			var inner string
			for j, icon := range modeIconList {
				if j > 0 {
					if bgColor != nil {
						inner += lipgloss.NewStyle().Background(bgColor).Render(" ")
					} else {
						inner += " "
					}
				}
				iconStyle := lipgloss.NewStyle().Foreground(icon.fg)
				if bgColor != nil {
					iconStyle = iconStyle.Background(bgColor)
				}
				inner += iconStyle.Render(icon.char)
			}
			return parenStyle.Render("(") + inner + parenStyle.Render(")")
		}
		modeIcons := renderModeIcons(nil)

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
			// Mode icons with background
			modePart := ""
			if len(modeIconList) > 0 {
				modePart = lipgloss.NewStyle().Background(bg).Render(" ") + renderModeIcons(bg)
			}
			space := lipgloss.NewStyle().Background(bg).Render(" ")
			closePart := lipgloss.NewStyle().Foreground(common.ColorMuted).Background(bg).Render("×")
			rendered = pad + indicatorPart + namePart + modePart + space + closePart + pad
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
			modeLabel := ""
			if modeIcons != "" {
				modeLabel = " " + modeIcons
			}
			content := indicatorStyled + nameStyled + modeLabel + " " + closeLabel
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

			// Add hit regions for individual mode icons
			if len(modeIconList) > 0 {
				namePartWidth := lipgloss.Width(agentStyle.Render(indicator) + name)
				iconStartX := x + leftFrame + namePartWidth + 1 // +1 for the leading space before "("
				// The whole group is wrapped in (...), so offset past the opening paren
				iconStartX++ // skip "("
				for j, icon := range modeIconList {
					if j > 0 {
						iconStartX++ // skip space between icons
					}
					iconW := lipgloss.Width(icon.char)
					m.tabHits = append(m.tabHits, tabHit{
						kind:  tabHitModeIcon,
						index: i,
						label: icon.tooltip,
						region: common.HitRegion{
							X:      iconStartX,
							Y:      0,
							Width:  iconW,
							Height: 1,
						},
					})
					iconStartX += iconW
				}
			}
		}
		x += renderedWidth
		renderedTabs = append(renderedTabs, rendered)
	}

	// Add control buttons (hidden for archived workspaces)
	if m.workspace == nil || !m.workspace.Archived() {
		btn := m.styles.TabPlus.Render("+ New")
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
		x += btnWidth

		// Add "+ New (Custom)" button to customize tab settings
		selectBtn := m.styles.TabPlus.Render("+ New (Custom)")
		selectBtnWidth := lipgloss.Width(selectBtn)
		if selectBtnWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitPlusSelect,
				index: -1,
				region: common.HitRegion{
					X:      x,
					Y:      0,
					Width:  selectBtnWidth,
					Height: 1,
				},
			})
		}
		renderedTabs = append(renderedTabs, selectBtn)
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
	// Check mode icon clicks first (they overlap with tab regions)
	for _, hit := range m.tabHits {
		if hit.kind == tabHitModeIcon && hit.region.Contains(localX, localY) {
			tooltip := hit.label
			return func() tea.Msg {
				return messages.Toast{Message: tooltip, Level: messages.ToastInfo}
			}
		}
	}
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
				return func() tea.Msg {
					return messages.LaunchAgent{
						Assistant:       "claude",
						Workspace:       m.workspace,
						AllowEdits:      m.config.UI.LastAllowEdits,
						Isolated:        m.config.UI.LastIsolated,
						SkipPermissions: m.config.UI.LastSkipPermissions,
					}
				}
			case tabHitPlusSelect:
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
