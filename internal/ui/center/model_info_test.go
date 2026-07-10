package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
)

// setWorkspaceModel builds a Model with one agent tab registered for ws,
// which is the precondition for the Info tab NOT being auto-selected.
// newTestModel() (model_activity_test.go) already initialises tabsByWorkspace
// and activeTabByWorkspace via New(), so this only needs to populate them.
func setWorkspaceModel(t *testing.T, ws *data.Workspace) *Model {
	t.Helper()
	m := newTestModel()
	wsID := string(ws.ID())
	m.tabsByWorkspace[wsID] = []*Tab{{ID: TabID("tab-1"), Name: "claude"}}
	m.activeTabByWorkspace[wsID] = 0
	return m
}

func TestSetWorkspaceWithNoteDoesNotSelectInfoTab(t *testing.T) {
	ws := data.NewWorkspace("ws-1", "", "", "/repo/ws-1", "/repo/ws-1")
	ws.Note = "fix the auth redirect loop"
	m := setWorkspaceModel(t, ws)

	m.SetWorkspace(ws)

	if m.infoTabActive {
		t.Fatal("workspace with a note must open on the agent tab, not Info")
	}
	if m.IsInfoTabActive() {
		t.Fatal("IsInfoTabActive() must be false when the workspace has agent tabs")
	}
}

func TestSetWorkspaceWithoutNoteDoesNotSelectInfoTab(t *testing.T) {
	ws := data.NewWorkspace("ws-2", "", "", "/repo/ws-2", "/repo/ws-2")
	m := setWorkspaceModel(t, ws)

	m.SetWorkspace(ws)

	if m.infoTabActive {
		t.Fatal("workspace without a note must open on the agent tab")
	}
}

// The Info tab must still auto-activate when there are no agent tabs.
// This path lives in IsInfoTabActive(), not in the infoTabActive field.
func TestSetWorkspaceWithNoTabsStillShowsInfo(t *testing.T) {
	ws := data.NewWorkspace("ws-3", "", "", "/repo/ws-3", "/repo/ws-3")
	ws.Note = "some note"
	m := newTestModel() // no tabs registered

	m.SetWorkspace(ws)

	if !m.IsInfoTabActive() {
		t.Fatal("a workspace with no agent tabs must fall back to the Info tab")
	}
}
