package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/git"
)

// beforeDiff and afterDiff are the same file before and after the agent inserts
// two lines above the commented one, which is the ordinary case a live window
// has to survive: nothing about the line the user cared about changed, but its
// number did.
const beforeDiff = `@@ -10,3 +10,4 @@
 kept
+target line
 tail`

const afterDiff = `@@ -10,3 +10,6 @@
 kept
+inserted one
+inserted two
+target line
 tail`

func loadedFrom(path, content string) filesLoaded {
	diff := git.ParseDiff(path, content)
	return filesLoaded{files: []fileEntry{{
		Change:  git.Change{Path: path},
		Diff:    diff,
		Added:   diff.AddedLines(),
		Deleted: diff.DeletedLines(),
	}}}
}

// TestRefreshReanchorsCommentsToMovedLines is the point of the live view: a
// note must follow the line it was written against. Left on its original
// number it would drift one line further out of place on every insertion the
// agent makes above it, and the review would arrive pointing at the wrong code.
func TestRefreshReanchorsCommentsToMovedLines(t *testing.T) {
	m := New(nil, 120, 40)
	m.applyLoaded(loadedFrom("f.go", beforeDiff))
	m.comments["f.go"] = []comment{{Line: 11, Quote: "target line", Body: "rename this"}}

	m.applyLoaded(loadedFrom("f.go", afterDiff))

	notes := m.comments["f.go"]
	if len(notes) != 1 {
		t.Fatalf("got %d comments, want 1", len(notes))
	}
	if notes[0].Line != 13 {
		t.Errorf("comment stayed on line %d; the target moved to 13", notes[0].Line)
	}
	if notes[0].Stale {
		t.Error("a comment whose line was found again must not be stale")
	}
}

// TestRefreshMarksCommentsStaleWhenTheirLineIsGone covers the other half: the
// note is kept — it still says something worth sending — but must not claim to
// point at a line the agent has removed.
func TestRefreshMarksCommentsStaleWhenTheirLineIsGone(t *testing.T) {
	m := New(nil, 120, 40)
	m.applyLoaded(loadedFrom("f.go", beforeDiff))
	m.comments["f.go"] = []comment{{Line: 11, Quote: "target line", Body: "rename this"}}

	m.applyLoaded(loadedFrom("f.go", `@@ -10,3 +10,3 @@
 kept
+something else entirely
 tail`))

	notes := m.comments["f.go"]
	if len(notes) != 1 {
		t.Fatalf("a comment must survive its line disappearing, got %d", len(notes))
	}
	if !notes[0].Stale {
		t.Error("a comment whose quoted line is gone must be marked stale")
	}
	if notes[0].Body != "rename this" {
		t.Errorf("the comment body was lost: %q", notes[0].Body)
	}
}

// TestRefreshPrefersTheNearestMatch guards against a note jumping across a file
// that repeats a line. Closing braces and blank lines occur everywhere, and the
// first textual match is usually not the one the user wrote against.
func TestRefreshPrefersTheNearestMatch(t *testing.T) {
	diff := git.ParseDiff("f.go", `@@ -1,6 +1,6 @@
+}
 a
 b
 c
+}
 d`)

	// Written against the second "}", at the far end of the hunk.
	got, ok := findQuotedLine(diff, "}", 5)
	if !ok {
		t.Fatal("no match found")
	}
	if got != 5 {
		t.Errorf("re-anchored to line %d, want the nearer match at 5", got)
	}
}

// TestRefreshKeepsAnnotatedFilesTheAgentReverted covers the case where dropping
// the user's work would be silent: the agent undoes a file, git stops reporting
// it, and the comments written against it would go with it.
func TestRefreshKeepsAnnotatedFilesTheAgentReverted(t *testing.T) {
	m := New(nil, 120, 40)
	m.applyLoaded(loadedFrom("f.go", beforeDiff))
	m.comments["f.go"] = []comment{{Line: 11, Quote: "target line", Body: "rename this"}}

	// The workspace comes back clean.
	m.applyLoaded(filesLoaded{})

	if len(m.files) != 1 {
		t.Fatalf("got %d files, want the annotated one kept", len(m.files))
	}
	if !m.files[0].Gone {
		t.Error("a reverted file must be marked gone")
	}
	if len(m.comments["f.go"]) != 1 {
		t.Error("comments must survive their file leaving the change set")
	}
	if !m.comments["f.go"][0].Stale {
		t.Error("a comment with no diff left to anchor to must be stale")
	}
}

