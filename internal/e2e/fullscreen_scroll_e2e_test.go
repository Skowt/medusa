package e2e

// End-to-end reproduction of the fullscreen-tab scrollback corruption: run the
// real medusa TUI in a PTY, with a stub `claude` that is a fullscreen pager
// (alt screen + SGR mouse + synchronized-output full repaints + internal
// scrollback). Wheel events are sent to MEDUSA (not the pane), exercising
// medusa's decode → forward → PTY → flush → vterm → compositor pipeline, and
// medusa's rendered screen is diffed against `tmux capture-pane` ground truth.
//
// MEDUSA_PTY_TRACE is enabled so that on divergence the traced byte stream
// (recorded in main-loop arrival order, before the flush/actor layers) can be
// replayed into a fresh vterm: if the replay matches tmux but medusa didn't,
// the corruption happened after updatePTYOutput.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/vterm"
)

// pagerStub is a fullscreen pager standing in for Claude Code's fullscreen
// renderer: alt screen, SGR mouse reporting, and wheel-driven scrollback with
// full-screen styled repaints wrapped in synchronized output.
const pagerStub = `#!/usr/bin/env python3
import os, re, sys, tty

fd = sys.stdin.fileno()
tty.setraw(fd)
size = os.get_terminal_size()
rows, cols = size.lines, size.columns

lines = []
for i in range(600):
    body = "x" * (cols - 7)
    lines.append(f"L{i:04d} {body}"[:cols])

top = len(lines) - rows

def draw():
    # Heavy per-run styling to approximate Claude Code's frame sizes
    # (~20-30KB per full repaint at this pane size).
    out = ["\x1b[?2026h\x1b[H"]
    for r in range(rows):
        line = lines[top + r]
        row = [f"\x1b[{r+1};1H"]
        for c in range(0, len(line), 4):
            fg = 16 + ((top + r) * 7 + c) % 216
            row.append(f"\x1b[0;38;5;{fg}m{line[c:c+4]}")
        row.append("\x1b[0m\x1b[K")
        out.append("".join(row))
    out.append("\x1b[?2026l")
    sys.stdout.write("".join(out))
    sys.stdout.flush()

sys.stdout.write("\x1b[?1049h\x1b[?1000h\x1b[?1006h")
draw()

buf = b""
wheel = re.compile(rb"\x1b\[<(\d+);(\d+);(\d+)([Mm])")
while True:
    try:
        data = os.read(fd, 1024)
    except OSError:
        break
    if not data:
        break
    buf += data
    changed = False
    while True:
        m = wheel.search(buf)
        if not m:
            break
        code = int(m.group(1))
        if code == 64:
            top = max(0, top - 3)
            changed = True
        elif code == 65:
            top = min(len(lines) - rows, top + 3)
            changed = True
        buf = buf[m.end():]
    if len(buf) > 64:
        buf = buf[-64:]
    if changed:
        draw()
`

func writePagerStub(t *testing.T, home string) string {
	t.Helper()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	scriptPath := filepath.Join(binDir, "claude")
	if err := os.WriteFile(scriptPath, []byte(pagerStub), 0o755); err != nil {
		t.Fatalf("write pager stub: %v", err)
	}
	return binDir
}

var lineToken = regexp.MustCompile(`L\d{4}`)

func lineTokens(text string) []string {
	return lineToken.FindAllString(text, -1)
}

// sendWheelToMedusa writes an SGR wheel report into medusa's own PTY, as the
// user's terminal would when they scroll over the center pane.
func sendWheelToMedusa(t *testing.T, session *PTYSession, code, col1, row1 int) {
	t.Helper()
	if err := session.SendString(fmt.Sprintf("\x1b[<%d;%d;%dM", code, col1, row1)); err != nil {
		t.Fatalf("send wheel: %v", err)
	}
}

// centerContentCoords finds a screen coordinate inside the rendered pager
// content within medusa's UI (1-based for SGR).
func centerContentCoords(t *testing.T, session *PTYSession) (int, int) {
	t.Helper()
	screen := session.ScreenASCII()
	for y, line := range strings.Split(screen, "\n") {
		idx := strings.Index(line, "L0")
		if idx >= 0 {
			return idx + 20, y + 3
		}
	}
	t.Fatalf("pager content not found in medusa screen:\n%s", screen)
	return 0, 0
}

func capturePaneText(t *testing.T, server, tmuxSession string) string {
	t.Helper()
	out, err := exec.Command("tmux", "-L", server, "capture-pane", "-p", "-t", tmuxSession).Output()
	if err != nil {
		t.Fatalf("capture-pane: %v", err)
	}
	return string(out)
}

