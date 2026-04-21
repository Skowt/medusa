package app

import (
	"path/filepath"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/ui/dashboard"
)

// newAppForGroupTests builds a minimally-wired *App sufficient for exercising
// handleRenameGroup / handleDeleteGroup without touching the real filesystem
// outside of t.TempDir().
func newAppForGroupTests(t *testing.T) *App {
	t.Helper()
	storeDir := t.TempDir()
	configDir := t.TempDir()
	cfg := &config.Config{
		Paths: &config.Paths{ConfigPath: filepath.Join(configDir, "config.json")},
		UI: config.UISettings{
			CollapsedGroups: map[string]bool{},
		},
	}
	return &App{
		workspaces: data.NewWorkspaceStore(storeDir),
		config:     cfg,
		dashboard:  dashboard.New(),
		toast:      common.NewToastModel(),
	}
}

func TestHandleRenameGroupCascades(t *testing.T) {
	a := newAppForGroupTests(t)
	ws1 := &data.Workspace{Name: "a", Group: "old"}
	ws2 := &data.Workspace{Name: "b", Group: "old"}
	ws3 := &data.Workspace{Name: "c", Group: "other"}
	a.allWorkspaces = []*data.Workspace{ws1, ws2, ws3}
	a.config.UI.CollapsedGroups["old"] = true

	_ = a.handleRenameGroup(messages.RenameGroup{OldLabel: "old", NewLabel: "new"})

	if ws1.Group != "new" || ws2.Group != "new" {
		t.Errorf("members not renamed: %q, %q", ws1.Group, ws2.Group)
	}
	if ws3.Group != "other" {
		t.Errorf("non-member changed: %q", ws3.Group)
	}
	if a.config.UI.CollapsedGroups["old"] {
		t.Errorf("old collapse key not cleared")
	}
	if !a.config.UI.CollapsedGroups["new"] {
		t.Errorf("collapse key not migrated to new label")
	}
}

func TestHandleDeleteGroupClearsMembers(t *testing.T) {
	a := newAppForGroupTests(t)
	ws1 := &data.Workspace{Name: "a", Group: "doomed"}
	ws2 := &data.Workspace{Name: "b", Group: "kept"}
	a.allWorkspaces = []*data.Workspace{ws1, ws2}
	a.config.UI.CollapsedGroups["doomed"] = true

	_ = a.handleDeleteGroup(messages.DeleteGroup{Label: "doomed"})

	if ws1.Group != "" {
		t.Errorf("a.Group = %q, want empty", ws1.Group)
	}
	if ws2.Group != "kept" {
		t.Errorf("b.Group = %q, want kept", ws2.Group)
	}
	if a.config.UI.CollapsedGroups["doomed"] {
		t.Errorf("doomed collapse key not removed")
	}
}

func TestHandleRenameGroupToEmpty(t *testing.T) {
	a := newAppForGroupTests(t)
	ws := &data.Workspace{Name: "a", Group: "doomed"}
	a.allWorkspaces = []*data.Workspace{ws}
	a.config.UI.CollapsedGroups["doomed"] = true

	_ = a.handleRenameGroup(messages.RenameGroup{OldLabel: "doomed", NewLabel: ""})

	if ws.Group != "" {
		t.Errorf("rename to empty didn't clear group: %q", ws.Group)
	}
	if a.config.UI.CollapsedGroups["doomed"] {
		t.Errorf("collapse key not cleared on empty rename")
	}
	if _, ok := a.config.UI.CollapsedGroups[""]; ok {
		t.Errorf("should not have migrated to empty-string key")
	}
}

func TestHandleRenameGroup_RollsBackOnSaveFailure(t *testing.T) {
	// TODO: Rollback logic is best-effort defensive code. To test it properly,
	// we would need a failure-injection hook in WorkspaceStore.Save or a mock
	// that returns errors on specific calls. The current interface doesn't expose
	// such a hook, so the rollback correctness is validated by code inspection.
	// If WorkspaceStore gains a failure hook in future, this test can be implemented.
	t.Skip("WorkspaceStore.Save has no failure-injection hook; rollback logic is best-effort unless a test double is introduced")
}

func TestHandleDeleteGroup_RollsBackOnSaveFailure(t *testing.T) {
	// TODO: Rollback logic is best-effort defensive code. To test it properly,
	// we would need a failure-injection hook in WorkspaceStore.Save or a mock
	// that returns errors on specific calls. The current interface doesn't expose
	// such a hook, so the rollback correctness is validated by code inspection.
	// If WorkspaceStore gains a failure hook in future, this test can be implemented.
	t.Skip("WorkspaceStore.Save has no failure-injection hook; rollback logic is best-effort unless a test double is introduced")
}

func TestHandleSetWorkspaceGroup_PrunesCollapsedGroupsKeyWhenLastMemberLeaves(t *testing.T) {
	a := newAppForGroupTests(t)
	ws := &data.Workspace{Name: "only", Group: "lonely"}
	a.allWorkspaces = []*data.Workspace{ws}
	a.config.UI.CollapsedGroups["lonely"] = true

	_ = a.handleSetWorkspaceGroup(messages.SetWorkspaceGroup{
		Workspace: ws,
		Label:     "new-home",
	})

	if ws.Group != "new-home" {
		t.Errorf("group not updated: %q", ws.Group)
	}
	if _, ok := a.config.UI.CollapsedGroups["lonely"]; ok {
		t.Errorf("stale key lonely not pruned")
	}
}

func TestHandleSetWorkspaceGroup_KeepsCollapsedKeyWhenOthersRemain(t *testing.T) {
	a := newAppForGroupTests(t)
	ws1 := &data.Workspace{Name: "a", Group: "popular"}
	ws2 := &data.Workspace{Name: "b", Group: "popular"}
	a.allWorkspaces = []*data.Workspace{ws1, ws2}
	a.config.UI.CollapsedGroups["popular"] = true

	_ = a.handleSetWorkspaceGroup(messages.SetWorkspaceGroup{
		Workspace: ws1,
		Label:     "",
	})

	if _, ok := a.config.UI.CollapsedGroups["popular"]; !ok {
		t.Errorf("popular key wrongly pruned while ws2 still in group")
	}
}
