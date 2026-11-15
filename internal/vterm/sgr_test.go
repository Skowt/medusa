package vterm

import "testing"

// styleAfter writes an SGR sequence followed by one character and returns the
// style that character was written with.
func styleAfter(t *testing.T, seq string) Style {
	t.Helper()
	vt := New(20, 2)
	vt.Write([]byte(seq + "X"))
	return vt.Screen[0][0].Style
}

// TestSGRUnderlineStylesAreSubParameters guards the colon form of SGR 4.
// Flattened, "4:3" read as underline plus italic, "4:2" as underline plus
// dim and "4:5" as underline plus blink -- and tmux forwards these from any
// pane whose app asks for a styled underline.
func TestSGRUnderlineStylesAreSubParameters(t *testing.T) {
	t.Parallel()
	cases := []struct {
		seq  string
		want UnderlineStyle
	}{
		{"\x1b[4m", UnderlineSingle},
		{"\x1b[4:1m", UnderlineSingle},
		{"\x1b[4:2m", UnderlineDouble},
		{"\x1b[4:3m", UnderlineCurly},
		{"\x1b[4:4m", UnderlineDotted},
		{"\x1b[4:5m", UnderlineDashed},
		{"\x1b[4::3m", UnderlineCurly}, // tmux's built-in usstyle form
		{"\x1b[4::2m", UnderlineDouble},
		{"\x1b[4:3m\x1b[4:0m", UnderlineNone},
		{"\x1b[4:3m\x1b[24m", UnderlineNone},
	}
	for _, tc := range cases {
		got := styleAfter(t, tc.seq)
		if got.Underline != tc.want {
			t.Errorf("%q: underline = %d, want %d", tc.seq, got.Underline, tc.want)
		}
		if got.Italic || got.Dim || got.Blink {
			t.Errorf("%q: sub-parameter leaked into another attribute: %+v", tc.seq, got)
		}
	}
}

// TestSGRUnderlineColorKeepsTheStyle covers the exact sequence tmux sends for
// an underline color when the outer terminfo is iTerm2's. Its empty fields,
// read flat, are full resets that wiped the foreground set just before.
func TestSGRUnderlineColorKeepsTheStyle(t *testing.T) {
	t.Parallel()
	for _, seq := range []string{
		"\x1b[31m\x1b[58::2::0::255::0m\x1b[4m",
		"\x1b[31m\x1b[58:2::0:255:0m\x1b[4m",
		"\x1b[31;58;2;0;255;0;4m",
		"\x1b[31;58;5;2;4m",
		"\x1b[31m\x1b[59m\x1b[4m",
		"\x1b[31m\x1b[58::5::2m\x1b[4m", // tmux's Setulc1
	} {
		got := styleAfter(t, seq)
		if got.Fg != (Color{Type: ColorIndexed, Value: 1}) {
			t.Errorf("%q: fg = %+v, want indexed red", seq, got.Fg)
		}
		if got.Underline != UnderlineSingle {
			t.Errorf("%q: underline = %d, want single", seq, got.Underline)
		}
		if got.Bold || got.Dim || got.Italic || got.Blink || got.Reverse {
			t.Errorf("%q: operands misread as attributes: %+v", seq, got)
		}
	}
}

func TestSGRColonColors(t *testing.T) {
	t.Parallel()
	rgb := Color{Type: ColorRGB, Value: 0x0a141e}
	cases := []struct {
		seq    string
		fg, bg Color
	}{
		{"\x1b[38:2::10:20:30m", rgb, Color{}},
		{"\x1b[38:2:10:20:30m", rgb, Color{}},
		{"\x1b[38:2:0:10:20:30m", rgb, Color{}},
		{"\x1b[48:2::10:20:30m", Color{}, rgb},
		{"\x1b[38:5:208m", Color{Type: ColorIndexed, Value: 208}, Color{}},
		{"\x1b[38;2;10;20;30m", rgb, Color{}},
		{"\x1b[38;2;300;20;30m", Color{Type: ColorRGB, Value: 0xff141e}, Color{}},
	}
	for _, tc := range cases {
		got := styleAfter(t, tc.seq)
		if got.Fg != tc.fg || got.Bg != tc.bg {
			t.Errorf("%q: fg=%+v bg=%+v, want fg=%+v bg=%+v", tc.seq, got.Fg, got.Bg, tc.fg, tc.bg)
		}
	}
}

func TestStyleToANSIUnderlineStyles(t *testing.T) {
	t.Parallel()
	if got := StyleToANSI(Style{Underline: UnderlineCurly}); got != "\x1b[0;4:3m" {
		t.Errorf("curly = %q", got)
	}
	if got := StyleToANSI(Style{Underline: UnderlineSingle}); got != "\x1b[0;4m" {
		t.Errorf("single = %q", got)
	}
	if got := StyleToDeltaANSI(Style{Underline: UnderlineSingle}, Style{Underline: UnderlineCurly}); got != "\x1b[4:3m" {
		t.Errorf("single->curly delta = %q", got)
	}
	if got := StyleToDeltaANSI(Style{Underline: UnderlineCurly}, Style{}); got != "\x1b[24m" {
		t.Errorf("curly->none delta = %q", got)
	}
}
