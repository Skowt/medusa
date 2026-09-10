package gitreview

import (
	"sync"
	"time"
)

// Session is one workspace's open review: the diff it is showing, the comment
// threads on it, and the live clients watching both.
type Session struct {
	// id is both the session's name and its URL token, so it must stay secret
	// enough that another local process cannot guess it and post as the agent.
	id           string
	root         string
	workspace    string
	workspaceID  string
	agentSession string
	agent        AgentProfile
	// store persists the threads. A nil store disables persistence, which is
	// what a Service built without a metadata dir gets.
	store *store

	mu       sync.Mutex
	scope    Scope
	snapshot Snapshot
	threads  []*Thread
	// viewed maps a path to the digest of the diff that was on screen when the
	// reader ticked it. See session_viewed.go for why it is not a bool.
	viewed map[string]string
	// autoSend sends each comment as it is written; protocolSentTo is the agent
	// session that has already been given the reply instructions. Both live in
	// session_send_mode.go.
	autoSend       bool
	protocolSentTo string
	// split and theme are the page's own appearance. They are remembered here
	// only so the host can persist them; nothing on this side reads either.
	split   bool
	theme   Theme
	subs    map[int]chan Event
	nextSub int
	// watchers counts live clients. The poller only re-reads git while at
	// least one page is open: a review left open in a background browser tab
	// must not keep running git over the repo forever.
	watchers int
}

