package e2e

// Live probe: attach a second vterm-backed client to a running medusa tmux
// session and scroll Claude's fullscreen transcript via forwarded SGR wheel
// events — exactly what medusa's mouse hand-off does — then diff the vterm
// screen against `tmux capture-pane` ground truth.
//
// Opt-in (it scrolls a real session, visual-only, restored afterwards):
//
//	MEDUSA_LIVE_PROBE_SESSION=medusa-... go test ./internal/e2e -run TestLiveScrollProbe -v

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLiveScrollProbe(t *testing.T) {
	session := os.Getenv("MEDUSA_LIVE_PROBE_SESSION")
	if session == "" {
		t.Skip("set MEDUSA_LIVE_PROBE_SESSION to run the live probe")
	}
	socket := os.Getenv("MEDUSA_LIVE_PROBE_SOCKET")
	if socket == "" {
		socket = "medusa"
	}

	sizeOut, err := exec.Command("tmux", "-L", socket, "display-message", "-p", "-t", session,
		"#{window_width} #{window_height}").Output()
	if err != nil {
		t.Fatalf("session lookup: %v", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(sizeOut)))
	if len(fields) != 2 {
		t.Fatalf("unexpected size output %q", sizeOut)
	}
	cols, _ := strconv.Atoi(fields[0])
	rows, _ := strconv.Atoi(fields[1])
	if cols < 10 || rows < 5 {
		t.Fatalf("implausible window size %dx%d", cols, rows)
	}
	t.Logf("probing session %s at %dx%d", session, cols, rows)

	client := attachReproClient(t, socket, session, cols, rows)
	time.Sleep(800 * time.Millisecond)

	capture := func() string {
		out, err := exec.Command("tmux", "-L", socket, "capture-pane", "-p", "-t", session).Output()
		if err != nil {
			t.Fatalf("capture-pane: %v", err)
		}
		return string(out)
	}

	// compare retries a few times since the live pane may repaint (spinner).
	compare := func(step string) bool {
		t.Helper()
		var vtermText, tmuxText string
		for attempt := 0; attempt < 4; attempt++ {
			time.Sleep(500 * time.Millisecond)
			tmuxText = capture()
			vtermText = client.screenText()
			if strings.Join(trimLines(vtermText), "\n") == strings.Join(trimLines(tmuxText), "\n") {
				t.Logf("%s: screens match (attempt %d)", step, attempt+1)
				return true
			}
		}
		t.Errorf("%s: vterm diverged from tmux ground truth\n%s", step, diffScreens(vtermText, tmuxText))
		return false
	}

	wheelCol, wheelRow := cols/2, rows/2

	compare("initial attach")

	for i := 0; i < 12; i++ {
		client.send(fmt.Sprintf("\x1b[<64;%d;%dM", wheelCol, wheelRow))
		time.Sleep(90 * time.Millisecond)
	}
	compare("after 12 paced wheel-up")

	for i := 0; i < 20; i++ {
		client.send(fmt.Sprintf("\x1b[<64;%d;%dM", wheelCol, wheelRow))
	}
	compare("after wheel-up burst")

	// Restore: scroll back down well past where we started.
	for i := 0; i < 40; i++ {
		client.send(fmt.Sprintf("\x1b[<65;%d;%dM", wheelCol, wheelRow))
		time.Sleep(30 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	compare("after restore scroll-down")

	if t.Failed() {
		raw := client.rawDump()
		path := filepath.Join(os.TempDir(), fmt.Sprintf("medusa-live-probe-%d.raw", os.Getpid()))
		_ = os.WriteFile(path, []byte(raw), 0o644)
		t.Logf("full raw client stream (%d bytes) written to %s", len(raw), path)
		tail := raw
		if len(tail) > 6000 {
			tail = tail[len(tail)-6000:]
		}
		t.Logf("raw tail: %q", tail)
	}
}
