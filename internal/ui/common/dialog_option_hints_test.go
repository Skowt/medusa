package common

import (
	"strings"
	"testing"
)

func newHintedDialog() *Dialog {
	d := NewSelectDialog("id", "Base Branch", "Which branch?",
		[]string{"Latest remote default", "Checked out branch", "Pick a branch"})
	d.SetOptionHints([]string{"Fetches origin.", "Does not fetch.", "Type a branch name."})
	d.SetSize(100, 40)
	d.Show()
	return d
}

func TestDialogRendersFocusedOptionHint(t *testing.T) {
	d := newHintedDialog()

	view := d.View()
	if !strings.Contains(view, "Fetches origin.") {
		t.Errorf("view is missing the focused option's hint:\n%s", view)
	}
	if strings.Contains(view, "Does not fetch.") {
		t.Errorf("view shows an unfocused option's hint:\n%s", view)
	}
}

func TestDialogHintFollowsCursor(t *testing.T) {
	d := newHintedDialog()
	d.cursor = 2

	view := d.View()
	if !strings.Contains(view, "Type a branch name.") {
		t.Errorf("hint did not follow the cursor to option 2:\n%s", view)
	}
	if strings.Contains(view, "Fetches origin.") {
		t.Errorf("view still shows option 0's hint after the cursor moved:\n%s", view)
	}
}

// A dialog with no hints must render exactly as before — SetOptionHints is
// opt-in and every other select dialog in the app relies on that.
func TestDialogWithoutHintsIsUnchanged(t *testing.T) {
	plain := NewSelectDialog("id", "Base Branch", "Which branch?", []string{"A", "B"})
	plain.SetSize(100, 40)
	plain.Show()

	hinted := NewSelectDialog("id", "Base Branch", "Which branch?", []string{"A", "B"})
	hinted.SetOptionHints(nil)
	hinted.SetSize(100, 40)
	hinted.Show()

	if plain.View() != hinted.View() {
		t.Error("SetOptionHints(nil) changed the rendered dialog")
	}
}

// Fewer hints than options must not panic — the trailing options just get none.
func TestDialogShortHintSliceIsSafe(t *testing.T) {
	d := NewSelectDialog("id", "T", "M", []string{"A", "B", "C"})
	d.SetOptionHints([]string{"only A"})
	d.SetSize(100, 40)
	d.Show()

	d.cursor = 2
	if view := d.View(); strings.Contains(view, "only A") {
		t.Errorf("option C rendered option A's hint:\n%s", view)
	}
}
