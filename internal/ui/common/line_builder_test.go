package common

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func frameStyle(inner int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Width(inner)
}

func TestLineBuilder_NoWrap_RegionsMatchRows(t *testing.T) {
	b := NewLineBuilder(frameStyle(40), 40)
	b.Append("", "Title")
	b.Blank()
	b.Append("first", "[ ] Option one")
	b.Append("second", "[ ] Option two")
	b.Append("third", "[ ] Option three")

	want := map[string]int{"first": 2, "second": 3, "third": 4}
	for id, wantY := range want {
		r, ok := b.RegionByID(id)
		if !ok {
			t.Fatalf("region %q missing", id)
		}
		if r.Y != wantY || r.Height != 1 {
			t.Errorf("%s: got Y=%d H=%d, want Y=%d H=1", id, r.Y, r.Height, wantY)
		}
	}
}

func TestLineBuilder_WrapsLongLine_AdvancesYByRenderedRows(t *testing.T) {
	// Inner width 20; line "0123456789 0123456789 0123456789" (32 cols) must wrap.
	b := NewLineBuilder(frameStyle(20), 20)
	b.Append("first", "short")
	b.Append("long", "0123456789 0123456789 0123456789")
	b.Append("after", "short again")

	longRegion, _ := b.RegionByID("long")
	if longRegion.Height < 2 {
		t.Fatalf("expected long line to wrap to ≥2 rows, got Height=%d", longRegion.Height)
	}

	after, _ := b.RegionByID("after")
	wantAfterY := longRegion.Y + longRegion.Height
	if after.Y != wantAfterY {
		t.Errorf("after: Y=%d, expected %d (long.Y=%d + long.Height=%d)",
			after.Y, wantAfterY, longRegion.Y, longRegion.Height)
	}
}

func TestLineBuilder_SizeMatchesRenderedView(t *testing.T) {
	b := NewLineBuilder(frameStyle(40), 40)
	b.Append("", "Title")
	b.Blank()
	b.Append("a", "[ ] Short item")
	b.Append("b", "[ ] This one is definitely long enough to wrap at forty columns")
	b.Append("c", "[ ] Another short")

	view := b.View()
	renderedHeight := strings.Count(view, "\n") + 1
	renderedWidth := 0
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > renderedWidth {
			renderedWidth = w
		}
	}

	w, h := b.Size()
	if w != renderedWidth {
		t.Errorf("width: Size=%d, rendered=%d", w, renderedWidth)
	}
	if h != renderedHeight {
		t.Errorf("height: Size=%d, rendered=%d", h, renderedHeight)
	}
}

func TestLineBuilder_ContentOffsetMatchesFrame(t *testing.T) {
	style := frameStyle(40) // border + padding(1,2) = frame 6x4
	b := NewLineBuilder(style, 40)
	x, y := b.ContentOffset()
	if x != 3 || y != 2 {
		t.Errorf("ContentOffset = (%d,%d), want (3,2)", x, y)
	}
}
