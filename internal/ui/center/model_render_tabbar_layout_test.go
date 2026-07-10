package center

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/vterm"
)

func TestNoteWidth(t *testing.T) {
	tests := []struct {
		name         string
		note         string
		contentWidth int
		want         int
	}{
		{"empty note reserves nothing", "", 120, 0},
		{"short note on a wide bar takes its natural width", "WIP", 120, 3},
		// Below minNoteWidth: reserve the note's own width, do NOT pad to 8.
		{"short note on a narrow bar is not padded", "WIP", 20, 3},
		// 120/3 = 40, note is longer, so cap at 40.
		{"long note on a wide bar is capped at a third", longNote(60), 120, 40},
		// 20/3 = 6, below minNoteWidth, so floor the cap at 8.
		{"long note on a narrow bar is floored at minNoteWidth", longNote(60), 20, 8},
		// Wide glyphs: 4 runes, 8 display cells.
		{"wide glyphs count display width not bytes", "日本語版", 120, 8},
		{"zero content width reserves nothing", "note", 0, 0},
		{"negative content width reserves nothing", "note", -5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := noteWidth(tt.note, tt.contentWidth); got != tt.want {
				t.Errorf("noteWidth(%q, %d) = %d, want %d",
					tt.note, tt.contentWidth, got, tt.want)
			}
		})
	}
}

