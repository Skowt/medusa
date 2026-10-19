package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
)

// A rename moves a tab's tmux session to a new name, but the agent running in
// it keeps the MEDUSA_SESSION_NAME it was launched with, and every hook it
// fires carries that name. The tab has to remember it, or those hooks match
// nothing and the tab's activity is lost until the agent is restarted.
func TestRenameTabSessions_RemembersTheNameTheAgentWasLaunchedWith(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{Assistant: "codex", Workspace: ws, SessionName: "medusa-old-1"}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	m.RenameTabSessions(wsID, map[string]string{"medusa-old-1": "medusa-new-1"})
	if tab.SessionName != "medusa-new-1" {
		t.Fatalf("SessionName = %q, want medusa-new-1", tab.SessionName)
	}
	if tab.LaunchSessionName != "medusa-old-1" {
		t.Fatalf("LaunchSessionName = %q, want medusa-old-1", tab.LaunchSessionName)
	}

	// A second rename does not touch the running agent either, so the name it
	// was launched with is still the first one.
	m.RenameTabSessions(wsID, map[string]string{"medusa-new-1": "medusa-newer-1"})
	if tab.LaunchSessionName != "medusa-old-1" {
		t.Fatalf("after a second rename LaunchSessionName = %q, want medusa-old-1", tab.LaunchSessionName)
	}
}

// The launch name has to survive a Medusa restart, since the agent does: it
// lives in tmux. So it is persisted and restored with the rest of the tab.
func TestLaunchSessionName_IsPersistedAndRestored(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	m.tabsByWorkspace[wsID] = []*Tab{{
		Assistant: "claude", Workspace: ws,
		SessionName: "medusa-new-1", LaunchSessionName: "medusa-old-1",
	}}

	infos, _ := m.GetTabsInfoForWorkspace(wsID)
	if len(infos) != 1 || infos[0].LaunchSessionName != "medusa-old-1" {
		t.Fatalf("persisted tab info = %+v, want LaunchSessionName medusa-old-1", infos)
	}

	restored := newTestModel()
	tab := restored.addPlaceholderTab(ws, data.TabInfo{
		Assistant: "claude", SessionName: "medusa-new-1", LaunchSessionName: "medusa-old-1",
	}, true)
	if tab.LaunchSessionName != "medusa-old-1" {
		t.Fatalf("restored LaunchSessionName = %q, want medusa-old-1", tab.LaunchSessionName)
	}
}

// Once the agent is relaunched it carries the current name, and the old one
// means nothing. Retiring it is what stops a later workspace that happens to
// reuse the old name from having its hooks claimed by this tab.
func TestRetireLaunchSessionName(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{Assistant: "codex", Workspace: ws, SessionName: "medusa-new-1", LaunchSessionName: "medusa-old-1"}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	if !m.RetireLaunchSessionName(wsID, "medusa-new-1") {
		t.Fatal("expected retiring a recorded launch name to report a change")
	}
	if tab.LaunchSessionName != "" {
		t.Fatalf("LaunchSessionName = %q, want it cleared", tab.LaunchSessionName)
	}
	if m.RetireLaunchSessionName(wsID, "medusa-new-1") {
		t.Error("retiring again must report no change")
	}
}

// A tab persisted as running is restored by reattaching to its tmux session,
// where the original agent is still running. That path rebuilds the tab from
// its launch options, so they have to carry the launch name through whichever
// assistant the tab belongs to.
func TestLaunchSessionName_SurvivesTheReattachPath(t *testing.T) {
	for _, assistant := range []string{"claude", "codex"} {
		info := data.TabInfo{Assistant: assistant, SessionName: "medusa-new-1", LaunchSessionName: "medusa-old-1"}
		opts := agentTabOptionsFromTabInfo(info).forAssistant(assistant)
		if opts.LaunchSessionName != "medusa-old-1" {
			t.Errorf("%s: LaunchSessionName = %q after forAssistant, want medusa-old-1", assistant, opts.LaunchSessionName)
		}
	}
}
