package review

import (
	"sort"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/git"
)

// submitKey ends whatever is in hand: it attaches the note being typed, and
// from anywhere else submits the whole review. One gesture for both, so
// finishing a comment and sending the review read as one motion.
//
// submitKeyFallback exists because ctrl+enter is not deliverable everywhere —
// a terminal without the Kitty keyboard protocol sends a bare CR for both enter
// and ctrl+enter, so the binding would simply be dead there with nothing to
// suggest why. alt+enter survives as ESC CR on every terminal.
const (
	submitKey         = "ctrl+enter"
	submitKeyFallback = "alt+enter"
)

// Result is what the overlay hands back when it closes. Saved is false for a
// discard, and for a close with nothing to send.
type Result struct {
	Saved bool
	// Review is the text to paste into the agent, empty when nothing was said.
	Review string
	// Edited names the files written to disk, so the caller can report them.
	Edited []string
	// Failed names files whose write was refused because they changed under
	// the buffer. Non-empty means the review was NOT sent: describing edits
	// that did not land would be worse than not sending at all.
	Failed []string
}

// Update handles one message. It returns a non-nil Result exactly once, when
// the overlay is finished.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd, *Result) {
	switch msg := msg.(type) {
	case filesLoaded:
		m.applyLoaded(msg)
		// A change that landed mid-read still has to be picked up, or the
		// window settles showing whatever the agent wrote second-to-last.
		if m.refreshPending {
			m.refreshPending = false
			return m, m.Refresh(), nil
		}
		return m, nil, nil

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scrollDiff(-3)
		case tea.MouseWheelDown:
			m.scrollDiff(3)
		}
		return m, nil, nil

	case tea.MouseClickMsg:
		return m.handleClick(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil, nil
}

// handleKey routes a keypress by focused pane. The text panes come first: while
// a comment or an edit is being typed, almost every key is content, so only the
// keys that leave the pane may be interpreted as commands.
func (m *Model) handleKey(msg tea.KeyPressMsg) (*Model, tea.Cmd, *Result) {
	switch m.focus {
	case paneComment:
		return m.handleCommentKey(msg)
	case paneEdit:
		return m.handleEditKey(msg)
	}

	switch msg.String() {
	case "esc", "q":
		return m, nil, &Result{}
	case "tab":
		if m.focus == paneFiles {
			m.focus = paneDiff
		} else {
			m.focus = paneFiles
		}
	case submitKey, submitKeyFallback:
		return m.save()
	case "ctrl+d":
		return m, nil, &Result{}
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "g":
		m.jumpCursor(0)
	case "G":
		m.jumpCursor(-1)
	case "n":
		m.jumpHunk(1)
	case "p":
		m.jumpHunk(-1)
	case "enter", "l", "right":
		m.focus = paneDiff
	case "h", "left":
		m.focus = paneFiles
	case "c":
		m.startComment()
	case "e":
		// On a note, `e` reopens it for editing; on code it opens the file.
		// One key rather than two because the cursor already says which is
		// meant, and a second key would be dead everywhere but one row type.
		if _, ok := m.cursorComment(); ok {
			m.editCursorComment()
		} else {
			m.startEdit()
		}
	case "d", "x", "delete", "backspace":
		if !m.deleteCursorComment() {
			m.statusMsg = "Put the cursor on one of your comments to delete it"
		}
	}
	return m, nil, nil
}

// editCursorComment reopens the note under the cursor, pre-filled, and removes
// the original so attaching the edited text replaces it rather than adding a
// second copy of the same note.
func (m *Model) editCursorComment() {
	note, ok := m.cursorComment()
	if !ok {
		return
	}
	area := newCommentArea(maxInt(20, m.diffPaneWidth()-6))
	area.SetValue(note.Body)
	area.MoveToEnd()
	m.commentArea = &area
	m.commentAnchor = *note
	m.deleteCursorComment()
	m.focus = paneComment
	m.statusMsg = ""
}

// moveCursor moves within whichever pane has focus.
func (m *Model) moveCursor(delta int) {
	if m.focus == paneFiles {
		m.cursor = clamp(m.cursor+delta, 0, len(m.files)-1)
		m.resetDiffCursor()
		return
	}
	m.diffLine = clamp(m.diffLine+delta, 0, m.diffRowCount()-1)
	m.followDiffCursor()
}

// jumpCursor sends the focused pane's cursor to an absolute row; -1 is the end.
func (m *Model) jumpCursor(row int) {
	limit := len(m.files) - 1
	if m.focus == paneDiff {
		limit = m.diffRowCount() - 1
	}
	if row < 0 {
		row = limit
	}
	if m.focus == paneFiles {
		m.cursor = clamp(row, 0, limit)
		m.resetDiffCursor()
		return
	}
	m.diffLine = clamp(row, 0, limit)
	m.followDiffCursor()
}

