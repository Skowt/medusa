package review

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/ui/common"
)

// gutterWidth is the fixed width of the "1234 +" column.
const gutterWidth = 7

// renderRightPane draws the diff, or the edit buffer when one is open.
func (m *Model) renderRightPane() string {
	entry := m.Selected()
	if entry == nil {
		return ""
	}
	if m.focus == paneEdit {
		return m.renderEditPane(entry)
	}
	return m.renderDiffPane(entry)
}

// renderDiffPane draws the selected file's diff with its comments inlined.
func (m *Model) renderDiffPane(entry *fileEntry) string {
	width := m.diffPaneWidth()
	height := m.paneHeight()
	diff := m.displayDiff(entry)

	rows := []string{m.renderDiffHeader(entry, width)}
	// The pane's own header takes the first body row; the diff starts below it.
	m.paneRowsMap = rowMap{top: bodyTop + 1}

	switch {
	case entry.Err != nil:
		rows = append(rows, lipgloss.NewStyle().Foreground(common.ColorError).
			Render("  Could not load diff: "+entry.Err.Error()))
	case entry.Gone:
		// The file left the change set while the window was open. Its notes are
		// the only reason the row is still here, so they have to be what the
		// pane shows — otherwise keeping the file is indistinguishable from
		// having dropped it.
		rows = append(rows, lipgloss.NewStyle().Foreground(common.ColorMuted).
			Render("  No longer changed — the agent reverted this file."))
		rows = append(rows, "")
		rows = append(rows, m.renderOrphanedComments(entry, width)...)
	case diff == nil, len(diff.Lines) == 0, diff.Empty:
		rows = append(rows, lipgloss.NewStyle().Foreground(common.ColorMuted).Render("  No changes"))
	case diff.Binary:
		rows = append(rows, lipgloss.NewStyle().Foreground(common.ColorMuted).Render("  Binary file"))
	case diff.Large:
		rows = append(rows, lipgloss.NewStyle().Foreground(common.ColorMuted).Render("  File too large to display"))
	default:
		rows = append(rows, m.renderDiffRows(entry, width, height-1)...)
	}

	for len(rows) < height {
		rows = append(rows, "")
	}
	return joinPaneRows(rows[:height], width)
}

// joinPaneRows pads every row to exactly the pane width before joining.
//
// A single row wider than the pane makes lipgloss.JoinHorizontal size the whole
// block to it, and the outer frame then wraps every line that no longer fits —
// the file list and the diff end up interleaved. Padding short rows matters just
// as much: without it the frame sees a ragged block and the divider bends.
func joinPaneRows(rows []string, width int) string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = padTo(row, width)
	}
	return strings.Join(out, "\n")
}

