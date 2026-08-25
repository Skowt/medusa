package app

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/ui/review"
)

// TestReviewResultFailedWriteIsNotSent covers the rule that makes the staleness
// guard worth having: if a file could not be written, nothing is sent. The
// review names the files the user edited by hand, so sending it while one of
// those edits sits unwritten would point the agent at a file that still holds
// its own version.
//
// The App here deliberately has no center model. Reaching the send path would
// panic, which is exactly the assertion: this result must never get that far.
func TestReviewResultFailedWriteIsNotSent(t *testing.T) {
	a := &App{toast: common.NewToastModel()}

	cmds := a.applyReviewResult("medusa-ws-1", review.Result{
		Saved:  true,
		Review: "please change this",
		Edited: []string{"internal/hooks/event.go"},
		Failed: []string{"internal/app/app_hooks.go"},
	})

	if len(cmds) != 1 {
		t.Fatalf("expected one toast command, got %d", len(cmds))
	}
}

// TestReviewResultDiscardSendsNothing covers the discard path: no writes, no
// message, no toast. A nil center again stands in for "must not reach the send".
func TestReviewResultDiscardSendsNothing(t *testing.T) {
	a := &App{toast: common.NewToastModel()}

	if cmds := a.applyReviewResult("medusa-ws-1", review.Result{}); len(cmds) != 0 {
		t.Fatalf("discard must produce no commands, got %d", len(cmds))
	}
}

// TestReviewResultEmptyReviewIsNotSent covers a save with nothing said — the
// agent should not receive an empty prompt.
func TestReviewResultEmptyReviewIsNotSent(t *testing.T) {
	a := &App{toast: common.NewToastModel()}

	cmds := a.applyReviewResult("medusa-ws-1", review.Result{Saved: true, Review: ""})
	if len(cmds) != 1 {
		t.Fatalf("expected one toast command, got %d", len(cmds))
	}
}

// TestReviewOverlaySizeLeavesTheScreenEdges keeps the window from covering the
// whole terminal, so it reads as a window over the app rather than as a mode
// switch — and keeps a tiny terminal from producing a negative size.
func TestReviewOverlaySizeLeavesTheScreenEdges(t *testing.T) {
	a := &App{width: 200, height: 60}
	w, h := a.reviewOverlaySize()
	if w >= a.width || h >= a.height {
		t.Fatalf("overlay %dx%d fills the %dx%d screen", w, h, a.width, a.height)
	}

	small := &App{width: 10, height: 4}
	w, h = small.reviewOverlaySize()
	if w < 1 || h < 1 {
		t.Fatalf("overlay collapsed to %dx%d on a tiny terminal", w, h)
	}
}

// TestReviewOverlayRendersBeforeItLoads asserts the window paints a frame while
// the diffs are still being read. Returning an empty string would make the
// overlay flash blank on open, which reads as a broken window.
func TestReviewOverlayRendersBeforeItLoads(t *testing.T) {
	ws := data.NewWorkspace("wt", "feature/x", "main", t.TempDir(), t.TempDir())
	m := review.New(ws, 120, 40)

	view := m.View()
	if !strings.Contains(view, "Review Changes") {
		t.Fatalf("loading view is missing its title:\n%s", view)
	}
	for _, want := range []string{"Save & Send", "Discard"} {
		if !strings.Contains(view, want) {
			t.Errorf("loading view is missing the %q button:\n%s", want, view)
		}
	}
}

// TestReviewClicksAreRebasedIntoTheWindow covers the one thing the window
// cannot do for itself: it works in coordinates relative to its own border,
// while a click arrives in screen coordinates. The app centres the window, so
// it is the only thing that knows the offset — and getting it wrong sends every
// click to a row or two away from where the user aimed.
func TestReviewClicksAreRebasedIntoTheWindow(t *testing.T) {
	a := &App{width: 200, height: 60, toast: common.NewToastModel()}
	w, h := a.reviewOverlaySize()
	a.reviewOverlay = review.New(nil, w, h)

	originX, originY := a.reviewOverlayOrigin()
	if originX <= 0 || originY <= 0 {
		t.Fatalf("the window is not inset from the screen: origin (%d,%d)", originX, originY)
	}

	screen := tea.MouseClickMsg{Button: tea.MouseLeft, X: originX + 7, Y: originY + 3}
	local, ok := a.localizeReviewMouse(screen).(tea.MouseClickMsg)
	if !ok {
		t.Fatal("the click was not forwarded as a click")
	}
	if local.X != 7 || local.Y != 3 {
		t.Errorf("click rebased to (%d,%d), want (7,3)", local.X, local.Y)
	}

	// Everything that is not a pointer event passes through untouched.
	key := tea.KeyPressMsg{Code: 'x', Text: "x"}
	if got := a.localizeReviewMouse(key); got != tea.Msg(key) {
		t.Errorf("a keypress was rewritten on its way to the window: %#v", got)
	}
}

// TestGitStatusRefreshIsNotGatedOnTheSidebar guards the reason the
// [Review Changes] button did not appear at all for a while: the only paths
// that request git status for the active workspace both skipped it while the
// sidebar was hidden. The button — and the live review window — are gated on
// that status, so with the sidebar collapsed neither could ever turn on.
func TestGitStatusRefreshIsNotGatedOnTheSidebar(t *testing.T) {
	source, err := os.ReadFile("app_input_pty.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"handleGitStatusTick", "handleFileWatcherEvent"} {
		body := functionBody(t, string(source), fn)
		if strings.Contains(body, "SidebarHidden") {
			t.Errorf("%s still gates the git status request on the sidebar; "+
				"the info bar and review window need it regardless", fn)
		}
	}
}

// functionBody returns the source of a top-level function, up to the closing
// brace in column zero.
func functionBody(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, "func (a *App) "+name+"(")
	if start < 0 {
		t.Fatalf("function %s not found", name)
	}
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("could not find the end of %s", name)
	}
	return source[start : start+end]
}
