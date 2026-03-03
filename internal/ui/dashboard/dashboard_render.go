package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/ui/common"
)

// renderRow renders a single dashboard row
func (m *Model) renderRow(row Row, selected bool) string {
	switch row.Type {
	case RowHome:
		contentWidth := m.width - 3
		if contentWidth < 1 {
			contentWidth = 1
		}
		style := lipgloss.NewStyle().Foreground(common.ColorMuted)
		title := style.Render(" Workspaces")
		sep := lipgloss.NewStyle().Foreground(common.ColorSurface2).Render(strings.Repeat("─", contentWidth))
		return title + "\n" + sep

	case RowWorkspace:
		return m.renderWorkspaceRow(row, selected)

	case RowCreate:
		style := m.styles.CreateButton
		if selected {
			style = m.styles.SelectedRow
		}
		return style.Render(" " + common.Icons.Add + " New Workspace ")

	case RowSectionHeader:
		style := lipgloss.NewStyle().Foreground(common.ColorMuted)
		return style.Render(" " + row.Label)

	case RowSpacer:
		return ""
	}

	return ""
}

// renderWorkspaceRow renders a 3-line workspace entry
func (m *Model) renderWorkspaceRow(row Row, selected bool) string {
	ws := row.Workspace
	if ws == nil {
		return ""
	}

	contentWidth := m.width - 3
	if contentWidth < 1 {
		contentWidth = 1
	}

	line1 := m.renderWorkspaceLine1(ws, selected, contentWidth)
	line2 := m.renderWorkspaceLine2(ws, selected, contentWidth)
	line3 := m.renderWorkspaceLine3(ws, selected, contentWidth)

	blankLine := ""
	if selected {
		bg := lipgloss.NewStyle().Background(common.ColorSelection)
		line1 = padWithBg(line1, contentWidth, bg)
		line2 = padWithBg(line2, contentWidth, bg)
		line3 = padWithBg(line3, contentWidth, bg)
		blankLine = padWithBg("", contentWidth, bg)
	}

	return line1 + "\n" + line2 + "\n" + line3 + "\n" + blankLine
}

// padWithBg right-pads a line to width using background-styled spaces.
func padWithBg(line string, width int, bg lipgloss.Style) string {
	w := lipgloss.Width(line)
	if w < width {
		return line + bg.Render(strings.Repeat(" ", width-w))
	}
	return line
}

// renderWorkspaceLine1: indicator + name + delete icon
func (m *Model) renderWorkspaceLine1(ws *data.Workspace, selected bool, contentWidth int) string {
	// Agent state indicator
	indicatorWidth := 2
	agentState := 0

	// Status-specific icon
	statusIcon := "●" // In Progress (default)
	switch ws.Status {
	case data.StatusBlocked:
		statusIcon = "⏸"
	case data.StatusMerged:
		statusIcon = "✓"
	case data.StatusArchived:
		statusIcon = "◇"
	}
	indicator := statusIcon + " "

	wsID := string(ws.ID())
	if state, hasAgents := m.workspaceAgentStates[wsID]; hasAgents {
		agentState = state
		if m.tmuxConfirmedActive[wsID] {
			indicator = common.SpinnerFrame(m.spinnerFrame) + " "
		}
	}

	// Status text for creating/deleting
	statusText := ""
	if m.deletingWorkspaces[ws.Root()] {
		frame := common.SpinnerFrame(m.spinnerFrame)
		pendingStyle := m.styles.StatusPending
		spaceStyle := lipgloss.NewStyle()
		if selected {
			pendingStyle = pendingStyle.Background(common.ColorSelection)
			spaceStyle = spaceStyle.Background(common.ColorSelection)
		}
		statusText = spaceStyle.Render(" ") + pendingStyle.Render(frame+" deleting")
	} else if _, ok := m.creatingWorkspaces[ws.Root()]; ok {
		frame := common.SpinnerFrame(m.spinnerFrame)
		pendingStyle := m.styles.StatusPending
		spaceStyle := lipgloss.NewStyle()
		if selected {
			pendingStyle = pendingStyle.Background(common.ColorSelection)
			spaceStyle = spaceStyle.Background(common.ColorSelection)
		}
		statusText = spaceStyle.Render(" ") + pendingStyle.Render(frame+" creating")
	}

	// Styles
	style := m.styles.WorkspaceRow
	if selected {
		style = lipgloss.NewStyle().Bold(true).Foreground(common.ColorForeground).Background(common.ColorSelection)
	}

	isCurrentWorkspace := ws.Root() == m.activeRoot
	hasUnread := m.unreadWorkspaces[wsID]

	// Icon color reflects workspace status
	iconFg := common.ColorSuccess // In Progress / None → green
	switch ws.Status {
	case data.StatusBlocked:
		iconFg = common.ColorError // Red
	case data.StatusMerged:
		iconFg = common.ColorPrimary // Blue
	case data.StatusArchived:
		iconFg = common.ColorMuted // Grey
	}
	// Override for unread notifications from another workspace
	if agentState >= 1 && hasUnread && !isCurrentWorkspace {
		iconFg = common.ColorWarning
	}
	iconStyle := lipgloss.NewStyle().Foreground(iconFg)
	if selected {
		iconStyle = iconStyle.Bold(true).Background(common.ColorSelection)
	}

	// Delete icon
	deleteSlot := "   "
	deleteSlotWidth := 3
	if selected {
		deleteSlot = " " + common.Icons.Close + " "
	}

	// Truncate name
	name := ws.Name
	prefixWidth := 2 + indicatorWidth // " " prefix + " " styled prefix + indicator
	maxNameWidth := contentWidth - lipgloss.Width(statusText) - deleteSlotWidth - prefixWidth
	if maxNameWidth > 0 && lipgloss.Width(name) > maxNameWidth {
		runes := []rune(name)
		for len(runes) > 0 && lipgloss.Width(string(runes)) > maxNameWidth-1 {
			runes = runes[:len(runes)-1]
		}
		name = string(runes) + "…"
	}

	if selected {
		m.deleteIconX = prefixWidth + lipgloss.Width(style.Render(name))
	}

	return style.Render(" ") + iconStyle.Render(indicator) + style.Render(name+deleteSlot) + statusText
}

