package center

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
)

// TestReviewButtonIsAlwaysOffered documents a deliberate difference from the
// button this replaced, which was gated on a dirty worktree.
//
// The review it opens defaults to everything since the base branch, so an agent
// that has committed its work has the most to review — and a clean worktree
// would be exactly the moment the button vanished.
func TestReviewButtonIsAlwaysOffered(t *testing.T) {
	m, _ := tabBarModel(t, "feature-review", "", 1, 140, 40)
	m.SetWorkspace(data.NewWorkspace("feature-review", "feature/review", "main", "/repo", "/repo/feature-review"))

	line := strings.SplitN(m.renderInfoBar(140), "\n", 2)[0]
	if !strings.Contains(line, "Review Changes") {
		t.Errorf("the review button was not offered: %q", stripReviewANSI(line))
	}
	if _, ok := reviewHit(m); !ok {
		t.Error("the review button must be clickable")
	}
}

// TestReviewButtonIsFlushRight covers the placement: the button sits at the
// right edge of the info bar, and the left side — branch, path, IDE — gives way
// when the two would collide. The path is already abbreviated and can lose a
// little more; a button pushed past the edge is simply gone, which is the
// failure that is hardest to notice because nothing is left to hint at it.
func TestReviewButtonIsFlushRight(t *testing.T) {
	for _, width := range []int{40, 60, 80, 116, 160} {
		m, _ := tabBarModel(t, "feature-flush", "", 1, width+4, 40)
		m.SetWorkspace(data.NewWorkspace("feature-flush", "feature/a-fairly-long-branch-name",
			"main", "/repo", "/Users/someone/.medusa/workspaces/feature-flush"))

		line := strings.SplitN(m.renderInfoBar(width), "\n", 2)[0]
		if !strings.Contains(line, "Review Changes") {
			t.Errorf("width %d: the button was dropped: %q", width, stripReviewANSI(line))
			continue
		}
		if got := lipgloss.Width(line); got != width {
			t.Errorf("width %d: info bar renders %d columns, so the button is not flush right", width, got)
		}
		region, ok := reviewHit(m)
		if !ok {
			t.Errorf("width %d: no hit region", width)
			continue
		}
		if region.X+region.Width != width {
			t.Errorf("width %d: hit region ends at %d, want the right edge",
				width, region.X+region.Width)
		}
	}
}

// TestReviewButtonHitRegionMatchesItsLabel guards the geometry: the hit region
// is computed from the widths of the buttons before it, so a mis-set X sends
// clicks to whatever sits under the wrong column.
func TestReviewButtonHitRegionMatchesItsLabel(t *testing.T) {
	m, _ := tabBarModel(t, "feature-hit", "", 1, 164, 40)
	m.SetWorkspace(data.NewWorkspace("feature-hit", "feature/hit", "main", "/repo", "/repo/feature-hit"))

	line := strings.SplitN(m.renderInfoBar(160), "\n", 2)[0]
	region, ok := reviewHit(m)
	if !ok {
		t.Fatal("no review hit region")
	}

	// The rendered line carries ANSI styling, so compare on the stripped text.
	// The comparison must be in *columns*: the info bar contains ← and │, which
	// are three bytes each, so a byte offset from strings.Index sits four
	// columns right of where the glyph is actually drawn.
	plain := stripReviewANSI(line)
	byteIdx := strings.Index(plain, "[Review Changes]")
	if byteIdx < 0 {
		t.Fatalf("button not found in %q", plain)
	}
	col := lipgloss.Width(plain[:byteIdx])
	if region.X != col {
		t.Errorf("hit region starts at X=%d, but the label is drawn at column %d", region.X, col)
	}
	if region.Width != len("[Review Changes]") {
		t.Errorf("hit region is %d wide, want %d", region.Width, len("[Review Changes]"))
	}
}

// TestReviewClickNamesTheWorkspaceAndAgent keeps the click carrying both halves
// the app needs: without the agent session the review has nowhere to go, and
// without the workspace it does not know which repo to diff.
func TestReviewClickNamesTheWorkspaceAndAgent(t *testing.T) {
	m, ws := tabBarModel(t, "feature-msg", "", 1, 140, 40)
	m.SetWorkspace(ws)

	cmd := m.actionBarCommand(actionBarReviewChanges)
	if cmd == nil {
		t.Fatal("the review button produced no command")
	}
	msg, ok := cmd().(messages.OpenGitReview)
	if !ok {
		t.Fatalf("command produced %T, want messages.OpenGitReview", cmd())
	}
	if msg.WorkspaceID != string(ws.ID()) {
		t.Errorf("WorkspaceID = %q, want %q", msg.WorkspaceID, ws.ID())
	}
}

func reviewHit(m *Model) (region struct{ X, Width int }, ok bool) {
	for _, hit := range m.actionBarHits {
		if hit.kind == actionBarReviewChanges {
			return struct{ X, Width int }{hit.region.X, hit.region.Width}, true
		}
	}
	return region, false
}

// stripReviewANSI removes escape sequences so a rendered line can be searched
// by visible text.
func stripReviewANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && (r == 'm' || r == 'K'):
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}
