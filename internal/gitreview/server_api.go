package gitreview

import (
	"net/http"
	"strconv"
	"strings"
)

// commentsRequest is a submitted batch of review comments.
type commentsRequest struct {
	Comments []NewComment `json:"comments"`
	// Submit distinguishes saving a draft from sending the review. The page
	// keeps unsent comments server-side so a browser reload does not lose them,
	// which means adding one cannot itself be the thing that pokes the agent.
	Submit bool `json:"submit"`
}

// commentsResponse tells the page what stuck and whether the agent was poked.
type commentsResponse struct {
	Threads []Thread `json:"threads"`
	Viewed  []string `json:"viewed"`
	Sent    []string `json:"sent,omitempty"`
	// Sending is true when the review was handed to the host for delivery. The
	// outcome arrives later over the live stream, because pasting into an
	// agent's PTY happens on the host's UI thread and cannot be awaited here.
	Sending bool   `json:"sending"`
	Error   string `json:"error,omitempty"`
}

// handleComments records comments and, on submit, hands the whole pending set
// to the host for delivery to the agent.
//
// Every pending thread is submitted, not just the ones in this request: the user
// may have written comments across several posts, and submitting only the last
// batch would silently drop the rest.
func (s *Server) handleComments(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(w, r)
	if !ok {
		return
	}

	var req commentsRequest
	if !decodeBody(w, r, &req) {
		return
	}

	added := session.AddComments(req.Comments)
	if len(added) > 0 {
		session.broadcast(session.withState(Event{Type: EventThreads}))
	}

	resp := commentsResponse{Threads: session.Threads(), Viewed: session.ViewedPaths()}
	// Auto-send is read from the session, not taken from the request. The mode
	// decides what happens to the reader's comments, and a page holding a stale
	// toggle -- a second tab, one left open across a change of mode -- would
	// otherwise queue a comment the review considers already sent.
	if !req.Submit && !session.AutoSend() {
		writeJSON(w, resp)
		return
	}

	s.submitPending(session, &resp)
	writeJSON(w, resp)
}

// submitPending hands everything unsent to the host, filling in the outcome.
//
// Everything pending goes, never just one request's comments: the reader may
// have written them across several posts, and submitting only the last batch
// would silently drop the rest. "Everything" includes replies on comments the
// agent already has -- a thread delivered an hour ago is no longer pending, so
// before they were tracked separately a reply written under one reached nobody
// in either mode.
func (s *Server) submitPending(session *Session, resp *commentsResponse) {
	pending := session.Pending()
	replies := session.PendingReplies()
	if len(pending) == 0 && len(replies) == 0 {
		resp.Error = "nothing to submit"
		return
	}

	var parts []string
	ids := make([]string, 0, len(pending)+len(replies))

	if len(pending) > 0 {
		if session.needsProtocol() {
			parts = append(parts, Compose(s.service.replyURL(session.ID()), session.Agent(), pending))
		} else {
			parts = append(parts, ComposeFollowUp(pending))
		}
		for _, t := range pending {
			ids = append(ids, t.ID)
			// A reply folded into an unsent comment's note goes with it, so it
			// must be marked delivered with it -- otherwise the next submit
			// announces it all over again as a fresh reply.
			for _, r := range t.Replies {
				if r.Author == AuthorUser && !r.Sent {
					ids = append(ids, r.ID)
				}
			}
		}
	}
	if len(replies) > 0 {
		parts = append(parts, ComposeReplies(replies))
		for _, r := range replies {
			ids = append(ids, r.Reply.ID)
		}
	}

	if s.service.send == nil {
		resp.Error = "no agent connected to send this review to"
		return
	}
	// One paste, not two: each send is a separate prompt to the agent, and
	// splitting new comments from replies would interrupt it twice for one
	// gesture -- and let it start on the first half before it had read the rest.
	s.service.send(SendRequest{
		SessionID:    session.ID(),
		AgentSession: session.AgentSession(),
		Text:         strings.Join(parts, "\n"),
		ThreadIDs:    ids,
	})

	resp.Sending = true
	resp.Sent = ids
}

// replyRequest is a message added under an existing thread. The agent posts
// these; so does the page when the user follows up on their own comment.
type replyRequest struct {
	Thread string `json:"thread"`
	Body   string `json:"body"`
	// Author defaults to the agent: this endpoint exists for the agent, and the
	// URL is what the composed review hands it. The page sends "user" explicitly.
	Author   string `json:"author"`
	Resolved bool   `json:"resolved"`
}

// handleReply appends a reply to a thread. This is the agent's way back onto the
// page — the composed review tells it this URL and the thread ids to use.
func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(w, r)
	if !ok {
		return
	}

	var req replyRequest
	if !decodeBody(w, r, &req) {
		return
	}

	author := AuthorAgent
	if req.Author == AuthorUser {
		author = AuthorUser
	}
	body := trimBody(req.Body)
	if body == "" && !req.Resolved {
		http.Error(w, "a reply needs a body, or resolved:true", http.StatusBadRequest)
		return
	}

	thread, found := session.AddReply(req.Thread, author, body, req.Resolved)
	if !found {
		// Name the ids that do exist: an agent that mistypes a thread id
		// otherwise has no way to recover except asking the user.
		writeJSON(w, map[string]any{
			"ok":      false,
			"error":   "no thread with id " + req.Thread,
			"threads": threadIDs(session.Threads()),
		})
		return
	}

	out := map[string]any{"ok": true, "thread": thread}
	// The reader's reply is one more thing the agent has to be told, so under
	// auto-send it goes now, exactly as a new comment would. Without this the
	// mode was only half true: new comments left immediately and replies queued
	// behind a submit bar the mode had already hidden.
	if author == AuthorUser && session.AutoSend() {
		var resp commentsResponse
		s.submitPending(session, &resp)
		out["sending"], out["sent"] = resp.Sending, resp.Sent
		if resp.Error != "" {
			out["error"] = resp.Error
		}
		out["threads"] = session.Threads()
	}
	writeJSON(w, out)
}

