package vterm

import "unicode/utf8"

// Grapheme combining, as tmux does it.
//
// A center tab's vterm draws what a tmux client is sent, and tmux positions
// the cursor from its own idea of how wide each cell is. Wherever the two
// disagree, every later cell on the row lands in the wrong column until tmux
// happens to reposition the cursor -- so the rules here follow tmux 3.6's
// observed behaviour rather than Unicode's segmentation rules, which differ
// (tmux joins any run of regional indicators into one cell, and widens a
// variation selector's base even when it is ASCII). TestGraphemeWidthsMatchTmux
// holds the table.

// maxClusterBytes bounds one cell's grapheme. tmux combines well past the
// longest real emoji sequence; the bound only stops an endless run of joiners
// from growing a cell without limit.
const maxClusterBytes = 64

const zeroWidthJoiner = 0x200D

func isVariationSelector(r rune) bool { return r >= 0xFE00 && r <= 0xFE0F }

func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

func isSkinToneModifier(r rune) bool { return r >= 0x1F3FB && r <= 0x1F3FF }

// combineWith decides whether r joins the grapheme already in cell rather than
// starting a cell of its own, and whether joining makes that cell two columns
// wide. width is r's own width.
func combineWith(cell Cell, r rune, width int) (combine, wide bool) {
	if cell.Rune == 0 {
		return false, false
	}
	cluster := cell.GraphemeCluster
	if len(cluster)+utf8.RuneLen(r) > maxClusterBytes {
		return false, false
	}
	switch {
	case isVariationSelector(r):
		// VS16 asks for emoji presentation, which is two columns; tmux
		// widens the base for VS15 too, so both do.
		return true, true
	case width == 0:
		return true, false
	case isRegionalIndicator(r):
		// Pairs of regional indicators are flags; tmux keeps joining them
		// for as long as the cell began with one.
		return isRegionalIndicator(cell.Rune), true
	case isSkinToneModifier(r):
		// tmux joins a skin tone only onto an emoji from the supplementary
		// planes; after anything else the modifier is a swatch of its own.
		return cell.Rune >= 0x1F000, true
	}
	if r, _ := utf8.DecodeLastRuneInString(cluster); r == zeroWidthJoiner {
		// Whatever follows a joiner is part of the same emoji.
		return true, false
	}
	return false, false
}

// combineIntoPrevious appends r to the cell before the cursor when tmux would
// combine it there, and reports whether it did.
func (v *VTerm) combineIntoPrevious(r rune, width int) bool {
	prevX, prevY, ok := v.previousCell()
	if !ok {
		// A zero-width rune with nothing to attach to is dropped, as before.
		return width == 0
	}
	cell := &v.Screen[prevY][prevX]
	combine, wide := combineWith(*cell, r, width)
	if !combine {
		return false
	}
	base := cell.GraphemeCluster
	if base == "" {
		base = string(cell.Rune)
	}
	cell.GraphemeCluster = base + string(r)
	if wide && cell.Width == 1 {
		v.widenCell(prevX, prevY)
	}
	v.markDirtyLine(prevY)
	return true
}

// mayCombine is the cheap pre-check putChar makes on every printable rune
// before combineIntoPrevious. Below the variation selectors, a rune with width
// can only join a cell that ends in a ZWJ, and only a cell holding a cluster
// can -- which nearly none do.
func (v *VTerm) mayCombine(r rune, width int) bool {
	return width == 0 || r >= 0xFE00 || v.previousEndsWithJoiner()
}

// previousEndsWithJoiner reports whether the cell before the cursor ends in a
// zero-width joiner.
func (v *VTerm) previousEndsWithJoiner() bool {
	x, y := v.CursorX-1, v.CursorY
	if y < 0 || y >= len(v.Screen) || x < 0 || x >= len(v.Screen[y]) {
		// Column 0 or off-screen: let the full lookup handle the wrap.
		return true
	}
	line := v.Screen[y]
	if line[x].Width == 0 && x > 0 {
		x--
	}
	cluster := line[x].GraphemeCluster
	return len(cluster) >= 3 && cluster[len(cluster)-3:] == "\u200d"
}

// previousCell locates the cell the cursor has just left, stepping back over a
// wide character's continuation and across a soft wrap.
func (v *VTerm) previousCell() (x, y int, ok bool) {
	x, y = v.CursorX-1, v.CursorY
	if x < 0 && y > 0 {
		y--
		x = v.Width - 1
	}
	if y < 0 || y >= len(v.Screen) {
		return 0, 0, false
	}
	line := v.Screen[y]
	if x > 0 && x < len(line) && line[x].Width == 0 {
		x--
	}
	if x < 0 || x >= len(line) || line[x].Rune == 0 {
		return 0, 0, false
	}
	return x, y, true
}

// widenCell turns a one-column cell into a two-column one, taking the column
// after it as the continuation, and moves the cursor past it. It only does so
// when that column is the cursor's own on the same row: a cell at the right
// margin, or one the cursor has wrapped away from, has nowhere to grow.
func (v *VTerm) widenCell(x, y int) {
	if y != v.CursorY || x+1 != v.CursorX || x+1 >= v.Width {
		return
	}
	line := v.Screen[y]
	if next := line[x+1]; next.Width == 2 && x+2 < len(line) {
		line[x+2] = DefaultCell()
	}
	line[x].Width = 2
	line[x+1] = Cell{Style: line[x].Style, Link: line[x].Link, Width: 0}
	v.CursorX++
}
