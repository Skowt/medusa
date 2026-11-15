package vterm

import (
	"strings"
	"testing"
)

func newRecordingVTerm(bg uint32) (*VTerm, *strings.Builder) {
	vt := New(10, 2)
	var out strings.Builder
	vt.SetResponseWriter(func(b []byte) { out.Write(b) })
	vt.SetDefaultColors(Color{Type: ColorRGB, Value: 0xffffff}, Color{Type: ColorRGB, Value: bg}, Color{})
	return vt, &out
}

// TestColorSchemeQuery covers tmux's attach-time "which scheme are you in"
// question. Unanswered, tmux has nothing to relay to an agent that enabled
// mode 2031, so the agent's auto theme cannot follow medusa's.
func TestColorSchemeQuery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		bg   uint32
		want string
	}{
		{0x1a1b26, "\x1b[?997;1n"}, // dark
		{0xfafafa, "\x1b[?997;2n"}, // light
	} {
		vt, out := newRecordingVTerm(tc.bg)
		vt.Write([]byte("\x1b[?996n"))
		if out.String() != tc.want {
			t.Errorf("bg %06x: reply %q, want %q", tc.bg, out.String(), tc.want)
		}
	}
}

// TestColorSchemeChangeIsReportedOnlyWhenAsked: with mode 2031 on, a theme
// switch that flips dark/light is pushed unsolicited; without it, or when the
// scheme does not flip, nothing is written into the agent's input.
func TestColorSchemeChangeIsReportedOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	light := Color{Type: ColorRGB, Value: 0xfafafa}
	darker := Color{Type: ColorRGB, Value: 0x101010}

	vt, out := newRecordingVTerm(0x1a1b26)
	vt.SetDefaultColors(Color{}, light, Color{})
	if out.Len() != 0 {
		t.Fatalf("reported %q without mode 2031", out.String())
	}

	vt, out = newRecordingVTerm(0x1a1b26)
	vt.Write([]byte("\x1b[?2031h"))
	vt.SetDefaultColors(Color{}, darker, Color{})
	if out.Len() != 0 {
		t.Fatalf("reported %q for a dark-to-dark change", out.String())
	}
	vt.SetDefaultColors(Color{}, light, Color{})
	if out.String() != "\x1b[?997;2n" {
		t.Fatalf("dark-to-light report = %q", out.String())
	}

	out.Reset()
	vt.Write([]byte("\x1b[?2031l"))
	vt.SetDefaultColors(Color{}, darker, Color{})
	if out.Len() != 0 {
		t.Fatalf("reported %q after mode 2031 was reset", out.String())
	}
}

// TestResetClientModesForgetsTheOldClient: a reattach reuses the vterm for a
// new tmux client. Until that client enables focus reporting itself, the
// vterm must not claim it has, or focus reports would be written to a PTY
// that may not belong to tmux yet.
func TestResetClientModesForgetsTheOldClient(t *testing.T) {
	t.Parallel()
	vt, out := newRecordingVTerm(0x1a1b26)
	vt.Write([]byte("\x1b[?1004h\x1b[?2031h"))
	if !vt.FocusReporting() {
		t.Fatal("mode 1004 not tracked")
	}
	vt.ResetClientModes()
	if vt.FocusReporting() {
		t.Error("focus reporting survived the reset")
	}
	vt.SetDefaultColors(Color{}, Color{Type: ColorRGB, Value: 0xfafafa}, Color{})
	if out.Len() != 0 {
		t.Errorf("colour-scheme report %q sent after the reset", out.String())
	}
}
