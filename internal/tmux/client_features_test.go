package tmux

import (
	"strings"
	"testing"
)

// TestClientCommandAdvertisesClientFeatures covers the tmux half of every
// rich-text chain. tmux forwards OSC 8 hyperlinks, 24-bit color, styled
// underlines and strikethrough only to a client whose terminal advertises
// them, and medusa attaches with TERM=xterm-256color, whose terminfo
// advertises none of them -- so without this tmux strips link URIs and
// quantises truecolor to 256 colors before the vterm ever sees them.
func TestClientCommandAdvertisesClientFeatures(t *testing.T) {
	cmd := ClientCommandWithOptions("test-session", "/tmp/work", "echo hello", DefaultOptions())

	for _, feature := range []string{"hyperlinks", "RGB", "usstyle", "strikethrough"} {
		if !strings.Contains(cmd, ":"+feature) {
			t.Errorf("command must advertise the %s terminal-feature for %s, got:\n%s", feature, ClientTerm, cmd)
		}
	}
	if !strings.Contains(cmd, "'"+ClientTerm+":") {
		t.Errorf("features must be keyed by the client TERM %s", ClientTerm)
	}

	// terminal-features is a server-wide array and this runs on every attach.
	// A bare -a append duplicates the entry per tab, and a check-then-append
	// races when tabs attach together; writing a fixed slot does neither, and
	// leaves the rest of the array alone.
	if strings.Contains(cmd, "-sa terminal-features") {
		t.Error("features must be written to a fixed slot, not appended")
	}
	if !strings.Contains(cmd, "terminal-features[") {
		t.Error("features must be written to an indexed slot rather than replacing the array")
	}

	// They must be in place before the client attaches, or the first attach
	// of a session renders without them.
	if strings.Index(cmd, "terminal-features") > strings.Index(cmd, "attach -dt") {
		t.Error("terminal-features must be set before attach")
	}
}