// renderWorkspaceLine2: repo names + git changes
func (m *Model) renderWorkspaceLine2(ws *data.Workspace, selected bool, contentWidth int) string {
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(common.ColorSelection)
	}

	indent := bg.Render("  ")

	var parts []string

	// Repo names
	if len(ws.Repos) > 0 {
		const maxRepos = 4
		repoStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
		if selected {
			repoStyle = repoStyle.Background(common.ColorSelection)
		}
		var names []string
		limit := len(ws.Repos)
		if limit > maxRepos {
			limit = maxRepos
		}
		for _, repo := range ws.Repos[:limit] {
			names = append(names, repo.Name)
		}
		repoStr := strings.Join(names, ", ")
		if len(ws.Repos) > maxRepos {
			repoStr += fmt.Sprintf(" (+%d)", len(ws.Repos)-maxRepos)
		}
		parts = append(parts, repoStyle.Render(repoStr))
	}

	// Git changes summary
	root := ws.PrimaryWorktreeRoot()
	if status, ok := m.statusCache[root]; ok && status != nil && !status.Clean {
		gitSummary := formatGitSummary(status)
		if gitSummary != "" {
			gitStyle := lipgloss.NewStyle().Foreground(common.ColorWarning)
			if selected {
				gitStyle = gitStyle.Background(common.ColorSelection)
			}
			parts = append(parts, gitStyle.Render(gitSummary))
		}
	}

	sep := bg.Render(" ")
	return indent + strings.Join(parts, sep)
}

// renderWorkspaceLine3: directory path
func (m *Model) renderWorkspaceLine3(ws *data.Workspace, selected bool, contentWidth int) string {
	indent := "  "
	dir := shortenPath(ws.Root())

	style := lipgloss.NewStyle().Foreground(common.ColorMuted)
	if selected {
		style = style.Background(common.ColorSelection)
	}

	maxDirWidth := contentWidth - lipgloss.Width(indent)
	if maxDirWidth > 0 && lipgloss.Width(dir) > maxDirWidth {
		runes := []rune(dir)
		for len(runes) > 0 && lipgloss.Width(string(runes)) > maxDirWidth-1 {
			runes = runes[:len(runes)-1]
		}
		dir = string(runes) + "…"
	}

	return style.Render(indent) + style.Render(dir)
}

// shortenPath shortens a path for display, using ~ for home dir
func shortenPath(path string) string {
	if path == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) > 3 {
		return ".../" + strings.Join(parts[len(parts)-3:], string(filepath.Separator))
	}
	return path
}

func (m *Model) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

// helpLineCount returns the number of help lines that will be displayed.
func (m *Model) helpLineCount() int {
	if !m.showKeymapHints {
		return 0
	}
	contentWidth := m.width - 3
	if contentWidth < 1 {
		contentWidth = 1
	}
	return len(m.helpLines(contentWidth))
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		m.helpItem("k/↑", "up"),
		m.helpItem("j/↓", "down"),
		m.helpItem("enter", "open"),
	}
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		if m.rows[m.cursor].Type == RowWorkspace {
			items = append(items, m.helpItem("r", "rename"))
			items = append(items, m.helpItem("D", "delete"))
			items = append(items, m.helpItem("P", "profile"))
		}
	}
	items = append(items, m.helpItem("R", "refresh"))
	focusKey := "C-Spc h/j/k"
	if m.canFocusRight {
		focusKey = "C-Spc h/j/k/l"
	}
	items = append(items, m.helpItem(focusKey, "focus (or ←↑↓→)"))
	items = append(items,
		m.helpItem("C-Spc m", "monitor"),
		m.helpItem("C-Spc ?", "help"),
		m.helpItem("q", "quit"),
	)
	return common.WrapHelpItems(items, contentWidth)
}

// formatGitSummary returns a short summary of git changes, e.g. "3M 2A 1?"
func formatGitSummary(status *git.StatusResult) string {
	if status == nil || status.Clean {
		return ""
	}
	var parts []string
	staged := len(status.Staged)
	unstaged := len(status.Unstaged)
	untracked := len(status.Untracked)
	if staged > 0 {
		parts = append(parts, fmt.Sprintf("%d+", staged))
	}
	if unstaged > 0 {
		parts = append(parts, fmt.Sprintf("%dM", unstaged))
	}
	if untracked > 0 {
		parts = append(parts, fmt.Sprintf("%d?", untracked))
	}
	return strings.Join(parts, " ")
}
