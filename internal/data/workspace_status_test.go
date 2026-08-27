package data

import "testing"

// TestNextStatusCycle pins the manual toggle's order. Review precedes Blocked:
// swapping the two puts the exceptional state on the common path, so every
// workspace heading for merged has to step through "blocked" first.
func TestNextStatusCycle(t *testing.T) {
	want := []WorkspaceStatus{StatusReview, StatusBlocked, StatusMerged, StatusNone}
	got := StatusNone
	for i, expect := range want {
		got = NextStatus(got)
		if got != expect {
			t.Fatalf("step %d: NextStatus = %q, want %q", i+1, got, expect)
		}
	}

	if next := NextStatus(StatusStarted); next != StatusReview {
		t.Fatalf("NextStatus(started) = %q, want %q", next, StatusReview)
	}
	if next := NextStatus(StatusArchived); next != StatusNone {
		t.Fatalf("NextStatus(archived) = %q, want %q", next, StatusNone)
	}
	if next := NextStatus(WorkspaceStatus("bogus")); next != StatusStarted {
		t.Fatalf("NextStatus(unknown) = %q, want %q", next, StatusStarted)
	}
}

// TestIsPrimaryCheckout_ToleratesPathSpelling guards the gate on deleting the
// root directory: the two paths reach a workspace from different places, and a
// mismatch in spelling alone would report the user's repo as a worktree.
func TestIsPrimaryCheckout_ToleratesPathSpelling(t *testing.T) {
	ws := Workspace{
		Repos:     []RepoRef{{Path: "/src/repo"}},
		Worktrees: []WorktreeRef{{Root: "/src/repo/"}},
	}
	if !ws.IsPrimaryCheckout() {
		t.Error("a trailing separator must not make the repo look like a worktree")
	}
	if ws.UsesWorktree() {
		t.Error("UsesWorktree must agree with IsPrimaryCheckout")
	}

	other := Workspace{
		Repos:     []RepoRef{{Path: "/src/repo"}},
		Worktrees: []WorktreeRef{{Root: "/src/worktrees/feature"}},
	}
	if other.IsPrimaryCheckout() || !other.UsesWorktree() {
		t.Error("a real worktree must still read as one")
	}
}
