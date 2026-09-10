package gitreview

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// pollInterval is how often an open review page re-reads the repository.
//
// The page has to repaint as the agent edits, and an agent's edits arrive with
// no signal this package can see, so polling is the only option. It runs only
// while a page is actually open (see Session.watched), which is what keeps a
// review left open in a background tab from running git forever.
const pollInterval = 2 * time.Second

// wakeBuffer bounds the queue of "this root changed" pokes from the host's file
// watcher. Overflow is dropped rather than blocked on: the poller's own tick
// catches anything missed, and the alternative is stalling the UI thread that
// sent the poke.
const wakeBuffer = 16

// SendRequest asks the host to paste a composed review into an agent tab.
//
// Delivery is the host's job, not this package's: writing to an agent's PTY
// belongs to whatever owns the tabs, and the review server is an HTTP handler
// running on its own goroutine. The host reports back through
// Service.ConfirmSend, which is what turns the page's "sending" into "sent".
type SendRequest struct {
	SessionID    string
	AgentSession string
	Text         string
	ThreadIDs    []string
}

// Preferences are the review settings that outlive one session: the reader ticks
// them once and every review after that opens the same way.
//
// They travel through the host rather than being persisted here. What "remember
// this" means is the host's business -- it owns the config file, and the review
// server has no idea one exists -- and the save has to happen on the host's own
// thread anyway.
type Preferences struct {
	Scope    Scope
	AutoSend bool
	// Split and Theme change nothing the server does. They are carried here only
	// because the page has nowhere durable of its own to keep them: each review
	// is served from a fresh ephemeral port, so anything the browser stored --
	// localStorage included -- would be lost on the next restart.
	Split bool
	Theme Theme
}

// Service serves every open review over one HTTP server.
//
// Like the skill-usage dashboard it listens on an ephemeral loopback port, so
// it cannot collide with anything and is not reachable off the machine. Each
// session's URL additionally carries a random token, because the agent is told
// that URL and anything else running locally must not be able to guess it and
// post replies as the agent.
type Service struct {
	mu       sync.Mutex
	sessions map[string]*Session
	byRoot   map[string]*Session
	url      string
	srv      *http.Server
	send     func(SendRequest)
	// prefs is called when a setting the host should remember changes. Like
	// send, it runs on an HTTP goroutine and must not block.
	prefs func(Preferences)
	stop  chan struct{}
	store *store
	// wake carries roots the host has noticed changing, so a commit or a stage
	// repaints immediately instead of waiting out a tick.
	wake chan string
}

// NewService describes the review server without starting anything.
//
// metadataRoot is where comments are persisted (~/.medusa/workspaces-metadata);
// an empty one disables persistence rather than failing. send is called from an
// HTTP goroutine and must not block.
func NewService(metadataRoot string, send func(SendRequest)) *Service {
	return &Service{
		sessions: make(map[string]*Session),
		byRoot:   make(map[string]*Session),
		send:     send,
		store:    &store{dir: metadataRoot},
		wake:     make(chan string, wakeBuffer),
	}
}

// OnPreferences registers the sink for settings the host remembers across
// sessions. It is set once, before serving.
func (s *Service) OnPreferences(fn func(Preferences)) {
	s.mu.Lock()
	s.prefs = fn
	s.mu.Unlock()
}

// notePrefs reports a session's current sticky settings to the host.
//
// It reads them off the session rather than taking them as arguments, so a
// caller that has just changed one cannot forget to send the other and quietly
// overwrite it with a stale value.
func (s *Service) notePrefs(session *Session) {
	s.mu.Lock()
	hook := s.prefs
	s.mu.Unlock()
	if hook == nil {
		return
	}
	hook(Preferences{
		Scope:    session.Scope(),
		AutoSend: session.AutoSend(),
		Split:    session.Split(),
		Theme:    session.Theme(),
	})
}

// FirePreferences reports a Preferences value to the host as if a page had
// changed one. It exists so a host can test its own wiring without standing up
// a browser, which is the only way the hook is otherwise reachable.
func (s *Service) FirePreferences(prefs Preferences) {
	s.mu.Lock()
	hook := s.prefs
	s.mu.Unlock()
	if hook != nil {
		hook(prefs)
	}
}

// OpenRequest describes the review to open.
//
// WorkspaceID is what comments are filed under, so they land beside the rest of
// that workspace's metadata; a review opened without one still persists, keyed
// by a hash of the root instead.
type OpenRequest struct {
	WorkspaceID  string
	Root         string
	Name         string
	AgentSession string
	Agent        AgentProfile
	// Scope and AutoSend are the host's remembered preferences. They seed a new
	// session; an existing one keeps what its open page is already showing,
	// which is the same value, since every change to either is reported back.
	Scope    Scope
	AutoSend bool
	Split    bool
	Theme    Theme
}

