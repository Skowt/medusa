package dashboard

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/ui/common"
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

	case RowQuickDuplicate:
		style := m.styles.CreateButton
		if selected {
			style = m.styles.SelectedRow
		}
		return "\n" + style.Render(" "+common.Icons.Add+" Quick Duplicate ")

	case RowCreateGroup:
		style := m.styles.CreateButton
		if selected {
			style = m.styles.SelectedRow
		}
		return style.Render("   " + common.Icons.Add + " New Group ")

	case RowSectionHeader:
		style := lipgloss.NewStyle().Foreground(common.ColorPrimary).Bold(true)
		contentWidth := m.width - 3
		if contentWidth < 1 {
			contentWidth = 1
		}
		if row.Label == "archived" {
			sep := lipgloss.NewStyle().Foreground(common.ColorSurface2).Render(strings.Repeat("─", contentWidth))
			return sep + "\n" + style.Render(" "+row.Label)
		}
		if row.Label == "archived-footer" {
			sep := lipgloss.NewStyle().Foreground(common.ColorSurface2).Render(strings.Repeat("─", contentWidth))
			return "\n" + sep
		}
		return style.Render(" " + row.Label)

	case RowGroupHeader:
		return m.renderGroupHeader(row, selected)

	case RowSpacer:
		return ""
	}

	return ""
}

// renderGroupHeader renders a user-defined collapsible group header.
// Indented under the repo header; shows ▼/▶ + name, plus (N) when collapsed.
func (m *Model) renderGroupHeader(row Row, selected bool) string {
	icon := common.Icons.DirOpen
	if !row.GroupExpanded {
		icon = common.Icons.DirClosed
	}
	label := row.GroupName
	if !row.GroupExpanded && row.GroupCount > 0 {
		label = fmt.Sprintf("%s (%d)", label, row.GroupCount)
	}
	style := lipgloss.NewStyle().Foreground(common.ColorSecondary)
	if selected {
		style = style.Bold(true).Background(common.ColorSelection).Foreground(common.ColorForeground)
	}
	line := style.Render("   " + icon + " " + label)
	if selected {
		contentWidth := m.width - 3
		if contentWidth < 1 {
			contentWidth = 1
		}
		bgStyle := lipgloss.NewStyle().Background(common.ColorSelection)
		line = padWithBg(line, contentWidth, bgStyle)
	}
	return line
}

// renderWorkspaceRow renders a 2-line workspace entry
func (m *Model) renderWorkspaceRow(row Row, selected bool) string {
	ws := row.Workspace
	if ws == nil {
		return ""
	}

	contentWidth := m.width - 3
	if contentWidth < 1 {
		contentWidth = 1
	}

	// Grouped workspaces are indented by 2 columns under their group header.
	indent := ""
	if row.GroupName != "" {
		indent = "  "
		contentWidth -= 2
		if contentWidth < 1 {
			contentWidth = 1
		}
	}

	// Orphaned workspaces get a distinct rendering
	if ws.IsOrphaned() {
		return prefixIndent(m.renderOrphanRow(ws, selected, contentWidth), indent, selected)
	}

	// Archived workspaces get single-line rendering
	if ws.Archived() {
		return prefixIndent(m.renderArchivedRow(ws, selected, contentWidth), indent, selected)
	}

	line1 := m.renderWorkspaceLine1(ws, selected, contentWidth)
	line2 := m.renderWorkspaceLine2(ws, selected, contentWidth)

	if selected {
		bg := lipgloss.NewStyle().Background(common.ColorSelection)
		line1 = padWithBg(line1, contentWidth, bg)
		line2 = padWithBg(line2, contentWidth, bg)
	}

	return prefixIndent(line1+"\n"+line2, indent, selected)
}