// Event is one live update pushed to an open page.
//
// Viewed has no `omitempty`: "no files are viewed" is a real state the page has
// to render, and with the field able to vanish it would be indistinguishable
// from "this event says nothing about viewed marks". Every event is built
// through Session.withState for the same reason.
type Event struct {
	Type     string    `json:"type"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Threads  []Thread  `json:"threads,omitempty"`
	Viewed   []string  `json:"viewed"`
	AutoSend bool      `json:"autoSend"`
	Split    bool      `json:"split"`
	Theme    Theme     `json:"theme"`
	Notice   string    `json:"notice,omitempty"`
}

// withState fills in the parts of the page's state that every event carries, so
// an event about one of them can never blank another.
//
// It must be called before broadcast, never inside it: broadcast holds the
// session lock and both getters take it.
func (s *Session) withState(ev Event) Event {
	ev.Threads = s.Threads()
	ev.Viewed = s.ViewedPaths()
	ev.AutoSend = s.AutoSend()
	ev.Split = s.Split()
	ev.Theme = s.Theme()
	return ev
}

// Event types.
const (
	EventSnapshot = "snapshot"
	EventThreads  = "threads"
	EventViewed   = "viewed"
	EventMode     = "mode"
	EventNotice   = "notice"
)

// ID returns the session's URL token.
func (s *Session) ID() string { return s.id }

// Root returns the repository the review covers.
func (s *Session) Root() string { return s.root }

// AgentSession returns the tmux session of the agent a submitted review goes to.
func (s *Session) AgentSession() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentSession
}

// Retarget points the session at a different agent tab.
//
// The user can close or replace the agent tab while a review page is open, and
// a review belongs to whichever agent is now working the workspace — sending it
// to a session name that no longer exists silently drops it. The profile moves
// with it: a review reopened on a Codex tab where it was last opened on a Claude
// one must not leave the page naming the wrong agent, or promising replies that
// agent cannot send.
func (s *Session) Retarget(agentSession string, agent AgentProfile) {
	s.mu.Lock()
	if agentSession != "" {
		s.agentSession = agentSession
	}
	if agent.Label != "" {
		s.agent = agent
	}
	s.mu.Unlock()
}

// Agent returns the profile of the agent this review is aimed at.
func (s *Session) Agent() AgentProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agent
}

// Snapshot returns the last diff read, refreshing it if none has been read yet.
func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	snap, scope := s.snapshot, s.scope
	s.mu.Unlock()
	if snap.Digest == "" && snap.Error == "" {
		return s.Refresh(scope)
	}
	return snap
}

// Scope returns the session's current scope.
func (s *Session) Scope() Scope {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scope
}

// Refresh re-reads the repository and pushes the result to live clients if the
// diff moved. It runs git, so callers must keep it off a UI thread.
func (s *Session) Refresh(scope Scope) Snapshot {
	s.mu.Lock()
	previous := s.snapshot.Digest
	sameScope := scope == s.scope
	root, workspace := s.root, s.workspace
	s.mu.Unlock()

	snap := Build(root, workspace, scope)
	snap.Agent = s.Agent()

	s.mu.Lock()
	s.snapshot, s.scope = snap, scope
	s.mu.Unlock()

	// A scope change always repaints: the digest covers the diff's content, so
	// switching between two scopes that happen to produce the same diff would
	// otherwise leave the page labelled with the scope it is no longer showing.
	if snap.Digest != previous || !sameScope {
		s.broadcast(s.withState(Event{Type: EventSnapshot, Snapshot: &snap}))
	}
	return snap
}

// Threads returns a copy of the thread list, newest anchor order preserved.
func (s *Session) Threads() []Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Thread, 0, len(s.threads))
	for _, t := range s.threads {
		out = append(out, *t)
	}
	return out
}

// AddComments opens a thread per comment and returns the ones that took.
//
// Comments arrive from a browser, so a malformed or empty one is dropped rather
// than rejected wholesale: one bad entry in a batch must not lose the others the
// user wrote.
func (s *Session) AddComments(comments []NewComment) []Thread {
	s.mu.Lock()
	defer s.mu.Unlock()

	var added []Thread
	for _, c := range comments {
		if !c.normalize() {
			continue
		}
		t := &Thread{
			ID:        newID("t"),
			Path:      c.Path,
			Side:      c.Side,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			Quote:     c.Quote,
			Body:      c.Body,
			Author:    AuthorUser,
			CreatedAt: time.Now(),
			Replies:   []Reply{},
		}
		s.threads = append(s.threads, t)
		added = append(added, *t)
	}
	if len(added) > 0 {
		s.persistLocked()
	}
	return added
}

// persistLocked writes the thread list. The caller must hold the lock: the list
// is copied out under it, so a concurrent mutation cannot be half-written.
//
// Saving is synchronous, but never on the UI thread -- every caller is an HTTP
// handler or the poller -- and the file is small enough that a human-paced
// review never notices it.
func (s *Session) persistLocked() {
	if s.store == nil {
		return
	}
	snapshot := make([]*Thread, len(s.threads))
	copy(snapshot, s.threads)
	s.store.save(s.workspaceID, s.root, snapshot, s.viewedSnapshotLocked())
}

// AddReply appends a message to a thread, optionally resolving it, and reports
// whether the thread existed.
func (s *Session) AddReply(threadID, author, body string, resolve bool) (Thread, bool) {
	s.mu.Lock()
	t := s.findLocked(threadID)
	if t == nil {
		s.mu.Unlock()
		return Thread{}, false
	}
	if body != "" {
		t.Replies = append(t.Replies, Reply{
			ID:        newID("r"),
			Author:    author,
			Body:      body,
			CreatedAt: time.Now(),
			// An agent's reply is delivered by definition: it came from there.
			// A user's starts pending, like any other thing the agent has to be
			// told, and is what PendingReplies then reports.
			Sent: author == AuthorAgent,
		})
	}
	if resolve {
		t.Resolved = true
	}
	updated := *t
	s.persistLocked()
	s.mu.Unlock()

	s.broadcast(s.withState(Event{Type: EventThreads}))
	return updated, true
}

// SetResolved marks a thread resolved or reopens it.
func (s *Session) SetResolved(threadID string, resolved bool) bool {
	s.mu.Lock()
	t := s.findLocked(threadID)
	if t == nil {
		s.mu.Unlock()
		return false
	}
	t.Resolved = resolved
	s.persistLocked()
	s.mu.Unlock()

	s.broadcast(s.withState(Event{Type: EventThreads}))
	return true
}

// DeleteThread drops a thread that has not been sent.
//
// A sent thread stays: the agent has been told about it and may be acting on it,
// so removing it from the page would leave the user with no record of what the
// agent is doing.
func (s *Session) DeleteThread(threadID string) bool {
	s.mu.Lock()
	removed := false
	for i, t := range s.threads {
		if t.ID == threadID && !t.Sent {
			s.threads = append(s.threads[:i], s.threads[i+1:]...)
			removed = true
			break
		}
	}
	if removed {
		s.persistLocked()
	}
	s.mu.Unlock()

	if removed {
		s.broadcast(s.withState(Event{Type: EventThreads}))
	}
	return removed
}

// Pending returns the threads not yet sent to the agent, in the order written.
func (s *Session) Pending() []Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Thread
	for _, t := range s.threads {
		if !t.Sent {
			out = append(out, *t)
		}
	}
	return out
}

// PendingReplies returns the user's replies on threads the agent already has.
//
// Only on threads it *has*: a reply written under a comment that is itself still
// a draft rides along with that comment when it goes (see composeNotes), because
// announcing a reply to something the agent has never seen tells it about half a
// conversation.
func (s *Session) PendingReplies() []ReplyOn {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ReplyOn
	for _, t := range s.threads {
		if !t.Sent {
			continue
		}
		for _, r := range t.Replies {
			if r.Author == AuthorUser && !r.Sent {
				out = append(out, ReplyOn{Thread: *t, Reply: r})
			}
		}
	}
	return out
}

// HasPendingWork reports whether anything is waiting to be sent, of either kind.
func (s *Session) HasPendingWork() bool {
	return len(s.Pending()) > 0 || len(s.PendingReplies()) > 0
}

// MarkSent records that the agent was given these threads. A failed send is
// reported as a notice and leaves the threads pending, so the user can retry
// rather than losing what they wrote.
func (s *Session) MarkSent(ids []string, err string) {
	if err != "" {
		s.broadcast(s.withState(Event{Type: EventNotice, Notice: err}))
		return
	}
	now := time.Now()
	s.mu.Lock()
	// One id space covers both: thread and reply ids are minted from the same
	// generator with different prefixes, so a caller can hand back everything it
	// sent as one list and never has to say which kind each id was.
	for _, id := range ids {
		if t := s.findLocked(id); t != nil {
			t.Sent, t.SentAt = true, now
			continue
		}
		if r := s.findReplyLocked(id); r != nil {
			r.Sent, r.SentAt = true, now
		}
	}
	// The paste landed, so whatever it carried reached the agent. Recording it
	// here rather than at compose time is what keeps a failed send from turning
	// the retry into a follow-up to a message nobody received.
	s.markProtocolDeliveredLocked()
	s.persistLocked()
	s.mu.Unlock()
	s.broadcast(s.withState(Event{Type: EventThreads}))
}

func (s *Session) findLocked(id string) *Thread {
	for _, t := range s.threads {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// findReplyLocked finds a reply by id anywhere in the session.
//
// It returns a pointer into the thread's slice, so the caller writes through to
// the stored reply rather than to a copy. The caller must hold the lock.
func (s *Session) findReplyLocked(id string) *Reply {
	for _, t := range s.threads {
		for i := range t.Replies {
			if t.Replies[i].ID == id {
				return &t.Replies[i]
			}
		}
	}
	return nil
}
