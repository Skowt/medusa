package common

import "sync"

// PTYBuffer holds PTY output between the goroutine that reads it and the UI
// thread that parses it into a vterm.
//
// The bytes live here rather than in the message that announces them because
// that message travels through the app's external queue, which drops under
// load. A dropped chunk is never re-sent: tmux only repaints cells that change,
// so the vterm stays out of step with the pane -- blank rows, half-drawn lines
// -- until those cells happen to change again. The message is now only a
// signal, and losing one costs latency, not bytes.
type PTYBuffer struct {
	mu       sync.Mutex
	data     []byte
	gen      uint64
	signaled bool
}

// Generation identifies the stream a reader is appending to. A reader captures
// it at start, and Reset moves it on so a stopped reader's leftovers are
// discarded instead of landing in the next attach's stream.
func (b *PTYBuffer) Generation() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gen
}

// AppendFrom appends p if gen is still current. It reports whether the caller
// must signal the UI thread, which is true only for the first append since the
// last ClearSignal: one outstanding signal per stream is enough.
func (b *PTYBuffer) AppendFrom(gen uint64, p []byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if gen != b.gen || len(p) == 0 {
		return false
	}
	b.data = append(b.data, p...)
	if b.signaled {
		return false
	}
	b.signaled = true
	return true
}

// Append appends p to the current stream without touching the signal.
func (b *PTYBuffer) Append(p []byte) {
	b.mu.Lock()
	b.data = append(b.data, p...)
	b.mu.Unlock()
}

// ClearSignal records that the UI thread has taken the outstanding signal, so
// the next append signals again.
func (b *PTYBuffer) ClearSignal() {
	b.mu.Lock()
	b.signaled = false
	b.mu.Unlock()
}

// Peek returns a copy of up to max buffered bytes without consuming them, so a
// chunk that cannot be parsed yet stays at the front of the stream.
func (b *PTYBuffer) Peek(max int) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(b.data)
	if n > max {
		n = max
	}
	if n == 0 {
		return nil
	}
	return append([]byte(nil), b.data[:n]...)
}

// Consume drops the first n bytes, once they have been parsed or handed off.
func (b *PTYBuffer) Consume(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n >= len(b.data) {
		b.data = b.data[:0]
		return
	}
	copy(b.data, b.data[n:])
	b.data = b.data[:len(b.data)-n]
}

// Len reports how many bytes are waiting to be parsed.
func (b *PTYBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.data)
}

// Reset discards the buffered output and starts a new stream.
func (b *PTYBuffer) Reset() {
	b.mu.Lock()
	b.data = nil
	b.gen++
	b.signaled = false
	b.mu.Unlock()
}
