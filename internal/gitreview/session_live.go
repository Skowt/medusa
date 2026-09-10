package gitreview

// subscribe registers a live client and returns its channel plus an unsubscribe.
//
// The channel is buffered because a broadcast must never block on a slow
// client: the poller and every HTTP handler broadcast, and one browser tab that
// has stopped reading would otherwise stall the whole session — including the
// submit path, which the user is waiting on.
func (s *Session) subscribe() (<-chan Event, func()) {
	ch := make(chan Event, liveBuffer)

	s.mu.Lock()
	if s.subs == nil {
		s.subs = make(map[int]chan Event)
	}
	s.nextSub++
	id := s.nextSub
	s.subs[id] = ch
	s.watchers++
	s.mu.Unlock()

	return ch, func() {
		s.mu.Lock()
		if existing, ok := s.subs[id]; ok {
			delete(s.subs, id)
			s.watchers--
			close(existing)
		}
		s.mu.Unlock()
	}
}

// liveBuffer is how many events a client may fall behind by.
//
// Events are whole snapshots, and a client that is behind only needs the latest
// one, so the buffer exists to absorb a burst rather than to queue history —
// hence a handful of slots, and a drop rather than a block when they fill.
const liveBuffer = 8

// broadcast pushes an event to every live client, skipping any that is full.
//
// Dropping is safe because every event carries the complete state it describes:
// a client that misses one is corrected by the next, and the poller guarantees
// there is a next.
//
// The lock is held across the sends, not just while the subscriber list is
// copied. Sending on a closed channel is a panic, and unsubscribe closes: with
// the list copied and the lock released, a page closing while the poller
// broadcast took down the whole TUI process. Holding it is safe precisely
// because every send is non-blocking, so the critical section is bounded by the
// number of open pages and never by how fast any of them reads.
//
// Callers must therefore not hold the lock, and must resolve arguments like
// Threads() before calling.
func (s *Session) broadcast(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// watched reports whether any page is open on this session.
func (s *Session) watched() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.watchers > 0
}
