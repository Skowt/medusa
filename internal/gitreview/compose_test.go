package gitreview

import (
	"strings"
	"testing"
)

func TestComposeEmptyReviewIsEmpty(t *testing.T) {
	if got := Compose("http://x/api/reply", replyCapable, nil); got != "" {
		t.Errorf("Compose(nil) = %q, want empty", got)
	}
}

// TestComposePutsTheReplyProtocolBeforeTheNotes guards the ordering. An agent
// that reads the notes first has already decided what to do by the time it
// reaches the tail of a long review, and will not go back for a protocol it did
// not know it needed.
func TestComposePutsTheReplyProtocolBeforeTheNotes(t *testing.T) {
	text := Compose("http://127.0.0.1:9/s/tok/api/reply", replyCapable, []Thread{
		{ID: "t1", Path: "a.go", StartLine: 10, EndLine: 10, Body: "rename this"},
	})

	curl := strings.Index(text, "curl")
	note := strings.Index(text, "rename this")
	if curl < 0 || note < 0 {
		t.Fatalf("composed review missing parts:\n%s", text)
	}
	if curl > note {
		t.Error("the reply instructions come after the notes")
	}
}

func TestComposeNamesAnchorsAndThreadIDs(t *testing.T) {
	text := Compose("http://127.0.0.1:9/s/tok/api/reply", replyCapable, []Thread{
		{ID: "t1", Path: "a.go", StartLine: 10, EndLine: 10, Body: "one", Quote: []string{"+x := 1"}},
		{ID: "t2", Path: "b.go", StartLine: 4, EndLine: 9, Body: "two", Side: SideOld},
	})

	for _, want := range []string{
		"a.go:10", "b.go:4-9", "t1", "t2", "> +x := 1", "one", "two",
		"http://127.0.0.1:9/s/tok/api/reply",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("composed review missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "removed line") {
		t.Error("an old-side comment must say it is on a removed line")
	}
}

// TestComposeSingleQuotesTheURL keeps the URL a URL when the agent pastes the
// command into a shell.
func TestComposeSingleQuotesTheURL(t *testing.T) {
	text := Compose("http://x/api/reply", replyCapable, []Thread{{ID: "t1", Path: "a", Body: "b"}})
	if !strings.Contains(text, "'http://x/api/reply'") {
		t.Errorf("URL was not quoted:\n%s", text)
	}
}

func TestShellQuoteEscapesQuotes(t *testing.T) {
	if got := shellQuote("a'b"); got != `'a'\''b'` {
		t.Errorf("shellQuote = %s", got)
	}
}

// TestComposeSaysNothingAboutResolving guards a deliberate omission. The user
// closes their own comments, and an agent that is told it may resolve them
// closes things it has not really dealt with.
func TestComposeSaysNothingAboutResolving(t *testing.T) {
	text := Compose("http://127.0.0.1:9/s/tok/api/reply", replyCapable, []Thread{
		{ID: "t1", Path: "a.go", StartLine: 10, Body: "fix this"},
	})

	for _, unwanted := range []string{"resolve", "resolved", "Resolve"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("composed review mentions %q:\n%s", unwanted, text)
		}
	}
	// One command, not a menu of them: the protocol is read before any comment
	// is, so every extra line pushes the review itself further down.
	if got := strings.Count(text, "curl -sS -X POST"); got != 1 {
		t.Errorf("composed review shows %d curl commands, want 1", got)
	}
}

// TestComposeAsksForShortRepliesInPlainEnglish covers the answer style. Left
// unsaid, an agent answers a one-line comment with several paragraphs, and the
// review page becomes unreadable.
func TestComposeAsksForShortRepliesInPlainEnglish(t *testing.T) {
	text := Compose("http://127.0.0.1:9/s/tok/api/reply", replyCapable, []Thread{
		{ID: "t1", Path: "a.go", StartLine: 10, Body: "why?"},
	})

	for _, want := range []string{"ASD-STE100", "one or two clear sentences"} {
		if !strings.Contains(text, want) {
			t.Errorf("composed review does not ask for short plain replies (missing %q):\n%s", want, text)
		}
	}
}

// replyCapable is the agent profile for a tab that can reach the review server,
// which is what most of these cases are about.
var replyCapable = AgentProfile{Label: "Claude", CanReply: true}

// TestComposeOmitsTheProtocolWhenTheAgentCannotReply covers a Codex tab on
// Medusa's default sandbox.
//
// Handing it the curl would be worse than saying nothing: a sandbox-blocked
// connection looks like an ordinary "couldn't connect", so the agent would
// report the review page as broken and send the user hunting for a server that
// is running perfectly well.
func TestComposeOmitsTheProtocolWhenTheAgentCannotReply(t *testing.T) {
	blocked := AgentProfile{Label: "Codex", CanReply: false, Blocked: "no network", Fix: "turn it on"}
	text := Compose("http://127.0.0.1:9/s/tok/api/reply", blocked, []Thread{
		{ID: "t1", Path: "a.go", StartLine: 10, Body: "rename this", Quote: []string{"+x := 1"}},
	})

	if strings.Contains(text, "curl") {
		t.Errorf("an agent that cannot reply was still told to curl:\n%s", text)
	}
	if strings.Contains(text, "127.0.0.1") {
		t.Error("the unreachable URL was still handed to the agent")
	}
	if !strings.Contains(text, "do not try") {
		t.Errorf("the agent was not told replies are unavailable:\n%s", text)
	}
	// The comments themselves, and their ids, still have to be there — the work
	// is the point, and the id is what lets the user quote it in the terminal.
	for _, want := range []string{"a.go:10", "rename this", "> +x := 1", "t1"} {
		if !strings.Contains(text, want) {
			t.Errorf("composed review lost %q:\n%s", want, text)
		}
	}
}

// TestComposeTellsTheAgentNotToBlameThePage: the failure mode being avoided is
// the agent concluding the review server is down.
func TestComposeTellsTheAgentNotToBlameThePage(t *testing.T) {
	text := Compose("http://x/api/reply", AgentProfile{Label: "Codex"}, []Thread{{ID: "t1", Path: "a", Body: "b"}})
	if !strings.Contains(text, "watches the diff") {
		t.Errorf("the agent was not told the page still tracks its work:\n%s", text)
	}
}
