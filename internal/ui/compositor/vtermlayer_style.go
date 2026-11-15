package compositor

// Conversion from medusa's vterm cell model to ultraviolet's style/color model.

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/Skowt/medusa/internal/vterm"
)

// vtermStyleToUV converts a vterm.Style to ultraviolet's Style.
func vtermStyleToUV(s vterm.Style) uv.Style {
	var uvStyle uv.Style

	// Convert colors
	uvStyle.Fg = vtermColorToUV(s.Fg)
	uvStyle.Bg = vtermColorToUV(s.Bg)

	// Convert attributes
	var attrs uint8
	if s.Bold {
		attrs |= uv.AttrBold
	}
	if s.Dim {
		attrs |= uv.AttrFaint
	}
	if s.Italic {
		attrs |= uv.AttrItalic
	}
	if s.Blink {
		attrs |= uv.AttrBlink
	}
	if s.Reverse {
		attrs |= uv.AttrReverse
	}
	if s.Hidden {
		attrs |= uv.AttrConceal
	}
	if s.Strike {
		attrs |= uv.AttrStrikethrough
	}
	uvStyle.Attrs = attrs

	uvStyle.Underline = vtermUnderlineToUV(s.Underline)

	return uvStyle
}

// vtermColorToUV converts a vterm.Color to a color.Color for ultraviolet.
func vtermColorToUV(c vterm.Color) color.Color {
	switch c.Type {
	case vterm.ColorDefault:
		return nil
	case vterm.ColorIndexed:
		// Use ANSI indexed colors
		return ansiColor(int(c.Value))
	case vterm.ColorRGB:
		// Extract RGB components
		r := uint8((c.Value >> 16) & 0xFF)
		g := uint8((c.Value >> 8) & 0xFF)
		b := uint8(c.Value & 0xFF)
		return color.RGBA{R: r, G: g, B: b, A: 255}
	}
	return nil
}

// vtermUnderlineToUV maps an SGR 4 underline style onto ultraviolet's, which
// re-emits the styled forms ("4:3" curly and so on) to the outer terminal.
func vtermUnderlineToUV(u vterm.UnderlineStyle) uv.Underline {
	switch u {
	case vterm.UnderlineSingle:
		return uv.UnderlineSingle
	case vterm.UnderlineDouble:
		return uv.UnderlineDouble
	case vterm.UnderlineCurly:
		return uv.UnderlineCurly
	case vterm.UnderlineDotted:
		return uv.UnderlineDotted
	case vterm.UnderlineDashed:
		return uv.UnderlineDashed
	}
	return uv.UnderlineNone
}

// ansiColor returns a palette color by index, typed so that ultraviolet emits
// it as a palette reference ("31", "91", "38;5;n") rather than as RGB.
//
// The outer terminal owns the palette. Resolving an index to RGB here (which
// this once did, against a hard-coded VS Code-like table) painted every ANSI
// color an agent printed -- link blue, diff red and green, the ANSI themes --
// in medusa's guess at the palette instead of the user's own.
func ansiColor(idx int) color.Color {
	if idx >= 0 && idx < 16 {
		return ansi.BasicColor(idx)
	}
	return ansi.IndexedColor(idx & 0xff)
}
