package compositor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/Skowt/medusa/internal/vterm"
)

// TestCanvasRenderEmitsOSC8 covers the last hop, which is the one that decides
// whether a link is actually clickable: the lipgloss canvas the app composes
// into (see App.canvasFor) must serialize a linked cell back out as an OSC 8
// sequence carrying the real URI. If it only emits the styled link text, the
// outer terminal falls back to guessing a URL from that text.
func TestCanvasRenderEmitsOSC8(t *testing.T) {
	term := vterm.New(20, 2)
	term.Write([]byte("\x1b]8;id=tmux1;" + mrURL + "\x1b\\MR\x1b]8;;\x1b\\"))

	canvas := lipgloss.NewCanvas(20, 2)
	canvas.Clear()
	canvas.Compose(&PositionedVTermLayer{
		VTermLayer: NewVTermLayer(NewVTermSnapshot(term, false)),
		PosX:       0,
		PosY:       0,
		Width:      20,
		Height:     2,
	})

	out := canvas.Render()
	if !strings.Contains(out, "\x1b]8;") {
		t.Fatalf("canvas emitted no OSC 8 sequence; output: %q", out)
	}
	if !strings.Contains(out, mrURL) {
		t.Fatalf("canvas emitted no hyperlink URI; output: %q", out)
	}

	// Bubble Tea re-parses this string before painting it (view.SetContent), so
	// the link has to survive that round trip too — otherwise the terminal is
	// back to guessing a URL from the link text.
	screen := uv.NewScreenBuffer(20, 2)
	styled := uv.StyledString{Text: out}
	styled.Draw(screen, screen.Bounds())

	if got := screen.CellAt(0, 0).Link.URL; got != mrURL {
		t.Errorf("after Bubble Tea's re-parse: URL = %q, want %q", got, mrURL)
	}
}
