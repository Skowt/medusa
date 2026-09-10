package gitreview

import (
	"strings"
	"testing"
)

// TestAutoSendDeliversACommentAsItIsWritten is the mode's whole purpose: no
// second gesture between writing a comment and the agent having it.
func TestAutoSendDeliversACommentAsItIsWritten(t *testing.T) {
	ts, service, session, rec := harness(t)
	base := "/s/" + session.ID() + "/api"

	// Off by default: the comment is queued and nothing is sent.
	res := postJSON(t, ts, base+"/comments", commentsRequest{
		Comments: []NewComment{{Path: "keep.txt", StartLine: 2, EndLine: 2, Body: "first"}},
	})
	_ = res.Body.Close()
	if _, sent := rec.last(); sent {
		t.Fatal("a comment was sent with auto-send off")
	}

	res = postJSON(t, ts, base+"/autosend", autoSendRequest{AutoSend: true})
	_ = res.Body.Close()
	if !session.AutoSend() {
		t.Fatal("auto-send did not take")
	}

	// Turning the mode on flushes what was already queued. It has to: the page
	// hides the submit bar while the mode is on, so a comment left behind here
	// would have nothing left to press and would sit there unreachable.
	flushed, sent := rec.last()
	if !sent {
		t.Fatal("turning auto-send on left the queued draft unsent and unreachable")
	}
	if !strings.Contains(flushed.Text, "first") {
		t.Errorf("the flush did not carry the queued comment:\n%s", flushed.Text)
	}
	service.ConfirmSend(session.ID(), flushed.ThreadIDs, "")

	res = postJSON(t, ts, base+"/comments", commentsRequest{
		Comments: []NewComment{{Path: "keep.txt", StartLine: 3, EndLine: 3, Body: "second"}},
	})
	_ = res.Body.Close()

	req, _ := rec.last()
	if !strings.Contains(req.Text, "second") {
		t.Errorf("a comment written with auto-send on was not delivered:\n%s", req.Text)
	}
	if len(req.ThreadIDs) != 1 {
		t.Errorf("delivered %d threads, want just the new one", len(req.ThreadIDs))
	}
}

// TestTurningAutoSendOffSendsNothing is the other half of the flush: it is the
// way back to the submit bar, and to any comment a failed flush left queued.
func TestTurningAutoSendOffSendsNothing(t *testing.T) {
	ts, _, session, rec := harness(t)
	base := "/s/" + session.ID() + "/api"

	res := postJSON(t, ts, base+"/comments", commentsRequest{
		Comments: []NewComment{{Path: "keep.txt", StartLine: 2, EndLine: 2, Body: "queued"}},
	})
	_ = res.Body.Close()

	res = postJSON(t, ts, base+"/autosend", autoSendRequest{AutoSend: false})
	_ = res.Body.Close()

	if _, sent := rec.last(); sent {
		t.Error("turning auto-send off delivered a comment")
	}
	if len(session.Pending()) != 1 {
		t.Errorf("the queued comment did not survive: %+v", session.Pending())
	}
}

// TestTheFirstSendTeachesTheProtocolAndLaterOnesDoNot covers the pair together:
// the agent has to be told how to reply once, and told nothing twice.
func TestTheFirstSendTeachesTheProtocolAndLaterOnesDoNot(t *testing.T) {
	ts, service, session, rec := harness(t)
	base := "/s/" + session.ID() + "/api"

	res := postJSON(t, ts, base+"/autosend", autoSendRequest{AutoSend: true})
	_ = res.Body.Close()

	res = postJSON(t, ts, base+"/comments", commentsRequest{
		Comments: []NewComment{{Path: "keep.txt", StartLine: 2, EndLine: 2, Body: "first"}},
	})
	_ = res.Body.Close()

	first, _ := rec.last()
	if !strings.Contains(first.Text, "curl") {
		t.Fatalf("the first send did not teach the agent how to reply:\n%s", first.Text)
	}
	// The host confirms the paste; only then does the agent count as taught.
	service.ConfirmSend(session.ID(), first.ThreadIDs, "")

	res = postJSON(t, ts, base+"/comments", commentsRequest{
		Comments: []NewComment{{Path: "keep.txt", StartLine: 3, EndLine: 3, Body: "second"}},
	})
	_ = res.Body.Close()

	second, _ := rec.last()
	if strings.Contains(second.Text, "curl") {
		t.Errorf("a follow-up repeated the reply instructions:\n%s", second.Text)
	}
	if !strings.Contains(second.Text, "second") || !strings.Contains(second.Text, "New review comment") {
		t.Errorf("the follow-up lost the comment itself:\n%s", second.Text)
	}
	if len(second.Text) >= len(first.Text) {
		t.Errorf("the follow-up (%d bytes) is not shorter than the first send (%d)",
			len(second.Text), len(first.Text))
	}
}

// TestExplicitSubmitStillWorksWithAutoSendOff keeps the Submit button as the way
// out for anything queued, including a comment whose auto-send failed.
func TestExplicitSubmitStillWorksWithAutoSendOff(t *testing.T) {
	ts, _, session, rec := harness(t)
	base := "/s/" + session.ID() + "/api"

	res := postJSON(t, ts, base+"/comments", commentsRequest{
		Comments: []NewComment{{Path: "keep.txt", StartLine: 2, EndLine: 2, Body: "queued"}},
	})
	_ = res.Body.Close()
	res = postJSON(t, ts, base+"/comments", commentsRequest{Submit: true})
	_ = res.Body.Close()

	req, sent := rec.last()
	if !sent || !strings.Contains(req.Text, "queued") {
		t.Errorf("explicit submit did not deliver the draft: %+v", req)
	}
}

// TestAutoSendIsReadFromTheSessionNotTheRequest: a page holding a stale toggle
// -- a second tab, or one left open across a change of mode -- must not queue a
// comment the review considers already sent.
func TestAutoSendIsReadFromTheSessionNotTheRequest(t *testing.T) {
	ts, _, session, rec := harness(t)
	base := "/s/" + session.ID() + "/api"

	session.SetAutoSend(true)
	res := postJSON(t, ts, base+"/comments", commentsRequest{
		Submit:   false, // the stale page believes it is still queueing
		Comments: []NewComment{{Path: "keep.txt", StartLine: 2, EndLine: 2, Body: "x"}},
	})
	_ = res.Body.Close()

	if _, sent := rec.last(); !sent {
		t.Error("a comment was queued because the page had the mode wrong")
	}
}
