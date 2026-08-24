package vterm

import "time"

const MaxScrollback = 10000

// ResponseWriter is called when the terminal needs to send a response back to the PTY
type ResponseWriter func([]byte)

// VTerm is a virtual terminal emulator with scrollback support.
//
// Synchronization contract: VTerm has no internal mutex. All callers must provide
// external synchronization. In practice, every call site (WriteToTerminal,
// SidebarPTYFlush, and TerminalLayer snapshot creation) holds TerminalState.mu
// for the duration of the operation. Snapshot-based rendering (TerminalLayer)
// copies data under the lock and then renders the immutable snapshot without locks.
type VTerm struct {
	// Screen buffer (visible area)
	Screen [][]Cell

	// Scrollback buffer (oldest at index 0)
	Scrollback [][]Cell

	// Cursor position (0-indexed)
	CursorX, CursorY int

	// Dimensions
	Width, Height int

	// Scroll viewing position (0 = live, >0 = lines scrolled up)
	ViewOffset int

	// Alt screen mode (full-screen TUI applications).
	AltScreen bool
	// AllowAltScreenScrollback keeps scrollback active even in alt screen.
	// Set it for vterms fed by a `tmux attach` client: the client enters the
	// alternate screen at attach no matter what the pane's app does, so
	// AltScreen describes tmux, not the app, and must not gate scrollback.
	AllowAltScreenScrollback bool
	// AppFullscreen marks an app launched as a fullscreen renderer (Claude with
	// CLAUDE_CODE_NO_FLICKER, which repaints in place without ever entering the
	// alt screen). It seeds appPaintsFrames before the app has enabled mouse
	// reporting.
	AppFullscreen bool
	altScreenBuf  [][]Cell
	altCursorX    int
	altCursorY    int

	// Scrolling region (for DECSTBM)
	ScrollTop    int
	ScrollBottom int
	// Origin mode (DECOM) - cursor positions are relative to scroll region.
	OriginMode bool

	// mouseModes tracks which xterm mouse reporting modes the application
	// has enabled (DECSET 1000/1002/1003). Used to decide whether scroll
	// input belongs to the app (e.g. Claude Code after /tui fullscreen).
	mouseModes uint8

	// TreatLFAsCRLF makes a bare LF also return to column 0. Used when parsing
	// tmux capture-pane history (newline-delimited rows) rather than a PTY stream.
	TreatLFAsCRLF bool

	// Current style for new characters
	CurrentStyle Style

	// CurrentLink is the interned ID of the OSC 8 hyperlink that new characters
	// fall under, or 0 outside a hyperlink. Kept apart from CurrentStyle because
	// SGR (including a full reset) must not end a hyperlink.
	CurrentLink uint32

	// OSC-managed terminal metadata and colors. Applications query these to
	// adapt their UI to the terminal and may update them for their session.
	IconName                  string
	Title                     string
	WorkingDirectory          string
	DefaultForeground         Color
	DefaultBackground         Color
	CursorColor               Color
	defaultForegroundModified bool
	defaultBackgroundModified bool
	cursorColorModified       bool
	Palette                   [256]Color
	PaletteModified           [256]bool
	ShellMarker               ShellMarker

	// links interns hyperlink targets; a cell stores the 1-based ID of its entry.
	links   []Link
	linkIDs map[string]uint32

	// Saved cursor state (for DECSC/DECRC)
	SavedCursorX int
	SavedCursorY int
	SavedStyle   Style

	// Parser state
	parser *Parser

	// Response writer for terminal queries (DSR, DA, etc.)
	responseWriter ResponseWriter

	// Selection state for copy/paste highlighting
	// Uses absolute line numbers (0 = first scrollback line)
	selActive               bool
	selStartX, selStartLine int
	selEndX, selEndLine     int
	selRect                 bool

	// Cursor visibility (controlled externally when pane is focused)
	ShowCursor     bool
	lastShowCursor bool
	lastCursorX    int
	lastCursorY    int

	// CursorHidden tracks if terminal app hid cursor via DECTCEM (mode 25)
	CursorHidden     bool
	lastCursorHidden bool

	// Synchronized output (DEC mode 2026)
	syncActive        bool
	syncScreen        [][]Cell
	syncScrollbackLen int
	syncDeferTrim     bool
	// syncStartedAt is when the open sync region began; used by the stall
	// failsafe (SyncStallTimeout).
	syncStartedAt time.Time

	// Render cache for live screen (ViewOffset == 0)
	renderCache    []string
	renderDirty    []bool
	renderDirtyAll bool

	// Version counter for snapshot caching - increments on visible content/cursor changes.
	// UI-driven cursor visibility (ShowCursor) is handled by the snapshot cache key.
	version uint64
}

// New creates a new VTerm with the given dimensions
func New(width, height int) *VTerm {
	v := &VTerm{
		Width:        width,
		Height:       height,
		ScrollTop:    0,
		ScrollBottom: height,
	}
	v.Screen = v.makeScreen(width, height)
	v.initOSCPalette()
	v.Scrollback = make([][]Cell, 0, MaxScrollback)
	v.parser = NewParser(v)
	// Initialize dirty tracking for layer-based rendering
	v.ensureRenderCache(height)
	return v
}

// SetDefaultColors supplies the host UI colors reported by OSC 10/11/12.
func (v *VTerm) SetDefaultColors(fg, bg, cursor Color) {
	if !v.defaultForegroundModified {
		v.DefaultForeground = fg
	}
	if !v.defaultBackgroundModified {
		v.DefaultBackground = bg
	}
	if !v.cursorColorModified {
		v.CursorColor = cursor
	}
}