func compareMedusaToTmux(t *testing.T, session *PTYSession, server, tmuxSession, step string) bool {
	t.Helper()
	var medusaTokens, paneTokens []string
	var screen, capture string
	for attempt := 0; attempt < 6; attempt++ {
		time.Sleep(400 * time.Millisecond)
		capture = capturePaneText(t, server, tmuxSession)
		screen = session.ScreenASCII()
		paneTokens = lineTokens(capture)
		medusaTokens = lineTokens(screen)
		if len(paneTokens) > 0 && strings.Join(medusaTokens, " ") == strings.Join(paneTokens, " ") {
			t.Logf("%s: medusa view matches tmux pane (%d rows)", step, len(paneTokens))
			return true
		}
	}
	t.Errorf("%s: medusa view diverged from tmux pane\n medusa tokens: %v\n tmux tokens:   %v\n\nmedusa screen:\n%s\n\ntmux pane:\n%s",
		step, medusaTokens, paneTokens, screen, capture)
	return false
}

// hexDumpLine matches hex.Dump body lines: "00000000  1b 5b ... |...|".
var hexDumpLine = regexp.MustCompile(`^[0-9a-f]{8}  ((?:[0-9a-f]{2} ?)+)`)

// readPTYTrace reconstructs the raw byte stream from a MEDUSA_PTY_TRACE file.
func readPTYTrace(t *testing.T, path string) ([]byte, bool) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trace: %v", err)
	}
	defer func() { _ = f.Close() }()
	var out []byte
	truncated := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "TRACE TRUNCATED") {
			truncated = true
			continue
		}
		m := hexDumpLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, hx := range strings.Fields(m[1]) {
			v, err := strconv.ParseUint(hx, 16, 8)
			if err != nil {
				continue
			}
			out = append(out, byte(v))
		}
	}
	return out, truncated
}

