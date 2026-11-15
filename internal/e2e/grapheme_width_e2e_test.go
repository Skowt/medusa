package e2e

// tmux positions the cursor from its own idea of how wide each cell is, and a
// center tab's vterm draws what the tmux client is sent. Where the two
// disagree on a grapheme's width, the rest of the row lands in the wrong
// columns until tmux next repositions the cursor: an emoji with a skin tone,
// a ZWJ family or a flag in an agent's output shifted everything after it.
// tmux is the oracle here, asked directly, so a tmux upgrade that changes its
// rules fails this test rather than silently corrupting rows. The vterm
// mirrors tmux 3.6, so older releases are skipped rather than failed.

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/vterm"
)

func TestGraphemeWidthsMatchTmux(t *testing.T) {
	requireTmuxAtLeast(t, 3, 6)

	cases := []string{
		"✻ ⏺ ⎿ ✳ ·", // Claude's own glyphs
		"你好",        // East Asian wide
		"🤖",         // wide emoji
		"👍🏽",        // skin tone modifier
		"👨‍👩‍👧",     // ZWJ family
		"🏳️‍🌈",      // VS16 + ZWJ
		"⚠️",        // VS16 widens a narrow base
		"1️⃣",       // keycap
		"🇬🇧",        // flag
		"🇬🇧🇩🇪",      // two flags (tmux joins them into one cell)
		"é",        // combining accent
		"a🏽",        // modifier after ASCII stands alone
		"⏺🏽",        // modifier after a BMP symbol stands alone
	}

	socket := fmt.Sprintf("medusa-width-%d", os.Getpid())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	// Each row is "<case>|<column tmux put the bar in>". tmux is asked for its
	// cursor after the case is printed, so the number is tmux's own answer.
	var script strings.Builder
	for i, c := range cases {
		fmt.Fprintf(&script, "printf '%%s' %s; sleep 0.05; printf '|%%s\\n' \"$(tmux -L %s display -p '#{cursor_x}')\"; ",
			shellQuote(fmt.Sprintf("%02d ", i)+c), socket)
	}
	script.WriteString("echo DONE; sleep 60")

	opts := tmux.DefaultOptions()
	opts.ServerName = socket
	client := attachClientCommand(t, tmux.ClientCommandWithOptions("s", "/tmp", script.String(), opts), 60, len(cases)+4)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(client.screenText(), "DONE") {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)

	client.mu.Lock()
	defer client.mu.Unlock()
	screen := client.term.VisibleScreen()
	for i, c := range cases {
		row := findCaseRow(screen, fmt.Sprintf("%02d ", i))
		if row == nil {
			t.Errorf("case %q: row not found on screen", c)
			continue
		}
		bar, want := barColumn(row)
		if bar < 0 || want < 0 {
			t.Errorf("case %q: no bar/column on row %q", c, rowText(row))
			continue
		}
		if bar != want {
			t.Errorf("case %q: vterm put the bar at column %d, tmux at %d", c, bar, want)
		}
	}
}

func findCaseRow(screen [][]vterm.Cell, prefix string) []vterm.Cell {
	for _, row := range screen {
		if strings.HasPrefix(rowText(row), prefix) {
			return row
		}
	}
	return nil
}

func rowText(row []vterm.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		if cell.Width == 0 {
			continue
		}
		if cell.GraphemeCluster != "" {
			b.WriteString(cell.GraphemeCluster)
		} else {
			b.WriteRune(vterm.RenderableRune(cell.Rune))
		}
	}
	return b.String()
}

// barColumn returns the column of the last '|' on the row and the number
// printed after it.
func barColumn(row []vterm.Cell) (col, want int) {
	col = -1
	for x, cell := range row {
		if cell.Rune == '|' && cell.GraphemeCluster == "" {
			col = x
		}
	}
	if col < 0 {
		return -1, -1
	}
	var digits strings.Builder
	for _, cell := range row[col+1:] {
		if cell.Rune < '0' || cell.Rune > '9' {
			break
		}
		digits.WriteRune(cell.Rune)
	}
	n, err := strconv.Atoi(digits.String())
	if err != nil {
		return col, -1
	}
	return col, n
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
