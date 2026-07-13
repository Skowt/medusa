package compositor

import (
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/Skowt/medusa/internal/vterm"
)

// asciiStrings avoids allocations when converting ASCII runes to strings.
var asciiStrings [128]string

func init() {
	for i := 0; i < 128; i++ {
		asciiStrings[i] = string(rune(i))
	}
}

// runeToString converts a rune to a string with minimal allocations.
func runeToString(r rune) string {
	if r >= 0 && r < 128 {
		return asciiStrings[r]
	}
	return string(r)
}

// VTermSnapshot captures the state needed to render a VTerm.
// This is created while holding Tab.mu and can be safely used for rendering
// without holding any locks, avoiding data races with PTY output.
type VTermSnapshot struct {
	Screen       [][]vterm.Cell
	DirtyLines   []bool
	AllDirty     bool
	CursorX      int
	CursorY      int
	ViewOffset   int
	CursorHidden bool
	ShowCursor   bool
	Width        int
	Height       int
	// Selection state (used during rendering)
	SelActive            bool
	SelStartX, SelStartY int
	SelEndX, SelEndY     int
	// Links resolves Cell.Link IDs to OSC 8 targets, indexed by ID-1. Copied
	// under the VTerm lock so rendering can resolve links lock-free.
	Links []vterm.Link
}

// NewVTermSnapshot creates a snapshot from a VTerm.
// MUST be called while holding the appropriate lock on the VTerm.
func NewVTermSnapshot(term *vterm.VTerm, showCursor bool) *VTermSnapshot {
	return NewVTermSnapshotWithCache(term, showCursor, nil)
}

