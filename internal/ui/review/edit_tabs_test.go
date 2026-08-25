package review

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestOpeningATabIndentedFileIsNotAnEdit is the first half of the tab problem.
// The textarea rewrites every tab as spaces on load and offers no way to stop
// it, so comparing the buffer with the file marks an untouched Go file as
// edited — which then lists it in the review as "I edited this by hand".
func TestOpeningATabIndentedFileIsNotAnEdit(t *testing.T) {
	buf := tabFileBuffer(t, "func f() {\n\tx := 1\n\t\ty := 2\n}\n")

	if buf.Dirty() {
		t.Error("a freshly opened tab-indented file must not read as edited")
	}
	if counts := buf.EditCounts(); counts.Any() {
		t.Errorf("EditCounts = %+v on an untouched file, want nothing", counts)
	}
}

// TestSavingPreservesUntouchedTabs is the half that actually destroys work.
// Saving the widget's value verbatim reindents the entire file — for Go that is
// a spurious whole-file diff, and for a Makefile it silently stops the recipes
// working, since there the tabs are syntax.
func TestSavingPreservesUntouchedTabs(t *testing.T) {
	source := "func f() {\n\tx := 1\n\t\ty := 2\n}\n"
	buf := tabFileBuffer(t, source)

	// Edit one line, leaving the rest alone.
	buf.area.SetValue(strings.Replace(buf.area.Value(), "x := 1", "x := 99", 1))
	if !buf.Dirty() {
		t.Fatal("the edit was not registered")
	}
	if err := buf.Save(); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, buf.path)
	want := "func f() {\n\tx := 99\n\t\ty := 2\n}\n"
	if got != want {
		t.Errorf("save reindented the file:\n got %q\nwant %q", got, want)
	}
}

// TestSavingLeavesSpaceIndentedFilesAlone keeps the tab restoration from
// running where it does not belong: a file that genuinely indents with spaces
// must not gain tabs because four of them happened to line up.
func TestSavingLeavesSpaceIndentedFilesAlone(t *testing.T) {
	source := "def f():\n    x = 1\n        y = 2\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "f.py")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	buf, err := newEditBuffer(path, "", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	buf.area.SetValue(strings.Replace(buf.area.Value(), "x = 1", "x = 99", 1))
	if err := buf.Save(); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	if strings.Contains(got, "\t") {
		t.Errorf("a space-indented file gained tabs on save: %q", got)
	}
	if want := "def f():\n    x = 99\n        y = 2\n"; got != want {
		t.Errorf("save changed the file:\n got %q\nwant %q", got, want)
	}
}

