package gitreview

// Auto-send: comments go to the agent the moment they are written, instead of
// queueing until the reader presses Submit.
//
// It is session state rather than something the page remembers, for one reason.
// The mode decides what happens to the reader's comments, so a page that quietly
// forgets it -- on a reload, or in a second tab -- would leave them queueing
// while the reader believed each one had gone. Held here, both tabs agree and a
// reload cannot lose it.
//
// It is deliberately *not* persisted to disk. Restarting Medusa returns the
// review to the safe default of off, so comments are never sent to an agent on
// the strength of a mode chosen in some earlier sitting.

// AutoSend reports whether comments are sent as they are written.
func (s *Session) AutoSend() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoSend
}

// SetAutoSend turns the mode on or off.
func (s *Session) SetAutoSend(on bool) {
	s.mu.Lock()
	s.autoSend = on
	s.mu.Unlock()
}

// Split reports whether the page is showing diffs side by side.
func (s *Session) Split() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.split
}

// SetSplit records the view the reader has chosen.
func (s *Session) SetSplit(on bool) {
	s.mu.Lock()
	s.split = on
	s.mu.Unlock()
}

// Theme returns the colour scheme the reader has chosen, or ThemeSystem.
func (s *Session) Theme() Theme {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.theme
}

// SetTheme records the colour scheme, normalising anything unrecognised to
// "follow the OS" rather than storing it.
func (s *Session) SetTheme(theme Theme) {
	s.mu.Lock()
	s.theme = ParseTheme(string(theme))
	s.mu.Unlock()
}

// needsProtocol reports whether the next composed review has to carry the reply
// instructions in full.
//
// The question is not "have we sent a review before" but "has *this* agent been
// told how to reply". A review page outlives the tab it was opened against: the
// reader can close the agent and open another, and Retarget points the session
// at it. An agent that never saw the curl cannot answer, and a follow-up that
// assumes it did fails silently -- so a changed agent session gets the full
// instructions again. Over-explaining once costs a few lines; under-explaining
// costs every reply the agent might have made.
func (s *Session) needsProtocol() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.protocolSentTo == "" || s.protocolSentTo != s.agentSession
}

// markProtocolDelivered records that the agent now has the reply instructions.
//
// It is called only from the success path of MarkSent, never at compose time: a
// review that failed to paste -- no agent tab open -- did not teach anybody
// anything, and recording it would make the retry a follow-up to a message the
// agent never received. The caller must hold the lock.
func (s *Session) markProtocolDeliveredLocked() {
	s.protocolSentTo = s.agentSession
}
