package center

import (
	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/ui/common"
)

// renderInfoTab renders the pinned virtual Info tab.
func (m *Model) renderInfoTab() string {
	const infoLabel = "Info"
	indicator := common.Icons.Running + " "

	if m.infoTabActive {
		bg := common.ColorSurface2
		pad := lipgloss.NewStyle().Background(bg).Render(" ")
		indicatorPart := lipgloss.NewStyle().Foreground(common.ColorInfo).Background(bg).Render(indicator)
		namePart := lipgloss.NewStyle().Foreground(common.ColorForeground).Background(bg).Render(infoLabel)
		return pad + indicatorPart + namePart + pad
	}

	indicatorStyled := lipgloss.NewStyle().Foreground(common.ColorInfo).Render(indicator)
	return m.styles.Tab.Render(indicatorStyled + m.styles.Muted.Render(infoLabel))
}

// renderAgentTab renders a single agent tab.
//
// activeIdx is -1 when the Info tab is selected, so the active branch tests
// only `i == activeIdx` — the old `&& !m.infoTabActive` guard moved into the
// caller.
func (m *Model) renderAgentTab(i int, tab *Tab, activeIdx int) string {
	name := tab.Name
	if name == "" {
		name = tab.Assistant
	}

	tab.mu.Lock()
	tabDisconnected := tab.Detached || !tab.Running
	tab.mu.Unlock()

	var indicator string
	var tabActive bool
	isChat := m.isChatTab(tab)
	isScript := tab.Assistant == "script"
	if isChat || isScript {
		if tabDisconnected {
			indicator = common.Icons.Idle + " "
		} else {
			indicator = common.Icons.Running + " "
		}
		if isChat {
			tabActive = m.IsTabActive(tab)
		}
	}

	agentStyle := m.styles.AgentClaude
	switch {
	case isScript:
		agentStyle = m.styles.AgentScript
	case tab.Assistant != "claude":
		agentStyle = m.styles.AgentTerm
	}

	if i == activeIdx {
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
		return pad + indicatorPart + namePart + space + closePart + pad
	}

	var nameStyled string
	switch {
	case tabDisconnected:
		nameStyled = m.styles.Muted.Render(name)
	case tabActive:
		nameStyled = lipgloss.NewStyle().Foreground(common.ColorPrimary).Bold(true).Render(name)
	default:
		nameStyled = m.styles.Muted.Render(name)
	}

	var indicatorStyled string
	if tabDisconnected {
		indicatorStyled = m.styles.Muted.Render(indicator)
	} else {
		indicatorStyled = agentStyle.Render(indicator)
	}

	closeLabel := m.styles.Muted.Render("×")
	return m.styles.Tab.Render(indicatorStyled + nameStyled + " " + closeLabel)
}
