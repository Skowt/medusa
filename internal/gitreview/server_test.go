package gitreview

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder captures the review a submit hands to the host.
type recorder struct {
	mu   sync.Mutex
	reqs []SendRequest
}

func (r *recorder) send(req SendRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req)
}

func (r *recorder) last() (SendRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reqs) == 0 {
		return SendRequest{}, false
	}
	return r.reqs[len(r.reqs)-1], true
}

// harness starts a real HTTP server over a real repo, which is the only way to
// exercise the routes, the SSE stream and the token check together.
func harness(t *testing.T) (*httptest.Server, *Service, *Session, *recorder) {
	t.Helper()
	skipIfNoGit(t)
	root := branchRepo(t)

	rec := &recorder{}
	service := NewService(t.TempDir(), rec.send)
	ts := httptest.NewServer(NewServer(service).Handler())
	t.Cleanup(ts.Close)

	// Point the service at the test server so composed reply URLs are reachable.
	service.mu.Lock()
	service.url = ts.URL
	service.mu.Unlock()

	if _, err := service.Open(OpenRequest{
		Root: root, Name: "demo", AgentSession: "agent-session", Scope: ScopeBranch,
		Agent: AgentProfile{Label: "Claude", CanReply: true},
	}); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	service.mu.Lock()
	var session *Session
	for _, s := range service.sessions {
		session = s
	}
	service.mu.Unlock()
	if session == nil {
		t.Fatal("no session created")
	}
	return ts, service, session, rec
}

func postJSON(t *testing.T, ts *httptest.Server, path string, payload any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ts.Client().Post(ts.URL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestUnknownTokenIs404 is the boundary between one workspace's review and
// everything else on the machine.
func TestUnknownTokenIs404(t *testing.T) {
	ts, _, _, _ := harness(t)

	for _, path := range []string{"/s/nope/", "/s/nope/api/snapshot"} {
		res, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, res.StatusCode)
		}
	}
}

