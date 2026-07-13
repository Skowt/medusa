package compositor

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/Skowt/medusa/internal/vterm"
)

const mrURL = "https://gitlab.cargo.one/services/protos/-/merge_requests/1638"

// TestVTermLayerCarriesHyperlink is the end of the chain inside medusa: a
// hyperlink parsed by the vterm must reach the ultraviolet cell, which is what
// re-emits the OSC 8 sequence to the outer terminal. Without the link on the
// cell the terminal only sees the link text and guesses a URL from it.
func TestVTermLayerCarriesHyperlink(t *testing.T) {
	term := vterm.New(40, 4)
	term.Write([]byte("\x1b]8;id=tmux1;" + mrURL + "\x1b\\MR\x1b]8;;\x1b\\ x"))

	snap := NewVTermSnapshot(term, false)
	if snap == nil {
		t.Fatal("nil snapshot")
	}

	buf := uv.NewScreenBuffer(40, 4)
	NewVTermLayer(snap).Draw(buf, buf.Bounds())

	if got := buf.CellAt(0, 0).Link.URL; got != mrURL {
		t.Errorf("linked cell: URL = %q, want %q", got, mrURL)
	}
	// The space after the closing sequence is outside the link.
	if got := buf.CellAt(3, 0).Link.URL; got != "" {
		t.Errorf("unlinked cell: URL = %q, want none", got)
	}
}
