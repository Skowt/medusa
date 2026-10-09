package sidebar

import (
	"io"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/safego"
)

func runPTYReader(term *pty.Terminal, msgCh chan tea.Msg, cancel <-chan struct{}, wsID string, tabID string, heartbeat *int64) {
	defer func() {
		defer func() { _ = recover() }()
		close(msgCh)
	}()

	if term == nil {
		return
	}
	beat := func() {
		if heartbeat != nil {
			atomic.StoreInt64(heartbeat, time.Now().UnixNano())
		}
	}
	beat()

	dataCh := make(chan []byte, ptyReadQueueSize)
	errCh := make(chan error, 1)

	safego.Go("sidebar.pty_read_loop", func() {
		buf := make([]byte, ptyReadBufferSize)
		for {
			n, err := term.Read(buf)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				close(dataCh)
				return
			}
			if n == 0 {
				continue
			}
			beat()
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			select {
			case dataCh <- chunk:
			case <-cancel:
				return
			}
		}
	})

	ticker := time.NewTicker(ptyFrameInterval)
	defer ticker.Stop()

	var pending []byte
	var stoppedErr error

	for {
		select {
		case <-cancel:
			close(msgCh)
			return
		case err := <-errCh:
			beat()
			stoppedErr = err
		case data, ok := <-dataCh:
			beat()
			if !ok {
				if len(pending) > 0 {
					if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: tabID, Data: pending}) {
						close(msgCh)
						return
					}
				}
				if stoppedErr == nil {
					stoppedErr = io.EOF
				}
				sendPTYMsg(msgCh, cancel, messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: tabID, Err: stoppedErr})
				close(msgCh)
				return
			}
			pending = append(pending, data...)
			if len(pending) >= ptyMaxPendingBytes {
				if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: tabID, Data: pending}) {
					close(msgCh)
					return
				}
				pending = nil
			}
		case <-ticker.C:
			beat()
			if len(pending) > 0 {
				if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: tabID, Data: pending}) {
					close(msgCh)
					return
				}
				pending = nil
			}
			if stoppedErr != nil {
				sendPTYMsg(msgCh, cancel, messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: tabID, Err: stoppedErr})
				close(msgCh)
				return
			}
		}
	}
}

func sendPTYMsg(msgCh chan tea.Msg, cancel <-chan struct{}, msg tea.Msg) bool {
	if msgCh == nil {
		return false
	}
	select {
	case <-cancel:
		return false
	case msgCh <- msg:
		return true
	}
}

// forwardPTYMsgs moves a reader's output into ts.output and signals the UI
// thread, passing every other message on unchanged. Output from a reader whose
// stream has since been reset (gen) is dropped.
func (m *TerminalModel) forwardPTYMsgs(ts *TerminalState, gen uint64, msgCh <-chan tea.Msg) {
	for msg := range msgCh {
		if msg == nil {
			continue
		}
		out, ok := msg.(messages.SidebarPTYOutput)
		if !ok {
			if m.msgSink != nil {
				m.msgSink(msg)
			}
			continue
		}
		if ts.output.AppendFrom(gen, out.Data) && m.msgSink != nil {
			m.msgSink(messages.SidebarPTYOutput{WorkspaceID: out.WorkspaceID, TabID: out.TabID})
		}
	}
}
