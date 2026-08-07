package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The modified cases carry no Text on purpose: both decoders clear it for
// anything held with more than shift, so this is what really arrives.
func TestKeyToBytes(t *testing.T) {
	tests := []struct {
		name     string
		key      tea.KeyPressMsg
		expected []byte
	}{
		{
			name:     "plain enter submits",
			key:      tea.KeyPressMsg{Code: tea.KeyEnter, Text: "\r"},
			expected: []byte{'\r'},
		},
		{
			name:     "shift+enter inserts a newline",
			key:      tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift},
			expected: []byte{0x1b, '\r'},
		},
		{
			name:     "alt+enter inserts a newline",
			key:      tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt},
			expected: []byte{0x1b, '\r'},
		},
		{
			name:     "alt+b moves a word left",
			key:      tea.KeyPressMsg{Code: 'b', Mod: tea.ModAlt},
			expected: []byte{0x1b, 'b'},
		},
		{
			name:     "alt+f moves a word right",
			key:      tea.KeyPressMsg{Code: 'f', Mod: tea.ModAlt},
			expected: []byte{0x1b, 'f'},
		},
		{
			name:     "alt+shift+b uses the shifted rune",
			key:      tea.KeyPressMsg{Code: 'b', ShiftedCode: 'B', Mod: tea.ModAlt | tea.ModShift},
			expected: []byte{0x1b, 'B'},
		},
		{
			name:     "alt+backspace deletes a word",
			key:      tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt},
			expected: []byte{0x1b, 0x7f},
		},
		{
			name:     "ctrl+backspace deletes a word",
			key:      tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModCtrl},
			expected: []byte{0x1b, 0x7f},
		},
		{
			name:     "plain backspace deletes a character",
			key:      tea.KeyPressMsg{Code: tea.KeyBackspace},
			expected: []byte{0x7f},
		},
		{
			name:     "ctrl+left moves a word left",
			key:      tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl},
			expected: []byte("\x1b[1;5D"),
		},
		{
			name:     "ctrl+right moves a word right",
			key:      tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl},
			expected: []byte("\x1b[1;5C"),
		},
		{
			name:     "alt+left keeps its xterm encoding",
			key:      tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt},
			expected: []byte("\x1b[1;3D"),
		},
		{
			name:     "shift+left carries the modifier",
			key:      tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift},
			expected: []byte("\x1b[1;2D"),
		},
		{
			name:     "plain left has no modifier parameter",
			key:      tea.KeyPressMsg{Code: tea.KeyLeft},
			expected: []byte("\x1b[D"),
		},
		{
			name:     "ctrl+home jumps to the start",
			key:      tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModCtrl},
			expected: []byte("\x1b[1;5H"),
		},
		{
			name:     "alt+delete deletes the next word",
			key:      tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt},
			expected: []byte("\x1b[3;3~"),
		},
		{
			name:     "plain delete has no modifier parameter",
			key:      tea.KeyPressMsg{Code: tea.KeyDelete},
			expected: []byte("\x1b[3~"),
		},
		{
			name:     "ctrl+a is a control byte",
			key:      tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl},
			expected: []byte{0x01},
		},
		{
			name:     "ctrl+q is a control byte",
			key:      tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl},
			expected: []byte{0x11},
		},
		{
			name:     "ctrl+alt+f keeps both modifiers",
			key:      tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl | tea.ModAlt},
			expected: []byte{0x1b, 0x06},
		},
		{
			name:     "ctrl+space is NUL",
			key:      tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl},
			expected: []byte{0x00},
		},
		{
			name:     "shift+tab cycles backwards",
			key:      tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift},
			expected: []byte{0x1b, '[', 'Z'},
		},
		{
			name:     "printable text is forwarded as-is",
			key:      tea.KeyPressMsg{Code: 'a', Text: "a"},
			expected: []byte("a"),
		},
		{
			name:     "cmd+k is not forwarded as text",
			key:      tea.KeyPressMsg{Code: 'k', Mod: tea.ModSuper},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KeyToBytes(tt.key)
			if string(got) != string(tt.expected) {
				t.Fatalf("KeyToBytes(%s) = %q, want %q", tt.key.String(), got, tt.expected)
			}
		})
	}
}
