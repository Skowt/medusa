package review

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/ui/common"
)

// Layout constants. The file list is fixed-width so the diff — the part that
// actually needs room — keeps every column the terminal can spare.
const (
	filesPaneWidth = 32
	minFilesWidth  = 22
	chromeRows     = 6 // title, header row, separator, footer, two borders
)

// filesWidth is the left pane's width, shrunk on narrow terminals.
func (m *Model) filesWidth() int {
	if m.width < filesPaneWidth*2 {
		return clamp(m.width/3, minFilesWidth, filesPaneWidth)
	}
	//nolint:gocritic // fixed width above the narrow-terminal threshold.
	return filesPaneWidth
}

// frameChrome is what the border and padding cost on each side: one column of
// border plus one of padding, twice over.
const frameChrome = 4

// bodyWidth is the room the two panes actually get. Deriving it from m.width
// without subtracting the frame made the window render four columns wider than
// it was sized, which on a tight terminal clips its own right border off.
func (m *Model) bodyWidth() int {
	return maxInt(10, m.width-frameChrome)
}

// diffPaneWidth is what is left for the diff after the list and the divider.
func (m *Model) diffPaneWidth() int {
	return maxInt(20, m.bodyWidth()-m.filesWidth()-3)
}

// paneHeight is how many content rows each pane gets.
func (m *Model) paneHeight() int {
	return maxInt(1, m.height-chromeRows)
}

// View renders the overlay.
func (m *Model) View() string {
	m.fileRows = rowMap{}
	m.paneRowsMap = rowMap{}
	m.saveHit, m.discardHit = common.HitRegion{}, common.HitRegion{}

	if !m.loaded {
		return m.frame(centered(m.bodyWidth(), m.paneHeight(), "Loading changes…"))
	}
	if m.err != nil {
		return m.frame(centered(m.bodyWidth(), m.paneHeight(),
			lipgloss.NewStyle().Foreground(common.ColorError).Render("Error: "+m.err.Error())))
	}
	if len(m.files) == 0 {
		return m.frame(centered(m.bodyWidth(), m.paneHeight(), "No changes to review."))
	}

	left := m.renderFileList()
	right := m.renderRightPane()
	divider := m.renderDivider()
	return m.frame(lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right))
}

// frame wraps the body in the titled border and the footer.
func (m *Model) frame(body string) string {
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(common.ColorPrimary).
		// lipgloss Width() is the *total* rendered width, border and padding
		// included, so this is m.width and the panes get bodyWidth inside it.
		Width(maxInt(frameChrome+1, m.width)).
		Padding(0, 1)
	return border.Render(m.renderTitle() + "\n" + body + "\n" + m.renderFooter())
}

// renderTitle is the header line: workspace branch plus the totals.
func (m *Model) renderTitle() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(common.ColorPrimary)
	mutedStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)

	title := titleStyle.Render("Review Changes")
	if m.workspace != nil {
		title += mutedStyle.Render("  " + m.workspace.Branch())
	}
	added, deleted := 0, 0
	for _, f := range m.files {
		added += f.Added
		deleted += f.Deleted
	}
	title += mutedStyle.Render(fmt.Sprintf("  ·  %s  ", plural(len(m.files), "file", "files")))
	title += lipgloss.NewStyle().Foreground(common.ColorSuccess).Render("+" + strconv.Itoa(added))
	title += " " + lipgloss.NewStyle().Foreground(common.ColorError).Render("−"+strconv.Itoa(deleted))

	// The window tracks the agent, so say so — otherwise a diff changing under
	// the reader looks like the window glitching rather than like work landing.
	if m.loading {
		title += lipgloss.NewStyle().Foreground(common.ColorInfo).Render("  ↻ updating")
	} else {
		title += mutedStyle.Render("  ↻ live")
	}
	return title
}

// renderDivider is the vertical rule between the panes.
func (m *Model) renderDivider() string {
	rule := lipgloss.NewStyle().Foreground(common.ColorSurface2).Render(" │ ")
	rows := make([]string, m.paneHeight())
	for i := range rows {
		rows[i] = rule
	}
	return strings.Join(rows, "\n")
}

// renderFileList draws the left pane.
func (m *Model) renderFileList() string {
	width := m.filesWidth()
	height := m.paneHeight()
	m.followFileCursor(height)

	mutedStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	rows := []string{mutedStyle.Render(pad("Files", width))}

	// The header occupies the first body row, so the list starts one below it.
	m.fileRows = rowMap{top: bodyTop + 1}
	end := minInt(m.filesTop+height-1, len(m.files))
	for i := m.filesTop; i < end; i++ {
		rows = append(rows, m.renderFileRow(i, width))
		m.fileRows.items = append(m.fileRows.items, i)
	}
	for len(rows) < height {
		rows = append(rows, strings.Repeat(" ", width))
	}
	return joinPaneRows(rows, width)
}

