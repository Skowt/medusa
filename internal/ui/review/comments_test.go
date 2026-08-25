package review

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const commentedDiff = `@@ -10,4 +10,6 @@
 kept
+first added
+second added
 tail`

// newCommentedModel is a file with two notes on separate lines.
func newCommentedModel(t *testing.T) *Model {
	t.Helper()
	m := New(nil, 120, 30)
	m.applyLoaded(loadedFrom("f.go", commentedDiff))
	m.comments["f.go"] = []comment{
		{Line: 11, Quote: "first added", Body: "rename this"},
		{Line: 12, Quote: "second added", Body: "and drop this one"},
	}
	m.focus = paneDiff
	return m
}

// press feeds a key through the model the way the app does.
func press(m *Model, key string) *Model {
	updated, _, _ := m.Update(tea.KeyPressMsg{Code: keyCodeFor(key), Text: key})
	return updated
}

func keyCodeFor(key string) rune {
	return []rune(key)[0]
}

// TestCommentsAreReachableWithTheCursor is the core of "selectable": the cursor
// moves over notes as well as diff lines. Navigating the diff alone leaves a
// comment visible but untouchable — there is no way to reach it to edit or
// delete it once it is written.
func TestCommentsAreReachableWithTheCursor(t *testing.T) {
	m := newCommentedModel(t)

	var reached []string
	for i := 0; i < len(m.paneRows()); i++ {
		m.diffLine = i
		if note, ok := m.cursorComment(); ok {
			reached = append(reached, note.Body)
		}
	}
	if len(reached) != 2 {
		t.Fatalf("cursor reached %d comments, want 2: %v", len(reached), reached)
	}
	if reached[0] != "rename this" || reached[1] != "and drop this one" {
		t.Errorf("comments reached out of order: %v", reached)
	}
}

// TestCommentRowsFollowTheirLine keeps a note directly under the line it was
// written against, which is the only thing that makes the anchor legible.
func TestCommentRowsFollowTheirLine(t *testing.T) {
	m := newCommentedModel(t)

	rows := m.paneRows()
	for i, row := range rows {
		if row.Kind != rowComment {
			continue
		}
		if i == 0 {
			t.Fatal("a comment row cannot be first; it has no line to hang off")
		}
		if prev := rows[i-1]; prev.Kind == rowDiff && anchorLine(prev.Line) != m.comments["f.go"][row.CommentIdx].Line {
			t.Errorf("comment %d sits under line %d, want %d",
				row.CommentIdx, anchorLine(prev.Line), m.comments["f.go"][row.CommentIdx].Line)
		}
	}
}

// TestDeleteRemovesTheSelectedComment covers `d`, and that it removes the one
// under the cursor rather than the first or the last.
func TestDeleteRemovesTheSelectedComment(t *testing.T) {
	m := newCommentedModel(t)
	selectComment(t, m, "and drop this one")

	m = press(m, "d")

	notes := m.comments["f.go"]
	if len(notes) != 1 {
		t.Fatalf("after delete there are %d comments, want 1", len(notes))
	}
	if notes[0].Body != "rename this" {
		t.Errorf("the wrong comment was deleted; %q survived", notes[0].Body)
	}
}

// TestDeletingTheLastCommentClearsTheFile keeps an empty slice from making a
// file look annotated — it would then be held open as "gone" on every refresh.
func TestDeletingTheLastCommentClearsTheFile(t *testing.T) {
	m := newCommentedModel(t)
	m.comments["f.go"] = m.comments["f.go"][:1]
	selectComment(t, m, "rename this")

	m = press(m, "d")

	if _, ok := m.comments["f.go"]; ok {
		t.Error("the file kept an empty comment list")
	}
	if m.HasFeedback() {
		t.Error("a review with no comments and no edits has no feedback to send")
	}
}

// TestEditingACommentReplacesItRatherThanDuplicating covers `e` on a note: the
// original is removed when the editor opens, so attaching the edited text does
// not leave two copies of the same comment on one line.
func TestEditingACommentReplacesItRatherThanDuplicating(t *testing.T) {
	m := newCommentedModel(t)
	selectComment(t, m, "rename this")

	m = press(m, "e")
	if m.focus != paneComment || m.commentArea == nil {
		t.Fatal("`e` on a comment did not open the editor")
	}
	if got := m.commentArea.Value(); got != "rename this" {
		t.Errorf("editor opened with %q, want the existing comment text", got)
	}

	m.commentArea.SetValue("renamed, and explain why")
	m.commitComment()

	notes := m.comments["f.go"]
	if len(notes) != 2 {
		t.Fatalf("editing produced %d comments, want 2", len(notes))
	}
	joined := notes[0].Body + "|" + notes[1].Body
	if strings.Count(joined, "rename") != 1 {
		t.Errorf("the original comment was duplicated: %q", joined)
	}
	// The edited note keeps its anchor rather than moving to the end.
	if notes[0].Line != 11 || notes[0].Body != "renamed, and explain why" {
		t.Errorf("edited note landed at line %d as %q", notes[0].Line, notes[0].Body)
	}
}

