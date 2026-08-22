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