// prefixIndent prepends indent to every line. When selected, the indent columns
// are rendered with the selection background so the highlight extends to the left edge.
func prefixIndent(block, indent string, selected bool) string {
	if indent == "" {
		return block
	}
	style := lipgloss.NewStyle()
	if selected {
		style = style.Background(common.ColorSelection)
	}
	rendered := style.Render(indent)
	lines := strings.Split(block, "\n")
	for i := range lines {
		lines[i] = rendered + lines[i]
	}
	return strings.Join(lines, "\n")
}

// renderOrphanRow renders a 2-line orphaned workspace entry.
func (m *Model) renderOrphanRow(ws *data.Workspace, selected bool, contentWidth int) string {
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(common.ColorSelection)
	}

	// Line 1: warning icon + name + delete icon
	warnStyle := lipgloss.NewStyle().Foreground(common.ColorError)
	nameStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	if selected {
		warnStyle = warnStyle.Bold(true).Background(common.ColorSelection)
		nameStyle = nameStyle.Bold(true).Background(common.ColorSelection)
	}

	deleteSlot := "   "
	if selected {
		deleteSlot = " " + common.Icons.Close + " "
	}

	line1 := bg.Render(" ") + warnStyle.Render("⚠ ") + nameStyle.Render(ws.Name) + nameStyle.Render(deleteSlot)

	// Line 2: description of orphan type
	arrowStyle := lipgloss.NewStyle().Foreground(common.ColorSurface2)
	mutedStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	if selected {
		arrowStyle = arrowStyle.Background(common.ColorSelection)
		mutedStyle = mutedStyle.Background(common.ColorSelection)
	}

	desc := "worktree missing"
	if ws.Orphan == data.OrphanDirectory {
		desc = "no metadata (directory orphan)"
	}

	indent := bg.Render(" ") + arrowStyle.Render("└ ")
	line2 := indent + mutedStyle.Render(desc)

	if selected {
		bgStyle := lipgloss.NewStyle().Background(common.ColorSelection)
		line1 = padWithBg(line1, contentWidth, bgStyle)
		line2 = padWithBg(line2, contentWidth, bgStyle)
	}

	return line1 + "\n" + line2
}

// renderArchivedRow renders a single-line archived workspace entry.
func (m *Model) renderArchivedRow(ws *data.Workspace, selected bool, contentWidth int) string {
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(common.ColorSelection)
	}

	iconStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	nameStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	if selected {
		iconStyle = iconStyle.Bold(true).Background(common.ColorSelection)
		nameStyle = nameStyle.Bold(true).Background(common.ColorSelection)
	}

	deleteSlot := "   "
	if selected {
		deleteSlot = " " + common.Icons.Close + " "
	}

	prefix := bg.Render(" ") + iconStyle.Render("◇ ")
	name := nameStyle.Render(ws.Name)
	line := prefix + name + nameStyle.Render(deleteSlot)

	if selected {
		// Record the delete icon column for this row so the click handler in
		// model.go can map a click on the "×" back to the delete action.
		// Without this, handleClick reads a stale deleteIconX from whatever
		// non-archived row was last rendered and clicks on the archived row's
		// × either miss entirely or hit the wrong column.
		m.deleteIconX = lipgloss.Width(prefix) + lipgloss.Width(name)

		bgStyle := lipgloss.NewStyle().Background(common.ColorSelection)
		line = padWithBg(line, contentWidth, bgStyle)
	}

	return line
}

// padWithBg right-pads a line to width using background-styled spaces.
func padWithBg(line string, width int, bg lipgloss.Style) string {
	w := lipgloss.Width(line)
	if w < width {
		return line + bg.Render(strings.Repeat(" ", width-w))
	}
	return line
}