// NewVTermSnapshotWithCache creates a snapshot from a VTerm, optionally reusing
// lines from a previous snapshot when dirty line tracking allows.
// MUST be called while holding the appropriate lock on the VTerm.
func NewVTermSnapshotWithCache(term *vterm.VTerm, showCursor bool, prev *VTermSnapshot) *VTermSnapshot {
	if term == nil {
		return nil
	}

	width := term.Width
	height := term.Height
	if width <= 0 || height <= 0 {
		return nil
	}

	// Copy dirty lines to avoid sharing the backing array
	dirtyLines, allDirty := term.DirtyLines()
	var dirtyLinesCopy []bool
	if dirtyLines != nil {
		dirtyLinesCopy = make([]bool, len(dirtyLines))
		copy(dirtyLinesCopy, dirtyLines)
	}

	// Ensure cursor-only changes mark lines dirty for layer rendering.
	// Cursor moves or visibility toggles don't always touch renderDirty,
	// so we force redraw of the previous and current cursor lines when needed.
	if !allDirty && term.ViewOffset == 0 {
		lastCursorX := term.LastCursorX()
		lastCursorY := term.LastCursorY()
		lastShowCursor := term.LastShowCursor()
		lastCursorHidden := term.LastCursorHidden()

		cursorChanged := showCursor != lastShowCursor ||
			term.CursorHidden != lastCursorHidden ||
			term.CursorX != lastCursorX ||
			term.CursorY != lastCursorY

		if cursorChanged {
			// Defensive: ensure dirtyLinesCopy matches screen height.
			if dirtyLinesCopy == nil {
				dirtyLinesCopy = make([]bool, height)
			}
			if lastCursorY >= 0 && lastCursorY < len(dirtyLinesCopy) {
				dirtyLinesCopy[lastCursorY] = true
			}
			if term.CursorY >= 0 && term.CursorY < len(dirtyLinesCopy) {
				dirtyLinesCopy[term.CursorY] = true
			}
		}
	}

	canReuse := prev != nil && prev.Width == width && prev.Height == height && len(prev.Screen) == height
	useDirty := canReuse &&
		prev.ViewOffset == term.ViewOffset &&
		!allDirty &&
		term.ViewOffset == 0 &&
		dirtyLines != nil &&
		len(dirtyLines) == height

	var screen [][]vterm.Cell
	if useDirty {
		screen = prev.Screen
		if screen == nil || len(screen) != height {
			screen = make([][]vterm.Cell, height)
		}

		visible, _ := term.RenderBuffers()
		for y := 0; y < height; y++ {
			needsCopy := dirtyLines[y]
			if screen[y] == nil || len(screen[y]) != width {
				needsCopy = true
			}
			if !needsCopy {
				continue
			}

			line := screen[y]
			if line == nil || len(line) != width {
				line = vterm.MakeBlankLine(width)
			}
			if y < len(visible) {
				copy(line, visible[y])
			} else {
				for i := range line {
					line[i] = vterm.DefaultCell()
				}
			}
			screen[y] = line
		}
	} else {
		// Full snapshot when dirty tracking isn't usable.
		screen = term.VisibleScreen()
		if len(screen) == 0 {
			return nil
		}
	}

	snap := prev
	if snap == nil {
		snap = &VTermSnapshot{}
	}

	snap.Screen = screen
	snap.DirtyLines = dirtyLinesCopy
	snap.AllDirty = allDirty
	snap.CursorX = term.CursorX
	snap.CursorY = term.CursorY
	snap.ViewOffset = term.ViewOffset
	snap.CursorHidden = term.CursorHidden
	snap.ShowCursor = showCursor
	snap.Width = width
	snap.Height = height
	snap.Links = term.LinkTable()
	snap.SelActive = term.SelActive()
	snap.SelStartX = 0
	snap.SelStartY = 0
	snap.SelEndX = 0
	snap.SelEndY = 0

	if snap.SelActive {
		startLine := term.SelStartLine()
		endLine := term.SelEndLine()
		startX := term.SelStartX()
		endX := term.SelEndX()

		// Normalize so start is before end.
		if startLine > endLine || (startLine == endLine && startX > endX) {
			startLine, endLine = endLine, startLine
			startX, endX = endX, startX
		}

		visibleStartLine := term.ScreenYToAbsoluteLine(0)
		visibleEndLine := term.ScreenYToAbsoluteLine(height - 1)

		// If selection is entirely outside viewport, disable selection rendering.
		if endLine < visibleStartLine || startLine > visibleEndLine {
			snap.SelActive = false
		} else {
			if startLine < visibleStartLine {
				snap.SelStartY = 0
				startX = 0
			} else {
				snap.SelStartY = startLine - visibleStartLine
			}

			if endLine > visibleEndLine {
				snap.SelEndY = height - 1
				endX = width - 1
			} else {
				snap.SelEndY = endLine - visibleStartLine
			}

			if startX < 0 {
				startX = 0
			}
			if startX >= width {
				startX = width - 1
			}
			if endX < 0 {
				endX = 0
			}
			if endX >= width {
				endX = width - 1
			}

			snap.SelStartX = startX
			snap.SelEndX = endX
		}
	}

	// Clear dirty state after snapshotting (while still holding the lock)
	// Also update cursor tracking for next frame
	term.ClearDirtyWithCursor(showCursor)

	return snap
}

// VTermLayer implements tea.Layer for direct cell-based rendering of a VTerm snapshot.
// This uses a snapshot to avoid data races - the snapshot is created while holding
// the VTerm lock, and rendering happens without any locks.
type VTermLayer struct {
	Snap *VTermSnapshot
}

// Ensure VTermLayer implements uv.Drawable (which is compatible with tea.Layer)
var _ uv.Drawable = (*VTermLayer)(nil)

// NewVTermLayer creates a new layer from a VTerm snapshot.
func NewVTermLayer(snap *VTermSnapshot) *VTermLayer {
	return &VTermLayer{Snap: snap}
}

// Draw renders the VTerm snapshot directly to the screen buffer.
// This is the hot path - every optimization here matters.
func (l *VTermLayer) Draw(s uv.Screen, r uv.Rectangle) {
	l.DrawAt(s, r.Min.X, r.Min.Y, r.Dx(), r.Dy())
}

