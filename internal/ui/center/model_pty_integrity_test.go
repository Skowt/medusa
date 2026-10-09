package center

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/vterm"
)

// These tests guard the byte stream between a tab's PTY and its vterm. tmux
// only repaints cells that change, so a chunk that is lost or parsed out of
// order is never corrected: the tab shows blank rows and half-drawn lines
// until those cells happen to change again.

func newIntegrityTab(t *testing.T) (*Model, *Tab, string) {
	t.Helper()
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{ID: "tab-1", Assistant: "codex", Workspace: ws, Running: true, Terminal: vterm.New(40, 4)}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	return m, tab, wsID
}

func firstRow(tab *Tab) string {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	var b strings.Builder
	for _, c := range tab.Terminal.Screen[0] {
		if c.Rune != 0 {
			b.WriteRune(c.Rune)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// drainFlushes runs PTYFlush until the tab has nothing left to parse.
func drainFlushes(t *testing.T, m *Model, tab *Tab, wsID string) {
	t.Helper()
	for i := 0; tab.output.Len() > 0; i++ {
		if i > 100 {
			t.Fatal("output never drained")
		}
		_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})
	}
}

// The app's external queue drops messages when it is full. Output must survive
// that: the bytes stay with the tab, and only the signal can be lost.
func TestPTYOutputSurvivesADroppingSink(t *testing.T) {
	m, tab, wsID := newIntegrityTab(t)
	var forwarded []tea.Msg
	m.msgSink = func(msg tea.Msg) {
		if _, ok := msg.(PTYOutput); ok {
			return // the queue is full: the message is gone
		}
		forwarded = append(forwarded, msg)
	}

	msgCh := make(chan tea.Msg, 8)
	for _, part := range []string{"hel", "lo ", "wor", "ld"} {
		msgCh <- PTYOutput{WorkspaceID: wsID, TabID: tab.ID, Data: []byte(part)}
	}
	msgCh <- PTYStopped{WorkspaceID: wsID, TabID: tab.ID}
	close(msgCh)
	m.forwardPTYMsgs(tab, tab.output.Generation(), msgCh)

	if len(forwarded) != 1 {
		t.Fatalf("non-output messages must still be forwarded, got %v", forwarded)
	}
	drainFlushes(t, m, tab, wsID)
	if got := firstRow(tab); got != "hello world" {
		t.Fatalf("row 0 = %q, want %q", got, "hello world")
	}
}

// A lost signal must not strand output: the watchdog picks it up.
func TestWatchdogFlushesOutputWhoseSignalWasLost(t *testing.T) {
	m, tab, wsID := newIntegrityTab(t)
	if !tab.output.AppendFrom(tab.output.Generation(), []byte("stranded")) {
		t.Fatal("expected the first append to signal")
	}
	// The signal was dropped, so nothing scheduled a flush.
	cmd := m.StartPTYReaders()
	if cmd == nil {
		t.Fatal("watchdog must schedule a flush for output nobody announced")
	}
	if !tab.flushScheduled {
		t.Fatal("watchdog must mark the flush scheduled")
	}
	if !tab.output.AppendFrom(tab.output.Generation(), []byte("!")) {
		t.Fatal("watchdog must clear the lost signal so new output signals again")
	}
	drainFlushes(t, m, tab, wsID)
	if got := firstRow(tab); got != "stranded!" {
		t.Fatalf("row 0 = %q, want %q", got, "stranded!")
	}
}

// queueEarlierChunk puts a chunk in the tab actor's queue, as an earlier flush
// would have.
func queueEarlierChunk(m *Model, tab *Tab, wsID, data string) {
	tab.outputInFlight.Add(1)
	m.tabEvents <- tabEvent{tab: tab, workspaceID: wsID, tabID: tab.ID, kind: tabEventWriteOutput, output: []byte(data)}
}

func runQueuedActorEvents(m *Model) {
	for {
		select {
		case ev := <-m.tabEvents:
			m.handleTabEvent(ev)
		default:
			return
		}
	}
}

// With the actor's queue full, the next chunk must wait its turn rather than be
// parsed inline ahead of the chunks still queued.
func TestFlushNeverOvertakesAFullActorQueue(t *testing.T) {
	m, tab, wsID := newIntegrityTab(t)
	m.tabEvents = make(chan tabEvent, 1)
	m.setTabActorReady()
	queueEarlierChunk(m, tab, wsID, "first ")
	tab.output.Append([]byte("second"))

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})
	if got := firstRow(tab); got != "" {
		t.Fatalf("a later chunk was parsed ahead of a queued one: row 0 = %q", got)
	}
	if tab.output.Len() == 0 {
		t.Fatal("a chunk the actor refused must stay buffered")
	}

	runQueuedActorEvents(m)
	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})
	runQueuedActorEvents(m)
	if got := firstRow(tab); got != "first second" {
		t.Fatalf("row 0 = %q, want %q", got, "first second")
	}
}

// The stall detector clears actor readiness while the actor may still hold
// queued chunks. Parsing inline then would put new bytes ahead of them.
func TestFlushWaitsForQueuedChunksWhenActorIsNotReady(t *testing.T) {
	m, tab, wsID := newIntegrityTab(t)
	queueEarlierChunk(m, tab, wsID, "first ")
	tab.output.Append([]byte("second"))

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})
	if got := firstRow(tab); got != "" {
		t.Fatalf("a later chunk was parsed ahead of a queued one: row 0 = %q", got)
	}

	runQueuedActorEvents(m)
	drainFlushes(t, m, tab, wsID)
	if got := firstRow(tab); got != "first second" {
		t.Fatalf("row 0 = %q, want %q", got, "first second")
	}
}
