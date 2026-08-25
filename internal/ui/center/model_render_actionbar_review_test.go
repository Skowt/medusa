package center

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/data"
)

// TestReviewButtonFollowsDirtyState covers the button's whole contract: it is
// offered only when there is something to review, and it is clickable when it
// is. Showing it on a clean worktree would open a window with nothing in it.
func TestReviewButtonFollowsDirtyState(t *testing.T) {
	m, _ := tabBarModel(t, "feature-review", "", 1, 140, 40)
	m.SetWorkspace(data.NewWorkspace("feature-review", "feature/review", "main", "/repo", "/repo/feature-review"))

	clean := strings.SplitN(m.renderInfoBar(140), "\n", 2)[0]
	if strings.Contains(clean, "Review Changes") {
		t.Errorf("a clean worktree must not offer the review button: %q", clean)
	}
	if hasReviewHit(m) {
		t.Error("a clean worktree must not leave a review hit region behind")
	}

	m.SetGitDirty(true)
	dirty := strings.SplitN(m.renderInfoBar(140), "\n", 2)[0]
	if !strings.Contains(dirty, "Review Changes") {
		t.Errorf("a dirty worktree must offer the review button: %q", dirty)
	}
	if !hasReviewHit(m) {
		t.Error("the review button must be clickable when shown")
	}

	// Going clean again has to withdraw it, or a stale button sits on a
	// workspace whose changes were just committed away.
	m.SetGitDirty(false)
	if again := strings.SplitN(m.renderInfoBar(140), "\n", 2)[0]; strings.Contains(again, "Review Changes") {
		t.Errorf("the review button survived the worktree going clean: %q", again)
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
		m.SetGitDirty(true)

		line := strings.SplitN(m.renderInfoBar(width), "\n", 2)[0]
		if !strings.Contains(line, "Review Changes") {
			t.Errorf("width %d: the button was dropped: %q", width, stripANSI(line))
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
	m.SetGitDirty(true)

	line := strings.SplitN(m.renderInfoBar(160), "\n", 2)[0]
	var region, ok = reviewHit(m)
	if !ok {
		t.Fatal("no review hit region")
	}

	// The rendered line carries ANSI styling, so compare on the stripped text.
	// The comparison must be in *columns*: the info bar contains ← and │, which
	// are three bytes each, so a byte offset from strings.Index sits four
	// columns right of where the glyph is actually drawn.
	plain := stripANSI(line)
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

func hasReviewHit(m *Model) bool {
	_, ok := reviewHit(m)
	return ok
}

func reviewHit(m *Model) (region struct{ X, Width int }, ok bool) {
	for _, hit := range m.actionBarHits {
		if hit.kind == actionBarReviewChanges {
			return struct{ X, Width int }{hit.region.X, hit.region.Width}, true
		}
	}
	return region, false
}

// stripANSI removes escape sequences so a rendered line can be searched by
// visible text.
func stripANSI(s string) string {
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
