package center

import (
	"strings"
	"testing"
)

// TestBracketedPasteKeepsAMultiLineReviewOneprompt guards the single detail
// that silently ruins the review feature: written raw, every newline in the
// message is an Enter, so the agent receives the first line as a prompt and
// each following line as its own.
func TestBracketedPasteKeepsAMultiLineReviewOnePrompt(t *testing.T) {
	review := "I reviewed your changes:\n\napp.go:12\ngate this on the tab"

	got := bracketedPaste(review)

	if !strings.HasPrefix(got, "\x1b[200~") {
		t.Errorf("missing the paste-start sequence: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[201~") {
		t.Errorf("missing the paste-end sequence: %q", got)
	}
	// The body must survive untouched — newlines included, since they are what
	// the bracketing exists to protect.
	if !strings.Contains(got, review) {
		t.Errorf("the review body was altered: %q", got)
	}
	// The submitting CR is sent separately: inside the brackets the terminal
	// reads it as pasted text and nothing is submitted at all.
	if strings.Contains(got, "\r") {
		t.Errorf("the paste must not carry a carriage return: %q", got)
	}
}

// TestIsAgentAssistant keeps a review from being pasted into a shell, where it
// would be run as a command rather than read.
func TestIsAgentAssistant(t *testing.T) {
	cases := map[string]bool{
		"claude": true,
		"codex":  true,
		"script": false,
		"term":   false,
		"shell":  false,
		"":       false,
	}
	for assistant, want := range cases {
		if got := isAgentAssistant(assistant); got != want {
			t.Errorf("isAgentAssistant(%q) = %v, want %v", assistant, got, want)
		}
	}
}

// TestSendToAgentSessionRejectsEmptyText keeps an empty review from submitting
// a bare Enter into the agent.
func TestSendToAgentSessionRejectsEmptyText(t *testing.T) {
	m := &Model{}
	if m.SendToAgentSession("medusa-ws-1", "") {
		t.Error("an empty review must not be sent")
	}
}
