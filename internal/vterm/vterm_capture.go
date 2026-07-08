package vterm

import "bytes"

// parseCaptureWithSize parses newline-delimited tmux capture-pane output
// (rather than a raw PTY byte stream) into a temporary VTerm sized to the
// given width/height. Returns nil for empty input.
func parseCaptureWithSize(data []byte, width, height int) *VTerm {
	if len(data) == 0 {
		return nil
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	tmp := New(width, height)
	tmp.TreatLFAsCRLF = true
	tmp.Write(trimCaptureTrailingNewline(data))
	return tmp
}

// captureRowCount returns the number of rows represented by newline-delimited
// capture data, treating the (optional) trailing newline as a row terminator
// rather than a separate empty row.
func captureRowCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	trimmed := trimCaptureTrailingNewline(data)
	if len(trimmed) == 0 {
		return 1
	}
	return bytes.Count(trimmed, []byte{'\n'}) + 1
}

// captureLines extracts the parsed capture rows from tmp: all scrollback
// lines (produced when the capture overflowed tmp's height), followed by as
// many screen rows as the original row count requires.
func captureLines(data []byte, tmp *VTerm) [][]Cell {
	if tmp == nil {
		return nil
	}
	lines := make([][]Cell, 0, len(tmp.Scrollback)+len(tmp.Screen))
	for _, line := range tmp.Scrollback {
		lines = append(lines, CopyLine(line))
	}
	screenRows := captureRowCount(data) - len(tmp.Scrollback)
	if screenRows <= 0 {
		return lines
	}
	if screenRows > len(tmp.Screen) {
		screenRows = len(tmp.Screen)
	}
	for i := 0; i < screenRows; i++ {
		lines = append(lines, CopyLine(tmp.Screen[i]))
	}
	return lines
}

// trimCaptureTrailingNewline strips a single trailing line terminator
// (\r\n, \n, or \r) from capture data, so it isn't counted as an extra row.
func trimCaptureTrailingNewline(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	if bytes.HasSuffix(data, []byte("\r\n")) {
		return data[:len(data)-2]
	}
	if data[len(data)-1] == '\n' || data[len(data)-1] == '\r' {
		return data[:len(data)-1]
	}
	return data
}
