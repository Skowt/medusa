package common

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// lineCount is the number of rendered rows: box border (2), vertical padding
// (2), title, the blank line from the title's bottom margin, and one row per
// step. Anything more means a step wrapped.
func lineCount(steps int) int { return 2 + 2 + 1 + 1 + steps }

func renderProgress(t *testing.T, termWidth int, detail string, steps ...string) []string {
	t.Helper()
	o := NewProgressOverlay("Creating Workspace", steps)
	o.SetSize(termWidth, 40)
	o.SetStepDetail(detail)
	return strings.Split(o.View(), "\n")
}

func TestProgressOverlayKeepsLongDetailOnOneLine(t *testing.T) {
	steps := []string{"Fetching latest changes", "Creating worktree", "Copying gitignored files"}
	lines := renderProgress(t, 120, "a-really-long-repository-name-that-would-wrap", steps...)

	if got, want := len(lines), lineCount(len(steps)); got != want {
		t.Fatalf("rendered %d lines, want %d (a step wrapped):\n%s", got, want, strings.Join(lines, "\n"))
	}

	boxWidth := lipgloss.Width(lines[0])
	if boxWidth > progressMaxText+2*progressPadX+2 {
		t.Fatalf("box grew to %d cells, wider than the cap", boxWidth)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != boxWidth {
			t.Errorf("line %d is %d cells wide, want %d: %q", i, w, boxWidth, line)
		}
	}
}

func TestProgressOverlayFitsNarrowTerminal(t *testing.T) {
	steps := []string{"Fetching latest changes", "Creating worktree"}
	const termWidth = 30
	lines := renderProgress(t, termWidth, "places", steps...)

	if got, want := len(lines), lineCount(len(steps)); got != want {
		t.Fatalf("rendered %d lines, want %d (a step wrapped):\n%s", got, want, strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > termWidth {
			t.Errorf("line %d is %d cells wide, wider than the %d-cell terminal: %q", i, w, termWidth, line)
		}
	}
}

func TestProgressOverlayShortDetailKeepsDefaultWidth(t *testing.T) {
	lines := renderProgress(t, 120, "app", "Creating worktree")

	if got, want := lipgloss.Width(lines[0]), progressMinText+2*progressPadX+2; got != want {
		t.Errorf("box is %d cells wide, want the default %d", got, want)
	}
}
