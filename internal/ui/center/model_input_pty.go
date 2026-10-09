package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/perf"
	"github.com/Skowt/medusa/internal/ui/common"
)

// updatePTYOutput handles PTYOutput. From the PTY reader it is only a signal --
// the bytes are already in tab.output. Data is accepted for direct senders.
func (m *Model) updatePTYOutput(msg PTYOutput) tea.Cmd {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil || tab.isClosed() {
		return nil
	}
	tab.output.ClearSignal()
	if len(msg.Data) > 0 {
		m.tracePTYOutput(tab, msg.Data)
		tab.output.Append(msg.Data)
	}
	tab.lastOutputAt = time.Now()
	return m.scheduleFlush(msg.WorkspaceID, tab)
}

// scheduleFlush arms a PTYFlush for tab unless one is already pending.
func (m *Model) scheduleFlush(wsID string, tab *Tab) tea.Cmd {
	if tab.flushScheduled {
		return nil
	}
	tab.flushScheduled = true
	tab.flushPendingSince = time.Now()
	quiet, _ := m.flushTiming(tab, m.isActiveTab(wsID, tab.ID))
	tabID := tab.ID
	return common.SafeTick(quiet, func(t time.Time) tea.Msg {
		return PTYFlush{WorkspaceID: wsID, TabID: tabID}
	})
}

// updatePTYFlush handles PTYFlush.
func (m *Model) updatePTYFlush(msg PTYFlush) tea.Cmd {
	var cmds []tea.Cmd
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil && !tab.isClosed() {
		now := time.Now()
		quietFor := now.Sub(tab.lastOutputAt)
		pendingFor := time.Duration(0)
		if !tab.flushPendingSince.IsZero() {
			pendingFor = now.Sub(tab.flushPendingSince)
		}
		quiet, maxInterval := m.flushTiming(tab, m.isActiveTab(msg.WorkspaceID, msg.TabID))
		if quietFor < quiet && pendingFor < maxInterval {
			delay := quiet - quietFor
			if delay < time.Millisecond {
				delay = time.Millisecond
			}
			tabID := msg.TabID
			tab.flushScheduled = true
			cmds = append(cmds, common.SafeTick(delay, func(t time.Time) tea.Msg {
				return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID}
			}))
			return common.SafeBatch(cmds...)
		}

		tab.flushScheduled = false
		tab.flushPendingSince = time.Time{}
		tab.mu.Lock()
		hasTerminal := tab.Terminal != nil
		tab.mu.Unlock()
		if !hasTerminal {
			tab.output.Reset()
			return nil
		}
		retry := time.Millisecond
		if chunk := tab.output.Peek(ptyFlushChunkSize); len(chunk) > 0 {
			if m.deliverTabOutput(msg.WorkspaceID, tab, chunk) {
				tab.output.Consume(len(chunk))
			} else {
				// Earlier chunks are still queued; give the actor a moment.
				retry = ptyFlushQuiet
			}
		}
		if tab.output.Len() > 0 {
			tab.flushScheduled = true
			tab.flushPendingSince = time.Now()
			tabID := msg.TabID
			cmds = append(cmds, common.SafeTick(retry, func(t time.Time) tea.Msg {
				return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID}
			}))
		}
	}
	return common.SafeBatch(cmds...)
}

// deliverTabOutput parses chunk, or hands it to the tab actor to parse, and
// reports false when it must wait. Chunks must reach the vterm in the order
// they were read, so a chunk is never parsed here while earlier ones sit in
// the actor's queue, and never parsed here because that queue is full.
func (m *Model) deliverTabOutput(wsID string, tab *Tab, chunk []byte) bool {
	if m.isTabActorReady() {
		tab.outputInFlight.Add(1)
		ev := tabEvent{
			tab:         tab,
			workspaceID: wsID,
			tabID:       tab.ID,
			kind:        tabEventWriteOutput,
			output:      chunk,
		}
		if m.sendTabEvent(ev) {
			return true
		}
		tab.outputInFlight.Add(-1)
		return false
	}
	if tab.outputInFlight.Load() > 0 {
		return false
	}
	writeTabOutput(tab, chunk)
	return true
}

