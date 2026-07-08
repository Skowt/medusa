package vterm

import "testing"

func TestMouseReportingModeTracking(t *testing.T) {
	v := New(80, 24)
	if v.MouseReporting() {
		t.Fatal("mouse reporting should be off initially")
	}

	// Claude Code fullscreen enables all-motion + SGR encoding.
	v.Write([]byte("\x1b[?1003h\x1b[?1006h"))
	if !v.MouseReporting() {
		t.Fatal("1003h should enable mouse reporting")
	}

	v.Write([]byte("\x1b[?1003l"))
	if v.MouseReporting() {
		t.Fatal("1003l should disable mouse reporting")
	}

	// Each mode tracked independently: enabling 1000 and 1002, then
	// disabling only 1002, keeps reporting on.
	v.Write([]byte("\x1b[?1000h\x1b[?1002h\x1b[?1002l"))
	if !v.MouseReporting() {
		t.Fatal("1000 still set — reporting should stay on")
	}
	v.Write([]byte("\x1b[?1000l"))
	if v.MouseReporting() {
		t.Fatal("all modes cleared — reporting should be off")
	}

	// RIS clears mouse modes.
	v.Write([]byte("\x1b[?1003h"))
	v.Write([]byte("\x1bc"))
	if v.MouseReporting() {
		t.Fatal("RIS should clear mouse reporting")
	}
}
