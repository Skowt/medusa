package review

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The editor is rendered here rather than by textarea.View(): the widget has no
// per-token styling hook, and its viewport only takes content during View, so
// neither syntax colour nor jumping to a line is possible through it.

// TestEditorScrollsToTheSelectedLine covers the second half of opening on a
// line: putting the caret there is useless if the pane still shows line 1. The
// widget's own viewport cannot do this — it takes its content during View, so
// any offset set before the first render is clamped to zero — which is why
// editScrollTop owns the scrolling instead.
func TestEditorScrollsToTheSelectedLine(t *testing.T) {
	const height = 20
	cases := []struct {
		name                   string
		cursor, total, wantTop int
	}{
		{"near the top stays at the top", 3, 500, 0},
		{"middle centres the caret", 250, 500, 240},
		{"near the end clamps to the last page", 498, 500, 480},
		{"a file shorter than the pane never scrolls", 5, 12, 0},
	}
	for _, tc := range cases {
		got := editScrollTop(tc.cursor, tc.total, height)
		if got != tc.wantTop {
			t.Errorf("%s: top = %d, want %d", tc.name, got, tc.wantTop)
		}
		// Whatever it picks, the caret has to be on screen — that is the point.
		if tc.cursor < got || tc.cursor >= got+height {
			if tc.total > height {
				t.Errorf("%s: caret at %d is outside rows %d..%d",
					tc.name, tc.cursor, got, got+height-1)
			}
		}
	}
}

// TestEditorRendersSyntaxColour checks the tokenizer is actually wired into the
// pane, not just present. The assertion is on distinct colours rather than on
// specific ones, so retheming does not break it.
func TestEditorRendersSyntaxColour(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte("func f() {\n\treturn \"hi\" // note\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(nil, 110, 20)
	m.applyLoaded(loadedFrom("f.go", "@@ -1,2 +1,3 @@\n kept\n+added\n tail"))
	buf, err := newEditBuffer(path, "", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	m.edits["f.go"] = buf
	m.focus = paneEdit

	view := m.renderEditPane(m.Selected())
	if strings.Count(view, "\x1b[") < 6 {
		t.Errorf("editor pane carries almost no styling:\n%s", view)
	}
	plain := stripStyles(view)
	for _, want := range []string{"func f() {", `return "hi" // note`, "1", "2"} {
		if !strings.Contains(plain, want) {
			t.Errorf("editor pane is missing %q:\n%s", want, plain)
		}
	}
}

// TestEditorShowsLiveChangeBars covers the GitHub-style feedback: as you type,
// the gutter has to say what you changed. Without it the only signal an edit
// landed is the text itself, which is exactly what you cannot check against
// what was there before.
func TestEditorShowsLiveChangeBars(t *testing.T) {
	m, buf := editorFixture(t,
		"package main\n\nfunc main() {\n\tkeep := 1\n\tdrop := 2\n\tgone := 3\n\tuse(keep)\n}\n")

	// Replace one line, insert one, delete two.
	buf.area.SetValue("package main\n\nfunc main() {\n    keep := 99\n    brand new\n    use(keep)\n}\n")

	// Two rewritten lines pair into modifications; only the genuinely dropped
	// line is a deletion. Reported as +2 −3 instead, a pair of typo fixes reads
	// as five lines of change.
	view := stripStyles(m.renderEditPane(m.Selected()))
	if !strings.Contains(view, "~2") || !strings.Contains(view, "−1") {
		t.Errorf("header does not report the edit as ~2 −1:\n%s", view)
	}
	if strings.Contains(view, "+") && strings.Contains(view, "+2") {
		t.Errorf("rewritten lines were counted as additions:\n%s", view)
	}
	// The deleted line keeps its text, on a row of its own.
	if !strings.Contains(view, "gone := 3") {
		t.Errorf("the deleted line left no trace:\n%s", view)
	}
	if !strings.Contains(view, "−") {
		t.Errorf("the deleted line carries no − marker:\n%s", view)
	}
	if strings.Count(view, "~") < 2 {
		t.Errorf("rewritten lines carry no ~ marker:\n%s", view)
	}
}

// TestEditorHeaderStartsUnchanged keeps an untouched buffer from claiming edits.
func TestEditorHeaderStartsUnchanged(t *testing.T) {
	m, _ := editorFixture(t, "package main\n\nfunc main() {}\n")

	if view := stripStyles(m.renderEditPane(m.Selected())); !strings.Contains(view, "unchanged") {
		t.Errorf("a freshly opened buffer does not read as unchanged:\n%s", view)
	}
}

// colourOf returns a line's first escape sequence, standing in for its colour.
func colourOf(line string) string {
	start := strings.Index(line, "\x1b[")
	if start < 0 {
		return ""
	}
	end := strings.Index(line[start:], "m")
	if end < 0 {
		return ""
	}
	return line[start : start+end]
}

// TestEnterInsertsANewLine covers both text panes. Almost every key in them is
// content, and the two switches that run before the textarea sees a key are the
// place a plain key gets swallowed by accident — a reserved binding added
// without a guard silently stops working as text.
//
// Note the fixtures here are short on purpose, to isolate the routing. The
// widget has its own reason to swallow Enter that only appears in a real file —
// see TestEnterWorksPastTheWidgetsHeightCeiling.
func TestEnterInsertsANewLine(t *testing.T) {
	t.Run("file editor", func(t *testing.T) {
		m, buf := editorFixture(t, "alpha\nbeta\n")
		before := buf.area.Value()

		m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

		after := m.edits["f.go"].area.Value()
		if strings.Count(after, "\n") != strings.Count(before, "\n")+1 {
			t.Errorf("enter did not add a line:\n was %q\nnow %q", before, after)
		}
	})

	t.Run("comment editor", func(t *testing.T) {
		m := New(nil, 110, 20)
		m.applyLoaded(loadedFrom("f.go", "@@ -1,2 +1,3 @@\n kept\n+added\n tail"))
		m.focus = paneDiff
		m.startComment()
		if m.commentArea == nil {
			t.Fatal("the comment editor did not open")
		}
		m.commentArea.SetValue("first line")

		m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m, _, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})

		if got := m.commentArea.Value(); !strings.Contains(got, "\n") {
			t.Errorf("enter did not add a line to the comment: %q", got)
		}
	})
}

