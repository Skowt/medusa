package vterm

import "testing"

const mrURL = "https://gitlab.cargo.one/services/protos/-/merge_requests/1638"

// linkAt returns the hyperlink URI attached to the cell at (x, y).
func linkAt(t *testing.T, v *VTerm, x, y int) string {
	t.Helper()
	cell := v.Screen[y][x]
	uri, _ := v.LinkTarget(cell.Link)
	return uri
}

// TestOSC8StringTerminator covers the form tmux emits: OSC 8, an id= param,
// the URI, terminated by ST (ESC \).
func TestOSC8StringTerminator(t *testing.T) {
	v := New(40, 4)
	v.Write([]byte("\x1b]8;id=tmux1;" + mrURL + "\x1b\\MR!1638\x1b]8;;\x1b\\ done"))

	// "MR!1638" spans cells 0..6; every cell of it carries the link.
	for x := 0; x <= 6; x++ {
		if got := linkAt(t, v, x, 0); got != mrURL {
			t.Errorf("cell %d: link = %q, want %q", x, got, mrURL)
		}
	}
	// The text after the closing OSC 8 must not be part of the link.
	for _, x := range []int{7, 9} {
		if got := linkAt(t, v, x, 0); got != "" {
			t.Errorf("cell %d after close: link = %q, want none", x, got)
		}
	}
	// The link markers must not leak into the visible text.
	if got := lineText(v, 0); got != "MR!1638 done" {
		t.Errorf("text = %q, want %q", got, "MR!1638 done")
	}
}

// TestOSC8BELTerminator covers the BEL-terminated form of the same sequence.
func TestOSC8BELTerminator(t *testing.T) {
	v := New(40, 4)
	v.Write([]byte("\x1b]8;;" + mrURL + "\x07link\x1b]8;;\x07"))

	if got := linkAt(t, v, 0, 0); got != mrURL {
		t.Errorf("link = %q, want %q", got, mrURL)
	}
	if got := lineText(v, 0); got != "link" {
		t.Errorf("text = %q, want %q", got, "link")
	}
}

// TestOSC8SurvivesSGRReset guards the ordering agents actually emit: styling is
// reset inside the hyperlink region. SGR must not clear the hyperlink, because
// OSC 8 state is independent of SGR state.
func TestOSC8SurvivesSGRReset(t *testing.T) {
	v := New(40, 4)
	v.Write([]byte("\x1b]8;;" + mrURL + "\x1b\\\x1b[34mab\x1b[0mcd\x1b]8;;\x1b\\"))

	for x := 0; x < 4; x++ {
		if got := linkAt(t, v, x, 0); got != mrURL {
			t.Errorf("cell %d: link = %q, want %q (SGR reset must not drop the link)", x, got, mrURL)
		}
	}
}

// TestOSC8Interning keeps the per-cell cost to an ID: repeating the same URI
// must not grow the table, since scrollback holds MaxScrollback*width cells.
func TestOSC8Interning(t *testing.T) {
	v := New(40, 4)
	for i := 0; i < 50; i++ {
		v.Write([]byte("\x1b]8;;" + mrURL + "\x1b\\x\x1b]8;;\x1b\\"))
	}
	if n := v.LinkTableLen(); n != 1 {
		t.Errorf("link table holds %d entries, want 1 (URIs must be interned)", n)
	}
}

// TestOSC8NonHyperlinkOSCIgnored ensures unrelated OSC sequences (title, colors)
// neither set a link nor print their payload.
func TestOSC8NonHyperlinkOSCIgnored(t *testing.T) {
	v := New(40, 4)
	v.Write([]byte("\x1b]0;my title\x07hi"))

	if got := linkAt(t, v, 0, 0); got != "" {
		t.Errorf("OSC 0 set a link %q, want none", got)
	}
	if got := lineText(v, 0); got != "hi" {
		t.Errorf("text = %q, want %q (OSC 0 payload must not print)", got, "hi")
	}
}

// lineText reads the visible text of a row, trimming trailing blanks.
func lineText(v *VTerm, y int) string {
	var out []rune
	for _, cell := range v.Screen[y] {
		out = append(out, RenderableRune(cell.Rune))
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return string(out)
}