func TestPageAndSnapshotServe(t *testing.T) {
	ts, _, session, _ := harness(t)

	res, err := ts.Client().Get(ts.URL + "/s/" + session.ID() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("page status %d", res.StatusCode)
	}

	res2, err := ts.Client().Get(ts.URL + "/s/" + session.ID() + "/api/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res2.Body.Close() }()
	var payload snapshotResponse
	if err := json.NewDecoder(res2.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Snapshot.Files) == 0 {
		t.Error("snapshot carried no files")
	}
}

// TestDraftThenSubmit is the whole comment path: a comment is a draft until the
// user submits, and submitting is what reaches the agent.
func TestDraftThenSubmit(t *testing.T) {
	ts, service, session, rec := harness(t)
	base := "/s/" + session.ID() + "/api"

	res := postJSON(t, ts, base+"/comments", commentsRequest{
		Comments: []NewComment{{
			Path: "keep.txt", Side: SideNew, StartLine: 2, EndLine: 2,
			Quote: []string{"+TWO"}, Body: "shout less",
		}},
	})
	var added commentsResponse
	if err := json.NewDecoder(res.Body).Decode(&added); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if len(added.Threads) != 1 {
		t.Fatalf("threads = %d, want 1", len(added.Threads))
	}
	if added.Threads[0].Sent {
		t.Error("adding a comment must not send it")
	}
	if _, sent := rec.last(); sent {
		t.Fatal("adding a comment poked the agent")
	}

	res = postJSON(t, ts, base+"/comments", commentsRequest{Submit: true})
	var submitted commentsResponse
	if err := json.NewDecoder(res.Body).Decode(&submitted); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if !submitted.Sending {
		t.Fatalf("submit did not report sending: %+v", submitted)
	}

	req, ok := rec.last()
	if !ok {
		t.Fatal("submit did not reach the host")
	}
	if req.AgentSession != "agent-session" {
		t.Errorf("agent session = %q", req.AgentSession)
	}
	threadID := added.Threads[0].ID
	if !strings.Contains(req.Text, threadID) {
		t.Error("composed review does not name the thread id, so the agent cannot reply to it")
	}
	if !strings.Contains(req.Text, "keep.txt:2") {
		t.Errorf("composed review does not name the anchor:\n%s", req.Text)
	}
	if !strings.Contains(req.Text, "shout less") {
		t.Error("composed review lost the comment body")
	}

	// Until the host confirms, the thread stays pending so a failed paste can be
	// retried rather than silently swallowing what the user wrote.
	if len(session.Pending()) != 1 {
		t.Error("thread was marked sent before the host confirmed")
	}
	service.ConfirmSend(session.ID(), req.ThreadIDs, "")
	if len(session.Pending()) != 0 {
		t.Error("confirmed send left the thread pending")
	}
}

// TestSubmitCoversEveryPendingComment guards against submitting only the batch
// in the request, which would drop comments written in earlier posts.
func TestSubmitCoversEveryPendingComment(t *testing.T) {
	ts, _, session, rec := harness(t)
	base := "/s/" + session.ID() + "/api"

	for _, body := range []string{"first", "second"} {
		res := postJSON(t, ts, base+"/comments", commentsRequest{
			Comments: []NewComment{{Path: "keep.txt", StartLine: 2, EndLine: 2, Body: body}},
		})
		_ = res.Body.Close()
	}
	res := postJSON(t, ts, base+"/comments", commentsRequest{Submit: true})
	_ = res.Body.Close()

	req, _ := rec.last()
	for _, want := range []string{"first", "second"} {
		if !strings.Contains(req.Text, want) {
			t.Errorf("submitted review dropped %q:\n%s", want, req.Text)
		}
	}
	if len(req.ThreadIDs) != 2 {
		t.Errorf("thread ids = %d, want 2", len(req.ThreadIDs))
	}
}

// TestAgentReplyLandsOnTheThread is the agent's way back onto the page.
func TestAgentReplyLandsOnTheThread(t *testing.T) {
	ts, _, session, _ := harness(t)
	base := "/s/" + session.ID() + "/api"

	threads := session.AddComments([]NewComment{{Path: "keep.txt", StartLine: 2, EndLine: 2, Body: "why?"}})
	id := threads[0].ID

	res := postJSON(t, ts, base+"/reply", replyRequest{Thread: id, Body: "fixed it", Resolved: true})
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reply status %d", res.StatusCode)
	}

	got := session.Threads()[0]
	if len(got.Replies) != 1 || got.Replies[0].Body != "fixed it" {
		t.Fatalf("reply did not land: %+v", got.Replies)
	}
	if got.Replies[0].Author != AuthorAgent {
		t.Errorf("reply author = %q, want %q", got.Replies[0].Author, AuthorAgent)
	}
	if !got.Resolved {
		t.Error("resolved:true did not resolve the thread")
	}
}

