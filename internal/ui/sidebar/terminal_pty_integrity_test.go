package sidebar

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/vterm"
)

// The app's external queue drops messages when it is full. Terminal output
// must survive that: the bytes stay with the terminal, and only the signal can
// be lost, which the watchdog then recovers.
func TestSidebarOutputSurvivesADroppingSink(t *testing.T) {
	wt := data.NewWorkspace("wt", "", "", "/repo", "/repo/wt")
	m := NewTerminalModel()
	m.workspace = wt
	wsID := string(wt.ID())
	ts := &TerminalState{VTerm: vterm.New(40, 3)}
	m.tabsByWorkspace[wsID] = []*TerminalTab{{ID: "tab-1", Name: "Terminal 1", State: ts}}
	m.activeTabByWorkspace[wsID] = 0
	m.msgSink = func(tea.Msg) {} // every message is dropped

	msgCh := make(chan tea.Msg, 4)
	for _, part := range []string{"lost ", "signal"} {
		msgCh <- messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: "tab-1", Data: []byte(part)}
	}
	close(msgCh)
	m.forwardPTYMsgs(ts, ts.output.Generation(), msgCh)

	if m.StartPTYReaders() == nil {
		t.Fatal("watchdog must schedule a flush for output nobody announced")
	}
	for i := 0; ts.output.Len() > 0; i++ {
		if i > 100 {
			t.Fatal("output never drained")
		}
		_, _ = m.Update(messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: "tab-1"})
	}

	var row strings.Builder
	for _, c := range ts.VTerm.Screen[0] {
		if c.Rune != 0 {
			row.WriteRune(c.Rune)
		}
	}
	if got := strings.TrimRight(row.String(), " "); got != "lost signal" {
		t.Fatalf("row 0 = %q, want %q", got, "lost signal")
	}
}
