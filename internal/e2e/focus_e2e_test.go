package e2e

// Claude Code enables focus reporting and acts on it -- the session recap is
// generated only while the terminal is unfocused, and PushNotification stays
// silent while it is focused. A pane's focus comes from tmux, and tmux takes
// it from its client's reports; a center tab's client is the tab's PTY, which
// never sent any, so tmux treated every tab as focused forever. This pins the
// half of the chain center.SyncAgentFocus relies on: tmux enables focus
// reporting on medusa's client, and forwards what that client reports to the
// agent.

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

func TestClientFocusReportsReachTheAgent(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	socket := fmt.Sprintf("medusa-focus-%d", os.Getpid())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })
	got := filepath.Join(t.TempDir(), "focus")

	// The agent enables focus reporting and records what tmux sends it.
	inner := fmt.Sprintf(`stty raw -echo; printf '\033[?1004h'; exec cat > %s`, got)
	opts := tmux.DefaultOptions()
	opts.ServerName = socket
	client := attachRespondingClient(t, tmux.ClientCommandWithOptions("s", "/tmp", inner, opts), 80, 10,
		vterm.Color{Type: vterm.ColorRGB, Value: 0x1a1b26})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client.mu.Lock()
		ready := client.term.FocusReporting()
		client.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	client.mu.Lock()
	ready := client.term.FocusReporting()
	client.mu.Unlock()
	if !ready {
		t.Fatal("tmux never enabled focus reporting on the client; center.SyncAgentFocus would never report")
	}
	time.Sleep(300 * time.Millisecond)

	client.send("\x1b[O")
	waitForFile(t, got, "\x1b[O")
	client.send("\x1b[I")
	waitForFile(t, got, "\x1b[O\x1b[I")
}

// waitForFile waits until path's content ends with suffix.
func waitForFile(t *testing.T, path, suffix string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var b []byte
	for time.Now().Before(deadline) {
		b, _ = os.ReadFile(path)
		if strings.HasSuffix(string(b), suffix) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("agent received %q, want it to end with %q", b, suffix)
}
