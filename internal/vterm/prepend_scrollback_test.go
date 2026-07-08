package vterm

import (
	"strings"
	"testing"
)

func TestPrependScrollbackEmpty(t *testing.T) {
	vt := New(80, 24)
	vt.PrependScrollback(nil)
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected empty scrollback, got %d lines", len(vt.Scrollback))
	}
	vt.PrependScrollback([]byte{})
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected empty scrollback, got %d lines", len(vt.Scrollback))
	}
}

func TestPrependScrollbackPlainText(t *testing.T) {
	vt := New(80, 24)
	vt.PrependScrollback([]byte("hello\nworld\n"))

	if len(vt.Scrollback) == 0 {
		t.Fatal("expected scrollback to have lines")
	}

	// First line should start with 'h'
	if vt.Scrollback[0][0].Rune != 'h' {
		t.Errorf("expected 'h', got %c", vt.Scrollback[0][0].Rune)
	}
}

func TestPrependScrollbackPreservesExisting(t *testing.T) {
	vt := New(80, 24)

	// Add existing scrollback
	existing := MakeBlankLine(80)
	existing[0] = Cell{Rune: 'E', Width: 1}
	vt.Scrollback = append(vt.Scrollback, existing)

	vt.PrependScrollback([]byte("prepended\n"))

	// Should have at least 2 lines: prepended + existing
	if len(vt.Scrollback) < 2 {
		t.Fatalf("expected at least 2 scrollback lines, got %d", len(vt.Scrollback))
	}

	// First line should be from prepended content
	if vt.Scrollback[0][0].Rune != 'p' {
		t.Errorf("first line should start with 'p', got %c", vt.Scrollback[0][0].Rune)
	}

	// Last line should be the original existing line
	last := vt.Scrollback[len(vt.Scrollback)-1]
	if last[0].Rune != 'E' {
		t.Errorf("last line should be existing 'E', got %c", last[0].Rune)
	}
}

func TestPrependScrollbackWithANSI(t *testing.T) {
	vt := New(80, 24)

	// Bold red text: ESC[1;31m
	vt.PrependScrollback([]byte("\x1b[1;31mred bold\x1b[0m\n"))

	if len(vt.Scrollback) == 0 {
		t.Fatal("expected scrollback lines")
	}

	cell := vt.Scrollback[0][0]
	if cell.Rune != 'r' {
		t.Errorf("expected 'r', got %c", cell.Rune)
	}
	if !cell.Style.Bold {
		t.Error("expected bold style")
	}
	if cell.Style.Fg.Type != ColorIndexed || cell.Style.Fg.Value != 1 {
		t.Errorf("expected red foreground, got type=%v value=%d", cell.Style.Fg.Type, cell.Style.Fg.Value)
	}
}

func TestPrependScrollbackTrimsTrailingBlankLines(t *testing.T) {
	vt := New(80, 5)

	// One line of text followed by nothing -- the temp vterm will have
	// that text on screen row 0 and blank rows 1-4. Those trailing blanks
	// should be trimmed.
	vt.PrependScrollback([]byte("only line"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected 1 scrollback line, got %d", len(vt.Scrollback))
	}
	if vt.Scrollback[0][0].Rune != 'o' {
		t.Errorf("expected 'o', got %c", vt.Scrollback[0][0].Rune)
	}
}

func TestPrependScrollbackLargeContent(t *testing.T) {
	vt := New(80, 24)

	// Generate content that exceeds the screen height to produce scrollback
	// in the temporary vterm.
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, strings.Repeat("x", 80))
	}
	content := strings.Join(lines, "\n") + "\n"

	vt.PrependScrollback([]byte(content))

	if len(vt.Scrollback) == 0 {
		t.Fatal("expected scrollback lines from large content")
	}
	// Should have captured all 50 lines (some in scrollback, some on screen)
	if len(vt.Scrollback) < 50 {
		t.Errorf("expected at least 50 scrollback lines, got %d", len(vt.Scrollback))
	}
}

func TestPrependScrollbackRespectsMaxScrollback(t *testing.T) {
	vt := New(80, 24)

	// Fill existing scrollback close to max
	for i := 0; i < MaxScrollback-5; i++ {
		vt.Scrollback = append(vt.Scrollback, MakeBlankLine(80))
	}

	// Prepend 100 more lines
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	vt.PrependScrollback([]byte(strings.Join(lines, "\n") + "\n"))

	if len(vt.Scrollback) > MaxScrollback {
		t.Errorf("scrollback exceeded max: %d > %d", len(vt.Scrollback), MaxScrollback)
	}
}

func TestPrependScrollbackAllBlankContent(t *testing.T) {
	vt := New(80, 24)
	// Feed whitespace-only content -- produces two rows of space cells.
	vt.PrependScrollback([]byte("   \n   \n"))
	if got := len(vt.Scrollback); got != 2 {
		t.Errorf("expected 2 scrollback lines for whitespace content, got %d", got)
	}

	// Feed truly empty (newline-only) content. Capture parsing is row-count
	// exact (it counts the newlines in the capture, not whatever happens to
	// be blank on the temp screen), so three newline-delimited rows produce
	// three blank scrollback rows rather than being silently dropped -- real
	// tmux capture-pane output is already bounded to actual pane content
	// (see `capture-pane -S - -E -1`), so this no longer needs a
	// trailing-blank-trim heuristic to avoid capturing unused padding.
	vt2 := New(80, 24)
	vt2.PrependScrollback([]byte("\n\n\n"))
	if got := len(vt2.Scrollback); got != 3 {
		t.Errorf("expected 3 scrollback lines for all-blank content, got %d", got)
	}
}

func TestPrependScrollbackFullWidthRows(t *testing.T) {
	v := New(4, 3)
	// Three capture rows, each exactly Width(4) chars, newline-delimited.
	data := []byte("AAAA\nBBBB\nCCCC\n")
	v.PrependScrollback(data)
	if got := len(v.Scrollback); got != 3 {
		t.Fatalf("expected 3 scrollback rows, got %d (raw PTY parse double-advances full-width rows)", got)
	}
}

func TestIsBlankLine(t *testing.T) {
	blank := MakeBlankLine(10)
	if !isBlankLine(blank) {
		t.Error("MakeBlankLine should produce a blank line")
	}

	nonBlank := MakeBlankLine(10)
	nonBlank[5] = Cell{Rune: 'x', Width: 1}
	if isBlankLine(nonBlank) {
		t.Error("line with 'x' should not be blank")
	}

	// Null rune cells should be considered blank
	nullLine := make([]Cell, 10)
	if !isBlankLine(nullLine) {
		t.Error("line with null runes should be blank")
	}
}