// updatePTYStopped handles PTYStopped.
func (m *Model) updatePTYStopped(msg PTYStopped) tea.Cmd {
	var cmds []tea.Cmd
	// Terminal closed - mark tab as not running, but keep it visible
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil {
		termAlive := tab.Agent != nil && tab.Agent.Terminal != nil && !tab.Agent.Terminal.IsClosed()
		m.stopPTYReader(tab)
		if termAlive {
			shouldRestart := true
			var backoff time.Duration
			tab.mu.Lock()
			if tab.ptyRestartSince.IsZero() || time.Since(tab.ptyRestartSince) > ptyRestartWindow {
				tab.ptyRestartSince = time.Now()
				tab.ptyRestartCount = 0
			}
			tab.ptyRestartCount++
			if tab.ptyRestartCount > ptyRestartMax {
				shouldRestart = false
				tab.Running = false
				// Mark as detached (tmux session may still be alive) and auto-reattach
				tab.Detached = true
				tab.ptyRestartBackoff = 0
			} else {
				backoff = tab.ptyRestartBackoff
				if backoff <= 0 {
					backoff = 200 * time.Millisecond
				} else {
					backoff *= 2
					if backoff > 5*time.Second {
						backoff = 5 * time.Second
					}
				}
				tab.ptyRestartBackoff = backoff
			}
			tab.mu.Unlock()
			if shouldRestart {
				tabID := msg.TabID
				wtID := msg.WorkspaceID
				cmds = append(cmds, common.SafeTick(backoff, func(time.Time) tea.Msg {
					return PTYRestart{WorkspaceID: wtID, TabID: tabID}
				}))
				logging.Warn("PTY stopped for tab %s; restarting in %s: %v", msg.TabID, backoff, msg.Err)
			} else {
				logging.Warn("PTY stopped for tab %s; restart limit reached, auto-reattaching: %v", msg.TabID, msg.Err)
				cmds = append(cmds, func() tea.Msg {
					return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
				})
				// Auto-reattach the detached tab
				if cmd := m.ReattachTabByID(msg.WorkspaceID, msg.TabID); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		} else {
			tab.mu.Lock()
			tab.Running = false
			// Mark as detached - tmux session may still be alive, auto-reattach
			tab.Detached = true
			tab.ptyRestartBackoff = 0
			tab.ptyRestartCount = 0
			tab.ptyRestartSince = time.Time{}
			tab.mu.Unlock()
			logging.Info("PTY stopped for tab %s, auto-reattaching: %v", msg.TabID, msg.Err)
			cmds = append(cmds, func() tea.Msg {
				return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
			})
			// Auto-reattach the detached tab
			if cmd := m.ReattachTabByID(msg.WorkspaceID, msg.TabID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return common.SafeBatch(cmds...)
}

// updatePTYRestart handles PTYRestart.
func (m *Model) updatePTYRestart(msg PTYRestart) tea.Cmd {
	var cmds []tea.Cmd
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return nil
	}
	if tab.Agent == nil || tab.Agent.Terminal == nil || tab.Agent.Terminal.IsClosed() {
		tab.mu.Lock()
		tab.ptyRestartBackoff = 0
		tab.mu.Unlock()
		return nil
	}
	if cmd := m.startPTYReader(msg.WorkspaceID, tab); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return common.SafeBatch(cmds...)
}

// updateSelectionScrollTick handles selectionScrollTick.
func (m *Model) updateSelectionScrollTick(msg selectionScrollTick) tea.Cmd {
	var cmds []tea.Cmd
	if m.isTabActorReady() {
		tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
		if tab == nil {
			return nil
		}
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: msg.WorkspaceID,
			tabID:       msg.TabID,
			kind:        tabEventSelectionScrollTick,
			gen:         msg.Gen,
		}) {
			return nil
		}
	}
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return nil
	}
	tab.mu.Lock()
	if !tab.Selection.Active || tab.selectionGen != msg.Gen || tab.Terminal == nil || tab.selectionScrollDir == 0 || !tab.selectionScrollActive {
		tab.selectionScrollActive = false
		tab.mu.Unlock()
		return nil
	}
	// Nudge selection to keep scrollback advancing while dragging.
	if !tab.FrameRendering {
		tab.Terminal.ScrollView(tab.selectionScrollDir)
	}
	tab.monitorDirty = true
	tab.mu.Unlock()
	tabID := msg.TabID
	wtID := msg.WorkspaceID
	cmds = append(cmds, common.SafeTick(100*time.Millisecond, func(time.Time) tea.Msg {
		return selectionScrollTick{WorkspaceID: wtID, TabID: tabID, Gen: msg.Gen}
	}))
	return common.SafeBatch(cmds...)
}

// writeTabOutput parses chunk into the tab's virtual terminal. Both the tab
// actor and the flush path that runs when the actor is unavailable go through
// it, so terminal writes have one shape.
func writeTabOutput(tab *Tab, chunk []byte) {
	if tab == nil || len(chunk) == 0 {
		return
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Terminal == nil {
		return
	}
	flushDone := perf.Time("pty_flush")
	tab.Terminal.Write(chunk)
	flushDone()
	perf.Count("pty_flush_bytes", int64(len(chunk)))
	tab.monitorDirty = true
}