func (v *VTerm) scrollbackEnabled() bool {
	if v.appPaintsFrames() {
		return false
	}
	return !v.AltScreen || v.AllowAltScreenScrollback
}

// appPaintsFrames reports whether the application is repainting whole frames
// rather than streaming a transcript. Rows that scroll off such an app are
// fragments of the previous frame, not history, so capturing them corrupts
// scrollback. Mouse reporting is the live signal — an app that grabs the mouse
// is driving the screen itself, and tmux replays an app's mouse modes to its
// clients, so it survives attach — and AppFullscreen covers a renderer that
// paints frames without the alt screen.
func (v *VTerm) appPaintsFrames() bool {
	return v.AppFullscreen || v.MouseReporting()
}

// makeScreen creates a blank screen buffer
func (v *VTerm) makeScreen(width, height int) [][]Cell {
	screen := make([][]Cell, height)
	for i := range screen {
		screen[i] = MakeBlankLine(width)
	}
	return screen
}

// Resize handles terminal resize
func (v *VTerm) Resize(width, height int) {
	oldWidth := v.Width
	oldHeight := v.Height
	// Enforce minimum dimensions to prevent negative cursor positions
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	if width == oldWidth && height == oldHeight {
		return
	}

	// If height shrinks, move lines to scrollback
	if height < oldHeight && v.scrollbackEnabled() {
		overflow := oldHeight - height
		if overflow > 0 {
			added := 0
			for i := 0; i < overflow; i++ {
				if len(v.Screen) > 0 {
					v.Scrollback = append(v.Scrollback, v.Screen[0])
					v.Screen = v.Screen[1:]
					added++
				}
			}
			if added > 0 && v.ViewOffset > 0 {
				v.ViewOffset += added
				if v.ViewOffset > len(v.Scrollback) {
					v.ViewOffset = len(v.Scrollback)
				}
			}
			v.trimScrollback()
		}
	}

	// If height grows, restore lines from scrollback so the screen fills.
	// This matches native terminal behavior where expanding reveals history above.
	if height > oldHeight && v.scrollbackEnabled() && v.ViewOffset == 0 {
		added := height - oldHeight
		restore := added
		if restore > len(v.Scrollback) {
			restore = len(v.Scrollback)
		}
		if restore > 0 {
			start := len(v.Scrollback) - restore
			restored := v.Scrollback[start:]
			v.Scrollback = v.Scrollback[:start]
			v.Screen = append(restored, v.Screen...)
			v.CursorY += restore
		}
	}

	// Resize screen buffer - preserve full row content to allow restoring
	// on resize back to larger width (e.g., exiting monitor mode)
	newScreen := make([][]Cell, height)
	for y := 0; y < height; y++ {
		if y < len(v.Screen) && len(v.Screen[y]) > 0 {
			// Preserve the original row content (may be wider than new width)
			// but ensure it's at least as wide as new width
			if len(v.Screen[y]) >= width {
				newScreen[y] = v.Screen[y]
			} else {
				// Expand row to new width
				newScreen[y] = MakeBlankLine(width)
				copy(newScreen[y], v.Screen[y])
			}
		} else {
			newScreen[y] = MakeBlankLine(width)
		}
	}
	v.Screen = newScreen

	// Update dimensions
	v.Width = width
	v.Height = height

	// Adjust scroll region
	if v.ScrollBottom > height || v.ScrollBottom == 0 {
		v.ScrollBottom = height
	}
	if v.ScrollTop >= v.ScrollBottom {
		v.ScrollTop = 0
	}

	// Clamp cursor
	if v.CursorX >= width {
		v.CursorX = width - 1
	}
	if v.CursorY >= height {
		v.CursorY = height - 1
	}
	v.clampCursor()

	// Also resize alt screen if it exists - preserve full row content
	if v.altScreenBuf != nil {
		newAlt := make([][]Cell, height)
		for y := 0; y < height; y++ {
			if y < len(v.altScreenBuf) && len(v.altScreenBuf[y]) > 0 {
				if len(v.altScreenBuf[y]) >= width {
					newAlt[y] = v.altScreenBuf[y]
				} else {
					newAlt[y] = MakeBlankLine(width)
					copy(newAlt[y], v.altScreenBuf[y])
				}
			} else {
				newAlt[y] = MakeBlankLine(width)
			}
		}
		v.altScreenBuf = newAlt
	}

	// Keep synchronized output snapshot aligned with new size - preserve full row content
	if v.syncScreen != nil {
		newSync := make([][]Cell, height)
		for y := 0; y < height; y++ {
			if y < len(v.syncScreen) && len(v.syncScreen[y]) > 0 {
				if len(v.syncScreen[y]) >= width {
					newSync[y] = v.syncScreen[y]
				} else {
					newSync[y] = MakeBlankLine(width)
					copy(newSync[y], v.syncScreen[y])
				}
			} else {
				newSync[y] = MakeBlankLine(width)
			}
		}
		v.syncScreen = newSync
	}
	v.invalidateRenderCache()
	// Re-initialize dirty tracking for new size
	v.ensureRenderCache(height)
}

// Write processes input bytes from PTY
func (v *VTerm) Write(data []byte) {
	v.maybeReleaseStaleSync()
	v.parser.Parse(data)
}

// SetResponseWriter sets the callback for terminal query responses
func (v *VTerm) SetResponseWriter(w ResponseWriter) {
	v.responseWriter = w
}

// respond sends a response back to the PTY (for terminal queries)
func (v *VTerm) respond(data []byte) {
	if v.responseWriter != nil {
		v.responseWriter(data)
	}
}
