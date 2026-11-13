package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestShouldKeepNewerReaderStateWhenStaleReaderStops(t *testing.T) {
	staleCancel := make(chan struct{})
	currentCancel := make(chan struct{})
	tab := &Tab{
		readerActive: true,
		readerCancel: currentCancel,
		ptyMsgCh:     make(chan tea.Msg, 1),
	}
	m := &Model{}

	m.markPTYReaderStopped(tab, staleCancel)
	if !tab.readerActive || tab.ptyMsgCh == nil {
		t.Fatalf("stale reader stop cleared the current reader state")
	}

	m.markPTYReaderStopped(tab, currentCancel)
	if tab.readerActive || tab.ptyMsgCh != nil {
		t.Fatalf("current reader stop did not clear its own state")
	}
}
