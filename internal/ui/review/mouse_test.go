package review

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/git"
)

// clickable renders the window and returns it with hit targets populated, plus
// the rendered lines so a test can assert a click lands on what it looks like
// it lands on.
func clickable(t *testing.T) (*Model, []string) {
	t.Helper()
	m := newCommentedModel(t)
	m.files = append(m.files, fileEntry{Change: git.Change{Path: "other.go"}})
	m.focus = paneFiles
	return m, strings.Split(m.View(), "\n")
}

func click(m *Model, x, y int) *Model {
	updated, _, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	return updated
}

func clickResult(m *Model, x, y int) *Result {
	_, _, res := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	return res
}

// TestClickSelectsTheFileUnderThePointer is the property that makes the hit map
// worth having: the row a click resolves to must be the row it visibly landed
// on. Recomputing geometry rather than recording it during render is how those
// two drift apart.
func TestClickSelectsTheFileUnderThePointer(t *testing.T) {
	m, lines := clickable(t)

	for y, line := range lines {
		plain := stripStyles(line)
		if !strings.Contains(plain, "other.go") {
			continue
		}
		m = click(m, frameLeft+2, y)
		if got := m.Selected().Path(); got != "other.go" {
			t.Fatalf("clicking the row showing other.go selected %q", got)
		}
		if m.focus != paneFiles {
			t.Error("clicking the file list did not focus it")
		}
		return
	}
	t.Fatal("other.go was not rendered")
}

// TestClickSelectsACommentRow covers reaching a note with the mouse alone,
// which is the whole point of tracking rows rather than diff lines.
func TestClickSelectsACommentRow(t *testing.T) {
	m, _ := clickable(t)
	m.focus = paneDiff
	lines := strings.Split(m.View(), "\n")

	diffX := frameLeft + m.filesWidth() + 4
	for y, line := range lines {
		if !strings.Contains(stripStyles(line), "and drop this one") {
			continue
		}
		m = click(m, diffX, y)
		note, ok := m.cursorComment()
		if !ok {
			t.Fatalf("clicking a comment row did not select a comment (row %d)", y)
		}
		if note.Body != "and drop this one" {
			t.Errorf("selected the wrong comment: %q", note.Body)
		}
		if m.focus != paneDiff {
			t.Error("clicking the diff pane did not focus it")
		}
		return
	}
	t.Fatal("the comment was not rendered")
}

// TestClickingDiscardClosesWithoutSaving covers the footer buttons, which have
// to be reachable by pointer or the window has two ways in and one way out.
func TestClickingDiscardClosesWithoutSaving(t *testing.T) {
	m, _ := clickable(t)

	res := clickResult(m, m.discardHit.X+1, m.discardHit.Y)
	if res == nil {
		t.Fatal("clicking Discard did not close the window")
	}
	if res.Saved {
		t.Error("Discard saved the review")
	}
}

// TestClickingSaveSendsTheReview covers the other button, and that it carries
// the comments with it.
func TestClickingSaveSendsTheReview(t *testing.T) {
	m, _ := clickable(t)

	res := clickResult(m, m.saveHit.X+1, m.saveHit.Y)
	if res == nil || !res.Saved {
		t.Fatalf("clicking Save & Send did not submit: %+v", res)
	}
	if !strings.Contains(res.Review, "rename this") {
		t.Errorf("the sent review is missing a comment:\n%s", res.Review)
	}
}

// TestClickingSaveWithNothingToSendSaysSo keeps the button from closing the
// window on an empty review, which reads as the comments having been lost.
func TestClickingSaveWithNothingToSendSaysSo(t *testing.T) {
	m := New(nil, 110, 20)
	m.applyLoaded(loadedFrom("f.go", "@@ -1,2 +1,3 @@\n kept\n+added\n tail"))
	_ = m.View()

	if res := clickResult(m, m.saveHit.X+1, m.saveHit.Y); res != nil {
		t.Fatalf("an empty review closed the window: %+v", res)
	}
	if m.statusMsg == "" {
		t.Error("clicking Save with nothing to send gave no feedback")
	}
}

// TestButtonsDoNotOverlap guards the footer arithmetic.
func TestButtonsDoNotOverlap(t *testing.T) {
	m, _ := clickable(t)
	if m.saveHit.Width == 0 || m.discardHit.Width == 0 {
		t.Fatal("a footer button has no hit region")
	}
	if m.saveHit.X+m.saveHit.Width > m.discardHit.X {
		t.Errorf("Save (%d..%d) overlaps Discard (%d)",
			m.saveHit.X, m.saveHit.X+m.saveHit.Width, m.discardHit.X)
	}
	if m.saveHit.Y != m.discardHit.Y {
		t.Error("the footer buttons are on different rows")
	}
}
