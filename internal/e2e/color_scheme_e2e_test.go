package e2e

// An agent follows the color scheme by asking tmux (CSI ? 996 n, or mode 2031
// for pushed updates, which Claude Code enables at startup), and tmux answers
// from what it knows of its client -- the center tab's vterm. At attach tmux
// works the scheme out from the vterm's OSC 11 background reply, so that much
// always worked. What it cannot see is a later change: it does not query
// again, and learns of one only from a "CSI ? 997 n" report, which the vterm
// never sent. A medusa theme switch from dark to light left every agent
// believing it was still on a dark background.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/vterm"
)

func TestAgentLearnsColorSchemeThroughTmux(t *testing.T) {
	requireTmuxAtLeast(t, 3, 6)
	dark := vterm.Color{Type: vterm.ColorRGB, Value: 0x1a1b26}
	light := vterm.Color{Type: vterm.ColorRGB, Value: 0xfafafa}
	for _, tc := range []struct {
		name           string
		start, switch_ vterm.Color
		want           string
	}{
		{"dark to light", dark, light, "?997;2n"},
		{"light to dark", light, dark, "?997;1n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := fmt.Sprintf("medusa-scheme-%d-%d", tc.start.Value, os.Getpid())
			t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })
			reply := filepath.Join(t.TempDir(), "reply")

			// Ask only after the theme has switched under the attached client.
			// (Enabling 2031 up front would make tmux push the starting scheme
			// into the pane's input, and that stale report is what would be read.)
			inner := fmt.Sprintf(`stty raw -echo; sleep 1.5; printf '\033[?996n'; dd bs=1 count=9 of=%s 2>/dev/null; sleep 30`, reply)
			opts := tmux.DefaultOptions()
			opts.ServerName = socket
			client := attachRespondingClient(t, tmux.ClientCommandWithOptions("s", "/tmp", inner, opts), 80, 10, tc.start)
			time.Sleep(700 * time.Millisecond)
			client.mu.Lock()
			client.term.SetDefaultColors(vterm.Color{Type: vterm.ColorRGB, Value: 0xc0c0c0}, tc.switch_, vterm.Color{})
			client.mu.Unlock()

			deadline := time.Now().Add(5 * time.Second)
			var got []byte
			for time.Now().Before(deadline) {
				got, _ = os.ReadFile(reply)
				if len(got) >= 9 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !strings.Contains(string(got), tc.want) {
				t.Fatalf("agent's scheme query got %q, want a reply containing %q", got, tc.want)
			}
		})
	}
}

// attachRespondingClient is attachClientCommand with the vterm's replies to
// terminal queries written back into the PTY, as a center tab's are, and the
// host background a center tab is given.
func attachRespondingClient(t *testing.T, command string, cols, rows int, bg vterm.Color) *reproClient {
	t.Helper()
	c := attachClientCommandEnv(t, command, os.Environ(), cols, rows)
	c.mu.Lock()
	c.term.SetDefaultColors(vterm.Color{Type: vterm.ColorRGB, Value: 0xc0c0c0}, bg, vterm.Color{})
	c.term.SetResponseWriter(func(b []byte) { _, _ = c.pty.Write(b) })
	c.mu.Unlock()
	return c
}