// renderFileRow draws one file entry: status code, name, markers, counts.
func (m *Model) renderFileRow(idx, width int) string {
	entry := m.files[idx]
	selected := idx == m.cursor

	code := entry.Change.DisplayCode()
	added, deleted := m.liveCounts(&entry)
	counts := ""
	if added > 0 {
		counts += "+" + strconv.Itoa(added)
	}
	if deleted > 0 {
		if counts != "" {
			counts += " "
		}
		counts += "−" + strconv.Itoa(deleted)
	}

	markers := ""
	if n := len(m.comments[entry.Path()]); n > 0 {
		markers += "*" + strconv.Itoa(n)
	}
	if buf, ok := m.edits[entry.Path()]; ok && buf.Dirty() {
		if markers != "" {
			markers += " "
		}
		markers += "~"
	}
	if entry.EditConflict {
		markers += "!"
	}

	// Widths here must be measured in columns, not bytes: the deletion count
	// uses "−" (U+2212), three bytes wide and one column, so len() would
	// reserve two columns too many for every file that deleted a line.
	countsWidth := lipgloss.Width(counts)

	// Name gets whatever the fixed columns leave, truncated from the left so
	// the basename — the part that identifies the file — always survives.
	fixed := lipgloss.Width(code) + 2 + countsWidth + 2
	if markers != "" {
		fixed += lipgloss.Width(markers) + 1
	}
	name := truncateLeft(entry.Path(), maxInt(4, width-fixed))

	line := " " + code + " " + name
	if markers != "" {
		line += " " + markers
	}
	line = pad(line, maxInt(0, width-countsWidth-1)) + counts + " "

	style := lipgloss.NewStyle()
	switch {
	case selected && m.focus == paneFiles:
		style = style.Bold(true).Foreground(common.ColorBackground).Background(common.ColorPrimary)
	case selected:
		style = style.Bold(true).Foreground(common.ColorPrimary)
	case entry.EditConflict:
		style = style.Foreground(common.ColorError)
	case entry.Gone:
		style = style.Foreground(common.ColorMuted).Strikethrough(true)
	default:
		style = style.Foreground(common.ColorForeground)
	}
	return style.Render(pad(line, width))
}

// followFileCursor keeps the file cursor inside its viewport.
func (m *Model) followFileCursor(height int) {
	rows := height - 1 // the "Files" header
	if m.cursor < m.filesTop {
		m.filesTop = m.cursor
	}
	if m.cursor >= m.filesTop+rows {
		m.filesTop = m.cursor - rows + 1
	}
	m.filesTop = clamp(m.filesTop, 0, maxInt(0, len(m.files)-rows))
}

// renderFooter is the button row and the hint line.
func (m *Model) renderFooter() string {
	mutedStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)

	saveStyle := lipgloss.NewStyle().Bold(true).
		Foreground(common.ColorBackground).Background(common.ColorSuccess)
	if !m.HasFeedback() {
		// Nothing to send: show it as unavailable rather than hiding it, so the
		// button does not move around as comments come and go.
		saveStyle = lipgloss.NewStyle().Foreground(common.ColorMuted)
	}
	save := saveStyle.Render(" Save & Send ")
	discard := lipgloss.NewStyle().Foreground(common.ColorError).Render(" Discard ")

	footerY := bodyTop + m.paneHeight()
	m.saveHit = common.HitRegion{
		X: frameLeft, Y: footerY, Width: lipgloss.Width(save), Height: 1,
	}
	m.discardHit = common.HitRegion{
		X: frameLeft + lipgloss.Width(save) + 1, Y: footerY,
		Width: lipgloss.Width(discard), Height: 1,
	}

	status := m.statusMsg
	if status == "" {
		status = m.keyHint()
	}
	left := save + " " + discard + "  " + mutedStyle.Render(status)
	return clip(left, m.bodyWidth())
}

// keyHint is the context-sensitive help line.
func (m *Model) keyHint() string {
	switch m.focus {
	case paneComment:
		return "ctrl+enter attach · esc cancel · enter newline"
	case paneEdit:
		return "editing · esc back to diff · ctrl+enter save & send"
	case paneFiles:
		return "j/k file · tab diff · ctrl+enter send · ctrl+d discard · esc close"
	default:
		return "j/k line · n/p hunk · c comment · e edit · d delete · ctrl+enter send"
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := lipgloss.Width(s)
	if w >= width {
		return clip(s, width)
	}
	return s + strings.Repeat(" ", width-w)
}

// clip cuts a string to a display width, ANSI-aware so styled text is never
// severed mid-escape.
func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// truncateLeft drops the front of a path, which is where the redundant
// directories live, and marks the cut.
func truncateLeft(s string, width int) string {
	if width <= 1 || len(s) <= width {
		return s
	}
	return "…" + s[len(s)-width+1:]
}

func centered(width, height int, body string) string {
	return lipgloss.Place(maxInt(1, width), maxInt(1, height),
		lipgloss.Center, lipgloss.Center, body)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
