package app

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// reorderWS builds a workspace with the repo and worktree a real one has, so
// Root() and ID() — which the reorder handler and the store both key off — are
// meaningful.
func reorderWS(name, group string) *data.Workspace {
	return &data.Workspace{
		Name:      name,
		Group:     group,
		Repos:     []data.RepoRef{{Name: "medusa", Path: "/src/medusa"}},
		Worktrees: []data.WorktreeRef{{Root: "/wt/" + name, Branch: name, Base: "main"}},
	}
}

func TestHandleReorderWorkspacesAssignsAscendingKeys(t *testing.T) {
	a := newAppForGroupTests(t)
	first := reorderWS("first", "shipping")
	second := reorderWS("second", "shipping")
	third := reorderWS("third", "shipping")
	a.allWorkspaces = []*data.Workspace{first, second, third}

	_ = a.handleReorderWorkspaces(messages.ReorderWorkspaces{
		Group:        "shipping",
		OrderedRoots: []string{third.Root(), first.Root(), second.Root()},
	})

	if third.SortKey >= first.SortKey || first.SortKey >= second.SortKey {
		t.Fatalf("keys must ascend in the given order, got third=%d first=%d second=%d",
			third.SortKey, first.SortKey, second.SortKey)
	}
	for _, ws := range a.allWorkspaces {
		if ws.SortKey == 0 {
			t.Errorf("%s kept SortKey 0, which reads as never placed", ws.Name)
		}
		if ws.Group != "shipping" {
			t.Errorf("%s.Group = %q, want shipping", ws.Name, ws.Group)
		}
	}
}

func TestHandleReorderWorkspacesPersists(t *testing.T) {
	a := newAppForGroupTests(t)
	ws := reorderWS("only", "shipping")
	a.allWorkspaces = []*data.Workspace{ws}

	_ = a.handleReorderWorkspaces(messages.ReorderWorkspaces{
		Group:        "shipping",
		OrderedRoots: []string{ws.Root()},
	})

	loaded, err := a.workspaces.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SortKey != ws.SortKey {
		t.Errorf("stored SortKey = %d, want %d: a manual order that does not reach disk is lost on restart",
			loaded.SortKey, ws.SortKey)
	}
}

func TestHandleReorderWorkspacesMovesAcrossGroups(t *testing.T) {
	a := newAppForGroupTests(t)
	member := reorderWS("member", "shipping")
	incoming := reorderWS("incoming", "infra")
	a.allWorkspaces = []*data.Workspace{member, incoming}
	a.config.UI.CollapsedGroups["infra"] = true

	_ = a.handleReorderWorkspaces(messages.ReorderWorkspaces{
		Group:        "shipping",
		OrderedRoots: []string{incoming.Root(), member.Root()},
	})

	if incoming.Group != "shipping" {
		t.Errorf("incoming.Group = %q, want shipping: a cross-group drop relabels in the same commit", incoming.Group)
	}
	if incoming.SortKey >= member.SortKey {
		t.Errorf("incoming should lead: keys are %d and %d", incoming.SortKey, member.SortKey)
	}
	if _, tracked := a.config.UI.CollapsedGroups["infra"]; tracked {
		t.Error("the emptied source group should have its collapse state pruned")
	}
}

func TestHandleReorderWorkspacesKeepsNonEmptySourceGroup(t *testing.T) {
	a := newAppForGroupTests(t)
	staying := reorderWS("staying", "infra")
	leaving := reorderWS("leaving", "infra")
	a.allWorkspaces = []*data.Workspace{staying, leaving}
	a.config.UI.CollapsedGroups["infra"] = true

	_ = a.handleReorderWorkspaces(messages.ReorderWorkspaces{
		Group:        "shipping",
		OrderedRoots: []string{leaving.Root()},
	})

	if !a.config.UI.CollapsedGroups["infra"] {
		t.Error("a source group that still has members must keep its collapse state")
	}
}

func TestHandleReorderWorkspacesSkipsUnknownRoots(t *testing.T) {
	a := newAppForGroupTests(t)
	ws := reorderWS("only", "shipping")
	a.allWorkspaces = []*data.Workspace{ws}

	_ = a.handleReorderWorkspaces(messages.ReorderWorkspaces{
		Group:        "shipping",
		OrderedRoots: []string{"/wt/deleted-between-drop-and-here", ws.Root()},
	})

	if ws.SortKey == 0 {
		t.Error("a workspace that vanished mid-drop must not block ordering the rest")
	}
}

func TestHandleReorderGroupsPersistsOrder(t *testing.T) {
	a := newAppForGroupTests(t)

	_ = a.handleReorderGroups(messages.ReorderGroups{Labels: []string{"infra", "", "shipping"}})

	want := []string{"infra", "", "shipping"}
	if len(a.config.UI.GroupOrder) != len(want) {
		t.Fatalf("GroupOrder = %v, want %v", a.config.UI.GroupOrder, want)
	}
	for i := range want {
		if a.config.UI.GroupOrder[i] != want[i] {
			t.Fatalf("GroupOrder = %v, want %v", a.config.UI.GroupOrder, want)
		}
	}
}

func TestHandleCreateGroupForWorkspaceMovesThenPrompts(t *testing.T) {
	a := newAppForGroupTests(t)
	ws := reorderWS("loose", "")
	a.allWorkspaces = []*data.Workspace{ws}
	a.width, a.height = 120, 40

	_ = a.handleCreateGroupForWorkspace(messages.CreateGroupForWorkspace{
		Root:  ws.Root(),
		Label: "wily-jackal",
		Order: []string{"wily-jackal"},
	})

	if ws.Group != "wily-jackal" {
		t.Errorf("Group = %q, want the generated label", ws.Group)
	}
	if ws.SortKey == 0 {
		t.Error("the workspace should be placed in its new group")
	}
	if len(a.config.UI.GroupOrder) != 1 || a.config.UI.GroupOrder[0] != "wily-jackal" {
		t.Errorf("GroupOrder = %v, want the new group pinned", a.config.UI.GroupOrder)
	}
	if a.dialog == nil || !a.dialog.Visible() {
		t.Fatal("the rename dialog must open on the group that was just created")
	}
	if a.dialogDefaultName != "wily-jackal" {
		t.Errorf("dialogDefaultName = %q, want the group being renamed", a.dialogDefaultName)
	}
}

// The dialog renames whatever dialogDefaultName points at, so opening it for a
// group that was never created would cascade a rename over nothing.
func TestHandleCreateGroupForWorkspaceSkipsPromptWhenTheMoveDidNotStick(t *testing.T) {
	a := newAppForGroupTests(t)
	a.allWorkspaces = []*data.Workspace{reorderWS("loose", "")}
	a.width, a.height = 120, 40

	_ = a.handleCreateGroupForWorkspace(messages.CreateGroupForWorkspace{
		Root:  "/wt/vanished",
		Label: "wily-jackal",
		Order: []string{"wily-jackal"},
	})

	if a.dialog != nil && a.dialog.Visible() {
		t.Error("no workspace moved, so there is no group to rename")
	}
}