// resetDiffCursor points the diff at the newly selected file's first hunk.
//
// Opening at row zero would put the `diff --git` / `index` preamble on screen
// instead — five lines that say nothing the window's own header does not
// already say, on every single file. The preamble is still there for anyone who
// scrolls up to it.
func (m *Model) resetDiffCursor() {
	m.diffLine, m.diffTop = 0, 0
	entry := m.Selected()
	if entry == nil || entry.Diff == nil || len(entry.Diff.Hunks) == 0 {
		return
	}
	m.diffTop = entry.Diff.Hunks[0].StartLine
	// Land on the first row of the hunk, not on its @@ header: the header is
	// not a line of the file, so opening on it means the very first `c` or `e`
	// is refused for a reason the user has no way to have anticipated.
	m.diffLine = m.diffTop
	for i := m.diffTop + 1; i < len(entry.Diff.Lines); i++ {
		if entry.Diff.Lines[i].Kind != git.DiffLineHeader {
			m.diffLine = i
			break
		}
	}
}

// jumpHunk moves the diff cursor to the next or previous hunk header, which is
// the only way to cross a large file at speed.
func (m *Model) jumpHunk(dir int) {
	entry := m.Selected()
	if entry == nil || entry.Diff == nil {
		return
	}
	hunks := entry.Diff.Hunks
	if len(hunks) == 0 {
		return
	}
	m.focus = paneDiff
	if dir > 0 {
		for _, h := range hunks {
			if h.StartLine > m.diffLine {
				m.diffLine = h.StartLine
				m.followDiffCursor()
				return
			}
		}
		return
	}
	for i := len(hunks) - 1; i >= 0; i-- {
		if hunks[i].StartLine < m.diffLine {
			m.diffLine = hunks[i].StartLine
			m.followDiffCursor()
			return
		}
	}
}

// scrollDiff moves the viewport without moving the cursor.
func (m *Model) scrollDiff(delta int) {
	m.diffTop = clamp(m.diffTop+delta, 0, maxInt(0, m.diffRowCount()-m.paneHeight()))
}

// followDiffCursor keeps the cursor row inside the viewport.
func (m *Model) followDiffCursor() {
	height := m.paneHeight()
	if m.diffLine < m.diffTop {
		m.diffTop = m.diffLine
	}
	if m.diffLine >= m.diffTop+height {
		m.diffTop = m.diffLine - height + 1
	}
	m.diffTop = clamp(m.diffTop, 0, maxInt(0, m.diffRowCount()-height))
}

// diffRowCount is how many navigable rows the selected file has, comments
// included — the cursor moves over paneRows, not over raw diff lines.
func (m *Model) diffRowCount() int {
	return len(m.paneRows())
}

// cursorDiffLine returns the diff line under the cursor. A comment row reports
// the line it hangs off, so acting on it still names a place in the file.
func (m *Model) cursorDiffLine() (git.DiffLine, bool) {
	row, ok := m.cursorRow()
	if !ok || row.LineIdx < 0 {
		return git.DiffLine{}, false
	}
	return row.Line, true
}

// startComment opens the comment editor on the diff's cursor line.
func (m *Model) startComment() {
	line, ok := m.cursorDiffLine()
	if !ok || line.Kind == git.DiffLineHeader {
		m.statusMsg = "Put the cursor on a line of the file to comment on it"
		return
	}
	area := newCommentArea(maxInt(20, m.diffPaneWidth()-6))
	m.commentArea = &area
	m.commentAnchor = comment{Line: anchorLine(line), Quote: trimDiffMarker(line.Content)}
	m.focus = paneComment
	m.statusMsg = ""
}

// newCommentArea builds the inline note editor.
func newCommentArea(width int) textarea.Model {
	area := textarea.New()
	area.Placeholder = "What should the agent do differently here?"
	area.ShowLineNumbers = false
	area.CharLimit = 0
	// Same ceilings as the file editor: past MaxHeight rows the widget silently
	// swallows Enter, so a long note would stop being able to gain a paragraph.
	area.MaxHeight = 0
	area.MaxWidth = 0
	area.SetWidth(width)
	area.SetHeight(3)
	area.Focus()
	return area
}

// handleCommentKey feeds the comment editor, reserving only the keys that end it.
func (m *Model) handleCommentKey(msg tea.KeyPressMsg) (*Model, tea.Cmd, *Result) {
	switch msg.String() {
	case "esc":
		m.commentArea = nil
		m.focus = paneDiff
		return m, nil, nil
	case submitKey, submitKeyFallback:
		// Finishing a note and submitting the review are the same gesture, one
		// after the other: this attaches the note and hands focus back, and the
		// next press submits. Enter stays a newline, so a note can be a
		// paragraph.
		m.commitComment()
		return m, nil, nil
	}
	if m.commentArea == nil {
		return m, nil, nil
	}
	area, cmd := m.commentArea.Update(msg)
	m.commentArea = &area
	return m, cmd, nil
}

