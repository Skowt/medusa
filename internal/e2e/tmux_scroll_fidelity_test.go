package e2e

// Reproduction harness for the fullscreen-tab scrollback corruption: attach to
// a tmux session through a PTY + vterm exactly the way medusa does, scroll a
// fullscreen (alt-screen) app via forwarded mouse wheel events, and diff the
// vterm's visible screen against `tmux capture-pane` ground truth.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/Skowt/medusa/internal/vterm"
)

type tmuxRepro struct {
	socket string
	t      *testing.T
}

func (r *tmuxRepro) run(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("tmux", append([]string{"-L", r.socket}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("tmux %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// reproClient is a tmux client attached through a PTY, mirroring how medusa's
// pty.Terminal + vterm pair consumes a tab's tmux session.
type reproClient struct {
	pty  *os.File
	term *vterm.VTerm
	raw  *strings.Builder
	mu   sync.Mutex
}

func attachReproClient(t *testing.T, socket, session string, cols, rows int) *reproClient {
	t.Helper()
	cmd := exec.Command("tmux", "-L", socket, "attach", "-t", session)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		t.Fatalf("attach client: %v", err)
	}
	c := &reproClient{pty: ptmx, term: vterm.New(cols, rows), raw: &strings.Builder{}}
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				c.mu.Lock()
				c.term.Write(buf[:n])
				c.raw.Write(buf[:n])
				c.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return c
}

func (c *reproClient) screenText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	screen := c.term.VisibleScreen()
	lines := make([]string, 0, len(screen))
	for _, row := range screen {
		var b strings.Builder
		for _, cell := range row {
			if cell.Width == 0 {
				continue // wide-char continuation cell
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
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}

func (c *reproClient) rawDump() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.raw.String()
}

func (c *reproClient) send(s string) {
	_, _ = c.pty.Write([]byte(s))
}

// wheelUp sends an SGR-encoded mouse wheel-up at column/row (1-based).
func (c *reproClient) wheelUp(col, row int) {
	c.send(fmt.Sprintf("\x1b[<64;%d;%dM", col, row))
}

func trimLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	// Drop trailing empty lines.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func diffScreens(vtermText, tmuxText string) string {
	vl := trimLines(vtermText)
	tl := trimLines(tmuxText)
	n := len(vl)
	if len(tl) > n {
		n = len(tl)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		var v, tx string
		if i < len(vl) {
			v = vl[i]
		}
		if i < len(tl) {
			tx = tl[i]
		}
		marker := "  "
		if v != tx {
			marker = "!!"
		}
		fmt.Fprintf(&b, "%s %2d vterm |%s|\n%s %2d tmux  |%s|\n", marker, i, v, marker, i, tx)
	}
	return b.String()
}

func TestVtermMatchesTmuxDuringFullscreenScroll(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	if _, err := exec.LookPath("less"); err != nil {
		t.Skip("less not installed")
	}

	const cols, rows = 100, 30

	// Numbered content so any misplaced chunk is obvious.
	dir := t.TempDir()
	file := filepath.Join(dir, "content.txt")
	var content strings.Builder
	for i := 1; i <= 400; i++ {
		switch i % 4 {
		case 0:
			// Exactly pane-width row: "line-NNNN " (10) + 90 filler = 100 cols.
			fmt.Fprintf(&content, "line-%04d %s\n", i, strings.Repeat("x", cols-10))
		case 1:
			// Wide glyphs (2 cells each) ending exactly at the right edge.
			fmt.Fprintf(&content, "line-%04d %s\n", i, strings.Repeat("界", (cols-10)/2))
		case 2:
			// Table-ish box drawing, full width.
			fmt.Fprintf(&content, "line-%04d ├%s┤\n", i, strings.Repeat("─", cols-12))
		default:
			fmt.Fprintf(&content, "line-%04d %s\n", i, strings.Repeat("x", 60))
		}
	}
	if err := os.WriteFile(file, []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &tmuxRepro{socket: fmt.Sprintf("medusa-repro-%d", os.Getpid()), t: t}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", r.socket, "kill-server").Run()
	})

	// Session mirrors a medusa fullscreen tab: alt-screen app, mouse on,
	// status off, prefix disabled.
	r.run("new-session", "-d", "-s", "s", "-x", fmt.Sprint(cols), "-y", fmt.Sprint(rows),
		fmt.Sprintf("less --mouse -R +200 %s", file))
	r.run("set-option", "-t", "s", "prefix", "None")
	r.run("set-option", "-t", "s", "status", "off")
	r.run("set-option", "-t", "s", "mouse", "on")
	r.run("set-option", "-t", "s", "default-terminal", "xterm-256color")

	client := attachReproClient(t, r.socket, "s", cols, rows)
	time.Sleep(500 * time.Millisecond)

	if !strings.Contains(client.screenText(), "line-0200") {
		t.Fatalf("initial screen never showed content:\n%s", client.screenText())
	}

	compare := func(step string) {
		t.Helper()
		time.Sleep(300 * time.Millisecond)
		tmuxText := r.run("capture-pane", "-p", "-t", "s")
		vtermText := client.screenText()
		if strings.Join(trimLines(vtermText), "\n") != strings.Join(trimLines(tmuxText), "\n") {
			t.Errorf("%s: vterm diverged from tmux ground truth\n%s", step, diffScreens(vtermText, tmuxText))
		} else {
			t.Logf("%s: screens match", step)
		}
	}

	compare("initial")

	// Scroll back through history one wheel notch at a time.
	for i := 0; i < 15; i++ {
		client.wheelUp(50, 15)
		time.Sleep(60 * time.Millisecond)
	}
	compare("after 15 wheel-up events")

	// A burst of wheel events without settling time between them.
	for i := 0; i < 20; i++ {
		client.wheelUp(50, 15)
	}
	compare("after wheel burst")

	if t.Failed() {
		dump := client.rawDump()
		if len(dump) > 8000 {
			dump = dump[len(dump)-8000:]
		}
		t.Logf("raw client byte stream (tail, escaped):\n%q", dump)
	}
}
