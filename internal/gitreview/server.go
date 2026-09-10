package gitreview

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"
)

//go:embed review.html highlight.min.js
var assets embed.FS

// maxBodyBytes caps a posted comment batch. Comments are typed by a person, so
// this is generous; the cap is only here so a malformed client cannot make the
// TUI's process read an unbounded body.
const maxBodyBytes = 1 << 20

// Server routes the review page and its API over a Service.
type Server struct {
	service *Service
}

// NewServer wraps a service in HTTP handlers.
func NewServer(service *Service) *Server { return &Server{service: service} }

// Handler returns the routed handler.
//
// Every route is scoped by session token, including the page itself: the token
// is the only thing separating one workspace's review from another's, and from
// anything else on the machine that might guess a URL.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /s/{id}/", s.handlePage)
	mux.HandleFunc("GET /s/{id}/highlight.js", s.handleHighlighter)
	mux.HandleFunc("GET /s/{id}/api/snapshot", s.handleSnapshot)
	mux.HandleFunc("GET /s/{id}/api/live", s.handleLive)
	mux.HandleFunc("POST /s/{id}/api/comments", s.handleComments)
	mux.HandleFunc("POST /s/{id}/api/reply", s.handleReply)
	mux.HandleFunc("POST /s/{id}/api/resolve", s.handleResolve)
	mux.HandleFunc("POST /s/{id}/api/delete", s.handleDelete)
	mux.HandleFunc("POST /s/{id}/api/viewed", s.handleViewed)
	mux.HandleFunc("GET /s/{id}/api/lines", s.handleLines)
	mux.HandleFunc("POST /s/{id}/api/autosend", s.handleAutoSend)
	mux.HandleFunc("POST /s/{id}/api/view", s.handleView)
	return mux
}

// session resolves the path's token, answering 404 for an unknown one.
//
// 404 rather than 403 on a bad token: a wrong token is indistinguishable from a
// review that has since been closed, and saying "exists but forbidden" tells a
// guesser that they found a real session.
func (s *Server) session(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	session, ok := s.service.session(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return nil, false
	}
	return session, true
}

// handlePage serves the single-page review view.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.session(w, r); !ok {
		return
	}
	page, err := assets.ReadFile("review.html")
	if err != nil {
		http.Error(w, "review page asset missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}

// snapshotResponse is the page's whole initial state, so a load or a reconnect
// takes one request rather than racing two.
type snapshotResponse struct {
	Snapshot Snapshot `json:"snapshot"`
	Threads  []Thread `json:"threads"`
	Viewed   []string `json:"viewed"`
	AutoSend bool     `json:"autoSend"`
	Split    bool     `json:"split"`
	Theme    Theme    `json:"theme"`
}

// handleHighlighter serves the vendored highlight.js.
//
// A separate request rather than an inline script: it is 127KB that never
// changes, so the browser caches it once and the page itself stays readable. It
// is still behind the session token, like every other route -- there is no
// reason for one review's URL to serve anything to a caller that cannot name it.
func (s *Server) handleHighlighter(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.session(w, r); !ok {
		return
	}
	script, err := assets.ReadFile("highlight.min.js")
	if err != nil {
		// The page checks for the global and falls back to plain text, so a
		// missing bundle costs colour and nothing else.
		http.Error(w, "highlighter asset missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600, immutable")
	_, _ = w.Write(script)
}

// handleSnapshot returns the current diff and threads, re-reading git when the
// page asks for a scope it is not already showing.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(w, r)
	if !ok {
		return
	}

	snap := session.Snapshot()
	if raw := r.URL.Query().Get("scope"); raw != "" {
		if scope := ParseScope(raw); scope != snap.Scope {
			snap = session.Refresh(scope)
			// Which scope the reader works in is a habit, not a per-review
			// decision, so the host remembers it for the next one.
			s.service.notePrefs(session)
		}
	}
	if r.URL.Query().Get("refresh") == "1" {
		snap = session.Refresh(snap.Scope)
	}
	writeJSON(w, snapshotResponse{
		Snapshot: snap,
		Threads:  session.Threads(),
		Viewed:   session.ViewedPaths(),
		AutoSend: session.AutoSend(),
		Split:    session.Split(),
		Theme:    session.Theme(),
	})
}

// handleLive streams snapshot and thread updates to an open page.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	// Chunked, unbuffered: an SSE stream that a proxy or the runtime buffers
	// delivers nothing until it closes, which is exactly never.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, unsubscribe := session.subscribe()
	defer unsubscribe()

	// Send the current state immediately. Subscribing is what makes the poller
	// start refreshing this session, so without an opening event a page that
	// connects to an idle session shows nothing until something changes.
	//
	// Through withState, like every other event. Built by hand this one carried
	// an empty viewed list and autoSend false, and since the page cannot tell an
	// absent field from a false one, connecting wiped the read ticks and the send
	// mode off a page that had just loaded them correctly.
	snap := session.Snapshot()
	if !writeEvent(w, flusher, session.withState(Event{Type: EventSnapshot, Snapshot: &snap})) {
		return
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, open := <-events:
			if !open {
				return
			}
			if !writeEvent(w, flusher, ev) {
				return
			}
		}
	}
}

// writeEvent serializes one SSE frame, reporting whether the client is still
// there.
func writeEvent(w http.ResponseWriter, flusher http.Flusher, ev Event) bool {
	payload, err := json.Marshal(ev)
	if err != nil {
		return true // a frame we cannot encode is not a dead client
	}
	// SSE frames are newline-delimited, so an embedded newline would split one
	// frame into two malformed ones. JSON encoding escapes them already, which
	// is why the payload is marshalled rather than streamed.
	if _, err := w.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// writeJSON writes a value as the response body.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

// decodeBody reads a JSON request body under the size cap.
func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(into); err != nil {
		http.Error(w, "malformed request body", http.StatusBadRequest)
		return false
	}
	return true
}

// trimBody normalizes a posted message: trailing whitespace carries no meaning
// and an all-whitespace body is the same as none.
func trimBody(s string) string { return strings.TrimSpace(s) }
