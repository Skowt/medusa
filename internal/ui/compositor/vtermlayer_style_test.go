package compositor

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/Skowt/medusa/internal/vterm"
)

// TestPaletteColorsStayPaletteReferences guards against resolving ANSI colors
// to RGB on the way out. The outer terminal owns the palette; emitting a
// hard-coded RGB for "red" or "bright blue" (Claude's link color) overrode the
// user's theme for every ANSI color an agent printed.
func TestPaletteColorsStayPaletteReferences(t *testing.T) {
	t.Parallel()
	cases := []struct {
		color vterm.Color
		want  string
	}{
		{vterm.Color{Type: vterm.ColorIndexed, Value: 1}, "31"},
		{vterm.Color{Type: vterm.ColorIndexed, Value: 12}, "94"},
		{vterm.Color{Type: vterm.ColorIndexed, Value: 208}, "38;5;208"},
		{vterm.Color{Type: vterm.ColorRGB, Value: 0xd77757}, "38;2;215;119;87"},
	}
	for _, tc := range cases {
		style := vtermStyleToUV(vterm.Style{Fg: tc.color})
		got := style.String()
		if !strings.Contains(got, tc.want) {
			t.Errorf("color %+v rendered as %q, want it to contain %q", tc.color, got, tc.want)
		}
		if tc.color.Type == vterm.ColorIndexed && strings.Contains(got, "38;2;") {
			t.Errorf("palette color %+v was resolved to RGB: %q", tc.color, got)
		}
	}
}

func TestUnderlineStylesReachUltraviolet(t *testing.T) {
	t.Parallel()
	want := map[vterm.UnderlineStyle]uv.Underline{
		vterm.UnderlineNone:   uv.UnderlineNone,
		vterm.UnderlineSingle: uv.UnderlineSingle,
		vterm.UnderlineDouble: uv.UnderlineDouble,
		vterm.UnderlineCurly:  uv.UnderlineCurly,
		vterm.UnderlineDotted: uv.UnderlineDotted,
		vterm.UnderlineDashed: uv.UnderlineDashed,
	}
	for in, out := range want {
		if got := vtermStyleToUV(vterm.Style{Underline: in}).Underline; got != out {
			t.Errorf("underline %d -> %d, want %d", in, got, out)
		}
	}
}
