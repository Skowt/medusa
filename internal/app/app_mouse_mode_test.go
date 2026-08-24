package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/ui/layout"
)

// The nudge exists so the requested mouse mode changes at least once after the
// alternate screen is up: bubbletea only writes the enable sequence on a change,
// and it writes it before entering the alt screen, so the enable medusa asks for
// at startup can be dropped on the way in.
func TestMouseModeNudgeReassertsAllMotion(t *testing.T) {
	a := newAppForGroupTests(t)
	a.layout = layout.NewManager()

	if got := a.mouseMode(); got != tea.MouseModeAllMotion {
		t.Fatalf("before the nudge: %v, want all-motion", got)
	}

	cmd := a.beginMouseModeNudge()
	if cmd == nil {
		t.Fatal("the first window size must start the nudge")
	}
	if got := a.mouseMode(); got != tea.MouseModeCellMotion {
		t.Fatalf("during the nudge: %v, want cell-motion so the mode differs", got)
	}

	if _, ok := cmd().(mouseModeSettledMsg); !ok {
		t.Fatalf("nudge command produced %T, want mouseModeSettledMsg", cmd())
	}
	a.mouseModePhase = 2
	if got := a.mouseMode(); got != tea.MouseModeAllMotion {
		t.Fatalf("after the nudge: %v, want all-motion again", got)
	}
}

func TestMouseModeNudgeRunsOnlyOnce(t *testing.T) {
	a := newAppForGroupTests(t)

	if a.beginMouseModeNudge() == nil {
		t.Fatal("first call must start the nudge")
	}
	if cmd := a.beginMouseModeNudge(); cmd != nil {
		t.Error("a resize must not re-nudge: it would drop motion reporting for a frame every time")
	}
	a.mouseModePhase = 2
	if cmd := a.beginMouseModeNudge(); cmd != nil {
		t.Error("nudging again after settling would flap the mode for no reason")
	}
}