// padTo forces a styled string to exactly width columns, clipping or padding.
func padTo(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = clip(s, width)
	if gap := width - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// renderDiffHeader names the file and its counts above the diff.
func (m *Model) renderDiffHeader(entry *fileEntry, width int) string {
	name := lipgloss.NewStyle().Bold(true).Foreground(common.ColorForeground).
		Render(truncateLeft(entry.Path(), maxInt(10, width-24)))
	added, deleted := m.liveCounts(entry)
	counts := lipgloss.NewStyle().Foreground(common.ColorSuccess).Render("+"+strconv.Itoa(added)) +
		" " + lipgloss.NewStyle().Foreground(common.ColorError).Render("−"+strconv.Itoa(deleted))
	if buf := m.edits[entry.Path()]; buf != nil && buf.Dirty() {
		counts += lipgloss.NewStyle().Foreground(common.ColorWarning).Render("  unsaved")
	}
	return clip(name+"  "+counts, width)
}

// renderDiffRows renders the visible slice of the pane.
//
// It walks paneRows, the same list the cursor moves over, so what is drawn and
// what is selectable cannot drift apart. Building the display from the diff
// while navigating something else is how a comment ends up visible but
// unreachable.
func (m *Model) renderDiffRows(entry *fileEntry, width, height int) []string {
	all := m.paneRows()
	notes := m.comments[entry.Path()]

	var rows []string
	// track attributes every row appended since the last call to one paneRow,
	// so a click resolves to the same item the cursor would. A comment wraps to
	// as many rows as its text needs, which is why this counts rows rather than
	// assuming one apiece.
	track := func(item int) {
		for len(m.paneRowsMap.items) < len(rows) {
			m.paneRowsMap.items = append(m.paneRowsMap.items, item)
		}
	}

	for i := m.diffTop; i < len(all) && len(rows) < height; i++ {
		row := all[i]
		selected := i == m.diffLine && m.focus == paneDiff

		if row.Kind == rowComment {
			if row.CommentIdx >= 0 && row.CommentIdx < len(notes) {
				rows = append(rows, m.renderCommentCard(notes[row.CommentIdx], width, selected)...)
			}
			track(i)
			continue
		}
		rows = append(rows, m.renderDiffLine(row.Line, i, width))
		track(i)
		if m.focus == paneComment && i == m.diffLine && m.commentArea != nil {
			rows = append(rows, m.renderCommentEditor(width)...)
			track(i)
		}
	}
	return rows
}

// renderDiffLine draws one row: gutter, marker, content.
//
// Added and deleted rows are tinted across the full pane width rather than just
// behind their text, so a block of changes reads as a block at a glance instead
// of as a ragged right edge.
func (m *Model) renderDiffLine(line git.DiffLine, idx, width int) string {
	var (
		marker  string
		style   = lipgloss.NewStyle().Foreground(common.ColorForeground)
		gutterS = lipgloss.NewStyle().Foreground(common.ColorMuted)
	)
	switch line.Kind {
	case git.DiffLineAdd:
		marker = "+"
		style = lipgloss.NewStyle().Foreground(common.ColorSuccess)
		gutterS = style
	case git.DiffLineDelete:
		marker = "-"
		style = lipgloss.NewStyle().Foreground(common.ColorError)
		gutterS = style
	case git.DiffLineHeader:
		marker = " "
		style = lipgloss.NewStyle().Foreground(common.ColorInfo).Bold(true)
	default:
		marker = " "
	}

	num := ""
	if n := anchorLine(line); n > 0 {
		num = strconv.Itoa(n)
	}
	gutter := gutterS.Render(padLeft(num, gutterWidth-2) + " " + marker)

	// Only +/-/space on an add, delete or context row is a diff marker. The
	// "---" and "+++" file headers open with the same characters and are not:
	// trimming one turns them into "--"/"++".
	content := line.Content
	if line.Kind != git.DiffLineHeader {
		content = trimDiffMarker(content)
	}
	body := clip(content, maxInt(1, width-gutterWidth-1))
	row := gutter + " " + style.Render(body)

	if idx == m.diffLine && m.focus == paneDiff {
		// The cursor is a left bar rather than a full-row inverse: the row's own
		// add/delete colour is the information here, and inverting would hide it.
		return lipgloss.NewStyle().Foreground(common.ColorPrimary).Render("▌") + clip(row, width-1)
	}
	return " " + clip(row, width-1)
}

// renderOrphanedComments lists the notes on a file that has no diff left to
// hang them off, so they stay visible until the user sends or discards them.
func (m *Model) renderOrphanedComments(entry *fileEntry, width int) []string {
	var rows []string
	for _, note := range m.comments[entry.Path()] {
		if note.Line > 0 {
			rows = append(rows, clip(lipgloss.NewStyle().Foreground(common.ColorMuted).
				Render("  was line "+strconv.Itoa(note.Line)), width))
		}
		rows = append(rows, m.renderCommentCard(note, width, false)...)
		rows = append(rows, "")
	}
	return rows
}

// renderCommentCard draws an attached comment under its line.
//
// A stale note — one whose quoted line the agent has since changed — is drawn
// muted and says so. It is still worth sending, but presenting it as if it
// still pointed at the line under it would be a lie the reader cannot check.
func (m *Model) renderCommentCard(c comment, width int, selected bool) []string {
	tone := common.ColorWarning
	if c.Stale {
		tone = common.ColorMuted
	}
	barTone := tone
	if selected {
		barTone = common.ColorPrimary
	}

	head := ""
	if c.Stale {
		head = "line has since changed"
	}
	if selected {
		if head != "" {
			head += " · "
		}
		head += "e edit · d delete"
	}

	var rows []string
	if head != "" {
		rows = append(rows, commentRow(barTone,
			lipgloss.NewStyle().Foreground(common.ColorMuted).Italic(true).Render(head), width))
	}
	body := lipgloss.NewStyle().Foreground(tone).Bold(selected)
	for _, line := range wrap(c.Body, maxInt(10, width-commentBodyIndent)) {
		rows = append(rows, commentRow(barTone, body.Render(line), width))
	}
	return rows
}

// commentBodyIndent is how far a note's text sits from the pane edge: the diff
// gutter, its separating space, and the bar plus its own space.
const commentBodyIndent = gutterWidth + 2

// commentRow draws one line of a note, aligned so its text starts in the same
// column as the code above it.
//
// Alignment is the whole point. A note indented to nowhere reads as stray text
// dropped into the middle of the file; sharing the code's left edge, with the
// bar standing in for the line number, makes it read as an annotation *of* the
// line it hangs under. The gutter is left blank rather than filled, because a
// note has no line number of its own and inventing one would be a lie the
// reader has to check.
func commentRow(tone color.Color, content string, width int) string {
	// The bar sits in the diff's marker column and the text starts where code
	// starts, so a note lines up under the line it annotates instead of at an
	// indent of its own. renderDiffLine lays out " " + num(5) + " " + marker +
	// " " + body, and this mirrors it exactly.
	bar := lipgloss.NewStyle().Foreground(tone).Render("▐")
	return padTo(" "+strings.Repeat(" ", gutterWidth-1)+bar+" "+content, width)
}

// renderCommentEditor draws the note being typed, in the same column as the
// notes it will become.
//
// It renders the text itself rather than calling textarea.View() for the same
// reason the file editor does: the widget draws its own prompt column and
// highlights the caret line across the full field, which put a stray bar and a
// black band through the middle of the diff. The textarea stays the model.
func (m *Model) renderCommentEditor(width int) []string {
	if m.commentArea == nil {
		return nil
	}
	hint := lipgloss.NewStyle().Foreground(common.ColorMuted).Italic(true).
		Render("ctrl+enter to attach · esc to cancel")
	rows := []string{commentRow(common.ColorPrimary, hint, width)}

	value := m.commentArea.Value()
	lines := strings.Split(value, "\n")
	row, col := m.commentArea.Line(), m.commentArea.Column()

	if value == "" {
		// Show the placeholder with the caret sitting on its first character,
		// so an empty editor still reads as focused rather than as a blank gap.
		placeholder := lipgloss.NewStyle().Foreground(common.ColorMuted).
			Render(m.commentArea.Placeholder)
		return append(rows, commentRow(common.ColorPrimary,
			caretCell(" ")+placeholder, width))
	}
	for i, line := range lines {
		text := lipgloss.NewStyle().Foreground(common.ColorWarning).Render(line)
		if i == row {
			text = withCaretPlain(line, col, common.ColorWarning)
		}
		rows = append(rows, commentRow(common.ColorPrimary, text, width))
	}
	return rows
}

// withCaretPlain draws a line of unhighlighted text with the caret on it.
func withCaretPlain(line string, col int, tone color.Color) string {
	runes := []rune(line)
	col = clamp(col, 0, len(runes))
	style := lipgloss.NewStyle().Foreground(tone)

	caret := " "
	if col < len(runes) {
		caret = string(runes[col])
	}
	after := ""
	if col < len(runes) {
		after = string(runes[col+1:])
	}
	return style.Render(string(runes[:col])) + caretCell(caret) + style.Render(after)
}

// caretCell draws the block cursor.
func caretCell(text string) string {
	return lipgloss.NewStyle().
		Foreground(common.ColorBackground).
		Background(common.ColorPrimary).
		Render(text)
}

// trimDiffMarker drops the leading +/-/space a unified diff puts on each row.
// The marker is rendered in the gutter instead, so leaving it on the content
// would show it twice and shift every line one column right of where it sits in
// the file.
func trimDiffMarker(content string) string {
	if content == "" {
		return ""
	}
	switch content[0] {
	case '+', '-', ' ':
		return content[1:]
	}
	return content
}

// trimBlank trims surrounding whitespace, including the newlines a textarea
// leaves behind.
func trimBlank(s string) string { return strings.TrimSpace(s) }

func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// wrap breaks text at width on word boundaries.
func wrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var out []string
	for _, paragraph := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			switch {
			case line == "":
				line = word
			case len(line)+1+len(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}

// joinPaths renders a path list for a one-line status message.
func joinPaths(paths []string) string { return strings.Join(paths, ", ") }
