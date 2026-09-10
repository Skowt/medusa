package gitreview

// AgentProfile describes the agent a review will be sent to, as far as the page
// and the composed message need to care.
//
// It exists because a review is not assistant-agnostic in two ways. The page has
// to name the agent it is talking to -- calling Codex "Claude" on every button
// is simply wrong -- and the agent may not be able to reply at all: a Codex tab
// on Medusa's default `workspace-write` sandbox cannot open a network connection,
// so the reply endpoint is unreachable from it.
//
// The capability is decided by the host, not here: what a sandbox permits is
// pty/config territory, and this package has no business knowing Codex's sandbox
// modes.
type AgentProfile struct {
	// Label is what to call the agent in the interface: "Claude", "Codex".
	Label string `json:"label"`
	// CanReply reports whether the agent can reach this server over loopback.
	// When false the composed review omits the reply protocol entirely, rather
	// than handing the agent instructions that will fail.
	CanReply bool `json:"canReply"`
	// Blocked says why replies are unavailable and Fix says how to enable them.
	// Both are shown to the user -- never to the agent, which can do nothing
	// about its own sandbox.
	Blocked string `json:"blocked,omitempty"`
	Fix     string `json:"fix,omitempty"`
}

// defaultAgentLabel is used when the host names no agent, so the interface still
// reads as a sentence.
const defaultAgentLabel = "the agent"

// LabelOr returns the agent's name, falling back to a neutral phrase.
func (a AgentProfile) LabelOr() string {
	if a.Label == "" {
		return defaultAgentLabel
	}
	return a.Label
}