// TestRestoreIndentLeavesAlignmentAlone keeps the fold-back to leading
// whitespace only: spaces that align a trailing comment are the author's.
func TestRestoreIndentLeavesAlignmentAlone(t *testing.T) {
	cases := map[string]string{
		"        y := 2":        "\t\ty := 2",
		"    x := 1":            "\tx := 1",
		"x := 1    // aligned":  "x := 1    // aligned",
		"    x := 1    // both": "\tx := 1    // both",
		"  two spaces":          "  two spaces", // not a full indent level
	}
	for in, want := range cases {
		if got := restoreIndent(in); got != want {
			t.Errorf("restoreIndent(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEditCountsTrackEditing covers the header's counters: they have to move as
// the user types, since a number that never changes reads as a broken live
// value rather than as information. Added and removed are reported separately —
// a replaced line is one of each, and rolling them together reads as twice the
// edit that was made.
func TestEditCountsTrackEditing(t *testing.T) {
	buf := tabFileBuffer(t, "a\nb\nc\nd\n")

	if counts := buf.EditCounts(); counts.Any() {
		t.Fatalf("untouched buffer counts %+v", counts)
	}

	// Rewriting a line is ONE modification. Reported as an add plus a remove —
	// which is all an LCS sees — changing a character shows a green bar and a
	// red "1 line removed" rule: two rows and two colours for one keystroke.
	buf.area.SetValue("a\nB\nc\nd\n")
	if got := buf.EditCounts(); got != (editCounts{Modified: 1}) {
		t.Errorf("replacing one line = %+v, want 1 modified and nothing else", got)
	}

	buf.area.SetValue("a\nb\nNEW\nc\nd\n") // pure insertion
	if got := buf.EditCounts(); got != (editCounts{Added: 1}) {
		t.Errorf("inserting one line = %+v, want 1 added", got)
	}

	// Deleting counts too, or removing a block reads as no edit at all.
	buf.area.SetValue("a\n")
	if got := buf.EditCounts(); got.Removed < 3 {
		t.Errorf("deleting three lines reported %+v", got)
	}
}

// TestRetypingWhatWasDeletedClearsTheLine covers undo by hand: delete a
// character and put it back and the line is what it always was, so it must stop
// reading as edited. A buffer that keeps its bars once touched cannot be
// trusted to say what is actually different.
func TestRetypingWhatWasDeletedClearsTheLine(t *testing.T) {
	buf := tabFileBuffer(t, "alpha\nbeta\n")

	buf.area.SetValue("alph\nbeta\n")
	if !buf.EditCounts().Any() {
		t.Fatal("deleting a character was not registered as a change")
	}

	buf.area.SetValue("alpha\nbeta\n")
	if got := buf.EditCounts(); got.Any() {
		t.Errorf("restoring the character left the line marked %+v", got)
	}
	if marks := buf.Marks(); len(marks) != 0 {
		t.Errorf("restored buffer still carries marks: %+v", marks)
	}
}

// TestSingleCharacterEditIsOneModifiedLine is the shape the gutter has to show:
// one amber bar, no removal rule, no second row. An LCS has no notion of
// "changed" — a rewritten line is a delete plus an insert — so without pairing
// them, fixing a typo draws a green bar AND a red "1 line removed" rule.
func TestSingleCharacterEditIsOneModifiedLine(t *testing.T) {
	buf := tabFileBuffer(t, "one\ntwo\nthree\n")
	buf.area.SetValue("one\ntwoo\nthree\n")

	marks := buf.Marks()
	if got := marks[1].Kind; got != lineModified {
		t.Errorf("the edited line is marked %v, want modified", got)
	}
	for i, mark := range marks {
		if len(mark.RemovedBefore) != 0 {
			t.Errorf("line %d reports %d removals; a rewrite is not a deletion",
				i, len(mark.RemovedBefore))
		}
	}
	if got := buf.EditCounts(); got != (editCounts{Modified: 1}) {
		t.Errorf("counts = %+v, want exactly one modification", got)
	}
}

// TestTwoDistantChangesLeaveTheMiddleAlone is the bug that made a real file
// unreadable: with two edits ninety lines apart, every line between them was
// drawn as changed.
//
// The cause was a hand-rolled quadratic LCS with an area budget — past the cap
// it gave up and marked the whole changed window. 91×91 exceeds 4000, which is
// an ordinary source file with two edits in it. A real diff has no such cliff,
// which is why this is go-udiff's Myers rather than something local.
func TestTwoDistantChangesLeaveTheMiddleAlone(t *testing.T) {
	var was, now []string
	for i := range 200 {
		was = append(was, "line "+strconv.Itoa(i))
		now = append(now, "line "+strconv.Itoa(i))
	}
	now[25] = "CHANGED A"
	now[115] = "CHANGED B"

	marks := diffLines(was, now)
	for i, mark := range marks {
		want := lineSame
		if i == 25 || i == 115 {
			want = lineModified
		}
		if mark.Kind != want {
			t.Errorf("line %d (%q) marked %v, want %v", i, now[i], mark.Kind, want)
		}
	}
	if got := countEdits(marks); got != (editCounts{Modified: 2}) {
		t.Errorf("counts = %+v, want exactly two modifications", got)
	}
}

// TestInsertionIsNotReportedAsARewrite guards the normalization. An edit script
// is not unique, and the line-level conversion readily emits "delete b, insert
// b, insert NEW" for what the user experienced as inserting one line. Taken at
// face value that reports two changed lines.
func TestInsertionIsNotReportedAsARewrite(t *testing.T) {
	marks := diffLines([]string{"a", "b", "c", "d"}, []string{"a", "b", "NEW", "c", "d"})

	if got := countEdits(marks); got != (editCounts{Added: 1}) {
		t.Errorf("inserting one line = %+v, want exactly one addition", got)
	}
	if marks[2].Kind != lineAdded {
		t.Errorf("the inserted line is marked %v, want added", marks[2].Kind)
	}
	for _, i := range []int{0, 1, 3, 4} {
		if marks[i].Kind != lineSame {
			t.Errorf("line %d was marked %v by an insertion elsewhere", i, marks[i].Kind)
		}
	}
}

// TestDiffIsFastOnALargeFile keeps the editor responsive per keystroke: the
// marks are recomputed every frame, so a diff that scales badly is felt as lag
// rather than seen as a wrong answer.
func TestDiffIsFastOnALargeFile(t *testing.T) {
	var was []string
	for i := range 20000 {
		was = append(was, "line "+strconv.Itoa(i))
	}
	now := append([]string(nil), was...)
	now[9000] = "edited"

	start := time.Now()
	marks := diffLines(was, now)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("diffing a 20k-line file took %s", elapsed)
	}
	if got := countEdits(marks); got != (editCounts{Modified: 1}) {
		t.Errorf("counts = %+v, want one modification", got)
	}
}

func tabFileBuffer(t *testing.T, source string) *editBuffer {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.go")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	buf, err := newEditBuffer(path, "", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	return buf
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
