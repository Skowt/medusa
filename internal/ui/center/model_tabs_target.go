package center

// AgentTarget describes the agent tab an action would be aimed at.
//
// It exists so callers do not have to ask three questions separately and risk
// getting answers from three different tabs: the user can switch tabs between
// calls, and a review sent to one tab described by another's settings would be
// launched with the wrong assumptions about what that agent can do.
type AgentTarget struct {
	SessionName  string
	Assistant    string
	CodexSandbox string
}

// ActiveAgentTarget returns the active agent tab, or a zero value when the
// active tab is not an agent (a script, a shell, the info tab).
func (m *Model) ActiveAgentTarget() AgentTarget {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx < 0 || activeIdx >= len(tabs) {
		return AgentTarget{}
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.isClosed() {
		return AgentTarget{}
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if !isAgentAssistant(tab.Assistant) {
		return AgentTarget{}
	}
	return AgentTarget{
		SessionName:  tab.SessionName,
		Assistant:    tab.Assistant,
		CodexSandbox: tab.CodexSandbox,
	}
}
