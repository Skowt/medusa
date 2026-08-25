package review

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/git"
)

// TestComposeReview covers the message the agent actually receives: it must
// name file:line, quote the source the note hangs off, and end with an explicit
// re-read instruction for hand-edited files — the agent still holds its own
// version of those in context and will keep reasoning about it otherwise.
func TestComposeReview(t *testing.T) {
	m := New(nil, 100, 40)
	m.files = []fileEntry{
		{Change: git.Change{Path: "internal/app/app_hooks.go"}},
		{Change: git.Change{Path: "internal/hooks/event.go"}},
	}
	m.comments["internal/app/app_hooks.go"] = []comment{{
		Line:  267,
		Quote: "case evt == hooks.EventPermissionRequest:",
		Body:  "gate this on the tab, not the payload",
	}}

	got := m.composeReview([]string{"internal/hooks/event.go"})

	for _, want := range []string{
		"internal/app/app_hooks.go:267",
		"> case evt == hooks.EventPermissionRequest:",
		"gate this on the tab, not the payload",
		"I also edited 1 file by hand",
		"- internal/hooks/event.go",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("review is missing %q:\n%s", want, got)
		}
	}

	// A file with no notes must not appear as an empty section.
	if strings.Count(got, "internal/hooks/event.go") != 1 {
		t.Errorf("uncommented file appeared more than once:\n%s", got)
	}
}

// TestComposeReviewCommentsOnly keeps the edit paragraph out when nothing was
// edited, so the agent is never told to re-read a file the user did not touch.
func TestComposeReviewCommentsOnly(t *testing.T) {
	m := New(nil, 100, 40)
	m.files = []fileEntry{{Change: git.Change{Path: "a.go"}}}
	m.comments["a.go"] = []comment{{Line: 3, Body: "rename this"}}

	got := m.composeReview(nil)
	if strings.Contains(got, "edited") {
		t.Errorf("review mentions edits when there were none:\n%s", got)
	}
}

