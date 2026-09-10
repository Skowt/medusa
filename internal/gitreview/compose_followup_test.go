package gitreview

import (
	"strings"
	"testing"
)

var followUp = []Thread{
	{ID: "t7", Path: "internal/git/diff.go", StartLine: 30, EndLine: 37,
		Quote: []string{"+// OldLine and NewLine are the line's position"}, Body: "Say why 0 means absent."},
}

// TestComposeFollowUpCarriesTheCommentAndNothingElse is the whole point of the
// short form. The agent is mid-task and already knows how to reply, so the curl,
// the reply style and the framing cost it context and tell it nothing.
func TestComposeFollowUpCarriesTheCommentAndNothingElse(t *testing.T) {
	text := ComposeFollowUp(followUp)

	for _, want := range []string{
		"New review comment", "internal/git/diff.go:30-37", "t7",
		"> +// OldLine and NewLine are the line's position", "Say why 0 means absent.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("follow-up is missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"curl", "ASD-STE100", "Content-Type", "thread-id>"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("follow-up repeats the protocol (%q):\n%s", unwanted, text)
		}
	}
	// Short enough to be worth having: the full form is four times this.
	if lines := strings.Count(strings.TrimSpace(text), "\n") + 1; lines > 5 {
		t.Errorf("follow-up for one comment is %d lines:\n%s", lines, text)
	}
}

// TestComposeFollowUpSeparatesOnlySeveralComments: a horizontal rule above three
// lines of text is noise, and one comment is the common case under auto-send.
func TestComposeFollowUpSeparatesOnlySeveralComments(t *testing.T) {
	if got := ComposeFollowUp(followUp); strings.Contains(got, "---") {
		t.Errorf("a single follow-up drew a separator:\n%s", got)
	}

	two := append([]Thread{}, followUp...)
	two = append(two, Thread{ID: "t8", Path: "a.go", StartLine: 1, Body: "and this"})
	got := ComposeFollowUp(two)
	if !strings.Contains(got, "---") {
		t.Errorf("several follow-ups were run together:\n%s", got)
	}
	if !strings.Contains(got, "New review comments") {
		t.Errorf("plural was not used:\n%s", got)
	}
}

func TestComposeFollowUpEmptyIsEmpty(t *testing.T) {
	if got := ComposeFollowUp(nil); got != "" {
		t.Errorf("ComposeFollowUp(nil) = %q", got)
	}
}

// TestNeedsProtocolUntilASendActuallyLands: recorded at compose time, a review
// that failed to paste -- no agent tab open -- would make the retry a follow-up
// to a message nobody ever received.
func TestNeedsProtocolUntilASendActuallyLands(t *testing.T) {
	s := &Session{
		agentSession: "agent-1", threads: []*Thread{}, subs: make(map[int]chan Event),
	}
	s.threads = append(s.threads, &Thread{ID: "t1", Path: "a.go", Body: "x"})

	if !s.needsProtocol() {
		t.Fatal("a session that has sent nothing must include the protocol")
	}

	s.MarkSent([]string{"t1"}, "could not paste: no agent tab")
	if !s.needsProtocol() {
		t.Error("a failed send was treated as having taught the agent the protocol")
	}

	s.MarkSent([]string{"t1"}, "")
	if s.needsProtocol() {
		t.Error("the protocol was re-sent to an agent that already has it")
	}
}

// TestNeedsProtocolAgainForANewAgent covers the case a follow-up would otherwise
// fail silently in: a review page outlives the tab it was opened against, and an
// agent that never saw the curl cannot answer.
func TestNeedsProtocolAgainForANewAgent(t *testing.T) {
	s := &Session{
		agentSession: "agent-1", threads: []*Thread{{ID: "t1", Path: "a.go", Body: "x"}},
		subs: make(map[int]chan Event),
	}
	s.MarkSent([]string{"t1"}, "")

	s.Retarget("agent-2", AgentProfile{Label: "Claude", CanReply: true})
	if !s.needsProtocol() {
		t.Error("a review retargeted at another agent skipped the reply instructions")
	}
}

// TestAutoSendDefaultsOff: comments must never leave for an agent on the
// strength of a mode the reader did not choose in this sitting.
func TestAutoSendDefaultsOff(t *testing.T) {
	s := &Session{subs: make(map[int]chan Event)}
	if s.AutoSend() {
		t.Error("auto-send was on by default")
	}
	s.SetAutoSend(true)
	if !s.AutoSend() {
		t.Error("auto-send did not turn on")
	}
	s.SetAutoSend(false)
	if s.AutoSend() {
		t.Error("auto-send did not turn off")
	}
}
