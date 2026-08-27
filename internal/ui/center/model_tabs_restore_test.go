package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
)

func runningTabsWorkspace(name, root string) *data.Workspace {
	ws := newTestWorkspace(name, root)
	ws.OpenTabs = []data.TabInfo{
		{Assistant: "claude", Name: "claude", SessionName: "medusa-ws-1", Status: "running"},
		{Assistant: "claude", Name: "claude (2)", SessionName: "medusa-ws-2", Status: "running"},
	}
	return ws
}

// Running tabs are recreated asynchronously, so tabsByWorkspace is still empty
// when a second restore request arrives (e.g. WorkspacesLoaded eager restore
// racing WorkspaceActivated after an unarchive). The second call must be a
// no-op or every agent tab is duplicated.
func TestRestoreTabsFromWorkspaceIsIdempotentWhileCreationInFlight(t *testing.T) {
	m := newTestModel()
	ws := runningTabsWorkspace("ws", "/repo/ws")

	if cmd := m.RestoreTabsFromWorkspace(ws); cmd == nil {
		t.Fatalf("first restore should produce tab creation commands")
	}
	if cmd := m.RestoreTabsFromWorkspace(ws); cmd != nil {
		t.Fatalf("second restore should be a no-op while tab creation is in flight")
	}
}

func TestCleanupWorkspaceAllowsRestoreAgain(t *testing.T) {
	m := newTestModel()
	ws := runningTabsWorkspace("ws", "/repo/ws")

	if cmd := m.RestoreTabsFromWorkspace(ws); cmd == nil {
		t.Fatalf("first restore should produce tab creation commands")
	}
	// Archive removes all tabs/state; a later unarchive must restore again.
	m.CleanupWorkspace(ws)
	if cmd := m.RestoreTabsFromWorkspace(ws); cmd == nil {
		t.Fatalf("restore after CleanupWorkspace should run again")
	}
}