// TestRefreshKeepsTheCursorOnItsFileAndLine covers what makes the live view
// usable: a refresh must not move the reader. The cursor is restored by path
// and by file line, because a re-diff changes which row any index names.
func TestRefreshKeepsTheCursorOnItsFileAndLine(t *testing.T) {
	m := New(nil, 120, 40)
	m.applyLoaded(loadedFrom("f.go", beforeDiff))

	// Put the cursor on "target line" (file line 11).
	m.rowForLine(11, "")
	before, ok := m.cursorDiffLine()
	if !ok || trimDiffMarker(before.Content) != "target line" {
		t.Fatalf("setup: cursor is on %q", before.Content)
	}

	m.applyLoaded(loadedFrom("f.go", afterDiff))

	after, ok := m.cursorDiffLine()
	if !ok {
		t.Fatal("cursor lost after refresh")
	}
	if trimDiffMarker(after.Content) != "target line" {
		t.Errorf("cursor drifted to %q, want it still on \"target line\"", after.Content)
	}
}

// TestRefreshDoesNotDiscardEditBuffers keeps unsaved typing alive across a
// refresh. The agent writing any file in the workspace triggers one, so losing
// buffers here would make editing unusable while an agent is running.
func TestRefreshDoesNotDiscardEditBuffers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(nil, 120, 40)
	m.applyLoaded(loadedFrom("f.go", beforeDiff))
	buf, err := newEditBuffer(path, "", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	buf.area.SetValue("my unsaved work\n")
	m.edits["f.go"] = buf

	m.applyLoaded(loadedFrom("f.go", afterDiff))

	kept := m.edits["f.go"]
	if kept == nil {
		t.Fatal("the edit buffer was discarded by a refresh")
	}
	if kept.area.Value() != "my unsaved work\n" {
		t.Errorf("buffer content changed to %q", kept.area.Value())
	}
	if len(m.EditedPaths()) != 1 {
		t.Error("the buffer must still count as edited after a refresh")
	}
}

// TestRefreshFlagsEditConflicts surfaces a collision when it happens rather
// than at save time, so the user is not still typing into a buffer that can no
// longer be written.
func TestRefreshFlagsEditConflicts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(nil, 120, 40)
	buf, err := newEditBuffer(path, "", false, 10)
	if err != nil {
		t.Fatal(err)
	}
	buf.area.SetValue("my unsaved work\n")
	m.edits["f.go"] = buf

	// No collision yet.
	m.applyLoaded(loadedFrom("f.go", beforeDiff))
	if m.files[0].EditConflict {
		t.Fatal("flagged a conflict before the file changed")
	}

	// The agent rewrites it underneath.
	if err := os.WriteFile(path, []byte("the agent's version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, futureTime(), futureTime()); err != nil {
		t.Fatal(err)
	}
	m.applyLoaded(loadedFrom("f.go", afterDiff))

	if !m.files[0].EditConflict {
		t.Error("a rewritten file with an open dirty buffer must be flagged")
	}
}

// TestRefreshCoalescesWhileLoading keeps a busy agent from queueing a read per
// event; the window would then always be showing a state the agent had already
// moved past.
func TestRefreshCoalescesWhileLoading(t *testing.T) {
	m := New(nil, 120, 40)
	m.loading = true

	if cmd := m.Refresh(); cmd != nil {
		t.Error("a refresh during a read must not start a second one")
	}
	if !m.refreshPending {
		t.Error("the dropped refresh must be remembered, or the last write is never shown")
	}
}

// TestRevertedFileShowsItsComments makes the kept-file behaviour visible. A row
// held open purely to preserve its notes has to actually show them, or keeping
// it is indistinguishable from having dropped them.
func TestRevertedFileShowsItsComments(t *testing.T) {
	m := New(nil, 120, 40)
	m.applyLoaded(loadedFrom("f.go", beforeDiff))
	m.comments["f.go"] = []comment{{Line: 11, Quote: "target line", Body: "rename this"}}

	m.applyLoaded(filesLoaded{})

	view := stripStyles(m.renderDiffPane(m.Selected()))
	for _, want := range []string{"No longer changed", "rename this", "was line 11"} {
		if !strings.Contains(view, want) {
			t.Errorf("reverted file's pane is missing %q:\n%s", want, view)
		}
	}
}