// renderWorkspaceLine1: hook indicator + name + delete icon
func (m *Model) renderWorkspaceLine1(ws *data.Workspace, selected bool, contentWidth int) string {
	indicatorWidth := 2

	wsID := string(ws.ID())

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

	// Default indicator based on workspace status
	indicator := "●"
	indicatorFg := common.ColorSuccess
	switch ws.Status {
	case data.StatusBlocked:
		indicator = common.Icons.Blocked
		indicatorFg = common.ColorError
	case data.StatusReview:
		indicator = common.Icons.Pending
		indicatorFg = common.ColorSecondary
	case data.StatusMerged:
		indicator = common.Icons.Completed
		indicatorFg = common.ColorPrimary
	}

	// Hook-based activity overrides: spinner on PreToolUse, warning symbols for notifications
	if hookState, ok := m.hookStates[wsID]; ok {
		switch hookState {
		case "PreToolUse", "PostToolUse", "UserPromptSubmit":
			indicator = common.SpinnerFrame(m.spinnerFrame)
			indicatorFg = common.ColorSuccess
		case "NotificationPermission", "NotificationElicitation", "PermissionRequest":
			indicator = "!"
			indicatorFg = common.ColorWarning
		}
	}

	// Override for unread workspaces: make indicator and name orange
	isCurrentWorkspace := ws.Root() == m.activeRoot
	hasUnread := m.unreadWorkspaces[wsID] && !isCurrentWorkspace

	if hasUnread {
		indicatorFg = common.ColorWarning
	}

	iconStyle := lipgloss.NewStyle().Foreground(indicatorFg)
	if selected {
		iconStyle = iconStyle.Bold(true).Background(common.ColorSelection)
	}
	renderedIndicator := iconStyle.Render(indicator + " ")

	// Styles
	style := m.styles.WorkspaceRow
	if hasUnread {
		style = lipgloss.NewStyle().Foreground(common.ColorWarning)
	}
	if selected {
		style = lipgloss.NewStyle().Bold(true).Foreground(common.ColorForeground).Background(common.ColorSelection)
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

	return style.Render(" ") + renderedIndicator + style.Render(name) + style.Render(deleteSlot) + statusText
}

// renderWorkspaceLine2: profile · git changes · created day
func (m *Model) renderWorkspaceLine2(ws *data.Workspace, selected bool, contentWidth int) string {
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(common.ColorSelection)
	}

	mutedStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	if selected {
		mutedStyle = mutedStyle.Background(common.ColorSelection)
	}

	arrowStyle := lipgloss.NewStyle().Foreground(common.ColorSurface2)
	if selected {
		arrowStyle = arrowStyle.Background(common.ColorSelection)
	}

	indent := bg.Render(" ") + arrowStyle.Render("└ ")

	var parts []string

	// Profile name
	profileName := "Default"
	if ws.Profile != "" {
		profileName = ws.Profile
	}
	parts = append(parts, mutedStyle.Render(profileName))

	// Git changes summary
	root := ws.PrimaryWorktreeRoot()
	if status, ok := m.statusCache[root]; ok && status != nil && !status.Clean {
		gitSummary := formatGitSummary(status)
		if gitSummary != "" {
			gitStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
			if selected {
				gitStyle = gitStyle.Background(common.ColorSelection)
			}
			parts = append(parts, gitStyle.Render(gitSummary))
		}
	} else {
		parts = append(parts, mutedStyle.Render("Clean"))
	}

	// Created day (e.g. "Mon")
	if !ws.Created.IsZero() {
		parts = append(parts, mutedStyle.Render(ws.Created.Format("Mon")))
	}

	sep := mutedStyle.Render(" · ")
	return indent + strings.Join(parts, sep)
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
		switch m.rows[m.cursor].Type {
		case RowWorkspace:
			items = append(items, m.helpItem("r", "rename"))
			ws := m.rows[m.cursor].Workspace
			if ws != nil && (ws.Archived() || ws.IsOrphaned()) {
				items = append(items, m.helpItem("D", "delete"))
			} else {
				items = append(items, m.helpItem("D", "archive"))
				items = append(items, m.helpItem("g", "group"))
			}
			items = append(items, m.helpItem("P", "profile"))
		case RowGroupHeader:
			items = append(items,
				m.helpItem("enter/l/h", "toggle"),
				m.helpItem("r", "rename"),
				m.helpItem("D", "delete"),
			)
		}
	}
	items = append(items, m.helpItem("N", "new group"))
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
