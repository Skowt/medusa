package messages

// OpenGitReview requests the live git review page for a workspace.
//
// AgentSession names the tmux session a submitted review is pasted into. It is
// resolved by the center pane at click time rather than looked up later: the
// user can switch tabs while the browser is open, and the review belongs to the
// agent whose changes it describes.
type OpenGitReview struct {
	WorkspaceID  string
	AgentSession string
	// Assistant and CodexSandbox describe the tab the review will be sent to.
	// They ride on the message rather than being looked up later because they
	// are the center pane's own per-tab state, and because what they decide --
	// whether the agent can reach the review server at all -- has to be settled
	// before the page tells the user anything about replies.
	Assistant    string
	CodexSandbox string
}

// GitReviewSubmitted carries a review composed in the browser back onto the UI
// thread so it can be pasted into an agent's PTY.
//
// It crosses threads: the HTTP handler that builds it runs on the review
// server's goroutine, and writing to a tab belongs to whatever owns the tabs.
type GitReviewSubmitted struct {
	SessionID    string
	AgentSession string
	Text         string
	ThreadIDs    []string
}

// GitReviewPrefsChanged carries the review page's sticky settings back onto the
// UI thread to be saved.
//
// It crosses threads for the same reason GitReviewSubmitted does -- the handler
// that notices the change runs on the review server's goroutine -- and it is
// saved on the UI thread rather than there because config.UI belongs to the UI
// thread and writing it from an HTTP handler is a data race.
type GitReviewPrefsChanged struct {
	Scope    string
	AutoSend bool
	Split    bool
	Theme    string
}
