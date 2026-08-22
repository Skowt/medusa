package center

import (
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/data"
)

// restoredTabInfos is a saved workspace: two Claude tabs around a Codex one.
func restoredTabInfos() []data.TabInfo {
	return []data.TabInfo{
		{Assistant: "claude", Name: "claude 1", SessionName: "medusa-ws-1", Status: "running"},
		{Assistant: "codex", Name: "codex 1", SessionName: "medusa-ws-2", Status: "running"},
		{Assistant: "claude", Name: "claude 2", SessionName: "medusa-ws-3", Status: "running"},
	}
}

// arrive simulates one restored tab finishing its attach.
func arrive(m *Model, ws *data.Workspace, info data.TabInfo) *Tab {
	tab := &Tab{
		Name:        info.Name,
		Assistant:   info.Assistant,
		Workspace:   ws,
		SessionName: info.SessionName,
		Running:     true,
	}
	m.appendTabOrdered(string(ws.ID()), tab)
	return tab
}

func tabNames(m *Model, wsID string) []string {
	var names []string
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab == nil {
			continue
		}
		names = append(names, tab.Name)
	}
	return names
}

// A restore attaches every tab concurrently and each lands when its own agent
// is ready — Codex and Claude take different amounts of time — so the bar must
// be ordered by the saved positions, not by who finished first.
func TestRestoredTabsKeepCreationOrderWhateverTheArrivalOrder(t *testing.T) {
	infos := restoredTabInfos()
	arrivals := [][]int{
		{0, 1, 2},
		{2, 1, 0},
		{1, 0, 2},
		{1, 2, 0},
		{2, 0, 1},
	}
	want := "claude 1, codex 1, claude 2"

	for _, order := range arrivals {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		wsID := string(ws.ID())
		m.recordTabRestoreOrder(wsID, infos)

		for _, i := range order {
			arrive(m, ws, infos[i])
		}
		if got := strings.Join(tabNames(m, wsID), ", "); got != want {
			t.Errorf("arrivals %v produced %q, want %q", order, got, want)
		}
	}
}

// The focused tab has to stay focused when a slower tab lands to its left,
// which is the same bug wearing a different hat: an index into a slice that
// shifted underneath it.
func TestRestoreKeepsFocusOnTheSameTabWhenAnEarlierTabLandsLater(t *testing.T) {
	infos := restoredTabInfos()
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	m.recordTabRestoreOrder(wsID, infos)

	// The last tab attaches first and takes focus, as the persisted-active one.
	focused := arrive(m, ws, infos[2])
	m.activeTabByWorkspace[wsID] = 0

	arrive(m, ws, infos[0])
	arrive(m, ws, infos[1])

	tabs := m.tabsByWorkspace[wsID]
	idx := m.activeTabByWorkspace[wsID]
	if idx < 0 || idx >= len(tabs) {
		t.Fatalf("active index %d is out of range for %d tabs", idx, len(tabs))
	}
	if tabs[idx] != focused {
		t.Errorf("focus moved to %q, want it to stay on %q", tabs[idx].Name, focused.Name)
	}
}

// Script tabs stay at the end of the bar, ahead of every ordering concern.
func TestScriptTabsStayLastAmongRestoredTabs(t *testing.T) {
	infos := restoredTabInfos()
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	m.recordTabRestoreOrder(wsID, infos)

	arrive(m, ws, infos[0])
	arrive(m, ws, data.TabInfo{Assistant: "script", Name: "run", SessionName: "medusa-ws-script"})
	arrive(m, ws, infos[1])
	arrive(m, ws, infos[2])

	got := tabNames(m, wsID)
	if last := got[len(got)-1]; last != "run" {
		t.Errorf("tab order %v, want the script tab last", got)
	}
	if strings.Join(got[:3], ", ") != "claude 1, codex 1, claude 2" {
		t.Errorf("agent tabs out of order: %v", got)
	}
}

// A tab the user creates after a restore belongs at the end, not slotted in
// among the restored ones.
func TestNewTabGoesAfterRestoredTabs(t *testing.T) {
	infos := restoredTabInfos()
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	m.recordTabRestoreOrder(wsID, infos)
	for _, info := range infos {
		arrive(m, ws, info)
	}

	arrive(m, ws, data.TabInfo{Assistant: "codex", Name: "codex 2", SessionName: "medusa-ws-9"})

	got := tabNames(m, wsID)
	if last := got[len(got)-1]; last != "codex 2" {
		t.Errorf("tab order %v, want the new tab last", got)
	}
}

// A persisted tab that never got a tmux session is keyed by its display name,
// so it still lands where it was saved.
func TestRestoreOrdersTabsWithoutASessionName(t *testing.T) {
	infos := []data.TabInfo{
		{Assistant: "claude", Name: "claude 1", SessionName: "medusa-ws-1"},
		{Assistant: "codex", Name: "codex 1"},
		{Assistant: "claude", Name: "claude 2", SessionName: "medusa-ws-3"},
	}
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	m.recordTabRestoreOrder(wsID, infos)

	arrive(m, ws, infos[2])
	arrive(m, ws, infos[1])
	arrive(m, ws, infos[0])

	if got := strings.Join(tabNames(m, wsID), ", "); got != "claude 1, codex 1, claude 2" {
		t.Errorf("tab order %q", got)
	}
}

// Tabs discovered after a restore rank behind it, so a late discovery cannot
// push itself in front of a restored tab that has not landed yet.
func TestLaterDiscoveryRanksAfterRestoredTabs(t *testing.T) {
	infos := restoredTabInfos()
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	m.recordTabRestoreOrder(wsID, infos)

	discovered := data.TabInfo{Assistant: "codex", Name: "codex 9", SessionName: "medusa-ws-9"}
	m.recordTabRestoreOrder(wsID, []data.TabInfo{discovered})

	arrive(m, ws, discovered)
	for _, info := range infos {
		arrive(m, ws, info)
	}

	if got := strings.Join(tabNames(m, wsID), ", "); got != "claude 1, codex 1, claude 2, codex 9" {
		t.Errorf("tab order %q", got)
	}
}

// Re-recording the same workspace must not renumber it: a restore that runs
// twice (a workspace reopened) would otherwise shuffle the bar.
func TestRecordTabRestoreOrderIsStable(t *testing.T) {
	infos := restoredTabInfos()
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	m.recordTabRestoreOrder(wsID, infos)
	first := m.restoreOrder[wsID]["medusa-ws-2"]
	m.recordTabRestoreOrder(wsID, infos)

	if second := m.restoreOrder[wsID]["medusa-ws-2"]; second != first {
		t.Errorf("rank moved from %d to %d on a repeat record", first, second)
	}
}
