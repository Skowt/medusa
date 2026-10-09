package vterm

import "testing"

// A line feed scrolls only when the cursor is on the bottom margin. Below the
// scroll region it just moves the cursor down. tmux depends on this: it leaves
// a region set after using it and moves the cursor down one row with a bare LF
// (cud1 in xterm-256color), so scrolling here shifted rows the pane never moved.

// regionWithHistory is a 10-row terminal with "h0".."h3" on rows 0-3 and a
// scroll region over rows 1-4 (0-indexed 0-3).
func regionWithHistory(t *testing.T) *VTerm {
	t.Helper()
	v := New(20, 10)
	v.Write([]byte("h0\r\nh1\r\nh2\r\nh3"))
	v.Write([]byte("\x1b[1;4r"))
	return v
}

func assertHistoryUnmoved(t *testing.T, v *VTerm) {
	t.Helper()
	for y, want := range []string{"h0", "h1", "h2", "h3"} {
		if got := lineText(v, y); got != want {
			t.Fatalf("row %d = %q, want %q: the region scrolled", y, got, want)
		}
	}
}

func TestLineFeedBelowTheRegionMovesWithoutScrolling(t *testing.T) {
	v := regionWithHistory(t)
	v.Write([]byte("\x1b[7;1H")) // row 6, below the region
	v.Write([]byte("\nX"))
	if v.CursorY != 7 {
		t.Fatalf("cursor row = %d, want 7", v.CursorY)
	}
	if got := lineText(v, 7); got != "X" {
		t.Fatalf("row 7 = %q, want %q", got, "X")
	}
	assertHistoryUnmoved(t, v)
}

func TestLineFeedOnTheLastRowBelowTheRegionDoesNothing(t *testing.T) {
	v := regionWithHistory(t)
	v.Write([]byte("\x1b[10;1H\n"))
	if v.CursorY != 9 {
		t.Fatalf("cursor row = %d, want 9", v.CursorY)
	}
	assertHistoryUnmoved(t, v)
}

func TestIndexBelowTheRegionMovesWithoutScrolling(t *testing.T) {
	v := regionWithHistory(t)
	v.Write([]byte("\x1b[7;1H\x1bD"))
	if v.CursorY != 7 {
		t.Fatalf("cursor row = %d, want 7", v.CursorY)
	}
	assertHistoryUnmoved(t, v)
}

func TestAutowrapBelowTheRegionMovesWithoutScrolling(t *testing.T) {
	v := regionWithHistory(t)
	v.Write([]byte("\x1b[7;1H"))
	v.Write([]byte("abcdefghijklmnopqrstU")) // 21 chars: wraps once
	if got := lineText(v, 7); got != "U" {
		t.Fatalf("row 7 = %q, want the wrapped %q", got, "U")
	}
	assertHistoryUnmoved(t, v)
}

func TestLineFeedOnTheBottomMarginScrollsOnlyTheRegion(t *testing.T) {
	v := regionWithHistory(t)
	v.Write([]byte("\x1b[8;1Hfooter"))
	v.Write([]byte("\x1b[4;1H\nh4"))
	for y, want := range []string{"h1", "h2", "h3", "h4"} {
		if got := lineText(v, y); got != want {
			t.Fatalf("row %d = %q, want %q", y, got, want)
		}
	}
	if got := lineText(v, 7); got != "footer" {
		t.Fatalf("row below the region = %q, want it untouched", got)
	}
}

// The sequence tmux sent when Codex printed a line above its input box: scroll
// the transcript region by one, then repaint the rows below it with CR LF. Each
// of those line feeds used to scroll the region again, shifting the transcript
// up a row per line and leaving the input box blank.
func TestTmuxInsertAboveInputBoxScrollsOnce(t *testing.T) {
	v := New(20, 10)
	v.Write([]byte("a0\r\na1\r\na2\r\na3\r\na4\r\na5"))
	v.Write([]byte("\x1b[1;6r\x1b[6;1H\na6"))
	v.Write([]byte("\x1b[7;1Hw1\r\nw2\r\nw3\r\nw4\x1b[1;10r"))
	for y, want := range []string{"a1", "a2", "a3", "a4", "a5", "a6", "w1", "w2", "w3", "w4"} {
		if got := lineText(v, y); got != want {
			t.Fatalf("row %d = %q, want %q", y, got, want)
		}
	}
}
