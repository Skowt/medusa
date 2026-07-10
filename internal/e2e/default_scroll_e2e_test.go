package e2e

// End-to-end regression test for scrolling a DEFAULT-mode (non-fullscreen) tab.
//
// A default-mode Claude is an inline app: no alt screen, no mouse reporting. It
// is medusa — not the app — that owns the wheel there, scrolling its own vterm
// scrollback. The trap is that medusa's vterm is fed by a `tmux attach` client,
// and a tmux CLIENT enters the alternate screen at attach no matter what the
// pane's app does. So vterm.AltScreen is always true and says nothing about the
// app; gating scrollback on it disables history for every tab and makes the
// wheel a silent no-op (ScrollView clamps to an empty scrollback).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/tmux"
)

// inlineStub stands in for default-mode Claude Code: plain inline output on the
// main screen, no alt screen, no mouse reporting. It waits for medusa's client
// to attach, emits enough lines to overflow the pane, then idles so the view is
// static while we scroll it.
const inlineStub = `#!/usr/bin/env python3
import sys, time
time.sleep(1.5)
for i in range(300):
    sys.stdout.write("L%04d inline transcript line\n" % i)
    sys.stdout.flush()
    time.sleep(0.01)
while True:
    time.sleep(1)
`

// writeStubClaude installs script as the `claude` binary on the session PATH.
func writeStubClaude(t *testing.T, home, script string) string {
	t.Helper()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write stub claude: %v", err)
	}
	return binDir
}

// visibleLineNumbers returns the L#### tokens on medusa's rendered screen.
func visibleLineNumbers(screen string) []int {
	var nums []int
	for _, tok := range lineTokens(screen) {
		n, err := strconv.Atoi(strings.TrimPrefix(tok, "L"))
		if err == nil {
			nums = append(nums, n)
		}
	}
	sort.Ints(nums)
	return nums
}

func TestDefaultModeWheelScrollsMedusaScrollback(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, true) // last_fullscreen unset => tab launches in default mode
	binDir := writeStubClaude(t, home, inlineStub)
	server := fmt.Sprintf("medusa-e2e-default-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	defer killTmuxServer(t, server)

	session, cleanup, err := StartPTYSession(PTYOptions{
		Home:   home,
		Env:    sessionEnv(binDir, server),
		Width:  160,
		Height: 45,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), persistenceTimeout)
	activatePrimaryWorkspace(t, session)
	waitForUIContains(t, session, "Branch:", persistenceTimeout)
	createAgentTab(t, session)
	waitForUIContains(t, session, "L02", persistenceTimeout)
	waitForSessionTypes(t, opts, map[string]bool{"agent": true}, persistenceTimeout)

	// Let the stub finish emitting and the view settle at the live bottom.
	time.Sleep(3 * time.Second)

	live := visibleLineNumbers(session.ScreenASCII())
	if len(live) == 0 {
		t.Fatalf("no transcript lines on medusa screen:\n%s", session.ScreenASCII())
	}
	liveTop := live[0]
	t.Logf("live view shows L%04d..L%04d", live[0], live[len(live)-1])
	if liveTop == 0 {
		t.Fatalf("pane never overflowed, nothing to scroll back to")
	}

	wheelX, wheelY := centerContentCoords(t, session)

	// Wheel up over the center pane: medusa must scroll its own scrollback.
	for i := 0; i < 6; i++ {
		sendWheelToMedusa(t, session, 64, wheelX, wheelY)
		time.Sleep(120 * time.Millisecond)
	}
	time.Sleep(600 * time.Millisecond)

	scrolled := visibleLineNumbers(session.ScreenASCII())
	if len(scrolled) == 0 {
		t.Fatalf("no transcript lines after wheel-up:\n%s", session.ScreenASCII())
	}
	t.Logf("after wheel-up shows L%04d..L%04d", scrolled[0], scrolled[len(scrolled)-1])

	if scrolled[0] >= liveTop {
		t.Errorf("wheel-up did not scroll into history: top line was L%04d, still L%04d after scrolling\n%s",
			liveTop, scrolled[0], session.ScreenASCII())
	}

	// Wheel back down: the view must return to the live bottom.
	for i := 0; i < 12; i++ {
		sendWheelToMedusa(t, session, 65, wheelX, wheelY)
		time.Sleep(60 * time.Millisecond)
	}
	time.Sleep(600 * time.Millisecond)

	back := visibleLineNumbers(session.ScreenASCII())
	if len(back) == 0 || back[len(back)-1] != live[len(live)-1] {
		got := "none"
		if len(back) > 0 {
			got = fmt.Sprintf("L%04d", back[len(back)-1])
		}
		t.Errorf("wheel-down did not return to the live view: last line %s, want L%04d",
			got, live[len(live)-1])
	}
}
