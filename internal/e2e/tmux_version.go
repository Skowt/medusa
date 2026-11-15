package e2e

import (
	"os/exec"
	"regexp"
	"strconv"
	"testing"
)

// requireTmuxAtLeast skips a test whose oracle is behaviour a given tmux
// release introduced: colour-scheme reports (3.6), and the grapheme-combining
// rules the vterm mirrors (those of 3.6). On an older tmux the expectation is
// simply different, not broken.
func requireTmuxAtLeast(t *testing.T, major, minor int) {
	t.Helper()
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		t.Skip("tmux not installed")
	}
	m := regexp.MustCompile(`(\d+)\.(\d+)`).FindSubmatch(out)
	if m == nil {
		return // "tmux next-…" or a build without a number: assume current
	}
	gotMajor, _ := strconv.Atoi(string(m[1]))
	gotMinor, _ := strconv.Atoi(string(m[2]))
	if gotMajor < major || (gotMajor == major && gotMinor < minor) {
		t.Skipf("needs tmux %d.%d+, have %s", major, minor, out)
	}
}