func longNote(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

func TestVisibleTabs(t *testing.T) {
	// Each tab is 10 cells wide unless the test says otherwise.
	ten := func(n int) []int {
		w := make([]int, n)
		for i := range w {
			w[i] = 10
		}
		return w
	}

	tests := []struct {
		name   string
		widths []int
		avail  int
		offset int
		active int
		want   tabViewport
	}{
		{
			name:   "no tabs",
			widths: nil, avail: 100, offset: 0, active: -1,
			want: tabViewport{start: 0, end: 0, showPrev: false, showNext: false},
		},
		{
			name:   "one tab exact fit",
			widths: ten(1), avail: 10, offset: 0, active: 0,
			want: tabViewport{start: 0, end: 1, showPrev: false, showNext: false},
		},
		{
			name:   "all tabs fit, no arrows",
			widths: ten(3), avail: 100, offset: 0, active: 0,
			want: tabViewport{start: 0, end: 3, showPrev: false, showNext: false},
		},
		{
			// avail 34; showNext costs 2 => 32 for tabs => 3 whole tabs.
			name:   "overflow right only",
			widths: ten(5), avail: 34, offset: 0, active: 0,
			want: tabViewport{start: 0, end: 3, showPrev: false, showNext: true},
		},
		{
			// offset 3 with 5 tabs: showPrev costs 2 => 32 => tabs 3,4 fit (20).
			name:   "overflow left only, scrolled to end",
			widths: ten(5), avail: 34, offset: 3, active: 4,
			want: tabViewport{start: 3, end: 5, showPrev: true, showNext: false},
		},
		{
			// offset 1, both arrows cost 4 => 30 for tabs => 3 whole tabs (1,2,3).
			name:   "overflow both sides",
			widths: ten(6), avail: 34, offset: 1, active: 2,
			want: tabViewport{start: 1, end: 4, showPrev: true, showNext: true},
		},
		{
			// active 0 is left of offset 2 => viewport pulled left to 0.
			name:   "active tab left of start pulls viewport left",
			widths: ten(5), avail: 34, offset: 2, active: 0,
			want: tabViewport{start: 0, end: 3, showPrev: false, showNext: true},
		},
		{
			// active 4 is right of the run starting at 0 => pull right until it
			// fits. offset 2: showPrev(2) + tabs 2,3,4 (30) = 32 <= 34. Stop
			// there — the pull is greedy and takes the leftmost offset that
			// makes active visible, not the tightest one.
			name:   "active tab right of end pulls viewport right",
			widths: ten(5), avail: 34, offset: 0, active: 4,
			want: tabViewport{start: 2, end: 5, showPrev: true, showNext: false},
		},
		{
			// offset past the last tab is clamped to the last index.
			name:   "offset past end is clamped",
			widths: ten(3), avail: 34, offset: 99, active: 2,
			want: tabViewport{start: 2, end: 3, showPrev: true, showNext: false},
		},
		{
			// Nothing fits. Empty range, but both arrows still render so the
			// user can scroll. active is inside [start,end) is impossible here.
			name:   "avail smaller than narrowest tab",
			widths: ten(3), avail: 4, offset: 1, active: 1,
			want: tabViewport{start: 1, end: 1, showPrev: true, showNext: true},
		},
		{
			name:   "info tab active (active == -1) does not force scroll",
			widths: ten(5), avail: 34, offset: 2, active: -1,
			want: tabViewport{start: 2, end: 5, showPrev: true, showNext: false},
		},
		{
			// Arrow-budget discrimination. avail 31: without a next arrow 3
			// tabs (30) would fit, but showing one costs 2, leaving 29 — so
			// only 2 whole tabs fit. An implementation that ignores
			// arrowWidth returns end: 3 here.
			name:   "next arrow reservation drops a tab",
			widths: ten(5), avail: 31, offset: 0, active: 0,
			want: tabViewport{start: 0, end: 2, showPrev: false, showNext: true},
		},
		{
			// Both arrows cost 4 of avail 31, leaving 27 => 2 whole tabs.
			// Ignoring arrowWidth returns end: 4.
			name:   "both arrow reservations drop a tab",
			widths: ten(5), avail: 31, offset: 1, active: 1,
			want: tabViewport{start: 1, end: 3, showPrev: true, showNext: true},
		},
		{
			// Tighter budget: avail 22, both arrows => 18 => 1 whole tab.
			// Ignoring arrowWidth returns end: 3.
			name:   "both arrow reservations on a tight budget",
			widths: ten(5), avail: 22, offset: 1, active: 1,
			want: tabViewport{start: 1, end: 2, showPrev: true, showNext: true},
		},
		{
			// Exact fit with no arrows needed. Guards the opposite bug: an
			// implementation that always reserves for a next arrow would
			// return end: 2 and showNext: true.
			name:   "exact fit reserves nothing for arrows",
			widths: ten(3), avail: 30, offset: 0, active: 0,
			want: tabViewport{start: 0, end: 3, showPrev: false, showNext: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := visibleTabs(tt.widths, tt.avail, tt.offset, tt.active)
			if got != tt.want {
				t.Errorf("visibleTabs(%v, %d, %d, %d) = %+v, want %+v",
					tt.widths, tt.avail, tt.offset, tt.active, got, tt.want)
			}
			// Invariant: an active tab is always visible.
			if tt.active >= 0 && got.end > got.start {
				if tt.active < got.start || tt.active >= got.end {
					t.Errorf("active tab %d outside visible range [%d,%d)",
						tt.active, got.start, got.end)
				}
			}
		})
	}
}

// tabBarModel builds a Model with tabCount agent tabs on a workspace carrying
// the given note, sized to w×h and rendered once so m.tabHits is populated.
//
// data.Workspace.ID is a METHOD returning data.WorkspaceID, not a field, and
// the tab maps are keyed by plain string — hence string(ws.ID()). Construct
// workspaces with newTestWorkspace (internal/ui/center/model_activity_test.go),
// never with a struct literal.
func tabBarModel(t *testing.T, name, note string, tabCount, w, h int) (*Model, *data.Workspace) {
	t.Helper()
	ws := newTestWorkspace(name, "/repo/"+name)
	ws.Note = note

	m := newTestModel()
	id := string(ws.ID())
	tabs := make([]*Tab, tabCount)
	for i := range tabs {
		tabs[i] = &Tab{
			ID:        TabID(fmt.Sprintf("tab-%d", i)),
			Name:      "claude",
			Assistant: "claude",
			Terminal:  vterm.New(80, 24),
		}
	}
	m.tabsByWorkspace[id] = tabs
	m.activeTabByWorkspace[id] = 0

	m.SetWorkspace(ws)
	m.SetSize(w, h)
	_ = m.renderTabBar()
	return m, ws
}

// findHit returns the first hit of the given kind, or nil.
func findHit(m *Model, kind tabHitKind) *tabHit {
	for i := range m.tabHits {
		if m.tabHits[i].kind == kind {
			return &m.tabHits[i]
		}
	}
	return nil
}

func TestRenderTabBarRegistersNoteHit(t *testing.T) {
	m, _ := tabBarModel(t, "ws-note", "fix the auth redirect loop", 1, 120, 40)

	note := findHit(m, tabHitNote)
	if note == nil {
		t.Fatal("a workspace with a note must register a tabHitNote region")
	}
	if note.region.Width <= 0 {
		t.Fatalf("note hit region has non-positive width %d", note.region.Width)
	}
	// The note is right-aligned: its region must end at the content edge.
	if got := note.region.X + note.region.Width; got != m.contentWidth() {
		t.Errorf("note right edge = %d, want contentWidth %d", got, m.contentWidth())
	}
}

func TestRenderTabBarNoNoteRegistersNoNoteHit(t *testing.T) {
	m, _ := tabBarModel(t, "ws-plain", "", 1, 120, 40)

	if findHit(m, tabHitNote) != nil {
		t.Fatal("a workspace without a note must not register a clickable note region")
	}
}

// tabBarFirstLine returns the tab bar's first rendered line (above the separator).
func tabBarFirstLine(m *Model) string {
	line := m.renderTabBar()
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		return line[:i]
	}
	return line
}

