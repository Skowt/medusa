package center

import "testing"

// TestIsInterruptInput verifies which forwarded key bytes count as an agent
// interrupt. Claude Code's Stop hook does not fire on user interrupts, so
// Medusa must clear the activity state itself — for Esc (Claude Code's
// primary interrupt key) as well as Ctrl+C. Multi-byte escape sequences
// (arrow keys etc.) start with 0x1b but are not interrupts.
func TestIsInterruptInput(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
		want  bool
	}{
		{"ctrl+c", []byte{0x03}, true},
		{"esc", []byte{0x1b}, true},
		{"arrow up sequence", []byte{0x1b, '[', 'A'}, false},
		{"plain text", []byte("hello"), false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		if got := isInterruptInput(tc.input); got != tc.want {
			t.Errorf("isInterruptInput(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
