package review

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/syntax"
	"github.com/Skowt/medusa/internal/ui/common"
)

// editGutter is the width of the line-number column in the editor.
const editGutter = 6

// renderEditPane draws the file being hand-edited.
//
// The lines are drawn here rather than by textarea.View() because the widget
// has no per-token styling hook — its StyleState applies to the whole field —
// so its own rendering cannot carry syntax colour. The textarea stays the
// editing *model* (it owns the value, the cursor and every key), and this reads
// its state back out: Value for the text, Line and LineInfo for the caret.
//
// Drawing it here also puts scrolling under our control, which it needs to be:
// the widget's viewport only takes its content during View, so any scroll
// computed before the first render is clamped to zero and the caret ends up off
// screen with the file still showing line 1.
func (m *Model) renderEditPane(entry *fileEntry) string {
	width := m.diffPaneWidth()
	height := m.paneHeight()
	buf := m.edits[entry.Path()]
	if buf == nil {
		return ""
	}

	rows := []string{m.renderEditHeader(entry, buf, width)}

	lines := strings.Split(buf.area.Value(), "\n")
	cursorRow := buf.area.Line()
	top := editScrollTop(cursorRow, len(lines), height-1)
	// Two live diffs, both over the buffer's current lines so neither can drift:
	// baseMarks is everything changed since the commit (the agent's work and the
	// user's), marks is what the user has done in this session. The git diff's
	// own line numbers are fixed at load, so using them here left every marker
	// below an inserted line pointing at whatever slid into its slot.
	baseMarks := buf.BaseMarks()
	marks := buf.Marks()
	lang, _ := syntax.LanguageFor(entry.Path())
	cursorCol := buf.CursorColumn()

	// One lexing pass over the rows about to be drawn, not one per line: the
	// lexer is stateful, so a string or comment spanning lines is only seen
	// when they are fed together.
	end := minInt(len(lines), top+height)
	painted := syntax.Highlight(lang, lines[top:end])

	for i := top; i < end && len(rows) < height; i++ {
		var mark, base lineMark
		if i < len(marks) {
			mark = marks[i]
		}
		if i < len(baseMarks) {
			base = baseMarks[i]
		}
		// Deleted lines keep their text and are drawn above the line that
		// closed over them, exactly where they used to be — with no number,
		// because they no longer have one.
		for _, gone := range mark.RemovedBefore {
			if len(rows) >= height {
				break
			}
			rows = append(rows, m.renderRemovedLine(gone, width))
		}
		if len(rows) >= height {
			break
		}
		rows = append(rows, m.renderEditLine(editLine{
			num:    i + 1,
			text:   lines[i],
			tokens: painted[i-top],
			// A line the agent added reads as changed until the user touches
			// it; from then on their own edit is the more useful thing to say.
			fromDiff: base.Kind != lineSame,
			mark:     mark.Kind,
			cursor:   i == cursorRow,
			col:      cursorCol,
			width:    width,
		}))
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	return joinPaneRows(rows[:height], width)
}

// editLine is one row's worth of state, grouped so the renderer's signature
// does not run to seven positional arguments.
type editLine struct {
	num      int
	text     string
	tokens   []syntax.Token
	fromDiff bool // the agent added this line
	mark     changeKind
	cursor   bool
	col      int
	width    int
}

// editScrollTop centres the caret in the visible window, clamped to the file.
// Centring rather than merely keeping it on screen is what makes jumping
// straight to a line useful: landing with the target on the last row shows the
// code above it and none of the code it runs into.
func editScrollTop(cursorRow, total, height int) int {
	if height < 1 || total <= height {
		return 0
	}
	top := cursorRow - height/2
	return clamp(top, 0, total-height)
}

// renderEditHeader names the file and reports how much has actually changed.
func (m *Model) renderEditHeader(entry *fileEntry, buf *editBuffer, width int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(common.ColorWarning).
		Render("editing " + truncateLeft(entry.Path(), maxInt(10, width-34)))

	// The counts are of the *user's* edits in this buffer, recomputed each
	// frame. They used to report the diff's added-line count, which never moved
	// however much was typed — a number that does not respond to editing is
	// worse than no number, since it reads as a broken live value.
	counts := buf.EditCounts()
	if !counts.Any() {
		return clip(title+lipgloss.NewStyle().Foreground(common.ColorMuted).
			Render("  unchanged"), width)
	}
	// Only the non-zero parts are shown. A run of zeroes is noise, and the
	// colours are doing the work anyway — the counts are here to say how much,
	// not which.
	for _, part := range []struct {
		n     int
		mark  string
		style lipgloss.Style
	}{
		{counts.Modified, "~", lipgloss.NewStyle().Foreground(common.ColorWarning)},
		{counts.Added, "+", lipgloss.NewStyle().Foreground(common.ColorSuccess)},
		{counts.Removed, "−", lipgloss.NewStyle().Foreground(common.ColorError)},
	} {
		if part.n > 0 {
			title += "  " + part.style.Render(part.mark+strconv.Itoa(part.n))
		}
	}
	if entry.EditConflict {
		title += lipgloss.NewStyle().Foreground(common.ColorError).Bold(true).
			Render("  ! changed on disk")
	}
	return clip(title, width)
}

// renderEditLine draws one line: gutter, change marker, syntax-coloured text,
// and the caret when it sits on this row.
func (m *Model) renderEditLine(l editLine) string {
	// The column reads as a diff gutter, because that is the vocabulary the
	// reader already has for it: + is a line that was not there, − one that is
	// gone, ~ one that was rewritten. A bar told them *that* something changed
	// without saying what kind of change it was.
	//
	// The user's own edits are bright and win the column; a line the agent
	// added that they have not touched is the same + in a muted tone. Two
	// tiers rather than two glyphs, so the meaning of + stays single.
	gutterStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	marker := " "
	switch {
	case l.mark == lineAdded:
		gutterStyle = lipgloss.NewStyle().Foreground(common.ColorSuccess).Bold(true)
		marker = "+"
	case l.mark == lineModified:
		gutterStyle = lipgloss.NewStyle().Foreground(common.ColorWarning).Bold(true)
		marker = "~"
	case l.fromDiff:
		gutterStyle = lipgloss.NewStyle().Foreground(common.ColorSuccess)
		marker = "+"
	}
	if l.cursor && marker == " " {
		gutterStyle = lipgloss.NewStyle().Foreground(common.ColorPrimary).Bold(true)
	}
	gutter := gutterStyle.Render(padLeft(strconv.Itoa(l.num), editGutter-2) + " " + marker)

	body := l.width - editGutter - 1
	text := renderTokens(l.tokens, body)
	if l.cursor {
		text = withCaret(l.text, l.col, body)
	}
	return gutter + " " + text
}

// renderRemovedLine draws a line the user deleted: no number, a red −, and the
// text that used to be there. The text is tinted rather than syntax-coloured;
// red on its own says "gone" more clearly than keeping it looking live.
//
// Showing the text is the point. A count says something went without saying
// what, which is the one thing the reader cannot look up — the line is no
// longer in the buffer to scroll back to. The number column is left blank
// because a deleted line has no position in the file any more, and filling it
// with the number of its neighbour would be a lie.
func (m *Model) renderRemovedLine(text string, width int) string {
	gutter := lipgloss.NewStyle().Foreground(common.ColorError).Bold(true).
		Render(padLeft("", editGutter-2) + " −")
	body := lipgloss.NewStyle().Foreground(common.ColorError).
		Render(clip(text, maxInt(1, width-editGutter-1)))
	return gutter + " " + body
}

// renderTokens paints a pre-lexed line and clips it to the available width.
func renderTokens(tokens []syntax.Token, width int) string {
	if width < 1 {
		return ""
	}
	var b strings.Builder
	for _, token := range tokens {
		b.WriteString(styleFor(token.Kind).Render(token.Text))
	}
	return clip(b.String(), width)
}

// withCaret renders the caret line, drawing the cursor cell in reverse video.
//
// It splits on the *rune* index the textarea reports and rebuilds each side
// separately, because a styled string cannot be indexed: slicing coloured text
// at a byte offset cuts escape sequences in half.
// The caret line is drawn unhighlighted. Splitting a lexed line at a rune
// offset would mean re-lexing the two halves, and a half-line lexes wrong by
// construction — a string cut in the middle is not a string. Losing colour on
// the one line under the cursor is the cheaper of the two errors.
func withCaret(line string, col, width int) string {
	runes := []rune(line)
	col = clamp(col, 0, len(runes))
	plain := lipgloss.NewStyle().Foreground(common.ColorForeground)

	caret := " "
	if col < len(runes) {
		caret = string(runes[col])
	}
	after := ""
	if col < len(runes) {
		after = string(runes[col+1:])
	}
	cursorCell := lipgloss.NewStyle().
		Foreground(common.ColorBackground).
		Background(common.ColorPrimary).
		Render(caret)
	return clip(plain.Render(string(runes[:col]))+cursorCell+plain.Render(after), width)
}

// styleFor maps a token kind onto the theme. Comments are muted rather than
// tinted so they recede, which is the point of dimming them at all.
func styleFor(kind syntax.Kind) lipgloss.Style {
	switch kind {
	case syntax.KindKeyword:
		return lipgloss.NewStyle().Foreground(common.ColorPrimary)
	case syntax.KindString:
		return lipgloss.NewStyle().Foreground(common.ColorSuccess)
	case syntax.KindComment:
		return lipgloss.NewStyle().Foreground(common.ColorMuted).Italic(true)
	case syntax.KindNumber:
		return lipgloss.NewStyle().Foreground(common.ColorWarning)
	case syntax.KindPunct:
		return lipgloss.NewStyle().Foreground(common.ColorSecondary)
	case syntax.KindFunction:
		return lipgloss.NewStyle().Foreground(common.ColorInfo)
	case syntax.KindType:
		return lipgloss.NewStyle().Foreground(common.ColorSecondary).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(common.ColorForeground)
	}
}
