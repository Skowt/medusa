package e2e

// Regression cover for SGR styling end to end: pane -> tmux -> medusa's
// client PTY -> vterm cell. Three things broke it silently, each a different
// half of the chain:
//
//   - tmux decides what to forward from what the client's terminal advertises.
//     Unless medusa declares RGB, usstyle and strikethrough itself, that came
//     from whatever terminfo the environment pointed at -- iTerm2's, through an
//     inherited TERMINFO_DIRS -- and without it tmux quantised truecolor to the
//     256-color cube and dropped styled underlines.
//   - The vterm flattened colon sub-parameters, so a curly underline ("4:3")
//     arrived as underline plus italic, and the empty fields in tmux's
//     underline-color sequence read as full resets that wiped the foreground.
//
// The environment is stripped of the outer terminal's identity so the test
// cannot pass on borrowed terminfo, which is the state CI and any non-iTerm2
// user are in.

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

func TestRichTextReachesVTermCells(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	socket := fmt.Sprintf("medusa-sgr-%d", os.Getpid())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	inner := strings.Join([]string{
		`printf 'TRUE \033[38;2;215;119;87mcolor\033[0m\n'`,
		`printf 'CURL \033[4:3mcurly\033[0m\n'`,
		`printf 'DASH \033[4:5mdashed\033[0m\n'`,
		`printf 'STRK \033[9mstruck\033[0m\n'`,
		`printf 'ULCO \033[31m\033[58;2;0;255;0m\033[4mcolored\033[0m\n'`,
		`printf 'PALT \033[94mlink\033[0m\n'`,
		`sleep 60`,
	}, "; ")

	opts := tmux.DefaultOptions()
	opts.ServerName = socket
	env := withoutOuterTerminal(os.Environ())
	client := attachClientCommandEnv(t, tmux.ClientCommandWithOptions("s", "/tmp", inner, opts), env, 80, 12)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(client.screenText(), "PALT") {
		time.Sleep(50 * time.Millisecond)
	}

	check := func(label string, want func(vterm.Style) bool, desc string) {
		t.Helper()
		style, ok := styleOfWordAfter(client, label)
		if !ok {
			t.Errorf("%s: row not on screen:\n%s", label, client.screenText())
			return
		}
		if !want(style) {
			t.Errorf("%s: %s, got %+v", label, desc, style)
		}
	}
	check("TRUE", func(s vterm.Style) bool { return s.Fg == vterm.Color{Type: vterm.ColorRGB, Value: 0xd77757} },
		"truecolor must arrive unquantised (tmux needs the RGB feature)")
	check("CURL", func(s vterm.Style) bool { return s.Underline == vterm.UnderlineCurly && !s.Italic },
		"must be a curly underline, not underline+italic")
	check("DASH", func(s vterm.Style) bool { return s.Underline == vterm.UnderlineDashed && !s.Blink },
		"must be a dashed underline, not underline+blink")
	check("STRK", func(s vterm.Style) bool { return s.Strike }, "must be struck through")
	check("ULCO", func(s vterm.Style) bool {
		return s.Fg == vterm.Color{Type: vterm.ColorIndexed, Value: 1} && s.Underline == vterm.UnderlineSingle
	}, "an underline color must not reset the red foreground")
	check("PALT", func(s vterm.Style) bool { return s.Fg == vterm.Color{Type: vterm.ColorIndexed, Value: 12} },
		"a palette color must stay a palette index")
}

// withoutOuterTerminal drops the variables through which the outer terminal
// identifies itself, so tmux learns nothing it was not told by medusa.
func withoutOuterTerminal(environ []string) []string {
	drop := map[string]bool{"COLORTERM": true, "TERMINFO": true, "TERMINFO_DIRS": true,
		"TERM_PROGRAM": true, "TERM_PROGRAM_VERSION": true, "LC_TERMINAL": true, "LC_TERMINAL_VERSION": true, "TMUX": true}
	var out []string
	for _, kv := range environ {
		key, _, _ := strings.Cut(kv, "=")
		if !drop[key] {
			out = append(out, kv)
		}
	}
	return out
}

// attachClientCommandEnv is attachClientCommand with an explicit environment.
func attachClientCommandEnv(t *testing.T, command string, environ []string, cols, rows int) *reproClient {
	t.Helper()
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(environ, "TERM="+tmux.ClientTerm)
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

// styleOfWordAfter returns the style of the first cell after "<label> ".
func styleOfWordAfter(c *reproClient, label string) (vterm.Style, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, row := range c.term.VisibleScreen() {
		if strings.HasPrefix(rowText(row), label+" ") {
			return row[len(label)+1].Style, true
		}
	}
	return vterm.Style{}, false
}
