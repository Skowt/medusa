package dashboard

import (
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/messages"
)

// TestToolbarFitsOneRow guards the coupling between the toolbar's column count
// and toolbarHeight, which always reports a single row: a wrapped second row
// renders outside the space the layout reserved for the toolbar.
func TestToolbarFitsOneRow(t *testing.T) {
	m := New()
	m.width = 40
	rendered := m.renderToolbar()
	if got := strings.Count(rendered, "\n"); got != 0 {
		t.Errorf("toolbar rendered %d extra rows, want a single row: %q", got, rendered)
	}
	if m.toolbarHeight() != 1 {
		t.Errorf("toolbarHeight = %d, want 1", m.toolbarHeight())
	}
	if got := len(m.toolbarHits); got != len(m.toolbarItems()) {
		t.Errorf("%d hit regions for %d items", got, len(m.toolbarItems()))
	}
}

// TestToolbarShowsSkillUsageButton verifies [U] renders alongside the other
// buttons.
func TestToolbarShowsSkillUsageButton(t *testing.T) {
	m := New()
	m.width = 40
	rendered := m.renderToolbar()
	for _, want := range []string{"[?]", "[M]", "[S]", "[U]"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("toolbar %q missing %s", rendered, want)
		}
	}
}

// TestToolbarSkillUsageCommand verifies the button emits the message the app
// listens for; a nil command would make the button silently inert.
func TestToolbarSkillUsageCommand(t *testing.T) {
	m := New()
	cmd := m.toolbarCommand(toolbarSkillUsage)
	if cmd == nil {
		t.Fatal("toolbarSkillUsage produced no command")
	}
	if _, ok := cmd().(messages.OpenSkillUsage); !ok {
		t.Errorf("command emitted %T, want messages.OpenSkillUsage", cmd())
	}
}

// TestToolbarClickHitsEachButton verifies every button's tracked region routes
// to its own command, so [U] cannot be shadowed by the button beside it.
func TestToolbarClickHitsEachButton(t *testing.T) {
	m := New()
	m.width = 40
	m.renderToolbar()

	hits := append([]toolbarButton(nil), m.toolbarHits...)
	if len(hits) < 4 {
		t.Fatalf("got %d hit regions, want one per button", len(hits))
	}
	for i, hit := range hits {
		// handleToolbarClick works in screen coordinates: undo the border and
		// toolbar offsets it subtracts.
		screenX := hit.region.X + 1
		screenY := hit.region.Y + m.toolbarY + 1
		cmd := m.handleToolbarClick(screenX, screenY)
		if cmd == nil {
			t.Errorf("button %d (%v) at x=%d produced no command", i, hit.kind, screenX)
			continue
		}
		if m.toolbarIndex != i {
			t.Errorf("clicking button %d focused index %d", i, m.toolbarIndex)
		}
	}

	// The last button's region must route to skill usage, matching item order.
	last := hits[len(hits)-1]
	if last.kind != toolbarSkillUsage {
		t.Errorf("last toolbar button is %v, want toolbarSkillUsage", last.kind)
	}
}
