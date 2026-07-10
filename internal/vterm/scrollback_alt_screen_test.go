package vterm

import "testing"

func TestAltScreenScrollbackDisabled(t *testing.T) {
	vt := New(3, 2)
	vt.Write([]byte("\x1b[?1049h"))
	if !vt.AltScreen {
		t.Fatalf("expected AltScreen to be true")
	}

	vt.Write([]byte("a\nb\nc\n"))
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected no scrollback in alt screen by default, got %d", len(vt.Scrollback))
	}
}

func TestAltScreenScrollbackEnabled(t *testing.T) {
	vt := New(3, 2)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))
	if !vt.AltScreen {
		t.Fatalf("expected AltScreen to be true")
	}

	vt.Write([]byte("a\nb\nc\n"))
	if len(vt.Scrollback) == 0 {
		t.Fatalf("expected scrollback in alt screen when enabled")
	}
}

// A tmux-backed vterm is always in the alt screen (the tmux client enters it at
// attach), so an inline agent streaming a transcript there must still get
// scrollback — otherwise ScrollView clamps to an empty buffer and the wheel does
// nothing.
func TestTmuxClientInlineAppKeepsScrollback(t *testing.T) {
	vt := New(3, 2)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h")) // tmux client attaches
	vt.Write([]byte("a\nb\nc\n"))   // default-mode agent streams output

	if len(vt.Scrollback) == 0 {
		t.Fatalf("expected scrollback for an inline app under a tmux client")
	}
}

// An app that grabs the mouse is painting frames, not streaming a transcript:
// rows scrolling off are frame fragments and must not be captured as history.
func TestMouseReportingAppSuppressesScrollback(t *testing.T) {
	vt := New(3, 2)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("\x1b[?1000h")) // agent enables mouse reporting (/tui fullscreen)

	vt.Write([]byte("a\nb\nc\n"))
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected no scrollback while the app owns the screen, got %d", len(vt.Scrollback))
	}

	// Dropping back to a transcript (e.g. /tui default) resumes capture.
	vt.Write([]byte("\x1b[?1000l"))
	vt.Write([]byte("d\ne\nf\n"))
	if len(vt.Scrollback) == 0 {
		t.Fatalf("expected scrollback to resume once the app released the mouse")
	}
}

// Claude's fullscreen renderer repaints in place without ever entering the alt
// screen, so the launch flag has to suppress capture on its own.
func TestAppFullscreenSuppressesScrollback(t *testing.T) {
	vt := New(3, 2)
	vt.AllowAltScreenScrollback = true
	vt.AppFullscreen = true

	vt.Write([]byte("a\nb\nc\n"))
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected no scrollback for a fullscreen renderer, got %d", len(vt.Scrollback))
	}
}