// DrawAt renders the VTerm snapshot at a specific position with given dimensions.
// This is the core rendering logic shared by VTermLayer and PositionedVTermLayer.
func (l *VTermLayer) DrawAt(s uv.Screen, posX, posY, maxWidth, maxHeight int) {
	snap := l.Snap
	if snap == nil || len(snap.Screen) == 0 {
		return
	}

	width := maxWidth
	height := maxHeight
	if width > snap.Width {
		width = snap.Width
	}
	if height > snap.Height {
		height = snap.Height
	}

	// When compositing layers, we must draw ALL cells every frame.
	// The dirty line optimization only works for single-layer rendering.
	// Ultraviolet's cell-level diffing handles the actual screen updates.
	for y := 0; y < height && y < len(snap.Screen); y++ {
		row := snap.Screen[y]
		if row == nil {
			continue
		}

		for x := 0; x < width && x < len(row); x++ {
			cell := row[x]

			if cell.Width == 0 {
				// Setting the wide base cell already made ultraviolet write this
				// placeholder. Writing it again reads to ultraviolet as a partial
				// overwrite of the wide cell, and it blanks the base glyph.
				if x > 0 && row[x-1].Width == 2 {
					continue
				}
				// Continuation with no wide base: emit a blank so the column
				// still clears stale content and occupies its width.
				uvCell := getCell()
				uvCell.Content = " "
				uvCell.Width = 1
				s.SetCell(posX+x, posY+y, uvCell)
				putCell(uvCell)
				continue
			}

			// Build the ultraviolet cell from pool
			uvCell := cellToUVSnapshot(cell, snap, x, y)

			// Set cell at screen position (ultraviolet copies the value)
			s.SetCell(posX+x, posY+y, uvCell)

			// Return cell to pool for reuse
			putCell(uvCell)
		}
	}
}

// cellToUVSnapshot converts a vterm.Cell to a pooled uv.Cell.
// Caller must call putCell() after passing to SetCell.
func cellToUVSnapshot(cell vterm.Cell, snap *VTermSnapshot, x, y int) *uv.Cell {
	style := cell.Style

	// Apply selection and cursor reverse (selection has precedence over cursor)
	inSel := inSelectionSnapshot(snap, x, y)
	cursorHere := snap.ShowCursor && !snap.CursorHidden &&
		y == snap.CursorY && x == snap.CursorX && snap.ViewOffset == 0
	if inSel || cursorHere {
		style.Reverse = !style.Reverse
	}

	// Suppress underline on blank cells (prevents visual scanlines)
	if style.Underline && (cell.Rune == 0 || cell.Rune == ' ') {
		style.Underline = false
	}

	content := cell.GraphemeCluster
	if content == "" {
		content = runeToString(vterm.RenderableRune(cell.Rune))
	}

	uvCell := getCell()
	uvCell.Content = content
	uvCell.Style = vtermStyleToUV(style)
	uvCell.Link = snapshotLink(snap, cell.Link)
	uvCell.Width = cell.Width
	return uvCell
}

// snapshotLink resolves an interned hyperlink ID against the snapshot's table.
// Ultraviolet re-emits this as an OSC 8 sequence, which is what makes the link
// clickable in the outer terminal; without it the terminal only sees the link
// text and falls back to guessing a URL from it.
func snapshotLink(snap *VTermSnapshot, id uint32) uv.Link {
	if snap == nil || id == 0 || int(id) > len(snap.Links) {
		return uv.Link{}
	}
	link := snap.Links[id-1]
	return uv.Link{URL: link.URI, Params: link.Params}
}

// inSelectionSnapshot checks if a coordinate is within the snapshot's selection.
func inSelectionSnapshot(snap *VTermSnapshot, x, y int) bool {
	if snap == nil {
		return false
	}
	return inSelection(snap.SelActive, snap.SelStartX, snap.SelStartY, snap.SelEndX, snap.SelEndY, x, y)
}

// PositionedVTermLayer wraps a VTermLayer with explicit positioning.
// This allows the layer to be positioned within a larger canvas.
type PositionedVTermLayer struct {
	*VTermLayer
	PosX, PosY    int
	Width, Height int
}

// Ensure PositionedVTermLayer implements uv.Drawable
var _ uv.Drawable = (*PositionedVTermLayer)(nil)

// Draw renders the VTerm snapshot at the specified position within the canvas.
func (l *PositionedVTermLayer) Draw(s uv.Screen, r uv.Rectangle) {
	if l.VTermLayer == nil {
		return
	}
	// Delegate to VTermLayer.DrawAt with our position and dimensions
	l.DrawAt(s, l.PosX, l.PosY, l.Width, l.Height)
}
