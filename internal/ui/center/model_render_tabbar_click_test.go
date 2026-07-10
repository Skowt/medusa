package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/messages"
)

func TestClickNoteOpensSetNoteDialog(t *testing.T) {
	m, ws := tabBarModel(t, "ws-note", "fix the auth redirect loop", 1, 120, 40)

	note := findHit(m, tabHitNote)
	if note == nil {
		t.Fatal("expected a tabHitNote region")
	}

	cmd := m.dispatchTabHit(note.region.X + 1)
	if cmd == nil {
		t.Fatal("clicking the note must return a command")
	}
	msg := cmd()
	dlg, ok := msg.(messages.ShowSetWorkspaceNoteDialog)
	if !ok {
		t.Fatalf("clicking the note returned %T, want ShowSetWorkspaceNoteDialog", msg)
	}
	if dlg.Workspace != ws {
		t.Error("dialog must carry the active workspace")
	}
}

func TestClickArrowsScrollViewport(t *testing.T) {
	// 8 tabs in a 60-cell pane: guarantees overflow.
	m, _ := tabBarModel(t, "ws-many", "", 8, 60, 40)

	next := findHit(m, tabHitNext)
	if next == nil {
		t.Fatal("8 tabs in a 60-cell pane must produce a next arrow")
	}

	before := m.tabScrollOffset
	if cmd := m.dispatchTabHit(next.region.X); cmd != nil {
		t.Error("arrow clicks are pure view state and must return nil")
	}
	if m.tabScrollOffset != before+1 {
		t.Errorf("next arrow: offset %d, want %d", m.tabScrollOffset, before+1)
	}

	// Re-render so the prev arrow now exists, then scroll back.
	_ = m.renderTabBar()
	prev := findHit(m, tabHitPrev)
	if prev == nil {
		t.Fatal("after scrolling right, a prev arrow must appear")
	}
	m.dispatchTabHit(prev.region.X)
	if m.tabScrollOffset != before {
		t.Errorf("prev arrow: offset %d, want %d", m.tabScrollOffset, before)
	}
}

// Scrolling must never run off either end.
func TestArrowScrollClamps(t *testing.T) {
	m, _ := tabBarModel(t, "ws-clamp", "", 1, 120, 40)

	m.scrollTabs(-1)
	if m.tabScrollOffset != 0 {
		t.Errorf("scrolling left at offset 0 gave %d, want 0", m.tabScrollOffset)
	}
	m.scrollTabs(1)
	if m.tabScrollOffset != 0 {
		t.Errorf("scrolling right with one tab gave %d, want 0", m.tabScrollOffset)
	}
}

// The bug this guards: renderTabBar runs on every View(), and an
// unconditional pull-to-active would undo a manual arrow scroll on the very
// next repaint — making the arrows inert for anyone on an agent tab.
func TestArrowScrollSurvivesRepaint(t *testing.T) {
	m, _ := tabBarModel(t, "ws-repaint", "", 8, 60, 40)
	if m.infoTabActive {
		t.Fatal("precondition: an agent tab must be active")
	}

	next := findHit(m, tabHitNext)
	if next == nil {
		t.Fatal("8 tabs in a 60-cell pane must show a next arrow")
	}
	m.dispatchTabHit(next.region.X)
	afterClick := m.tabScrollOffset
	if afterClick == 0 {
		t.Fatal("clicking the next arrow must advance the offset")
	}

	_ = m.renderTabBar() // any repaint: PTY tick, animation, resize
	if m.tabScrollOffset != afterClick {
		t.Errorf("repaint reverted the manual scroll: offset %d, want %d",
			m.tabScrollOffset, afterClick)
	}
}

// Changing the active tab still pulls the viewport to reveal it.
func TestActiveTabChangePullsViewport(t *testing.T) {
	m, _ := tabBarModel(t, "ws-pull", "", 8, 60, 40)

	// Scroll the strip away from the active tab (index 0).
	m.tabScrollOffset = 4
	_ = m.renderTabBar()
	if m.tabScrollOffset != 4 {
		t.Fatalf("manual scroll should persist, got offset %d", m.tabScrollOffset)
	}

	// Selecting a different tab pulls the viewport to it.
	m.setActiveTabIdx(1)
	_ = m.renderTabBar()
	if m.tabScrollOffset > 1 {
		t.Errorf("selecting tab 1 must pull the viewport to reveal it, offset %d",
			m.tabScrollOffset)
	}
}

