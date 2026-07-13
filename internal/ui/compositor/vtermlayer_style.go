package compositor

// Conversion from medusa's vterm cell model to ultraviolet's style/color model.

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/Skowt/medusa/internal/vterm"
)

// ansiPalette is the standard ANSI color palette (0-15).
// Colors 0-7 are standard, 8-15 are bright variants.
var ansiPalette = []color.RGBA{
	{0, 0, 0, 255},       // 0: Black
	{205, 49, 49, 255},   // 1: Red
	{13, 188, 121, 255},  // 2: Green
	{229, 229, 16, 255},  // 3: Yellow
	{36, 114, 200, 255},  // 4: Blue
	{188, 63, 188, 255},  // 5: Magenta
	{17, 168, 205, 255},  // 6: Cyan
	{229, 229, 229, 255}, // 7: White
	{102, 102, 102, 255}, // 8: Bright Black
	{241, 76, 76, 255},   // 9: Bright Red
	{35, 209, 139, 255},  // 10: Bright Green
	{245, 245, 67, 255},  // 11: Bright Yellow
	{59, 142, 234, 255},  // 12: Bright Blue
	{214, 112, 214, 255}, // 13: Bright Magenta
	{41, 184, 219, 255},  // 14: Bright Cyan
	{255, 255, 255, 255}, // 15: Bright White
}

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

	// Handle underline
	if s.Underline {
		uvStyle.Underline = uv.UnderlineSingle
	}

	return uvStyle
}

// vtermColorToUV converts a vterm.Color to a color.Color for ultraviolet.
func vtermColorToUV(c vterm.Color) color.Color {
	switch c.Type {
	case vterm.ColorDefault:
		return nil
	case vterm.ColorIndexed:
		// Use ANSI indexed colors
		return ansiColor(c.Value)
	case vterm.ColorRGB:
		// Extract RGB components
		r := uint8((c.Value >> 16) & 0xFF)
		g := uint8((c.Value >> 8) & 0xFF)
		b := uint8(c.Value & 0xFF)
		return color.RGBA{R: r, G: g, B: b, A: 255}
	}
	return nil
}

// ansiColor returns an indexed ANSI color.
// Uses ultraviolet's BasicColor for 0-15, ExtendedColor for 16-255.
type ansiColor uint32

func (c ansiColor) RGBA() (r, g, b, a uint32) {
	idx := uint32(c)
	if idx < 16 {
		col := ansiPalette[idx]
		return uint32(col.R) * 257, uint32(col.G) * 257, uint32(col.B) * 257, 65535
	}

	// For 16-255: compute from 6x6x6 color cube or grayscale
	if idx < 232 {
		// 6x6x6 color cube (indices 16-231)
		idx -= 16
		rVal := (idx / 36) % 6
		gVal := (idx / 6) % 6
		bVal := idx % 6

		// Each component is 0, 95, 135, 175, 215, or 255
		rLevel := uint32(0)
		if rVal > 0 {
			rLevel = uint32(55 + rVal*40)
		}
		gLevel := uint32(0)
		if gVal > 0 {
			gLevel = uint32(55 + gVal*40)
		}
		bLevel := uint32(0)
		if bVal > 0 {
			bLevel = uint32(55 + bVal*40)
		}

		return rLevel * 257, gLevel * 257, bLevel * 257, 65535
	}

	// Grayscale (indices 232-255)
	gray := uint32(8 + (idx-232)*10)
	return gray * 257, gray * 257, gray * 257, 65535
}
