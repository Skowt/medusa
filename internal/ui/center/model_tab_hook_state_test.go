package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/hooks"
)

func TestSetTabHookStateMarksOnlyCompletedHiddenTabUnread(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	first := &Tab{ID: "first", SessionName: "session-first"}
	second := &Tab{ID: "second", SessionName: "session-second"}
	m.workspace = ws
	m.tabsByWorkspace[wsID] = []*Tab{first, second}
	m.setActiveTabIdx(0)

	m.SetTabHookState(wsID, second.SessionName, string(hooks.EventPreToolUse), false)
	if second.Unread || second.HookState != string(hooks.EventPreToolUse) {
		t.Fatalf("processing state = (%q, unread=%v)", second.HookState, second.Unread)
	}
	m.SetTabHookState(wsID, second.SessionName, "", true)
	if !second.Unread {
		t.Fatal("completed hidden tab was not marked unread")
	}
	if first.Unread {
		t.Fatal("completion leaked unread state to another tab")
	}

	m.setActiveTabIdx(1)
	if second.Unread {
		t.Fatal("selecting a tab did not clear its unread state")
	}
}

func TestCompletedTabBehindInfoIsUnread(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{ID: "tab", SessionName: "session"}
	m.workspace = ws
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.setActiveTabIdx(0)
	m.infoTabActive = true

	m.SetTabHookState(wsID, tab.SessionName, "", true)
	if !tab.Unread {
		t.Fatal("tab hidden behind Info was treated as visible")
	}
}

func TestSetWorkspaceClearsRememberedSelectedTabUnread(t *testing.T) {
	m := newTestModel()
	firstWS := newTestWorkspace("first", "/repo/first")
	secondWS := newTestWorkspace("second", "/repo/second")
	firstID := string(firstWS.ID())
	selected := &Tab{ID: "selected", Workspace: firstWS, Unread: true}
	other := &Tab{ID: "other", Workspace: firstWS, Unread: true}
	m.tabsByWorkspace[firstID] = []*Tab{selected, other}
	m.activeTabByWorkspace[firstID] = 0
	m.SetWorkspace(secondWS)

	m.SetWorkspace(firstWS)

	if selected.Unread {
		t.Fatal("selected tab remained unread after its workspace became visible")
	}
	if !other.Unread {
		t.Fatal("switching workspaces cleared an unread hidden tab")
	}
}
