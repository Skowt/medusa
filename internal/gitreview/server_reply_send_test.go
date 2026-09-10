package gitreview

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeStoreFile drops a comments file straight onto disk, which is the only way
// to test loading a format version this build no longer writes.
func writeStoreFile(t *testing.T, st *store, body string) {
	t.Helper()
	path := st.path("ws", "/root")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// openThread writes a comment and gets it delivered, so what follows is a reply
// on a thread the agent already has -- which is the case that reached nobody.
func openThread(t *testing.T, ts *httptest.Server, service *Service, session *Session, rec *recorder, body string) string {
	t.Helper()
	base := "/s/" + session.ID() + "/api"
	res := postJSON(t, ts, base+"/comments", commentsRequest{
		Comments: []NewComment{{Path: "keep.txt", StartLine: 2, EndLine: 2, Body: body}},
		Submit:   true,
	})
	_ = res.Body.Close()
	req, sent := rec.last()
	if !sent {
		t.Fatal("the opening comment was never delivered")
	}
	service.ConfirmSend(session.ID(), req.ThreadIDs, "")
	return req.ThreadIDs[0]
}

// TestAutoSendDeliversAReplyToAnExistingComment is the reported bug. New
// comments left immediately while replies queued behind a submit bar the mode
// had already hidden, so they reached nobody at all.
func TestAutoSendDeliversAReplyToAnExistingComment(t *testing.T) {
	ts, service, session, rec := harness(t)
	id := openThread(t, ts, service, session, rec, "please rename this")
	session.SetAutoSend(true)

	res := postJSON(t, ts, "/s/"+session.ID()+"/api/reply", replyRequest{
		Thread: id, Author: AuthorUser, Body: "on reflection, leave it",
	})
	_ = res.Body.Close()

	req, _ := rec.last()
	if !strings.Contains(req.Text, "on reflection, leave it") {
		t.Fatalf("the reply was not delivered:\n%s", req.Text)
	}
	// It has to name the thread, twice over: a reply on its own names no place,
	// and an agent several turns on may not have the original in front of it.
	for _, want := range []string{id, "keep.txt:2", "please rename this"} {
		if !strings.Contains(req.Text, want) {
			t.Errorf("the delivered reply is missing %q:\n%s", want, req.Text)
		}
	}
	if strings.Contains(req.Text, "curl") {
		t.Errorf("the reply repeated the reply protocol:\n%s", req.Text)
	}

	service.ConfirmSend(session.ID(), req.ThreadIDs, "")
	if got := session.PendingReplies(); len(got) != 0 {
		t.Errorf("the reply stayed pending after delivery: %+v", got)
	}
}

// TestAReplyIsNotDeliveredTwice: without a Sent flag of its own, every submit
// would announce every reply ever written under a delivered thread.
func TestAReplyIsNotDeliveredTwice(t *testing.T) {
	ts, service, session, rec := harness(t)
	id := openThread(t, ts, service, session, rec, "first comment")
	base := "/s/" + session.ID() + "/api"

	res := postJSON(t, ts, base+"/reply", replyRequest{Thread: id, Author: AuthorUser, Body: "a follow-up"})
	_ = res.Body.Close()

	res = postJSON(t, ts, base+"/comments", commentsRequest{Submit: true})
	_ = res.Body.Close()
	first, _ := rec.last()
	if !strings.Contains(first.Text, "a follow-up") {
		t.Fatalf("submit did not carry the queued reply:\n%s", first.Text)
	}
	service.ConfirmSend(session.ID(), first.ThreadIDs, "")

	// Nothing left, so a second submit has nothing to say.
	res = postJSON(t, ts, base+"/comments", commentsRequest{Submit: true})
	_ = res.Body.Close()
	second, _ := rec.last()
	if second.Text != first.Text {
		t.Errorf("a second submit re-announced the reply:\n%s", second.Text)
	}
}

// TestAnAgentsOwnReplyIsNeverSentBack: it came from there.
func TestAnAgentsOwnReplyIsNeverSentBack(t *testing.T) {
	ts, service, session, rec := harness(t)
	id := openThread(t, ts, service, session, rec, "why is this here?")
	before, _ := rec.last()
	session.SetAutoSend(true)

	res := postJSON(t, ts, "/s/"+session.ID()+"/api/reply", replyRequest{
		Thread: id, Body: "because of the sandbox", // author defaults to agent
	})
	_ = res.Body.Close()

	after, _ := rec.last()
	if after.Text != before.Text {
		t.Errorf("the agent's own reply was sent back to it:\n%s", after.Text)
	}
	if got := session.PendingReplies(); len(got) != 0 {
		t.Errorf("an agent reply was recorded as pending: %+v", got)
	}
}

// TestAReplyOnAnUnsentCommentRidesAlongWithIt: announcing a reply to something
// the agent has never seen tells it about half a conversation.
func TestAReplyOnAnUnsentCommentRidesAlongWithIt(t *testing.T) {
	ts, service, session, rec := harness(t)
	base := "/s/" + session.ID() + "/api"

	res := postJSON(t, ts, base+"/comments", commentsRequest{
		Comments: []NewComment{{Path: "keep.txt", StartLine: 2, EndLine: 2, Body: "the comment"}},
	})
	_ = res.Body.Close()
	id := session.Pending()[0].ID

	res = postJSON(t, ts, base+"/reply", replyRequest{Thread: id, Author: AuthorUser, Body: "and also this"})
	_ = res.Body.Close()

	// Not pending on its own -- the thread it hangs off is still a draft.
	if got := session.PendingReplies(); len(got) != 0 {
		t.Errorf("a reply on an unsent comment was announced separately: %+v", got)
	}

	res = postJSON(t, ts, base+"/comments", commentsRequest{Submit: true})
	_ = res.Body.Close()
	req, _ := rec.last()
	for _, want := range []string{"the comment", "and also this"} {
		if !strings.Contains(req.Text, want) {
			t.Errorf("submit lost %q:\n%s", want, req.Text)
		}
	}
	if strings.Contains(req.Text, "New repl") {
		t.Errorf("the folded reply was also announced as a separate reply:\n%s", req.Text)
	}

	// And it is marked delivered with the thread, or the next submit repeats it.
	service.ConfirmSend(session.ID(), req.ThreadIDs, "")
	if got := session.PendingReplies(); len(got) != 0 {
		t.Errorf("the folded reply stayed pending: %+v", got)
	}
}

// TestUpgradingTheStoreDoesNotReAnnounceOldReplies: a v1 file has no Sent field,
// so every reply in it decodes as unsent.
func TestUpgradingTheStoreDoesNotReAnnounceOldReplies(t *testing.T) {
	meta := t.TempDir()
	st := &store{dir: meta}

	// A v1 file, written the way the previous version would have.
	writeStoreFile(t, st, `{"version":1,"root":"/root","threads":[
	  {"id":"t1","path":"a.go","startLine":1,"endLine":1,"body":"old comment","sent":true,
	   "replies":[{"id":"r1","author":"user","body":"an old reply"}]}]}`)

	threads, _ := st.load("ws", "/root")
	if len(threads) != 1 || len(threads[0].Replies) != 1 {
		t.Fatalf("the v1 file did not load: %+v", threads)
	}
	if !threads[0].Replies[0].Sent {
		t.Error("a reply from a v1 store loaded as pending, so the next submit would re-announce it")
	}
}