func TestFullscreenWheelScrollThroughMedusa(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, true)
	binDir := writePagerStub(t, home)
	server := fmt.Sprintf("medusa-e2e-fs-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	defer killTmuxServer(t, server)

	env := append(sessionEnv(binDir, server), "MEDUSA_PTY_TRACE=claude")
	session, cleanup, err := StartPTYSession(PTYOptions{
		Home:   home,
		Env:    env,
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
	waitForUIContains(t, session, "L0", persistenceTimeout)
	waitForSessionTypes(t, opts, map[string]bool{"agent": true}, persistenceTimeout)

	sessions, err := tmux.ListSessionsMatchingTags(map[string]string{"@medusa": "1"}, opts)
	if err != nil || len(sessions) == 0 {
		t.Fatalf("no medusa tmux sessions found: %v", err)
	}
	tmuxSession := sessions[0]
	time.Sleep(500 * time.Millisecond)

	wheelX, wheelY := centerContentCoords(t, session)
	t.Logf("wheel coords in medusa screen: %d,%d", wheelX, wheelY)

	if !compareMedusaToTmux(t, session, server, tmuxSession, "initial") {
		t.Fatal("initial state already divergent")
	}

	diverged := false
	for round := 1; round <= 3 && !diverged; round++ {
		for i := 0; i < 10; i++ {
			sendWheelToMedusa(t, session, 64, wheelX, wheelY)
			time.Sleep(80 * time.Millisecond)
		}
		for i := 0; i < 30; i++ {
			sendWheelToMedusa(t, session, 64, wheelX, wheelY)
		}
		diverged = !compareMedusaToTmux(t, session, server, tmuxSession, fmt.Sprintf("burst round %d", round))
		if !diverged {
			// Scroll back down so the next round has room to scroll up.
			for i := 0; i < 45; i++ {
				sendWheelToMedusa(t, session, 65, wheelX, wheelY)
				time.Sleep(10 * time.Millisecond)
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	if !diverged {
		t.Log("no divergence after 3 burst rounds")
		runClassicAdoptPhase(t, session, home, binDir, server, repo, tmuxSession)
		return
	}

	// Diagnose: does one more frame (a single wheel event) repair the view?
	sendWheelToMedusa(t, session, 64, wheelX, wheelY)
	time.Sleep(600 * time.Millisecond)
	recovered := strings.Join(lineTokens(session.ScreenASCII()), " ") ==
		strings.Join(lineTokens(capturePaneText(t, server, tmuxSession)), " ")
	t.Logf("recovery after one extra wheel frame: %v", recovered)

	// Replay the traced byte stream (arrival order at updatePTYOutput) into a
	// fresh vterm and compare against the pane at divergence time. Note the
	// extra recovery frame above is also in the trace, so compare replay to
	// the CURRENT pane.
	paneText := capturePaneText(t, server, tmuxSession)
	sizeOut, err := exec.Command("tmux", "-L", server, "display-message", "-p", "-t", tmuxSession,
		"#{pane_width} #{pane_height}").Output()
	if err != nil {
		t.Fatalf("pane size: %v", err)
	}
	var pw, ph int
	_, _ = fmt.Sscanf(strings.TrimSpace(string(sizeOut)), "%d %d", &pw, &ph)

	traces, _ := filepath.Glob(filepath.Join(home, ".medusa", "logs", "medusa-pty-claude-*.log"))
	if len(traces) == 0 {
		t.Fatalf("no PTY trace files found")
	}
	raw, truncated := readPTYTrace(t, traces[len(traces)-1])
	t.Logf("trace %s: %d bytes, truncated=%v", traces[len(traces)-1], len(raw), truncated)
	if truncated {
		t.Log("trace truncated — replay comparison not conclusive")
		return
	}

	replay := vterm.New(pw, ph)
	replay.Write(raw)
	replayTokens := lineTokens(func() string {
		screen := replay.VisibleScreen()
		var b strings.Builder
		for _, row := range screen {
			for _, cell := range row {
				if cell.Width == 0 {
					continue
				}
				if cell.GraphemeCluster != "" {
					b.WriteString(cell.GraphemeCluster)
					continue
				}
				r := cell.Rune
				if r == 0 {
					r = ' '
				}
				b.WriteRune(r)
			}
			b.WriteByte('\n')
		}
		return b.String()
	}())
	paneTokens := lineTokens(paneText)
	if strings.Join(replayTokens, " ") == strings.Join(paneTokens, " ") {
		t.Logf("REPLAY MATCHES PANE: traced input is intact and parses correctly — corruption happens AFTER updatePTYOutput (flush/actor layer)")
	} else {
		t.Logf("REPLAY DIVERGES TOO: corruption is in the traced stream or the vterm parser\n replay: %v\n pane:   %v", replayTokens, paneTokens)
	}
}

// runClassicAdoptPhase reproduces the pre-upgrade scenario: the persisted tab
// metadata has no fullscreen flag (created by an older medusa), the app inside
// the still-running tmux session is in the alt screen with mouse reporting on
// (as after `/tui fullscreen` in Claude Code). After restart+adoption, wheel
// events in medusa must reach the app — the tmux pane itself must scroll and
// medusa's view must track it — instead of scrolling medusa's stale local
// scrollback.
func runClassicAdoptPhase(t *testing.T, session *PTYSession, home, binDir, server, repo, tmuxSession string) {
	t.Helper()

	quitApp(t, session)
	if err := session.WaitForExit(persistenceTimeout); err != nil {
		t.Fatalf("waiting for exit: %v", err)
	}

	// Simulate a tab persisted by a pre-fullscreen medusa: drop the flag.
	metas, _ := filepath.Glob(filepath.Join(home, ".medusa", "workspaces-metadata", "*", "workspace.json"))
	if len(metas) == 0 {
		t.Fatal("no workspace metadata found")
	}
	for _, meta := range metas {
		raw, err := os.ReadFile(meta)
		if err != nil {
			t.Fatalf("read metadata: %v", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse metadata: %v", err)
		}
		tabs, _ := doc["open_tabs"].([]any)
		for _, entry := range tabs {
			if tab, ok := entry.(map[string]any); ok {
				delete(tab, "fullscreen")
			}
		}
		edited, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatalf("marshal metadata: %v", err)
		}
		if err := os.WriteFile(meta, edited, 0o644); err != nil {
			t.Fatalf("write metadata: %v", err)
		}
	}

	env := append(sessionEnv(binDir, server), "MEDUSA_PTY_TRACE=claude")
	restart, restartCleanup, err := StartPTYSession(PTYOptions{
		Home:   home,
		Env:    env,
		Width:  160,
		Height: 45,
	})
	if err != nil {
		t.Fatalf("restart session: %v", err)
	}
	defer restartCleanup()
	defer func() {
		quitApp(t, restart)
		_ = restart.WaitForExit(persistenceTimeout)
	}()

	waitForUIContains(t, restart, filepath.Base(repo), persistenceTimeout)
	activatePrimaryWorkspace(t, restart)
	waitForUIContains(t, restart, "L0", persistenceTimeout)
	// Dismiss any stray dialog (e.g. Set Note) opened by the activation
	// keypress, mirroring quitApp's workaround.
	if err := restart.SendBytes([]byte{0x1b}); err != nil {
		t.Fatalf("send escape: %v", err)
	}
	time.Sleep(1 * time.Second)

	wheelX, wheelY := centerContentCoords(t, restart)
	if !compareMedusaToTmux(t, restart, server, tmuxSession, "classic adopt: initial") {
		t.Fatal("adopted view already divergent")
	}

	paneBefore := capturePaneText(t, server, tmuxSession)
	for i := 0; i < 10; i++ {
		sendWheelToMedusa(t, restart, 64, wheelX, wheelY)
		time.Sleep(80 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond)
	paneAfter := capturePaneText(t, server, tmuxSession)

	if strings.Join(lineTokens(paneBefore), " ") == strings.Join(lineTokens(paneAfter), " ") {
		t.Errorf("classic adopt: wheel in medusa did not scroll the alt-screen app (pane unchanged)\npane:\n%s", paneAfter)
	}
	compareMedusaToTmux(t, restart, server, tmuxSession, "classic adopt: after wheel")
}