// The assembled tab bar must never be wider than the pane. A line wider than
// contentWidth tears the compositor border (cf. commit 163add0).
func TestRenderTabBarNeverExceedsContentWidth(t *testing.T) {
	const note = "fix the auth redirect loop before the demo"
	for _, paneW := range []int{16, 20, 24, 26, 30, 32, 36, 44, 60, 80, 120} {
		for _, tabCount := range []int{0, 1, 2, 5, 8} {
			m, _ := tabBarModel(t, "ws-overflow", note, tabCount, paneW, 40)
			cw := m.contentWidth()

			// The pinned Info tab and "+ New Agent" button are never
			// sacrificed for the note (Finding 1's product decision) and
			// have no responsive narrowing of their own — a separate,
			// pre-existing limitation this fix does not touch. Below their
			// combined footprint the bar necessarily overflows regardless
			// of the note or arrows, so skip those panes to keep this test
			// scoped to the overflow sources 1a/1b actually fix.
			chromeFloor := lipgloss.Width(m.renderInfoTab())
			if m.workspace == nil || !m.workspace.Archived() {
				chromeFloor += lipgloss.Width(m.styles.TabPlus.Render("+ New Agent"))
			}
			if cw < chromeFloor {
				continue
			}

			got := lipgloss.Width(tabBarFirstLine(m))
			if got > cw {
				t.Errorf("pane=%d tabs=%d: rendered width %d exceeds contentWidth %d",
					paneW, tabCount, got, cw)
			}
			if h := findHit(m, tabHitNote); h != nil {
				if end := h.region.X + h.region.Width; end > cw {
					t.Errorf("pane=%d tabs=%d: note hit ends at %d, past contentWidth %d",
						paneW, tabCount, end, cw)
				}
			}
		}
	}
}

// A set note stays at least partially visible wherever any cells remain.
//
// paneW 32 (contentWidth 28) is chosen so the room left after the pinned
// Info+"+ New Agent" chrome (28-21-1=6) sits strictly below minNoteWidth
// (8) — exercising the clamp below its floor. A wider pane like 36
// (contentWidth 32) leaves exactly cap-sized room (10) with no need to
// shrink further, so it wouldn't exercise this path.
func TestRenderTabBarNoteShrinksRatherThanOverflowing(t *testing.T) {
	const note = "fix the auth redirect loop before the demo"
	m, _ := tabBarModel(t, "ws-narrow", note, 2, 32, 40)

	h := findHit(m, tabHitNote)
	if h == nil {
		t.Fatalf("at contentWidth %d there is still room for a shrunken note", m.contentWidth())
	}
	if h.region.Width >= minNoteWidth {
		t.Errorf("note width %d: expected it to shrink below minNoteWidth %d on a narrow pane",
			h.region.Width, minNoteWidth)
	}
	if h.region.Width <= 0 {
		t.Errorf("note width %d: must stay at least partially visible", h.region.Width)
	}
	if end := h.region.X + h.region.Width; end != m.contentWidth() {
		t.Errorf("note right edge %d, want contentWidth %d", end, m.contentWidth())
	}
}

// A note longer than its allocation is truncated, never overflowed.
func TestRenderTabBarTruncatesLongNote(t *testing.T) {
	const note = "fix the auth redirect loop before the demo on friday afternoon"
	m, _ := tabBarModel(t, "ws-long", note, 1, 120, 40)

	h := findHit(m, tabHitNote)
	if h == nil {
		t.Fatal("expected a note hit region")
	}
	if h.region.Width >= lipgloss.Width(note) {
		t.Fatalf("note region %d should be narrower than the full note %d",
			h.region.Width, lipgloss.Width(note))
	}
	if !strings.Contains(tabBarFirstLine(m), "…") {
		t.Error("a truncated note must show an ellipsis")
	}
}

