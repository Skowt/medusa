package e2e

// Regression cover for OSC 8 hyperlinks. Claude Code prints links as shorthand
// text wrapped in an OSC 8 sequence carrying the real URL ("services/protos!1638"
// -> https://gitlab.cargo.one/...). Two things silently broke that end to end:
// tmux only forwards hyperlinks to a client whose terminal advertises the
// "hyperlinks" feature (medusa attaches with TERM=xterm-256color, which does
// not), and the vterm dropped every OSC sequence. With either half missing the
// terminal sees only the link text and guesses a URL from it, so CMD+click
// opened http://services/protos!1638 instead of the MR.
//
// This drives the production client command, so removing either half fails here.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/vterm"
)

const (
	hyperlinkURL  = "https://gitlab.cargo.one/services/protos/-/merge_requests/1638"
	hyperlinkText = "services/protos!1638"
)

func TestTmuxHyperlinkReachesVTermCell(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	socket := fmt.Sprintf("medusa-osc8-%d", os.Getpid())
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
	})

	const cols, rows = 100, 24

	// A pane that emits an OSC 8 hyperlink the way an agent does.
	inner := fmt.Sprintf(`printf '\033]8;;%s\033\\%s\033]8;;\033\\\n'; sleep 60`, hyperlinkURL, hyperlinkText)

	opts := tmux.DefaultOptions()
	opts.ServerName = socket

	// The real thing: create + configure + attach, exactly as medusa launches a
	// tab (pty.NewWithSize runs this command string under `sh -c`).
	client := attachClientCommand(t, tmux.ClientCommandWithOptions("s", "/tmp", inner, opts), cols, rows)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(client.screenText(), hyperlinkText) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if raw := client.rawDump(); !strings.Contains(raw, "\x1b]8;") {
		t.Fatal("tmux stripped the OSC 8 sequence: no hyperlink reached the client. " +
			"The client command must advertise the hyperlinks terminal-feature for " + tmux.ClientTerm)
	}

	uri := linkUnderText(client, hyperlinkText)
	if uri != hyperlinkURL {
		t.Fatalf("cell hyperlink = %q, want %q (link text must carry its real URL)", uri, hyperlinkURL)
	}
}

// attachClientCommand runs a full medusa tmux client command under a PTY with
// medusa's client TERM, feeding the bytes into a vterm as the PTY reader does.
func attachClientCommand(t *testing.T, command string, cols, rows int) *reproClient {
	t.Helper()

	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "TERM="+tmux.ClientTerm)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		t.Fatalf("start client command: %v", err)
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

// linkUnderText finds want on screen and returns the hyperlink target of its
// first cell.
func linkUnderText(c *reproClient, want string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, row := range c.term.VisibleScreen() {
		var b strings.Builder
		for _, cell := range row {
			b.WriteRune(vterm.RenderableRune(cell.Rune))
		}
		col := strings.Index(b.String(), want)
		if col < 0 {
			continue
		}
		uri, _ := c.term.LinkTarget(row[col].Link)
		return uri
	}
	return ""
}