// Open returns the URL of a review page for a workspace, starting the server on
// first use.
//
// A workspace that already has a session gets the same one back, so reopening
// the page keeps the comments already written on it — and re-points it at
// whichever agent tab is current.
//
// It reads the repository, so callers must keep it off a UI thread.
func (s *Service) Open(req OpenRequest) (string, error) {
	if req.Root == "" {
		return "", fmt.Errorf("no workspace root to review")
	}
	if err := s.ensureServing(); err != nil {
		return "", err
	}

	s.mu.Lock()
	session, existing := s.byRoot[req.Root]
	if !existing {
		session = &Session{
			id:           newID("s"),
			root:         req.Root,
			workspace:    req.Name,
			workspaceID:  req.WorkspaceID,
			agentSession: req.AgentSession,
			agent:        req.Agent,
			scope:        req.Scope,
			autoSend:     req.AutoSend,
			split:        req.Split,
			theme:        req.Theme,
			threads:      []*Thread{},
			subs:         make(map[int]chan Event),
			store:        s.store,
		}
		// Comments written before the last restart are outstanding work, and the
		// viewed marks are how far the reader had got, so the session opens on
		// both rather than on an empty review.
		session.threads, session.viewed = s.store.load(req.WorkspaceID, req.Root)
		if session.threads == nil {
			session.threads = []*Thread{}
		}
		if session.viewed == nil {
			session.viewed = make(map[string]string)
		}
		s.sessions[session.id] = session
		s.byRoot[req.Root] = session
	}
	base := s.url
	s.mu.Unlock()

	session.Retarget(req.AgentSession, req.Agent)
	if !existing {
		session.Refresh(req.Scope)
	}
	return base + "/s/" + session.id + "/", nil
}

// ensureServing starts the listener and poller once.
func (s *Service) ensureServing() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.url != "" {
		return nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	srv := &http.Server{
		Handler: NewServer(s).Handler(),
		// No WriteTimeout: the live endpoint is a long-lived SSE stream, and a
		// write deadline would cut every open review page at the same interval.
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Serve owns the listener from here. A serve error after startup only means
	// reviews stopped working, which must not take the TUI down with it.
	go func() { _ = srv.Serve(ln) }()

	s.srv = srv
	s.url = "http://" + ln.Addr().String()
	s.stop = make(chan struct{})
	go s.poll(s.stop)
	return nil
}

// poll keeps open pages tracking the agent's work, from two signals.
//
// The tick is not redundant with the wake, and neither replaces the other.
// Medusa's file watcher watches a workspace's **.git directory**, so it fires on
// commits, staging, checkouts and ref updates -- and not at all when an agent
// simply edits a file in the working tree, which is the review page's main case.
// So the wake makes git operations instant, and only the tick sees plain edits.
// Removing the tick would leave a page frozen for as long as an agent works
// without committing.
//
// Both paths refresh on this one goroutine, so a burst of pokes can never fan
// out into concurrent git runs over the same repository.
func (s *Service) poll(stop <-chan struct{}) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case root := <-s.wake:
			if session, ok := s.sessionByRoot(root); ok && session.watched() {
				session.Refresh(session.Scope())
			}
		case <-ticker.C:
			for _, session := range s.watchedSessions() {
				session.Refresh(session.Scope())
			}
		}
	}
}

// NotifyRootChanged tells the review server that a repository changed under it.
//
// It is safe to call from the UI thread: the send is non-blocking and the actual
// git work happens on the poll goroutine. A root with no open review, or one
// whose page is closed, costs nothing.
func (s *Service) NotifyRootChanged(root string) {
	if root == "" {
		return
	}
	s.mu.Lock()
	wake, serving := s.wake, s.url != ""
	s.mu.Unlock()
	if !serving || wake == nil {
		return
	}
	select {
	case wake <- root:
	default:
	}
}

// sessionByRoot looks up the review open on a repository.
func (s *Service) sessionByRoot(root string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.byRoot[root]
	return session, ok
}

// watchedSessions snapshots the sessions with a page open, so the poll loop does
// not hold the service lock across git calls.
func (s *Service) watchedSessions() []*Session {
	s.mu.Lock()
	all := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		all = append(all, session)
	}
	s.mu.Unlock()

	var watched []*Session
	for _, session := range all {
		if session.watched() {
			watched = append(watched, session)
		}
	}
	return watched
}

// session looks up a session by its URL token.
func (s *Service) session(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	return session, ok
}

// replyURL is the endpoint the agent is told to POST its answers to.
func (s *Service) replyURL(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.url + "/s/" + id + "/api/reply"
}

// ConfirmSend reports the outcome of a SendRequest back to the page. An empty
// errMsg marks the threads sent; anything else surfaces on the page and leaves
// them pending so the user can retry.
func (s *Service) ConfirmSend(sessionID string, threadIDs []string, errMsg string) {
	session, ok := s.session(sessionID)
	if !ok {
		return
	}
	session.MarkSent(threadIDs, errMsg)
}

// Notify pushes a plain notice onto a session's open pages.
func (s *Service) Notify(sessionID, notice string) {
	if session, ok := s.session(sessionID); ok {
		session.broadcast(session.withState(Event{Type: EventNotice, Notice: notice}))
	}
}

// Close stops the server if it was ever started. A Service that was never used
// is a no-op, so hosts can call this unconditionally on shutdown.
func (s *Service) Close() error {
	s.mu.Lock()
	srv, stop := s.srv, s.stop
	s.srv, s.url, s.stop = nil, "", nil
	s.sessions = make(map[string]*Session)
	s.byRoot = make(map[string]*Session)
	s.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// Sessions returns every open review session. It exists for the host's tests:
// the sessions are otherwise reachable only through a URL token the caller does
// not hold.
func (s *Service) Sessions() []*Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		out = append(out, session)
	}
	return out
}
