package app

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
)

// The "creating" placeholder is what the sidebar renders while the worktree is
// built and gitignored files are copied. It must carry the group and profile the
// user picked, or the row sits under Ungrouped (and reads "Default") until
// creation finishes and the saved workspace replaces it.
func TestPendingWorkspace_CarriesGroupAndProfile(t *testing.T) {
	t.Run("single repo", func(t *testing.T) {
		ws := pendingWorkspace(
			"test-endpoint",
			[]data.RepoRef{{Name: "places", Path: "/src/places"}},
			[]string{"main"},
			"Work", "Places", "/wsroot",
		)
		if ws == nil {
			t.Fatal("pendingWorkspace returned nil")
		}
		if ws.Group != "Places" {
			t.Errorf("Group = %q, want %q", ws.Group, "Places")
		}
		if ws.Profile != "Work" {
			t.Errorf("Profile = %q, want %q", ws.Profile, "Work")
		}
	})

	t.Run("multi repo", func(t *testing.T) {
		ws := pendingWorkspace(
			"test-endpoint",
			[]data.RepoRef{
				{Name: "management", Path: "/src/management"},
				{Name: "places", Path: "/src/places"},
			},
			[]string{"main", "main"},
			"Work", "Places", "/wsroot",
		)
		if ws == nil {
			t.Fatal("pendingWorkspace returned nil")
		}
		if ws.Group != "Places" {
			t.Errorf("Group = %q, want %q", ws.Group, "Places")
		}
		if ws.Profile != "Work" {
			t.Errorf("Profile = %q, want %q", ws.Profile, "Work")
		}
	})

	t.Run("no repos yields no placeholder", func(t *testing.T) {
		if ws := pendingWorkspace("x", nil, nil, "Work", "Places", "/wsroot"); ws != nil {
			t.Errorf("pendingWorkspace(no repos) = %v, want nil", ws)
		}
	})
}
