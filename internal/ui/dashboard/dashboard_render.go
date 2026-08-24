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

	case RowNewGroup:
		contentWidth := m.width - 3
		if contentWidth < 1 {
			contentWidth = 1
		}
		return m.renderNewGroupRow(contentWidth)

	case RowSpacer:
		return ""
	}

	return ""
}

// renderNewGroupRow renders the "New group" drop target: muted while the drag is
// elsewhere, accented once the workspace is projected into it, so the row reads
// as the outcome rather than as a button.
func (m *Model) renderNewGroupRow(contentWidth int) string {
	style := lipgloss.NewStyle().Foreground(common.ColorMuted)
	if m.drag.placeNewGroup {
		style = style.Foreground(common.ColorSecondary).Bold(true)
	}
	line := style.Render(" " + common.Icons.Add + " New group")
	if m.drag.placeNewGroup {
		return padWithBg(line, contentWidth, lipgloss.NewStyle())
	}
	return line
}

// renderWorkspaceRow renders an active workspace entry: wrapped name line(s),
// metadata, an optional repo list, and (when selected) an action-button footer.
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

	if selected {
		m.wsButtonHits = nil
	}

	lines := m.renderWorkspaceNameLines(ws, selected, contentWidth)
	lines = append(lines, m.renderWorkspaceLine2(ws, selected, contentWidth))
	if selected && len(ws.Repos) >= 2 {
		if line3 := m.renderWorkspaceLine3(ws, contentWidth); line3 != "" {
			lines = append(lines, line3)
		}
	}
	if selected {
		footerLine := len(lines)
		for _, h := range footerButtonHits() {
			h.line += footerLine
			m.wsButtonHits = append(m.wsButtonHits, h)
		}
		lines = append(lines, m.renderFooterLine())
	}

	if m.isHoveredWorkspace(ws) && len(lines) > 0 {
		bg := lipgloss.NewStyle()
		if selected {
			bg = bg.Background(common.ColorSelection)
		}
		lines[0] = withDragHandle(lines[0], contentWidth, bg)
	}

	if selected {
		bg := lipgloss.NewStyle().Background(common.ColorSelection)
		for i := range lines {
			lines[i] = padWithBg(lines[i], contentWidth, bg)
		}
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

	nameW := contentWidth - 6 // " " + "⚠ " + name + "   "
	if nameW < 1 {
		nameW = 1
	}
	prefix := bg.Render(" ") + warnStyle.Render("⚠ ")
	name := nameStyle.Render(truncateRunes([]rune(ws.Name), nameW))
	line1 := prefix + name + nameStyle.Render(deleteSlot)

	if selected {
		// Orphan rows expose only the delete action (× hard-deletes the orphan).
		x0 := lipgloss.Width(prefix) + lipgloss.Width(name) + 1 // skip leading space in " × "
		m.wsButtonHits = []wsButtonHit{{action: btnArchive, line: 0, x0: x0, x1: x0 + lipgloss.Width(common.Icons.Close)}}
	}

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

	nameW := contentWidth - 6 // " " + "◇ " + name + "   "
	if nameW < 1 {
		nameW = 1
	}
	prefix := bg.Render(" ") + iconStyle.Render("◇ ")
	name := nameStyle.Render(truncateRunes([]rune(ws.Name), nameW))
	line := prefix + name + nameStyle.Render(deleteSlot)

	if selected {
		// Archived rows expose only the delete action (× hard-deletes). Record
		// its column so the click handler maps a click on the "×" to the
		// delete action; without this a click would read a stale hit from
		// whatever row was last rendered.
		x0 := lipgloss.Width(prefix) + lipgloss.Width(name) + 1 // skip leading space in " × "
		m.wsButtonHits = []wsButtonHit{{action: btnArchive, line: 0, x0: x0, x1: x0 + lipgloss.Width(common.Icons.Close)}}

		bgStyle := lipgloss.NewStyle().Background(common.ColorSelection)
		line = padWithBg(line, contentWidth, bgStyle)
	}

	return line
}

// dragHandle is the grip glyph: shown at the right edge of a hovered row to
// advertise that it can be dragged, and in place of the status indicator on the
// row actually being carried.
const dragHandle = "⠿"

// withDragHandle right-aligns the drag handle on a row's first line. It pads to
// width - handle first, so the handle sits flush at the right edge and the line
// comes back exactly width wide — any later padWithBg is then a no-op and cannot
// push the handle out of alignment.
//
// A line that still fills the width is clipped to make room rather than losing
// its handle: workspace rows keep handleGutter clear so this never bites them,
// but a long group label has no such reservation, and a handle that silently
// disappears on the rows that need it most is worse than one truncated glyph of
// label. Clipping goes through MaxWidth because these lines carry ANSI styling
// that plain slicing would cut mid-sequence.
//
// The handle is drawn in the theme's primary color, never a Surface one. Surface
// tokens are background tiers: Surface3 on the dark themes is #292e42 against a
// #1a1b26 background, so the handle rendered correctly and was invisible. The
// glyph is also sparse — six braille dots, not a solid block — which costs it
// more perceived contrast than its nominal ratio suggests, and it has one job,
// which is to be noticed. It only ever appears on the row under the pointer, so
// it cannot accumulate into noise. Primary is the "this is interactive" accent;
// the secondary accent is reserved for a drag actually in flight.
func withDragHandle(line string, width int, bg lipgloss.Style) string {
	handleWidth := lipgloss.Width(dragHandle)
	if width <= handleWidth {
		return line
	}
	room := width - handleWidth
	if lipgloss.Width(line) > room {
		line = lipgloss.NewStyle().MaxWidth(room).Render(line)
	}
	return padWithBg(line, room, bg) + bg.Foreground(common.ColorPrimary).Render(dragHandle)
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
			if ws := m.rows[m.cursor].Workspace; m.draggableWorkspace(ws) {
				items = append(items, m.helpItem("drag", "reorder"))
			}
		}
		if m.rows[m.cursor].Type == RowSectionHeader && m.rows[m.cursor].IsUserGroup {
			items = append(items,
				m.helpItem("enter/space", "toggle"),
				m.helpItem("r", "rename"),
				m.helpItem("D", "delete"),
				m.helpItem("drag", "reorder"),
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

	// The marker takes the header's leading space rather than being prepended to
	// it, so the label stays put and the row keeps its width.
	key := labelToKey(row.Label)
	marker := " "
	if row.DragLifted {
		// The section being carried takes the grip and an accent, and nothing
		// else: the chevron still reports its real collapse state, because a
		// drag must not look like — or turn into — a collapse.
		marker = dragHandle
		style = style.Foreground(common.ColorSecondary)
	}

	line := style.Render(marker + chevron + label)
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(common.ColorSelection)
	}
	if m.isHoveredGroup(key) {
		line = withDragHandle(line, contentWidth, bg)
	}
	if selected {
		return padWithBg(line, contentWidth, bg)
	}
	return line
}
