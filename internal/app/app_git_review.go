package app

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/gitreview"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
)

// wireGitReview builds the review service and connects both of the hooks it
// hands back to the host.
//
// Both cross the same boundary: the review server notices things on an HTTP
// goroutine, and both pasting into a PTY and writing config.UI belong to the UI
// thread, so each takes the message pump every other external event does.
//
// It is one function rather than two lines in New because the second hook is
// easy to lose: registered in the wrong scope it still compiles, and the symptom
// is a preference that silently never saves. Named here, one test covers the
// wiring that New depends on.
func (a *App) wireGitReview(metadataRoot string) {
	a.gitReview = gitreview.NewService(metadataRoot, func(req gitreview.SendRequest) {
		a.enqueueExternalMsg(messages.GitReviewSubmitted{
			SessionID:    req.SessionID,
			AgentSession: req.AgentSession,
			Text:         req.Text,
			ThreadIDs:    req.ThreadIDs,
		})
	})
	a.gitReview.OnPreferences(func(prefs gitreview.Preferences) {
		a.enqueueExternalMsg(messages.GitReviewPrefsChanged{
			Scope:    string(prefs.Scope),
			AutoSend: prefs.AutoSend,
			Split:    prefs.Split,
			Theme:    string(prefs.Theme),
		})
	})
}

// handleOpenGitReview opens the live review page for a workspace in the browser.
//
// The first press starts the review server and reads the whole diff, so all of
// it runs off the UI thread — the same reason the skill dashboard does.
func (a *App) handleOpenGitReview(msg messages.OpenGitReview) tea.Cmd {
	ws := a.workspaceForReview(msg.WorkspaceID)
	if ws == nil {
		return a.toast.ShowError("No workspace to review")
	}
	service := a.gitReview
	if service == nil {
		return a.toast.ShowError("Review server unavailable")
	}

	// A nil config is a test's App, not a real one, but reading through it here
	// would panic rather than fall back -- and the fallbacks are the same
	// defaults a fresh config carries.
	scope, autoSend, split := gitreview.ScopeWorking, false, false
	theme := gitreview.ThemeSystem
	if a.config != nil {
		scope = gitreview.ParseScope(a.config.UI.LastReviewScope)
		autoSend = a.config.UI.LastReviewAutoSend
		split = a.config.UI.LastReviewSplit
		theme = gitreview.ParseTheme(a.config.UI.LastReviewTheme)
	}

	req := gitreview.OpenRequest{
		WorkspaceID:  string(ws.ID()),
		Root:         ws.PrimaryWorktreeRoot(),
		Name:         ws.Name,
		AgentSession: msg.AgentSession,
		Agent:        reviewAgentProfile(a.config, ws, msg.Assistant, msg.CodexSandbox),
		Scope:        scope,
		AutoSend:     autoSend,
		Split:        split,
		Theme:        theme,
	}

	return func() tea.Msg {
		url, err := service.Open(req)
		if err != nil {
			return messages.Toast{
				Message: "Could not open the review: " + err.Error(),
				Level:   messages.ToastError,
			}
		}
		if err := openInBrowser(url); err != nil {
			// The server is up regardless, so show the address to open by hand.
			return messages.Toast{Message: "Review at " + url, Level: messages.ToastWarning}
		}
		return messages.Toast{Message: "Opened review in browser", Level: messages.ToastSuccess}
	}
}

// handleGitReviewPrefsChanged remembers the review page's sticky settings.
//
// It runs on the UI thread because that is what owns config.UI: the change is
// noticed on the review server's own goroutine, and mutating shared config from
// there is a data race the detector would find. A failed save is logged and
// swallowed -- the setting is already live in this session, and a toast about
// config.json is not what the reader wants back from ticking a checkbox.
func (a *App) handleGitReviewPrefsChanged(msg messages.GitReviewPrefsChanged) tea.Cmd {
	if a.config == nil {
		return nil
	}
	if a.config.UI.LastReviewScope == msg.Scope &&
		a.config.UI.LastReviewAutoSend == msg.AutoSend &&
		a.config.UI.LastReviewSplit == msg.Split &&
		a.config.UI.LastReviewTheme == msg.Theme {
		return nil
	}
	a.config.UI.LastReviewScope = msg.Scope
	a.config.UI.LastReviewAutoSend = msg.AutoSend
	a.config.UI.LastReviewSplit = msg.Split
	a.config.UI.LastReviewTheme = msg.Theme
	if err := a.config.SaveUISettings(); err != nil {
		logging.Warn("Could not save review preferences: %v", err)
	}
	return nil
}

// workspaceForReview resolves the workspace a review button names, falling back
// to the active one.
//
// The fallback matters because the button is drawn in the center pane's info
// bar, which always describes the active workspace: an ID that no longer
// resolves means the registry moved under the click, not that the user meant a
// different workspace.
func (a *App) workspaceForReview(id string) *data.Workspace {
	if id != "" {
		for _, ws := range a.allWorkspaces {
			if string(ws.ID()) == id {
				return ws
			}
		}
	}
	return a.activeWorkspace
}

// handleGitReviewSubmitted pastes a submitted review into its agent tab and
// reports the outcome back to the page.
//
// Both outcomes have to reach the browser. A silent failure leaves the page
// showing comments it believes were delivered, and the user waiting on an agent
// that was never told anything.
func (a *App) handleGitReviewSubmitted(msg messages.GitReviewSubmitted) tea.Cmd {
	service := a.gitReview
	if service == nil {
		return nil
	}

	if msg.Text == "" {
		service.ConfirmSend(msg.SessionID, nil, "Nothing to send")
		return nil
	}

	if !a.center.SendToAgentSession(msg.AgentSession, msg.Text) {
		logging.Warn("Review composed but no agent tab to send it to (session %q)", msg.AgentSession)
		const notice = "No running agent to send this review to — open an agent tab and submit again"
		service.ConfirmSend(msg.SessionID, nil, notice)
		return a.toast.ShowError("No running agent to send the review to")
	}

	service.ConfirmSend(msg.SessionID, msg.ThreadIDs, "")
	logging.Info("Sent review with %d comments to %s", len(msg.ThreadIDs), msg.AgentSession)
	return a.toast.ShowSuccess(reviewSentSummary(len(msg.ThreadIDs)))

}

// reviewSentSummary describes what was sent, for the TUI toast.
func reviewSentSummary(comments int) string {
	if comments == 1 {
		return "Review sent · 1 comment"
	}
	return "Review sent · " + strconv.Itoa(comments) + " comments"
}