// Close-button regions overlap their tab's region, so dispatchTabHit scans
// them in a separate first pass. Merging the passes would make clicking the
// × select the tab instead of closing it.
func TestClickCloseBeatsTabSelection(t *testing.T) {
	m, _ := tabBarModel(t, "ws-close", "", 2, 120, 40)

	closeHit := findHit(m, tabHitClose)
	if closeHit == nil {
		t.Fatal("expected a close-button hit region")
	}
	x := closeHit.region.X

	// Sanity: that x really does fall inside a tab region too, otherwise this
	// test would pass even with the passes merged.
	var overlapped bool
	for _, h := range m.tabHits {
		if h.kind == tabHitTab && h.region.Contains(x, 0) {
			overlapped = true
		}
	}
	if !overlapped {
		t.Fatal("close region must overlap a tab region for this test to mean anything")
	}

	cmd := m.dispatchTabHit(x)
	if cmd == nil {
		t.Fatal("clicking the close button must return a command")
	}
	if _, ok := cmd().(messages.CloseTabAt); !ok {
		t.Errorf("clicking × dispatched %T, want messages.CloseTabAt", cmd())
	}
}

// Visiting the Info tab and returning to the SAME agent tab is not a change of
// active tab, so it must not force-pull the viewport and discard a manual
// scroll. Regression: renderTabBar used to record activeID ("" while the Info
// tab is active), making every return look like a change.
func TestInfoTabRoundTripPreservesScroll(t *testing.T) {
	m, _ := tabBarModel(t, "ws-info-detour", "", 8, 60, 40)

	m.setActiveTabIdx(2)
	_ = m.renderTabBar() // pull settles on tab 2

	m.tabScrollOffset = 5 // user scrolls away
	_ = m.renderTabBar()
	if m.tabScrollOffset != 5 {
		t.Fatalf("manual scroll should persist, got offset %d", m.tabScrollOffset)
	}

	m.infoTabActive = true // click the Info tab
	_ = m.renderTabBar()

	m.infoTabActive = false // click back onto the same agent tab
	_ = m.renderTabBar()

	if m.tabScrollOffset != 5 {
		t.Errorf("returning to the same tab must not pull the viewport; offset %d, want 5",
			m.tabScrollOffset)
	}
}

// Returning from the Info tab to a DIFFERENT agent tab must still pull.
func TestInfoTabRoundTripToDifferentTabPulls(t *testing.T) {
	m, _ := tabBarModel(t, "ws-info-switch", "", 8, 60, 40)

	m.setActiveTabIdx(2)
	_ = m.renderTabBar()
	m.tabScrollOffset = 5
	_ = m.renderTabBar()

	m.infoTabActive = true
	_ = m.renderTabBar()

	// Select a different agent tab while returning.
	m.infoTabActive = false
	m.setActiveTabIdx(0)
	_ = m.renderTabBar()

	if m.tabScrollOffset != 0 {
		t.Errorf("selecting a different tab must pull the viewport to it; offset %d, want 0",
			m.tabScrollOffset)
	}
}

// Closing the active tab slides a different tab into the same index. The
// viewport must reveal it, even though activeTabIdx is numerically unchanged.
// An index-based "did the active tab change?" check misses this.
func TestCloseActiveTabRevealsReplacement(t *testing.T) {
	m, ws := tabBarModel(t, "ws-close-active", "", 8, 60, 40)
	id := string(ws.ID())

	m.setActiveTabIdx(3)
	_ = m.renderTabBar() // pull settles on tab 3

	m.tabScrollOffset = 5 // user scrolls away from the active tab
	_ = m.renderTabBar()
	if m.tabScrollOffset != 5 {
		t.Fatalf("manual scroll should persist, got offset %d", m.tabScrollOffset)
	}

	// Close slot 3: a different tab takes that index, activeTabIdx stays 3.
	tabs := m.tabsByWorkspace[id]
	m.tabsByWorkspace[id] = append(tabs[:3], tabs[4:]...)
	m.activeTabByWorkspace[id] = 3

	_ = m.renderTabBar()
	if m.tabScrollOffset > 3 {
		t.Errorf("closing the active tab must reveal its replacement; offset %d leaves index 3 off-screen",
			m.tabScrollOffset)
	}
}
