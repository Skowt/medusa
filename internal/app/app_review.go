package app

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/ui/review"
)

// reviewOverlayInset is how much of the screen the review window leaves around
// itself. It is deliberately small: the window is a working surface, not a
// notice, and a diff needs every column it can get.
const reviewOverlayInset = 4

// reviewOverlayVisible reports whether the review window is up.
func (a *App) reviewOverlayVisible() bool { return a.reviewOverlay != nil }

// openReviewOverlay builds and shows the review window for the active
// workspace, and remembers which agent tab its result should be sent to.
func (a *App) openReviewOverlay() tea.Cmd {
	if a.activeWorkspace == nil || a.reviewOverlay != nil {
		return nil
	}
	width, height := a.reviewOverlaySize()
	a.reviewOverlay = review.New(a.activeWorkspace, width, height)
	a.reviewSession = a.center.ActiveAgentSession()
	return a.reviewOverlay.Init()
}

// reviewOverlaySize is the window's size for the current terminal.
func (a *App) reviewOverlaySize() (int, int) {
	return maxInt(20, a.width-reviewOverlayInset*2), maxInt(8, a.height-reviewOverlayInset)
}

// localizeReviewMouse rebases a pointer event into the window's own
// coordinates, leaving every other message untouched.
//
// The app centres the window, so it is the only thing that knows where the
// window's top-left corner is; the window itself works in coordinates relative
// to its own border and would otherwise have to duplicate the centring
// arithmetic and stay in step with it.
func (a *App) localizeReviewMouse(msg tea.Msg) tea.Msg {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		return msg
	}
	x, y := a.reviewOverlayOrigin()
	click.X -= x
	click.Y -= y
	return click
}

// reviewOverlayOrigin is where the window's top-left corner is drawn. It
// mirrors composeOverlays exactly: both measure the rendered view, because a
// window shorter than its nominal height is centred on what it actually drew.
func (a *App) reviewOverlayOrigin() (int, int) {
	if a.reviewOverlay == nil {
		return 0, 0
	}
	w, h := viewDimensions(a.reviewOverlay.View())
	return a.centeredPosition(w, h)
}

// closeReviewOverlay tears the window down.
func (a *App) closeReviewOverlay() {
	a.reviewOverlay = nil
	a.reviewSession = ""
}

// updateReviewOverlay forwards a message to the review window and acts on its
// result when it closes.
func (a *App) updateReviewOverlay(msg tea.Msg) []tea.Cmd {
	if a.reviewOverlay == nil {
		return nil
	}
	updated, cmd, result := a.reviewOverlay.Update(a.localizeReviewMouse(msg))
	a.reviewOverlay = updated

	var cmds []tea.Cmd
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	if result == nil {
		return cmds
	}

	session := a.reviewSession
	a.closeReviewOverlay()
	return append(cmds, a.applyReviewResult(session, *result)...)
}

// applyReviewResult turns a finished review into edits-on-disk plus a message
// to the agent.
//
// The three outcomes are deliberately distinct. A refused write means nothing
// is sent at all: the review names the files the user edited by hand, and
// sending it while one of those edits sits unwritten would point the agent at a
// file that still holds its own version.
func (a *App) applyReviewResult(session string, result review.Result) []tea.Cmd {
	if len(result.Failed) > 0 {
		logging.Warn("Review not sent; files changed on disk: %v", result.Failed)
		return []tea.Cmd{a.toast.ShowError(
			"Review not sent — these changed on disk while you were editing: " +
				joinList(result.Failed))}
	}
	if !result.Saved {
		return nil
	}
	if result.Review == "" {
		return []tea.Cmd{a.toast.ShowInfo("Nothing to send")}
	}

	if !a.center.SendToAgentSession(session, result.Review) {
		logging.Warn("Review composed but no agent tab to send it to (session %q)", session)
		return []tea.Cmd{a.toast.ShowError("No running agent to send the review to")}
	}
	logging.Info("Sent review to %s (%d edited files)", session, len(result.Edited))

	summary := "Review sent"
	if n := len(result.Edited); n > 0 {
		summary += " · " + plural(n, "file", "files") + " written"
	}
	return []tea.Cmd{a.toast.ShowSuccess(summary)}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func joinList(items []string) string { return strings.Join(items, ", ") }

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