// TestReplyToUnknownThreadNamesTheRealIDs keeps a mistyped id recoverable by the
// agent instead of needing the user to intervene.
func TestReplyToUnknownThreadNamesTheRealIDs(t *testing.T) {
	ts, _, session, _ := harness(t)
	threads := session.AddComments([]NewComment{{Path: "keep.txt", StartLine: 2, EndLine: 2, Body: "x"}})

	res := postJSON(t, ts, "/s/"+session.ID()+"/api/reply", replyRequest{Thread: "t-nope", Body: "hi"})
	defer func() { _ = res.Body.Close() }()

	var payload struct {
		OK      bool     `json:"ok"`
		Threads []string `json:"threads"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.OK {
		t.Error("reply to an unknown thread reported success")
	}
	if len(payload.Threads) != 1 || payload.Threads[0] != threads[0].ID {
		t.Errorf("error did not name the real thread ids: %v", payload.Threads)
	}
}

// TestCommentOutsideTheWorkspaceIsDropped keeps a path from the browser from
// being quoted back to the agent as a file in this workspace.
func TestCommentOutsideTheWorkspaceIsDropped(t *testing.T) {
	_, _, session, _ := harness(t)

	added := session.AddComments([]NewComment{
		{Path: "../../.ssh/config", StartLine: 1, Body: "read this"},
		{Path: "keep.txt", StartLine: 2, Body: "keep this"},
	})
	if len(added) != 1 || added[0].Path != "keep.txt" {
		t.Errorf("escaping path was not dropped: %+v", added)
	}
}

// TestBodylessCommentIsDropped: an anchor with nothing said about it gives the
// agent a location and no instruction.
func TestBodylessCommentIsDropped(t *testing.T) {
	_, _, session, _ := harness(t)
	if added := session.AddComments([]NewComment{{Path: "keep.txt", StartLine: 2, Body: "   "}}); len(added) != 0 {
		t.Errorf("empty comment was accepted: %+v", added)
	}
}

// TestReversedRangeIsRepaired: dragging upwards is the same selection as
// dragging down.
func TestReversedRangeIsRepaired(t *testing.T) {
	_, _, session, _ := harness(t)
	added := session.AddComments([]NewComment{
		{Path: "keep.txt", StartLine: 9, EndLine: 3, Body: "range"},
	})
	if len(added) != 1 {
		t.Fatal("comment dropped")
	}
	if added[0].StartLine != 3 || added[0].EndLine != 9 {
		t.Errorf("range = %d-%d, want 3-9", added[0].StartLine, added[0].EndLine)
	}
}

// TestLiveStreamOpensWithCurrentState: subscribing is what starts the poller, so
// a page connecting to an idle session must still be given something to draw.
func TestLiveStreamOpensWithCurrentState(t *testing.T) {
	ts, _, session, _ := harness(t)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/s/"+session.ID()+"/api/live", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}

	line := make(chan string, 1)
	go func() {
		reader := bufio.NewReader(res.Body)
		for {
			text, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(text, "data: ") {
				line <- strings.TrimPrefix(strings.TrimSpace(text), "data: ")
				return
			}
		}
	}()

	select {
	case raw := <-line:
		var ev Event
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatalf("event was not JSON: %v", err)
		}
		if ev.Type != EventSnapshot || ev.Snapshot == nil {
			t.Fatalf("opening event = %+v, want a snapshot", ev)
		}
		if len(ev.Snapshot.Files) == 0 {
			t.Error("opening snapshot had no files")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("live stream sent nothing")
	}
}

// TestReopenKeepsComments: the review page is reopened all the time, and losing
// the comments already written on it would make it unusable.
func TestReopenKeepsComments(t *testing.T) {
	_, service, session, _ := harness(t)
	session.AddComments([]NewComment{{Path: "keep.txt", StartLine: 2, Body: "keep me"}})

	if _, err := service.Open(OpenRequest{Root: session.Root(), Name: "demo", AgentSession: "another-session", Scope: ScopeBranch}); err != nil {
		t.Fatal(err)
	}
	if got := len(session.Threads()); got != 1 {
		t.Errorf("threads after reopen = %d, want 1", got)
	}
	if session.AgentSession() != "another-session" {
		t.Error("reopen did not retarget the agent tab")
	}
}

// TestAgentCanResolveWithoutSaying covers the agent closing a comment it has
// actioned when there is nothing to add. A reply endpoint that demanded a body
// would force it to pad the thread with "done" just to close it.
func TestAgentCanResolveWithoutSaying(t *testing.T) {
	ts, _, session, _ := harness(t)
	threads := session.AddComments([]NewComment{{Path: "keep.txt", StartLine: 2, Body: "rename this"}})
	id := threads[0].ID

	res := postJSON(t, ts, "/s/"+session.ID()+"/api/reply", replyRequest{Thread: id, Resolved: true})
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("body-less resolve returned %d", res.StatusCode)
	}

	got := session.Threads()[0]
	if !got.Resolved {
		t.Error("the thread was not resolved")
	}
	if len(got.Replies) != 0 {
		t.Errorf("a body-less resolve invented a reply: %+v", got.Replies)
	}
}

// TestEmptyReplyWithNoResolveIsRejected keeps a no-op POST from looking like it
// did something.
func TestEmptyReplyWithNoResolveIsRejected(t *testing.T) {
	ts, _, session, _ := harness(t)
	threads := session.AddComments([]NewComment{{Path: "keep.txt", StartLine: 2, Body: "x"}})

	res := postJSON(t, ts, "/s/"+session.ID()+"/api/reply", replyRequest{Thread: threads[0].ID, Body: "   "})
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("empty reply returned %d, want 400", res.StatusCode)
	}
}

// TestResolveEndpointReopens covers the page's own toggle, which has to work
// both ways so a reviewer can reopen something closed too eagerly.
func TestResolveEndpointReopens(t *testing.T) {
	ts, _, session, _ := harness(t)
	threads := session.AddComments([]NewComment{{Path: "keep.txt", StartLine: 2, Body: "x"}})
	id := threads[0].ID
	base := "/s/" + session.ID() + "/api/resolve"

	res := postJSON(t, ts, base, resolveRequest{Thread: id, Resolved: true})
	_ = res.Body.Close()
	if !session.Threads()[0].Resolved {
		t.Fatal("resolve did not take")
	}
	res = postJSON(t, ts, base, resolveRequest{Thread: id, Resolved: false})
	_ = res.Body.Close()
	if session.Threads()[0].Resolved {
		t.Error("a resolved thread could not be reopened")
	}
}

// TestReopenRetargetsTheAgentProfile covers switching tabs under an open review.
// A review reopened on a Codex tab where it was last opened on a Claude one must
// not keep promising replies the new agent cannot send.
func TestReopenRetargetsTheAgentProfile(t *testing.T) {
	_, service, session, _ := harness(t)

	if got := session.Agent(); got.Label != "Claude" || !got.CanReply {
		t.Fatalf("initial profile = %+v", got)
	}

	if _, err := service.Open(OpenRequest{
		Root:         session.Root(),
		Name:         "demo",
		AgentSession: "codex-session",
		Agent:        AgentProfile{Label: "Codex", CanReply: false, Blocked: "no network", Fix: "turn it on"},
	}); err != nil {
		t.Fatal(err)
	}

	got := session.Agent()
	if got.Label != "Codex" || got.CanReply {
		t.Errorf("profile after retarget = %+v", got)
	}
	// And it has to reach the page, which reads it off the snapshot.
	if snap := session.Refresh(ScopeBranch); snap.Agent.Label != "Codex" {
		t.Errorf("snapshot still names %q", snap.Agent.Label)
	}
}

// TestSubmitToAnAgentThatCannotReplyOmitsTheProtocol is the end-to-end of the
// Codex case, through the real HTTP handler.
func TestSubmitToAnAgentThatCannotReplyOmitsTheProtocol(t *testing.T) {
	ts, service, session, rec := harness(t)
	if _, err := service.Open(OpenRequest{
		Root: session.Root(), Name: "demo", AgentSession: "codex-session",
		Agent: AgentProfile{Label: "Codex", CanReply: false, Blocked: "no network"},
	}); err != nil {
		t.Fatal(err)
	}

	res := postJSON(t, ts, "/s/"+session.ID()+"/api/comments", commentsRequest{
		Comments: []NewComment{{Path: "keep.txt", StartLine: 2, Body: "tidy this"}},
		Submit:   true,
	})
	_ = res.Body.Close()

	req, ok := rec.last()
	if !ok {
		t.Fatal("submit did not reach the host")
	}
	if strings.Contains(req.Text, "curl") {
		t.Errorf("a Codex tab that cannot reply was told to curl:\n%s", req.Text)
	}
	if !strings.Contains(req.Text, "tidy this") {
		t.Error("the comment itself was lost")
	}
}
