package center

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/vterm"
)

// focusRecorder stands in for a tab's tmux client: a PTY whose reader writes
// everything it is sent to a file.
type focusRecorder struct {
	tab  *Tab
	file string
}

func newFocusRecorder(t *testing.T, id string, ws *data.Workspace) *focusRecorder {
	t.Helper()
	file := filepath.Join(t.TempDir(), id)
	term, err := appPty.NewWithSize("stty raw -echo; exec cat > "+file, t.TempDir(), nil, 24, 80)
	if err != nil {
		t.Fatalf("start recorder: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })
	// Let stty run before anything is written, or the line discipline
	// would still be cooked.
	time.Sleep(150 * time.Millisecond)
	return &focusRecorder{
		tab: &Tab{
			ID:        TabID(id),
			Workspace: ws,
			Terminal:  vterm.New(80, 24),
			Agent:     &appPty.Agent{Terminal: term},
			Running:   true,
		},
		file: file,
	}
}

// enableFocusReporting is what tmux does to its client at attach.
func (r *focusRecorder) enableFocusReporting() {
	r.tab.Terminal.Write([]byte("\x1b[?1004h"))
}

func (r *focusRecorder) received(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		got, _ = os.ReadFile(r.file)
		if string(got) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("tab %s received %q, want %q", r.tab.ID, got, want)
}

func TestSyncAgentFocusReportsOnlyTheVisibleTabAsFocused(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	ws := data.NewWorkspace("ws", "", "", "/tmp/repo", "/tmp/repo")
	m.SetWorkspace(ws)
	a := newFocusRecorder(t, "a", ws)
	b := newFocusRecorder(t, "b", ws)
	wsID := string(ws.ID())
	m.tabsByWorkspace[wsID] = []*Tab{a.tab, b.tab}
	m.activeTabByWorkspace[wsID] = 0

	// Before the client enables focus reporting, the PTY may still belong to
	// the shell running the client command: nothing may be written.
	m.SyncAgentFocus(true)
	a.received(t, "")
	b.received(t, "")

	a.enableFocusReporting()
	b.enableFocusReporting()
	m.SyncAgentFocus(true)
	a.received(t, focusInReport)
	b.received(t, focusOutReport)

	// Steady state sends nothing more.
	m.SyncAgentFocus(true)
	a.received(t, focusInReport)
	b.received(t, focusOutReport)

	// Switching tabs moves focus.
	m.setActiveTabIdx(1)
	m.SyncAgentFocus(true)
	a.received(t, focusInReport+focusOutReport)
	b.received(t, focusOutReport+focusInReport)

	// Losing terminal focus unfocuses the visible tab too.
	m.SyncAgentFocus(false)
	b.received(t, focusOutReport+focusInReport+focusOutReport)

	// Monitor mode counts every agent as unfocused.
	m.SyncAgentFocus(true)
	b.received(t, focusOutReport+focusInReport+focusOutReport+focusInReport)
	m.monitorMode = true
	m.SyncAgentFocus(true)
	b.received(t, focusOutReport+focusInReport+focusOutReport+focusInReport+focusOutReport)
	if got, _ := os.ReadFile(a.file); strings.Count(string(got), "\x1b[") != 2 {
		t.Fatalf("an already-unfocused tab was re-sent reports: %q", got)
	}
}