// A note of wide glyphs must not overflow its reserved cells.
func TestRenderTabBarWideGlyphNoteFits(t *testing.T) {
	m, _ := tabBarModel(t, "ws-cjk", "日本語版のテストです", 2, 60, 40)
	cw := m.contentWidth()
	if got := lipgloss.Width(tabBarFirstLine(m)); got > cw {
		t.Errorf("wide-glyph note: rendered width %d exceeds contentWidth %d", got, cw)
	}
}

// Overflowing tabs produce a next arrow; scrolling reveals a prev arrow.
func TestRenderTabBarShowsArrowsOnOverflow(t *testing.T) {
	m, _ := tabBarModel(t, "ws-arrows", "", 8, 60, 40)
	if findHit(m, tabHitNext) == nil {
		t.Fatal("8 tabs in a 60-cell pane must show a next arrow")
	}
	if findHit(m, tabHitPrev) != nil {
		t.Error("at offset 0 there is nothing to the left, so no prev arrow")
	}

	m.tabScrollOffset = 3
	_ = m.renderTabBar()
	if findHit(m, tabHitPrev) == nil {
		t.Error("scrolled right, a prev arrow must appear")
	}
}

// Tabs that all fit produce no arrows at all.
func TestRenderTabBarNoArrowsWhenTabsFit(t *testing.T) {
	m, _ := tabBarModel(t, "ws-fits", "", 1, 120, 40)
	if findHit(m, tabHitPrev) != nil || findHit(m, tabHitNext) != nil {
		t.Error("a single tab on a wide pane needs no scroll arrows")
	}
}

// renderTabBar clamps the scroll offset and writes it back.
func TestRenderTabBarWritesBackScrollOffset(t *testing.T) {
	m, _ := tabBarModel(t, "ws-clamp-render", "", 3, 120, 40)

	m.tabScrollOffset = 99
	_ = m.renderTabBar()
	if m.tabScrollOffset < 0 || m.tabScrollOffset > 2 {
		t.Errorf("offset %d not clamped into [0,2]", m.tabScrollOffset)
	}
}

// The note must never starve the agent tabs: it takes its minimum, the
// viewport takes what it needs, and only then does the note grow.
func TestNoteDoesNotStarveTabs(t *testing.T) {
	const note = "Fix the auth redirect loop"
	// contentWidth >= 44: the note's full 8-cell floor and a tab both fit.
	for _, paneW := range []int{48, 52, 56, 60, 80} {
		m, _ := tabBarModel(t, "ws-starve", note, 4, paneW, 40)

		var tabs int
		for _, h := range m.tabHits {
			if h.kind == tabHitTab {
				tabs++
			}
		}
		if tabs == 0 {
			t.Errorf("pane=%d (cw=%d): a note must not hide every agent tab",
				paneW, m.contentWidth())
		}
	}
}

// On a pane where the tabs need the room, the note keeps only its minimum.
func TestNoteKeepsOnlyMinimumWhenTabsNeedRoom(t *testing.T) {
	const note = "Fix the auth redirect loop"
	m, _ := tabBarModel(t, "ws-min", note, 4, 48, 40)

	h := findHit(m, tabHitNote)
	if h == nil {
		t.Fatal("a set note must stay at least partially visible")
	}
	if h.region.Width != minNoteWidth {
		t.Errorf("note width %d, want minNoteWidth %d when tabs need the room",
			h.region.Width, minNoteWidth)
	}
}

// On a wide pane the note grows to its full width (under the one-third cap),
// exactly as before this change.
func TestNoteGrowsWhenThereIsSpare(t *testing.T) {
	const note = "Fix the auth redirect loop"
	m, _ := tabBarModel(t, "ws-grow", note, 2, 120, 40)

	h := findHit(m, tabHitNote)
	if h == nil {
		t.Fatal("expected a note hit region")
	}
	if want := lipgloss.Width(note); h.region.Width != want {
		t.Errorf("note width %d, want the full note %d on a wide pane",
			h.region.Width, want)
	}
}

// Growing the note must never push the tabs into needing scroll arrows.
func TestNoteGrowthNeverForcesArrows(t *testing.T) {
	const note = "Fix the auth redirect loop before the demo on friday afternoon"
	for _, paneW := range []int{80, 100, 120, 160} {
		m, _ := tabBarModel(t, "ws-noarrows", note, 2, paneW, 40)
		if findHit(m, tabHitPrev) != nil || findHit(m, tabHitNext) != nil {
			t.Errorf("pane=%d: two tabs fit, so note growth must not create arrows", paneW)
		}
	}
}