// TestReservedEditorKeysStillReachTheBuffer guards the rest of the keys the
// pane switches on. `d` and `c` are commands in the diff pane and must be plain
// text once a text pane has focus, or typing "add" in a comment deletes it.
func TestReservedEditorKeysStillReachTheBuffer(t *testing.T) {
	m, _ := editorFixture(t, "x\n")
	for _, r := range []rune{'d', 'c', 'e', 'n', 'p', 'j', 'k', 'q', 'G'} {
		m, _, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.edits["f.go"].area.Value(); !strings.Contains(got, "dcenpjkqG") {
		t.Errorf("editor swallowed keys that should be text: %q", got)
	}
}

// TestEnterWorksPastTheWidgetsHeightCeiling is the case the two-line fixture
// above misses entirely, and the one that actually bit.
//
// textarea defaults to MaxHeight 99, and once the value reaches it the widget
// makes Enter a *silent* no-op — no error, no bell, nothing. Every source file
// worth reviewing is longer than that, so Enter appeared simply not to be
// wired up. MaxWidth 500 truncates long lines the same quiet way.
func TestEnterWorksPastTheWidgetsHeightCeiling(t *testing.T) {
	var source strings.Builder
	for i := 1; i <= 400; i++ {
		source.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	m, buf := editorFixture(t, source.String())

	buf.FocusLine(200)
	before := strings.Count(buf.area.Value(), "\n")

	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	after := strings.Count(m.edits["f.go"].area.Value(), "\n")
	if after != before+1 {
		t.Errorf("enter did nothing in a %d-line file: %d newlines before, %d after",
			before, before, after)
	}
}

// TestLongLinesAreNotTruncated guards the other default ceiling. A minified
// file or a long string literal would come back shortened on save, which is
// data loss that no diff of the visible pane would show.
func TestLongLinesAreNotTruncated(t *testing.T) {
	long := strings.Repeat("x", 900)
	_, buf := editorFixture(t, "short\n"+long+"\n")

	if got := buf.area.Value(); !strings.Contains(got, long) {
		t.Errorf("a %d-character line was truncated on load", len(long))
	}
}

// TestEditorGutterReadsAsADiff covers the vocabulary of the change column: +
// for a line that was not there, − for one that is gone, ~ for one rewritten.
// A bar told the reader *that* something changed without saying what kind.
func TestEditorGutterReadsAsADiff(t *testing.T) {
	source := "package main\n\nfunc main() {\n\tkeep := 1\n\tdrop := 2\n\tuse(keep)\n}\n"

	t.Run("insertion is a green plus", func(t *testing.T) {
		m, buf := editorFixture(t, source)
		buf.area.SetValue("package main\n\nfunc main() {\n    keep := 1\n    INSERTED\n    drop := 2\n    use(keep)\n}\n")

		row := rowContaining(t, m, "INSERTED")
		if !strings.Contains(stripStyles(row), "+") {
			t.Errorf("an inserted line carries no +: %q", stripStyles(row))
		}
	})

	t.Run("rewrite is a tilde, not an add and a delete", func(t *testing.T) {
		m, buf := editorFixture(t, source)
		buf.area.SetValue("package main\n\nfunc main() {\n    keep := 99\n    drop := 2\n    use(keep)\n}\n")

		row := stripStyles(rowContaining(t, m, "keep := 99"))
		if !strings.Contains(row, "~") {
			t.Errorf("a rewritten line is not marked ~: %q", row)
		}
		if view := stripStyles(m.renderEditPane(m.Selected())); strings.Contains(view, "keep := 1") {
			t.Errorf("a rewrite drew a removal row for the line it replaced:\n%s", view)
		}
	})
}

// TestDeletedLinesShowTheirTextWithNoNumber is what replaced "1 line removed".
// A count says something went without saying what, which is the one thing the
// reader cannot look up — the line is no longer in the buffer to scroll back
// to. The number column stays blank because a deleted line has no position in
// the file any more.
func TestDeletedLinesShowTheirTextWithNoNumber(t *testing.T) {
	m, buf := editorFixture(t,
		"package main\n\nfunc main() {\n\tkeep := 1\n\tdrop := 2\n\talso := 3\n\tuse(keep)\n}\n")
	buf.area.SetValue("package main\n\nfunc main() {\n    keep := 1\n    use(keep)\n}\n")

	view := stripStyles(m.renderEditPane(m.Selected()))
	if strings.Contains(view, "line removed") || strings.Contains(view, "lines removed") {
		t.Errorf("the removal is still reported as a count:\n%s", view)
	}
	for _, gone := range []string{"drop := 2", "also := 3"} {
		row := stripStyles(rowContaining(t, m, gone))
		if !strings.Contains(row, "−") {
			t.Errorf("removed line %q carries no −: %q", gone, row)
		}
		// The gutter up to the marker must hold no digits: the line is gone, so
		// numbering it with its old neighbour's position would be a lie.
		if before, _, ok := strings.Cut(row, "−"); ok && strings.ContainsAny(before, "0123456789") {
			t.Errorf("removed line %q was given a line number: %q", gone, row)
		}
	}
}

// TestRemovedLineIsColouredApartFromAdded keeps the two readable at a glance.
func TestRemovedLineIsColouredApartFromAdded(t *testing.T) {
	m, buf := editorFixture(t, "a\nb\nc\nd\n")
	buf.area.SetValue("a\nNEW\nb\nc\n")

	added := rowContaining(t, m, "NEW")
	removed := rowContaining(t, m, "d")
	if colourOf(added) == colourOf(removed) {
		t.Error("added and removed rows are drawn in the same colour")
	}
}

// rowContaining returns the rendered editor row holding a substring.
func rowContaining(t *testing.T, m *Model, want string) string {
	t.Helper()
	for _, row := range strings.Split(m.renderEditPane(m.Selected()), "\n") {
		if strings.Contains(stripStyles(row), want) {
			return row
		}
	}
	t.Fatalf("no rendered row contains %q", want)
	return ""
}

// editorFixture opens a file for editing and returns the model and its buffer.
func editorFixture(t *testing.T, source string) (*Model, *editBuffer) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(nil, 100, 20)
	m.absPathOverride = dir
	m.applyLoaded(loadedFrom("f.go", "@@ -1,2 +1,3 @@\n kept\n+added\n tail"))
	m.startEdit()
	buf := m.edits["f.go"]
	if buf == nil {
		t.Fatal("the editor did not open")
	}
	return m, buf
}
