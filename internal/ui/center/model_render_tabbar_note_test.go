package center

import "testing"

// A set note is never hidden entirely, at any width where the chrome leaves
// even one cell for it. The user must always know a note exists.
func TestNoteNeverFullyHidden(t *testing.T) {
	const note = "Fix the auth redirect loop"
	for _, paneW := range []int{28, 32, 36, 40, 44, 48, 60, 80, 120} {
		m, _ := tabBarModel(t, "ws-visible", note, 4, paneW, 40)
		cw := m.contentWidth()
		if cw < 23 {
			continue // chrome (21) + gap (1) leaves no cell for the note
		}
		h := findHit(m, tabHitNote)
		if h == nil || h.region.Width < 1 {
			t.Errorf("pane=%d (cw=%d): a set note must stay at least 1 cell wide", paneW, cw)
		}
	}
}

// Above the 14-cell viewport threshold, the note yields cells so a tab renders.
func TestNoteYieldsToFirstTab(t *testing.T) {
	const note = "Fix the auth redirect loop"
	// contentWidth 37 is the first width where nWidth=1 leaves avail=14.
	for _, paneW := range []int{41, 44, 48, 60} {
		m, _ := tabBarModel(t, "ws-yield", note, 4, paneW, 40)
		var tabs int
		for _, h := range m.tabHits {
			if h.kind == tabHitTab {
				tabs++
			}
		}
		if tabs == 0 {
			t.Errorf("pane=%d (cw=%d): the note must yield so an agent tab renders",
				paneW, m.contentWidth())
		}
	}
}

// Where no visible note can leave room for a tab, the note keeps its floor
// rather than shrinking for nothing.
func TestNoteKeepsFloorWhenNoTabCanFit(t *testing.T) {
	const note = "Fix the auth redirect loop"
	m, _ := tabBarModel(t, "ws-nofit", note, 4, 40, 40) // contentWidth 36

	if cw := m.contentWidth(); cw != 36 {
		t.Fatalf("fixture drifted: contentWidth %d, want 36", cw)
	}
	var tabs int
	for _, h := range m.tabHits {
		if h.kind == tabHitTab {
			tabs++
		}
	}
	if tabs != 0 {
		t.Fatalf("contentWidth 36 cannot fit a tab alongside any visible note, got %d tabs", tabs)
	}
	h := findHit(m, tabHitNote)
	if h == nil {
		t.Fatal("note must remain visible")
	}
	if h.region.Width != minNoteWidth {
		t.Errorf("note width %d, want its %d-cell floor when shrinking would buy nothing",
			h.region.Width, minNoteWidth)
	}
}
