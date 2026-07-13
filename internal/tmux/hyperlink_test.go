package tmux

import (
	"strings"
	"testing"
)

// TestClientCommandAdvertisesHyperlinks covers the first half of the OSC 8
// chain. tmux only forwards hyperlinks to a client whose terminal advertises
// the "hyperlinks" feature; medusa attaches with TERM=xterm-256color, which
// does not by default, so tmux silently strips the URI from pane output.
func TestClientCommandAdvertisesHyperlinks(t *testing.T) {
	cmd := ClientCommandWithOptions("test-session", "/tmp/work", "echo hello", DefaultOptions())

	if !strings.Contains(cmd, ClientTerm+":hyperlinks") {
		t.Fatalf("command must advertise the hyperlinks terminal-feature for %s, got:\n%s", ClientTerm, cmd)
	}

	// terminal-features is a server-wide array and this runs on every attach,
	// so a bare append would add a duplicate entry per tab. It must be guarded.
	if !strings.Contains(cmd, "set-option -sa terminal-features") {
		t.Error("hyperlinks feature should be appended to terminal-features, not overwrite it")
	}
	if !strings.Contains(cmd, "||") {
		t.Error("the append must be guarded so repeated attaches don't duplicate the entry")
	}

	// The feature must be advertised before the client attaches, or the first
	// attach of a session renders without hyperlink support.
	if strings.Index(cmd, "terminal-features") > strings.Index(cmd, "attach -dt") {
		t.Error("terminal-features must be set before attach")
	}
}