// TestAnchorLine covers where a comment points. A deleted line has no
// post-image number, so it falls back to its old one rather than reporting 0 —
// a note on ":0" names nothing at all.
func TestAnchorLine(t *testing.T) {
	cases := []struct {
		name string
		line git.DiffLine
		want int
	}{
		{"added line uses the new number", git.DiffLine{Kind: git.DiffLineAdd, NewLine: 42}, 42},
		{"context uses the new number", git.DiffLine{Kind: git.DiffLineContext, OldLine: 10, NewLine: 12}, 12},
		{"deleted line falls back to the old number", git.DiffLine{Kind: git.DiffLineDelete, OldLine: 7}, 7},
	}
	for _, tc := range cases {
		if got := anchorLine(tc.line); got != tc.want {
			t.Errorf("%s: anchorLine = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestEditBufferStalenessGuard is the important one: the agent may be writing
// the same file while it is open for editing, and saving over its work would be
// both destructive and invisible.
func TestEditBufferStalenessGuard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	buf, err := newEditBuffer(path, "", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	buf.area.SetValue("edited by hand\n")

	// The agent writes the same file underneath us. mtime has a coarse
	// resolution on some filesystems, so move it explicitly rather than
	// relying on the clock to have ticked.
	if err := os.WriteFile(path, []byte("written by the agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	if !buf.Stale() {
		t.Fatal("a file rewritten under the buffer must read as stale")
	}
	if err := buf.Save(); err == nil {
		t.Fatal("Save must refuse a stale file")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "written by the agent\n" {
		t.Fatalf("the agent's write was clobbered: %q", content)
	}
}

// TestEditBufferSaveWritesAndRestamps covers the ordinary path, and that a
// second save compares against what was just written rather than against the
// pre-edit file — otherwise every save after the first reads as stale.
func TestEditBufferSaveWritesAndRestamps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	buf, err := newEditBuffer(path, "", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	buf.area.SetValue("first edit\n")
	if err := buf.Save(); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if content, _ := os.ReadFile(path); string(content) != "first edit\n" {
		t.Fatalf("first save wrote %q", content)
	}

	buf.area.SetValue("second edit\n")
	if err := buf.Save(); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if content, _ := os.ReadFile(path); string(content) != "second edit\n" {
		t.Fatalf("second save wrote %q", content)
	}
}

// TestEditBufferCleanSaveIsNoop keeps an opened-but-untouched file from
// tripping the guard, or from being rewritten for no reason.
func TestEditBufferCleanSaveIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte("untouched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf, err := newEditBuffer(path, "", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if buf.Dirty() {
		t.Fatal("a freshly read buffer must not be dirty")
	}
	// Even with the file changed underneath, a clean buffer has nothing to write.
	if err := os.WriteFile(path, []byte("agent wrote this\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := buf.Save(); err != nil {
		t.Fatalf("clean save must be a no-op, got %v", err)
	}
	if content, _ := os.ReadFile(path); string(content) != "agent wrote this\n" {
		t.Fatalf("clean save overwrote the file: %q", content)
	}
}

// TestCollectChangesDedupesStagedAndUnstaged covers a file that is both staged
// and modified again in the working tree: the review is about the tree as a
// whole, so it must be one row spanning both rather than two rows of the same
// path that scroll apart.
func TestCollectChangesDedupesStagedAndUnstaged(t *testing.T) {
	status := &git.StatusResult{
		Staged:    []git.Change{{Path: "b.go", Kind: git.ChangeModified, Staged: true}},
		Unstaged:  []git.Change{{Path: "b.go", Kind: git.ChangeModified}},
		Untracked: []git.Change{{Path: "a.go", Kind: git.ChangeUntracked}},
	}
	got := collectChanges(status)

	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	// Sorted by path, so the untracked a.go leads.
	if got[0].Path() != "a.go" || got[1].Path() != "b.go" {
		t.Fatalf("entries out of order: %s, %s", got[0].Path(), got[1].Path())
	}
	if got[1].Mode != git.DiffModeBoth {
		t.Errorf("a file staged and modified again must diff both, got mode %v", got[1].Mode)
	}
}

// TestTrimDiffMarker covers the column alignment: the +/- is drawn in the
// gutter, so leaving it on the content shows it twice and shifts every line one
// column right of where it sits in the file.
func TestTrimDiffMarker(t *testing.T) {
	cases := map[string]string{
		"+added":      "added",
		"-deleted":    "deleted",
		" context":    "context",
		"":            "",
		"@@ -1 +1 @@": "@@ -1 +1 @@",
	}
	for in, want := range cases {
		if got := trimDiffMarker(in); got != want {
			t.Errorf("trimDiffMarker(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEditedPathsTracksDirtyOnly keeps an opened-but-unchanged file out of the
// "I edited these" list.
func TestEditedPathsTracksDirtyOnly(t *testing.T) {
	dir := t.TempDir()
	touched := filepath.Join(dir, "touched.go")
	opened := filepath.Join(dir, "opened.go")
	for _, p := range []string{touched, opened} {
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := New(nil, 100, 40)
	m.files = []fileEntry{
		{Change: git.Change{Path: "opened.go"}},
		{Change: git.Change{Path: "touched.go"}},
	}
	for _, name := range []string{"opened.go", "touched.go"} {
		buf, err := newEditBuffer(filepath.Join(dir, name), "", false, 10)
		if err != nil {
			t.Fatal(err)
		}
		m.edits[name] = buf
	}
	m.edits["touched.go"].area.SetValue("changed\n")

	got := m.EditedPaths()
	if len(got) != 1 || got[0] != "touched.go" {
		t.Fatalf("EditedPaths = %v, want [touched.go]", got)
	}
	if !m.HasFeedback() {
		t.Error("an edited file counts as feedback")
	}
}

// TestOpeningAFileSkipsTheDiffPreamble covers what the window shows first. A
// diff opens with `diff --git`, `index`, `---` and `+++` — five lines that
// repeat what the window's own header already says. Landing on them would waste
// the top of every file the user selects.
func TestOpeningAFileSkipsTheDiffPreamble(t *testing.T) {
	diff := parsedFixture(t, `diff --git a/f.go b/f.go
index abc..def 100644
--- a/f.go
+++ b/f.go
@@ -10,3 +10,4 @@
 kept
+added
 tail`)

	m := New(nil, 120, 40)
	m.files = []fileEntry{{Change: git.Change{Path: "f.go"}, Diff: diff}}
	m.resetDiffCursor()

	// The viewport starts at the hunk header so its context is on screen...
	if top := entryLines(m)[m.diffTop]; !strings.HasPrefix(top.Content, "@@") {
		t.Errorf("viewport starts on %q, want the first hunk header", top.Content)
	}
	// ...but the cursor sits on the first real line of the file. Opening on the
	// header instead makes the very first `c` or `e` fail, since a header is
	// not a line anything can be anchored to.
	row, ok := m.cursorDiffLine()
	if !ok {
		t.Fatal("no cursor row after opening the file")
	}
	if row.Kind == git.DiffLineHeader {
		t.Fatalf("cursor opened on the header %q", row.Content)
	}
	if anchorLine(row) == 0 {
		t.Errorf("cursor opened on %q, which names no line of the file", row.Content)
	}
}

func entryLines(m *Model) []git.DiffLine { return m.Selected().Diff.Lines }

// TestFileHeadersKeepTheirMarkers guards the one place trimDiffMarker must not
// be applied: "---" and "+++" open with the same characters a diff marker does,
// and trimming turns them into "--" and "++".
func TestFileHeadersKeepTheirMarkers(t *testing.T) {
	diff := parsedFixture(t, `--- a/f.go
+++ b/f.go
@@ -1,1 +1,1 @@
+x`)

	m := New(nil, 120, 40)
	m.files = []fileEntry{{Change: git.Change{Path: "f.go"}, Diff: diff}}

	view := m.renderDiffPane(m.Selected())
	for _, want := range []string{"--- a/f.go", "+++ b/f.go"} {
		if !strings.Contains(view, want) {
			t.Errorf("file header %q was mangled:\n%s", want, view)
		}
	}
}

// TestNoDeletionsStillAlignsCounts guards the file-list column arithmetic. The
// deletion count uses "−" (U+2212) — three bytes, one column — so measuring it
// with len() reserves two columns too many and runs the name into the numbers.
func TestNoDeletionsStillAlignsCounts(t *testing.T) {
	m := New(nil, 120, 40)
	m.files = []fileEntry{
		{Change: git.Change{Path: "added-only.go"}, Added: 9},
		{Change: git.Change{Path: "both.go"}, Added: 8, Deleted: 2},
	}

	width := m.filesWidth()
	for i := range m.files {
		row := m.renderFileRow(i, width)
		if got := lipgloss.Width(stripStyles(row)); got != width {
			t.Errorf("row %d renders %d columns, want %d: %q", i, got, width, row)
		}
	}
}

// parsedFixture runs a diff fixture through the real parser so the tests
// exercise the same line numbering the window does.
func parsedFixture(t *testing.T, content string) *git.DiffResult {
	t.Helper()
	return git.ParseDiff("f.go", content)
}

// futureTime is an mtime comfortably past now, used to move a file's stamp
// explicitly rather than relying on the clock to have ticked — mtime
// resolution is coarse on some filesystems.
func futureTime() time.Time { return time.Now().Add(2 * time.Second) }

// stripStyles removes ANSI escapes so a rendered row can be measured.
func stripStyles(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && (r == 'm' || r == 'K'):
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestCommentEditorStaysInsideItsPane is the regression for a comment box that
// spilled across the file list and doubled the window's height. Its rows were a
// nested lipgloss border, which the outer frame re-measured and wrapped. Every
// row of a pane must be exactly the pane's width, or JoinHorizontal sizes the
// block to the widest one and the frame wraps everything that no longer fits.
func TestCommentEditorStaysInsideItsPane(t *testing.T) {
	m := New(nil, 120, 24)
	m.applyLoaded(loadedFrom("f.go", `@@ -10,3 +10,4 @@
 kept
+target line
 tail`))
	m.rowForLine(11, "")
	m.startComment()
	if m.commentArea == nil {
		t.Fatal("the comment editor did not open on a content line")
	}

	width := m.diffPaneWidth()
	for i, row := range m.renderCommentEditor(width) {
		if got := lipgloss.Width(row); got != width {
			t.Errorf("editor row %d is %d columns, want exactly %d: %q",
				i, got, width, stripStyles(row))
		}
	}

	// And the window as a whole stays rectangular at its stated width.
	for i, line := range strings.Split(m.View(), "\n") {
		if got := lipgloss.Width(line); got != 120 {
			t.Errorf("view line %d is %d columns, want 120: %q", i, got, stripStyles(line))
		}
	}
}

// TestCommentEditorSurvivesANilArea keeps a render from panicking if focus and
// the editor ever disagree.
func TestCommentEditorSurvivesANilArea(t *testing.T) {
	m := New(nil, 120, 24)
	if rows := m.renderCommentEditor(80); rows != nil {
		t.Errorf("expected no rows without an editor, got %d", len(rows))
	}
}

// TestEditorOpensOnTheSelectedLine covers the second half of "edit this": the
// buffer opens where the reader was looking, not at the top of the file. In a
// long file, hunting back to the line you just chose is most of the cost.
func TestEditorOpensOnTheSelectedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	body := ""
	for i := 1; i <= 40; i++ {
		body += "line " + strconv.Itoa(i) + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	buf, err := newEditBuffer(path, "", false, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !buf.area.ShowLineNumbers {
		t.Error("the editor must show line numbers")
	}

	buf.FocusLine(27)
	if got := buf.area.Line(); got != 26 {
		t.Errorf("cursor is on 0-based row %d, want 26 (file line 27)", got)
	}

	// A line past the end clamps rather than running off or doing nothing.
	buf.FocusLine(500)
	if got := buf.area.Line(); got < 39 {
		t.Errorf("cursor landed on row %d for an out-of-range line, want the last row", got)
	}
}