// commitComment attaches the typed note to the cursor line.
func (m *Model) commitComment() {
	entry := m.Selected()
	if entry == nil || m.commentArea == nil {
		return
	}
	body := trimBlank(m.commentArea.Value())
	note := m.commentAnchor
	m.commentArea = nil
	m.focus = paneDiff
	if body == "" {
		// An emptied note is a deletion. The original was already removed when
		// editing opened, so there is nothing left to do.
		return
	}
	note.Body = body
	m.comments[entry.Path()] = append(m.comments[entry.Path()], note)
	m.sortComments(entry.Path())
	m.commentsRev++
}

// sortComments keeps a file's notes in line order so an edited one returns to
// where it was written rather than to the end of the list.
func (m *Model) sortComments(path string) {
	notes := m.comments[path]
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].Line < notes[j].Line })
	m.comments[path] = notes
}

// anchorLine picks the file line a comment should name. A deleted line has no
// post-image number of its own, so it borrows its old number — the reader is
// being pointed at a place, not at a line that must still exist.
func anchorLine(line git.DiffLine) int {
	if line.NewLine > 0 {
		return line.NewLine
	}
	return line.OldLine
}

// startEdit opens the selected file for hand editing.
func (m *Model) startEdit() {
	entry := m.Selected()
	if entry == nil {
		return
	}
	path := m.absPath(entry.Path())
	if !fileExists(path) {
		m.statusMsg = "That file is not on disk — nothing to edit"
		return
	}
	buf, ok := m.edits[entry.Path()]
	if !ok {
		// The committed version is read once, here, so every later frame can
		// diff the live buffer against it without touching git again.
		base, tracked := "", false
		if m.workspace != nil {
			base, tracked, _ = git.FileAtHead(m.workspace.PrimaryWorktreeRoot(), entry.Path())
		}
		created, err := newEditBuffer(path, base, tracked, m.paneHeight()-1)
		if err != nil {
			m.statusMsg = "Could not open " + entry.Path() + ": " + err.Error()
			return
		}
		m.edits[entry.Path()] = created
		buf = created
	}
	buf.SetHeight(m.paneHeight() - 1)
	// Open on whatever row the reader had selected, so editing continues from
	// the line they were looking at rather than from the top of the file.
	if line, ok := m.cursorDiffLine(); ok {
		buf.FocusLine(anchorLine(line))
	}
	buf.area.Focus()
	m.focus = paneEdit
	m.statusMsg = ""
}

// handleEditKey feeds the edit buffer, reserving only esc to leave it.
func (m *Model) handleEditKey(msg tea.KeyPressMsg) (*Model, tea.Cmd, *Result) {
	entry := m.Selected()
	if entry == nil {
		m.focus = paneDiff
		return m, nil, nil
	}
	buf := m.edits[entry.Path()]
	if buf == nil {
		m.focus = paneDiff
		return m, nil, nil
	}
	switch msg.String() {
	case "esc":
		m.leaveEdit(buf)
		return m, nil, nil
	case submitKey, submitKeyFallback:
		m.leaveEdit(buf)
		return m.save()
	}
	area, cmd := buf.area.Update(msg)
	buf.area = area
	return m, cmd, nil
}

// leaveEdit returns to the diff, landing the cursor on the line that was being
// edited.
//
// The rebuilt diff has a different shape from the one that was on screen when
// editing started — every unsaved insertion adds a row — so the old cursor
// index names a different line, and leaving it put the reader somewhere in the
// middle of a file they had not scrolled to.
func (m *Model) leaveEdit(buf *editBuffer) {
	line := buf.area.Line() + 1
	buf.area.Blur()
	m.focus = paneDiff
	m.rowForLine(line, "")
}

// save writes every dirty buffer and composes the review.
//
// A refused write aborts the send: the message names the files that were edited
// by hand, and sending it while one of those edits sits unwritten would tell the
// agent to go and re-read a file that still holds its own version.
func (m *Model) save() (*Model, tea.Cmd, *Result) {
	var edited, failed []string
	for _, path := range m.EditedPaths() {
		buf := m.edits[path]
		if err := buf.Save(); err != nil {
			failed = append(failed, path)
			continue
		}
		edited = append(edited, path)
	}
	if len(failed) > 0 {
		m.statusMsg = "Not sent — changed on disk: " + joinPaths(failed)
		return m, nil, &Result{Failed: failed, Edited: edited}
	}
	return m, nil, &Result{Saved: true, Review: m.composeReview(edited), Edited: edited}
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
