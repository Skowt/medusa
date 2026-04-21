package dashboard

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/data"
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

	case RowSectionHeader:
		contentWidth := m.width - 3
		if contentWidth < 1 {
			contentWidth = 1
		}
		if row.IsUserGroup {
			return m.renderUserGroupHeader(row, selected, contentWidth)
		}
		// Non-interactive drawer headers (archived/orphans) — existing behavior.
		style := lipgloss.NewStyle().Foreground(common.ColorPrimary).Bold(true)
		if row.Label == "archived" {
			sep := lipgloss.NewStyle().Foreground(common.ColorSurface2).Render(strings.Repeat("─", contentWidth))
			return sep + "\n" + style.Render(" "+row.Label)
		}
		if row.Label == "archived-footer" {
			sep := lipgloss.NewStyle().Foreground(common.ColorSurface2).Render(strings.Repeat("─", contentWidth))
			return "\n" + sep
		}
		return style.Render(" " + row.Label)

	case RowSpacer:
		return ""
	}

	return ""
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

	// Orphaned workspaces get a distinct rendering
	if ws.IsOrphaned() {
		return m.renderOrphanRow(ws, selected, contentWidth)
	}

	// Archived workspaces get single-line rendering
	if ws.Archived() {
		return m.renderArchivedRow(ws, selected, contentWidth)
	}

	line1 := m.renderWorkspaceLine1(ws, selected, contentWidth)
	line2 := m.renderWorkspaceLine2(ws, selected, contentWidth)

	if selected {
		bg := lipgloss.NewStyle().Background(common.ColorSelection)
		line1 = padWithBg(line1, contentWidth, bg)
		line2 = padWithBg(line2, contentWidth, bg)
	}

	if selected && len(ws.Repos) >= 2 {
		line3 := m.renderWorkspaceLine3(ws, contentWidth)
		if line3 != "" {
			return line1 + "\n" + line2 + "\n" + line3
		}
	}

	return line1 + "\n" + line2
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
		// Reset duplicate/group icon positions since orphan rows don't expose them
		m.duplicateIconX = 0
		m.groupIconX = 0
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
		// Reset duplicate/group icon positions since archived rows don't expose them
		m.duplicateIconX = 0
		m.groupIconX = 0
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
			ws := m.rows[m.cursor].Workspace
			if ws != nil && (ws.Archived() || ws.IsOrphaned()) {
				items = append(items, m.helpItem("D", "delete"))
			} else {
				items = append(items, m.helpItem("D", "archive"))
			}
			items = append(items, m.helpItem("P", "profile"))
			items = append(items, m.helpItem("g", "group"))
			items = append(items, m.helpItem("+", "duplicate"))
		}
		if m.rows[m.cursor].Type == RowSectionHeader && m.rows[m.cursor].IsUserGroup {
			items = append(items,
				m.helpItem("enter/space", "toggle"),
				m.helpItem("r", "rename"),
				m.helpItem("D", "delete"),
			)
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

// renderUserGroupHeader renders a collapsible user-group header with a chevron
// and a "(N)" member count when collapsed.
func (m *Model) renderUserGroupHeader(row Row, selected bool, contentWidth int) string {
	chevron := "▾ "
	if row.Collapsed {
		chevron = "▸ "
	}

	style := lipgloss.NewStyle().Foreground(common.ColorPrimary).Bold(true)
	if selected {
		style = style.Background(common.ColorSelection)
	}

	label := row.Label
	if row.Collapsed && row.MemberCount > 0 {
		label = fmt.Sprintf("%s (%d)", label, row.MemberCount)
	}

	line := style.Render(" " + chevron + label)
	if selected {
		return padWithBg(line, contentWidth, lipgloss.NewStyle().Background(common.ColorSelection))
	}
	return line
}