// TestEmptyingACommentDeletesIt gives the comment editor the same meaning the
// rename dialog has elsewhere in medusa: submitting nothing removes it.
func TestEmptyingACommentDeletesIt(t *testing.T) {
	m := newCommentedModel(t)
	selectComment(t, m, "rename this")

	m = press(m, "e")
	m.commentArea.SetValue("   ")
	m.commitComment()

	for _, note := range m.comments["f.go"] {
		if strings.Contains(note.Body, "rename") {
			t.Fatal("emptying a comment left it in place")
		}
	}
	if len(m.comments["f.go"]) != 1 {
		t.Errorf("got %d comments, want the other one left", len(m.comments["f.go"]))
	}
}

// TestDeleteOnACodeLineSaysSo keeps `d` from silently doing nothing when the
// cursor is not on a note.
func TestDeleteOnACodeLineSaysSo(t *testing.T) {
	m := newCommentedModel(t)
	m.diffLine = 1 // a diff row

	m = press(m, "d")

	if len(m.comments["f.go"]) != 2 {
		t.Error("`d` on a code line deleted something")
	}
	if m.statusMsg == "" {
		t.Error("`d` on a code line gave no feedback")
	}
}

// selectComment moves the cursor onto the note with the given body.
func selectComment(t *testing.T, m *Model, body string) {
	t.Helper()
	for i := range m.paneRows() {
		m.diffLine = i
		if note, ok := m.cursorComment(); ok && note.Body == body {
			return
		}
	}
	t.Fatalf("comment %q not reachable", body)
}

// TestCommentsAlignWithTheCode is what "looks right" reduces to: a note's text
// starts in the same column as the code it annotates, with its bar in the
// diff's marker column. Indented to anywhere else it reads as stray text
// dropped into the middle of the file rather than as a note about a line.
func TestCommentsAlignWithTheCode(t *testing.T) {
	m := newCommentedModel(t)
	width := m.diffPaneWidth()

	// Render the code row unselected (index 1 is not the cursor row), or its
	// cursor bar shifts the line by a column and the comparison is meaningless.
	m.diffLine = 0
	var code string
	for i, row := range m.paneRows() {
		if row.Kind == rowDiff && trimDiffMarker(row.Line.Content) == "first added" {
			code = stripStyles(m.renderDiffLine(row.Line, i, width))
			break
		}
	}
	note := stripStyles(m.renderCommentCard(m.comments["f.go"][0], width, false)[0])

	codeCol := columnOf(code, "first added")
	noteCol := columnOf(note, "rename this")
	if codeCol < 0 || noteCol < 0 {
		t.Fatalf("could not locate the text:\ncode %q\nnote %q", code, note)
	}
	if codeCol != noteCol {
		t.Errorf("note text starts at column %d, code at %d:\ncode %q\nnote %q",
			noteCol, codeCol, code, note)
	}
	if lipgloss.Width(note) != width {
		t.Errorf("note row is %d columns, want %d", lipgloss.Width(note), width)
	}
}

// TestCommentEditorDrawsNoWidgetChrome is the regression for the black band and
// stray bar the textarea painted through the diff. It draws its own prompt
// column and highlights the caret line across the whole field, so its View is
// unusable inline — the text is rendered here and the widget is only the model.
func TestCommentEditorDrawsNoWidgetChrome(t *testing.T) {
	m := newCommentedModel(t)
	m.diffLine = 1
	m.startComment()
	if m.commentArea == nil {
		t.Fatal("the editor did not open")
	}
	m.commentArea.SetValue("a note")

	rows := m.renderCommentEditor(m.diffPaneWidth())
	if len(rows) < 2 {
		t.Fatalf("editor rendered %d rows", len(rows))
	}
	for i, row := range rows {
		plain := stripStyles(row)
		if strings.Contains(plain, "┃") {
			t.Errorf("row %d carries the widget's prompt column: %q", i, plain)
		}
		if lipgloss.Width(row) != m.diffPaneWidth() {
			t.Errorf("row %d is %d columns, want %d", i, lipgloss.Width(row), m.diffPaneWidth())
		}
	}
	if !strings.Contains(stripStyles(rows[1]), "a note") {
		t.Errorf("the typed text is missing: %q", stripStyles(rows[1]))
	}
}

// columnOf reports the display column a substring starts at.
//
// strings.Index gives a *byte* offset, which is not the column whenever the
// line contains a wide or multi-byte glyph — the panes are full of ▐, ▌ and −,
// each three bytes and one column, so a byte offset reads two columns right of
// the glyph per occurrence.
func columnOf(line, sub string) int {
	at := strings.Index(line, sub)
	if at < 0 {
		return -1
	}
	return lipgloss.Width(line[:at])
}
