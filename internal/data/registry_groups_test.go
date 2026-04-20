package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	return NewRegistry(filepath.Join(dir, "workspaces.json"))
}

func TestRegistry_AddGroup_AssignsAscendingOrder(t *testing.T) {
	r := newTestRegistry(t)

	if err := r.AddGroup("billing", "medusa"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if err := r.AddGroup("perf", "medusa"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if err := r.AddGroup("billing", "other-repo"); err != nil { // same name, different scope
		t.Fatalf("AddGroup: %v", err)
	}

	got, err := r.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 groups, got %d", len(got))
	}

	orderByName := map[string]int{}
	for _, g := range got {
		if g.RepoKey == "medusa" {
			orderByName[g.Name] = g.Order
		}
		if !g.Expanded {
			t.Errorf("new group %q should start expanded", g.Name)
		}
	}
	if orderByName["billing"] != 0 || orderByName["perf"] != 1 {
		t.Errorf("expected billing=0, perf=1; got %v", orderByName)
	}
}

func TestRegistry_AddGroup_DuplicateIsNoOp(t *testing.T) {
	r := newTestRegistry(t)

	if err := r.AddGroup("billing", "medusa"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if err := r.AddGroup("billing", "medusa"); err != nil {
		t.Fatalf("second AddGroup: %v", err)
	}

	groups, err := r.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group after duplicate add, got %d", len(groups))
	}
}

func TestRegistry_SetGroupExpanded(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.AddGroup("billing", "medusa"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if err := r.SetGroupExpanded("billing", "medusa", false); err != nil {
		t.Fatalf("SetGroupExpanded: %v", err)
	}
	groups, _ := r.ListGroups()
	if groups[0].Expanded {
		t.Fatal("want collapsed after SetGroupExpanded(false)")
	}
}

func TestRegistry_RenameGroup(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.AddGroup("billing", "medusa"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if err := r.AddGroup("perf", "medusa"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	if err := r.RenameGroup("billing", "billing-v2", "medusa"); err != nil {
		t.Fatalf("RenameGroup: %v", err)
	}
	groups, _ := r.ListGroups()
	var got []string
	for _, g := range groups {
		got = append(got, g.Name)
	}
	if !contains(got, "billing-v2") || contains(got, "billing") {
		t.Fatalf("rename failed: %v", got)
	}

	// Collision should fail.
	if err := r.RenameGroup("perf", "billing-v2", "medusa"); err == nil {
		t.Fatal("expected error renaming to existing group name in same scope")
	}
}

func TestRegistry_RemoveGroup(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.AddGroup("billing", "medusa"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if err := r.AddGroup("perf", "medusa"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if err := r.RemoveGroup("billing", "medusa"); err != nil {
		t.Fatalf("RemoveGroup: %v", err)
	}
	groups, _ := r.ListGroups()
	if len(groups) != 1 || groups[0].Name != "perf" {
		t.Fatalf("unexpected groups after remove: %+v", groups)
	}
}

func TestRegistry_GroupsPersistAlongsideWorkspaces(t *testing.T) {
	r := newTestRegistry(t)

	if err := r.AddWorkspace("ws1", "abc", "default"); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	if err := r.AddGroup("billing", "medusa"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	// Mutating workspaces must not drop groups.
	if err := r.SetProfile("abc", "other"); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}

	groups, err := r.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups lost after workspace mutation: %+v", groups)
	}

	// And the JSON on disk round-trips.
	raw, err := os.ReadFile(r.path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var file registryFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(file.Workspaces) != 1 || len(file.Groups) != 1 {
		t.Fatalf("on-disk contents wrong: %+v", file)
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