// resolveRequest toggles a thread's resolved flag.
type resolveRequest struct {
	Thread   string `json:"thread"`
	Resolved bool   `json:"resolved"`
}

// handleResolve marks a thread resolved or reopens it.
func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(w, r)
	if !ok {
		return
	}
	var req resolveRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !session.SetResolved(req.Thread, req.Resolved) {
		http.Error(w, "no such thread", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// deleteRequest drops an unsent draft comment.
type deleteRequest struct {
	Thread string `json:"thread"`
}

// handleDelete removes a draft comment the user has thought better of.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(w, r)
	if !ok {
		return
	}
	var req deleteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !session.DeleteThread(req.Thread) {
		http.Error(w, "no unsent thread with that id", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// viewedRequest ticks a file as read, or clears the tick.
type viewedRequest struct {
	Path   string `json:"path"`
	Viewed bool   `json:"viewed"`
}

// handleViewed records that the reader is done with a file.
//
// An unknown path is a 404 rather than a silent no-op: the page only ever sends
// paths it was given, so a miss means the diff moved under it and the tick it
// just drew is wrong. That check is also why nothing here needs a traversal
// guard -- the only paths that get through are ones the snapshot produced.
func (s *Server) handleViewed(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(w, r)
	if !ok {
		return
	}
	var req viewedRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !session.SetViewed(req.Path, req.Viewed) {
		http.Error(w, "no such file in this diff", http.StatusNotFound)
		return
	}
	// Broadcast, so a second tab showing the same review ticks too.
	session.broadcast(session.withState(Event{Type: EventViewed}))
	writeJSON(w, map[string]any{"ok": true, "viewed": session.ViewedPaths()})
}

// autoSendRequest turns "send comments as I write them" on or off.
type autoSendRequest struct {
	AutoSend bool `json:"autoSend"`
}

// handleAutoSend records the send mode, and flushes the queue on the way on.
//
// The flush is not a flourish: the page hides the submit bar while the mode is
// on, so a comment left queued would have nothing left to press and would sit
// there unreachable. Turning the mode on is a decision to stop queueing, so what
// is already queued goes.
//
// Turning it *off* sends nothing, obviously -- and it is what brings the bar,
// and any comment a failed flush left behind, back into reach.
func (s *Server) handleAutoSend(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(w, r)
	if !ok {
		return
	}
	var req autoSendRequest
	if !decodeBody(w, r, &req) {
		return
	}
	session.SetAutoSend(req.AutoSend)
	// Broadcast, so a second tab showing the same review agrees about the mode.
	session.broadcast(session.withState(Event{Type: EventMode}))
	// And tell the host, so the next review opens the same way.
	s.service.notePrefs(session)

	resp := commentsResponse{Threads: session.Threads(), Viewed: session.ViewedPaths()}
	if req.AutoSend && session.HasPendingWork() {
		s.submitPending(session, &resp)
		// "nothing to submit" cannot happen here -- there was pending work --
		// and a real failure is the page's to report, so it rides on as-is.
	}
	writeJSON(w, map[string]any{
		"ok":       true,
		"autoSend": session.AutoSend(),
		"threads":  resp.Threads,
		"sending":  resp.Sending,
		"sent":     resp.Sent,
		"error":    resp.Error,
	})
}

// viewRequest changes how the page looks: the diff layout, the colour scheme, or
// both.
//
// Both fields are pointers so a request can set one without saying anything
// about the other. Plain values would make every request an assertion about
// everything, and a page that had one of them stale -- a second tab, one left
// open across a change -- would quietly overwrite the other.
type viewRequest struct {
	Split *bool   `json:"split"`
	Theme *string `json:"theme"`
}

// handleView records the appearance the reader prefers.
//
// It is a separate endpoint from handleAutoSend because the two are not the same
// kind of thing: that one decides where comments go and flushes a queue, these
// change nothing but how the page is drawn. Sharing a handler would put a
// behavioural side effect behind a presentational switch.
func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(w, r)
	if !ok {
		return
	}
	var req viewRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Split != nil {
		session.SetSplit(*req.Split)
	}
	if req.Theme != nil {
		session.SetTheme(Theme(*req.Theme))
	}
	// Broadcast, so a second tab on the same review follows, and tell the host
	// so the next review opens looking the same.
	session.broadcast(session.withState(Event{Type: EventMode}))
	s.service.notePrefs(session)
	writeJSON(w, map[string]any{
		"ok":    true,
		"split": session.Split(),
		"theme": session.Theme(),
	})
}

// handleLines serves the unchanged lines between two hunks, so the reader can
// open up the context a diff leaves out.
//
// The path is checked against the snapshot for the same reason handleViewed
// checks it: this handler does open the file, so a path the diff never produced
// must not reach the filesystem at all. readFileLines re-checks lexically, since
// it is the one doing the opening.
func (s *Server) handleLines(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	path := q.Get("path")
	if !session.HasFile(path) {
		http.Error(w, "no such file in this diff", http.StatusNotFound)
		return
	}
	from, _ := strconv.Atoi(q.Get("from"))
	to, _ := strconv.Atoi(q.Get("to"))

	lines, err := readFileLines(session.Root(), path, from, to)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error(), "path": path})
		return
	}
	writeJSON(w, lines)
}

func threadIDs(threads []Thread) []string {
	out := make([]string, 0, len(threads))
	for _, t := range threads {
		out = append(out, t.ID)
	}
	return out
}
