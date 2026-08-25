package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
)

// gitFixture builds a repo with one committed file, then rewrites it the way
// an agent would, and opens the review window on it.
func gitFixture(t *testing.T, committed, working string) *Model {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(committed)
	run("add", "-A")
	run("commit", "-qm", "base")
	write(working)

	m := New(data.NewWorkspace("w", "b", "main", dir, dir), 96, 22)
	m.Update(m.load()())
	if m.Selected() == nil {
		t.Fatal("the fixture produced no changed files")
	}
	return m
}

const committedFile = "package main\n\nfunc main() {\n\tone()\n\ttwo()\n\tthree()\n}\n"
const agentEdited = "package main\n\nfunc main() {\n\tone()\n\ttwo()\n\tAGENT()\n}\n"

// TestLeavingTheEditorShowsUnsavedEdits is the bug that made the two views
// disagree. A file's diff describes what is on disk, so a buffer typed into and
// not saved is invisible in it — pressing esc showed the diff exactly as it was
// before the user started, which reads as their work having been thrown away.
func TestLeavingTheEditorShowsUnsavedEdits(t *testing.T) {
	m := gitFixture(t, committedFile, agentEdited)
	m.startEdit()
	m.edits["f.go"].area.SetValue(
		"package main\n\nINSERTED A\nINSERTED B\nfunc main() {\n    one()\n    two()\n    AGENT()\n}\n")

	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	// Assert on the diff itself, not the visible window: the cursor lands on
	// the line that was being edited, so the top of the file is legitimately
	// scrolled off.
	diff := allRowText(m)
	for _, want := range []string{"INSERTED A", "INSERTED B"} {
		if !strings.Contains(diff, want) {
			t.Errorf("the rebuilt diff does not contain %q:\n%s", want, diff)
		}
	}
	if view := stripStyles(m.renderDiffPane(m.Selected())); !strings.Contains(view, "unsaved") {
		t.Errorf("the pane does not say the edits are unsaved:\n%s", view)
	}
}

// allRowText joins every navigable row of the selected file, so a test can
// assert on the whole diff rather than on whatever fits the viewport.
func allRowText(m *Model) string {
	var b strings.Builder
	for _, row := range m.paneRows() {
		b.WriteString(row.Line.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// TestBaseMarkersFollowTheirLines is the other half. The git diff's line
// numbers are fixed when the window opens, so an unsaved insertion above a
// changed line left every marker below pointing at whatever slid into its slot
// — the indicator stayed with the number instead of with the line.
func TestBaseMarkersFollowTheirLines(t *testing.T) {
	m := gitFixture(t, committedFile, agentEdited)
	m.startEdit()
	buf := m.edits["f.go"]

	// AGENT() is line 6 on disk. Two insertions above push it to line 8.
	buf.area.SetValue(
		"package main\n\nINSERTED A\nINSERTED B\nfunc main() {\n    one()\n    two()\n    AGENT()\n}\n")

	view := stripStyles(m.renderEditPane(m.Selected()))
	marked := markerFor(t, view, "AGENT()")
	if marked != "+" {
		t.Errorf("the agent's line lost its marker after an insertion above it: %q\n%s", marked, view)
	}
	// And the line that merely moved must not have acquired one.
	if got := markerFor(t, view, "two()"); got != " " {
		t.Errorf("an unchanged line was marked %q after an insertion above it:\n%s", got, view)
	}
}

// TestRewriteShowsBothHalvesInTheDiff covers the split between the two views of
// one edit: the editor's gutter calls a rewrite a single change, but a *diff*
// of it has to show what was replaced, or the new line reads as a pure
// insertion and the old text is simply gone.
func TestRewriteShowsBothHalvesInTheDiff(t *testing.T) {
	m := gitFixture(t, committedFile, agentEdited)
	m.startEdit()
	m.edits["f.go"].area.SetValue(
		"package main\n\nfunc main() {\n    one()\n    two()\n    MINE()\n}\n")
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	diff := allRowText(m)
	if !strings.Contains(diff, "-\tthree()") && !strings.Contains(diff, "-    three()") {
		t.Errorf("the diff lost the committed line that was replaced:\n%s", diff)
	}
	if !strings.Contains(diff, "+    MINE()") {
		t.Errorf("the diff does not show the replacement:\n%s", diff)
	}
}

// TestUntrackedFileIsAllNew keeps a file with no committed version from
// crashing or reading as unchanged — every line of it is an addition.
func TestUntrackedFileIsAllNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.go")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh, err := newEditBuffer(path, "", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	for i, mark := range fresh.BaseMarks() {
		if mark.Kind != lineAdded {
			t.Errorf("line %d of an untracked file is marked %v, want added", i, mark.Kind)
		}
	}
}

// TestViewIsCheapEnoughToScroll guards the frame cost. The window redraws on
// every wheel event and every keystroke, and the two structures it is built
// from — the line diff and the row list — are each read several times per
// frame. Uncached, one scroll ran four or five full diffs of the file.
func TestViewIsCheapEnoughToScroll(t *testing.T) {
	src, err := os.ReadFile("view_edit.go")
	if err != nil {
		t.Skip(err)
	}
	body := string(src)
	m := gitFixture(t, body, strings.Replace(body, "editGutter = 6", "editGutter = 7", 1))
	m.SetSize(100, 30)
	m.startEdit()
	m.edits["f.go"].area.SetValue(m.edits["f.go"].area.Value() + "// edited\n")
	m.View() // prime the caches, as the first frame does

	if raceEnabled {
		// The budget below is wall-clock, and the race detector multiplies it
		// by enough that the number means nothing. The caching this guards is
		// still exercised — only the assertion is dropped.
		t.Skip("timing budget is meaningless under -race")
	}

	const frames = 60
	start := time.Now()
	for i := range frames {
		m.scrollDiff(1 + i%3)
		m.View()
	}
	per := time.Since(start) / frames
	if per > 8*time.Millisecond {
		t.Errorf("a scrolled frame costs %v; the view would visibly lag", per)
	}
}

// TestMarksAreCachedOnContent is the mechanism the budget above relies on. The
// marks are read by the gutter, the header and the rebuilt diff, so recomputing
// per read means several full diffs a frame.
func TestMarksAreCachedOnContent(t *testing.T) {
	buf := tabFileBuffer(t, "a\nb\nc\n")
	buf.area.SetValue("a\nB\nc\n")

	first := buf.Marks()
	if &first[0] != &buf.Marks()[0] {
		t.Error("the marks were recomputed for an unchanged buffer")
	}

	buf.area.SetValue("a\nB\nC\n")
	if &first[0] == &buf.Marks()[0] {
		t.Error("the marks survived a change to the buffer")
	}
}

// markerFor returns the gutter marker on the rendered row holding a substring.
func markerFor(t *testing.T, view, want string) string {
	t.Helper()
	for _, row := range strings.Split(view, "\n") {
		if !strings.Contains(row, want) {
			continue
		}
		// The marker is the last column of the gutter, just before the body.
		if len(row) > editGutter {
			return string(row[editGutter-1])
		}
	}
	t.Fatalf("no row contains %q in:\n%s", want, view)
	return ""
}
