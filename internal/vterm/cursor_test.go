package vterm

import "testing"

// TestExitAltScreenClampsCursor verifies that exitAltScreen clamps the
// restored main-screen cursor into range. If the terminal was resized
// smaller while an app was running in the alt screen (e.g. shrinking from
// 80x24 to 10x5 while vim was open), the saved main-screen cursor position
// can land outside the new bounds once the app exits alt screen.
func TestExitAltScreenClampsCursor(t *testing.T) {
	v := New(80, 24)
	// Position the main-screen cursor somewhere that is in range for the
	// current 80x24 size but will be out of range after the shrink below.
	v.CursorX = 40
	v.CursorY = 20
	// enterAltScreen captures the current cursor as altCursorX/altCursorY
	// before switching to the alt-screen buffer.
	v.enterAltScreen()
	if v.altCursorX != 40 || v.altCursorY != 20 {
		t.Fatalf("expected altCursor to capture (40,20), got (%d,%d)", v.altCursorX, v.altCursorY)
	}

	// Shrink the terminal while still in the alt screen. Resize clamps the
	// live (alt-screen) cursor, but must not touch the saved altCursorX/Y.
	v.Resize(10, 5)

	v.exitAltScreen()
	if v.CursorX < 0 || v.CursorX >= v.Width || v.CursorY < 0 || v.CursorY >= v.Height {
		t.Fatalf("cursor not clamped after alt-screen exit: (%d,%d) in %dx%d", v.CursorX, v.CursorY, v.Width, v.Height)
	}
}
