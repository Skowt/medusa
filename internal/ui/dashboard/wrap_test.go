package dashboard

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestWrapName_ShortFitsOneLine(t *testing.T) {
	got := wrapName("alpha", 20, 3)
	if len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("want [alpha], got %q", got)
	}
}

func TestWrapName_WrapsAtWidth(t *testing.T) {
	got := wrapName("abcdefghij", 4, 3)
	if len(got) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(got), got)
	}
	for _, l := range got {
		if w := lipgloss.Width(l); w > 4 {
			t.Errorf("line %q width %d exceeds 4", l, w)
		}
	}
}

func TestWrapName_PrefersHyphenBoundary(t *testing.T) {
	// "no-ticket-x" at width 10 should break after a hyphen, not mid-token.
	got := wrapName("no-ticket-x", 10, 3)
	if !strings.HasSuffix(got[0], "-") {
		t.Errorf("expected first line to end at a hyphen, got %q", got[0])
	}
}

func TestWrapName_CapEllipsizesLastLine(t *testing.T) {
	got := wrapName("abcdefghijklmnop", 4, 2)
	if len(got) != 2 {
		t.Fatalf("want 2 lines (capped), got %d: %q", len(got), got)
	}
	if !strings.HasSuffix(got[1], "…") {
		t.Errorf("capped last line should end with ellipsis, got %q", got[1])
	}
	if w := lipgloss.Width(got[1]); w > 4 {
		t.Errorf("capped line width %d exceeds 4", w)
	}
}

func TestTruncateRunes_AppendsEllipsis(t *testing.T) {
	got := truncateRunes([]rune("abcdefgh"), 5)
	if lipgloss.Width(got) > 5 {
		t.Errorf("width %d exceeds 5: %q", lipgloss.Width(got), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("want trailing ellipsis, got %q", got)
	}
}
