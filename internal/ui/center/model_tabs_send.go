package center

import (
	"github.com/Skowt/medusa/internal/logging"
)

// bracketedPaste wraps text in the paste sequences a terminal sends around
// pasted content.
//
// This is what keeps a multi-line message one prompt. Written raw, every
// newline is an Enter, so an agent receives the first line as a prompt and each
// following line as its own — a review arrives as a dozen half-sentences, and
// the agent starts answering the first one before it has read the rest.
func bracketedPaste(text string) string {
	return "\x1b[200~" + text + "\x1b[201~"
}

// ActiveAgentSession returns the tmux session name of the active agent tab, or
// "" when the active tab is not an agent (a script, a shell, the info tab).
func (m *Model) ActiveAgentSession() string {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx < 0 || activeIdx >= len(tabs) {
		return ""
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.isClosed() {
		return ""
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if !isAgentAssistant(tab.Assistant) {
		return ""
	}
	return tab.SessionName
}

// SendToAgentSession pastes text into a named agent tab and submits it,
// reporting whether it landed.
//
// It resolves by session name rather than sending to the active tab: the user
// can switch tabs while a review window is open, and the review belongs to the
// agent whose changes it describes. An empty session falls back to the active
// agent, which covers the case where the tab had not been named yet.
func (m *Model) SendToAgentSession(sessionName, text string) bool {
	if text == "" {
		return false
	}
	tab := m.agentTabForSession(sessionName)
	if tab == nil {
		return false
	}
	tab.mu.Lock()
	agent := tab.Agent
	tab.mu.Unlock()
	if agent == nil || agent.Terminal == nil {
		return false
	}

	if err := agent.Terminal.SendString(bracketedPaste(text)); err != nil {
		logging.Warn("Sending to agent session %s failed: %v", sessionName, err)
		return false
	}
	// Submit separately. The paste has to be closed before the Enter, or the
	// terminal reads the CR as part of the pasted body and nothing is sent.
	if err := agent.Terminal.SendString("\r"); err != nil {
		logging.Warn("Submitting to agent session %s failed: %v", sessionName, err)
		return false
	}
	return true
}

// agentTabForSession finds a live agent tab by session name, falling back to
// the active agent tab when the name is empty or no longer present.
func (m *Model) agentTabForSession(sessionName string) *Tab {
	if sessionName != "" {
		for _, tabs := range m.tabsByWorkspace {
			for _, tab := range tabs {
				if tab == nil || tab.isClosed() {
					continue
				}
				tab.mu.Lock()
				match := tab.SessionName == sessionName && isAgentAssistant(tab.Assistant)
				tab.mu.Unlock()
				if match {
					return tab
				}
			}
		}
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx < 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.isClosed() {
		return nil
	}
	tab.mu.Lock()
	ok := isAgentAssistant(tab.Assistant)
	tab.mu.Unlock()
	if !ok {
		return nil
	}
	return tab
}

// isAgentAssistant reports whether an assistant name is a conversational agent
// rather than a script or a bare shell. Only an agent can be handed a review:
// pasting one into a shell would just run it as a command.
func isAgentAssistant(assistant string) bool {
	switch assistant {
	case "", "script", "term", "shell":
		return false
	}
	return true
}
