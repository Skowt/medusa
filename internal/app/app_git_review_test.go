package app

import (
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/gitreview"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/center"
	"github.com/Skowt/medusa/internal/ui/common"
)

// TestGitReviewSubmittedIsCritical keeps a submitted review off the droppable
// queue. It is written by hand and submitted once: dropping it under load loses
// the user's comments with nothing to retry from, and leaves the page reporting
// "sending" forever.
func TestGitReviewSubmittedIsCritical(t *testing.T) {
	if !isCriticalExternalMsg(messages.GitReviewSubmitted{}) {
		t.Error("a submitted review must be a critical external message")
	}
}

// TestGitReviewSubmitWithNoAgentTellsThePage covers the failure that would
// otherwise be silent. With no agent tab the paste cannot happen, and a page
// that is not told keeps showing comments it believes were delivered while the
// user waits on an agent that was never asked anything.
func TestGitReviewSubmitWithNoAgentTellsThePage(t *testing.T) {
	cfg := &config.Config{}
	a := &App{
		toast:     common.NewToastModel(),
		center:    center.New(cfg),
		gitReview: gitreview.NewService(t.TempDir(), nil),
	}
	t.Cleanup(func() { _ = a.gitReview.Close() })

	root := t.TempDir()
	if _, err := a.gitReview.Open(gitreview.OpenRequest{Root: root, Name: "demo", AgentSession: "no-such-session", Scope: gitreview.ScopeBranch}); err != nil {
		t.Fatalf("open: %v", err)
	}
	session := onlySession(t, a.gitReview)
	threads := session.AddComments([]gitreview.NewComment{
		{Path: "a.txt", StartLine: 1, EndLine: 1, Body: "change this"},
	})

	cmd := a.handleGitReviewSubmitted(messages.GitReviewSubmitted{
		SessionID:    session.ID(),
		AgentSession: "no-such-session",
		Text:         "Code review …",
		ThreadIDs:    []string{threads[0].ID},
	})
	if cmd == nil {
		t.Error("a failed send should surface in the TUI too")
	}

	// The thread stays pending, so the user can open an agent tab and submit the
	// same comments again rather than retyping them.
	if got := len(session.Pending()); got != 1 {
		t.Errorf("pending threads = %d, want 1 — a failed send must not consume the draft", got)
	}
	if session.Threads()[0].Sent {
		t.Error("a thread was marked sent even though no agent received it")
	}
}

// TestGitReviewSubmitWithNoTextIsRejected: an empty review must not reach an
// agent as a bare prompt.
func TestGitReviewSubmitWithNoTextIsRejected(t *testing.T) {
	cfg := &config.Config{}
	a := &App{
		toast:     common.NewToastModel(),
		center:    center.New(cfg),
		gitReview: gitreview.NewService(t.TempDir(), nil),
	}
	t.Cleanup(func() { _ = a.gitReview.Close() })

	if _, err := a.gitReview.Open(gitreview.OpenRequest{Root: t.TempDir(), Name: "demo", AgentSession: "s", Scope: gitreview.ScopeBranch}); err != nil {
		t.Fatalf("open: %v", err)
	}
	session := onlySession(t, a.gitReview)

	if cmd := a.handleGitReviewSubmitted(messages.GitReviewSubmitted{
		SessionID: session.ID(),
	}); cmd != nil {
		t.Error("an empty review should not produce a success toast")
	}
}

// TestOpenGitReviewWithNoWorkspaceIsAToast keeps the button from panicking when
// the registry has moved under the click.
func TestOpenGitReviewWithNoWorkspaceIsAToast(t *testing.T) {
	a := &App{toast: common.NewToastModel(), gitReview: gitreview.NewService(t.TempDir(), nil)}
	t.Cleanup(func() { _ = a.gitReview.Close() })

	cmd := a.handleOpenGitReview(messages.OpenGitReview{WorkspaceID: "gone"})
	if cmd == nil {
		t.Fatal("an unresolvable workspace should produce a toast")
	}
}

// TestReviewSentSummaryReadsNaturally guards the toast wording, which is the
// only confirmation the TUI side gives.
func TestReviewSentSummaryReadsNaturally(t *testing.T) {
	if got := reviewSentSummary(1); !strings.Contains(got, "1 comment") || strings.Contains(got, "comments") {
		t.Errorf("summary(1) = %q", got)
	}
	if got := reviewSentSummary(3); !strings.Contains(got, "3 comments") {
		t.Errorf("summary(3) = %q", got)
	}
}

// onlySession returns the single open review session, failing if there is not
// exactly one.
func onlySession(t *testing.T, service *gitreview.Service) *gitreview.Session {
	t.Helper()
	sessions := service.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	return sessions[0]
}
