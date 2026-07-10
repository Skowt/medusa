# Tab Bar Note + Scrollable Tabs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Workspaces always open on their agent tab; a workspace note renders right-aligned in the tab bar, and agent tabs become horizontally scrollable with `‹` `›` arrows.

**Architecture:** Extract the tab bar's width arithmetic into pure integer functions in a new sibling file (`model_render_tabbar_layout.go`), leaving `renderTabBar()` as a composer of four zones: pinned Info tab, scrollable agent-tab viewport, pinned `+ New Agent`, right-aligned note. Scroll position is one `int` on `Model`, clamped every render so the active tab is always visible — which makes keyboard tab-cycling auto-scroll with zero changes to nav code.

**Tech Stack:** Go, Bubble Tea v2, lipgloss v2, `github.com/charmbracelet/x/ansi` (width-aware truncation).

**Spec:** `docs/superpowers/specs/2026-07-10-tabbar-note-and-scroll-design.md`

## Global Constraints

- No `.go` file may exceed **500 lines**. `make lint` enforces this.
- `make fmt` (gofmt + goimports) before any commit.
- `golangci-lint run ./internal/ui/center/...` must exit 0.
- Tests use the standard library `testing` package, table-driven where the spec calls for tables. This is Go — the repo-wide "all tests use pytest" instruction does not apply here.
- Commit prefixes are load-bearing for `.goreleaser.yml` changelog filters: `feat:` / `fix:` / `refactor:` / `perf:` surface in release notes; `docs:` / `test:` / `ci:` / `chore:` do not.
- Display width must be computed with `lipgloss.Width` or `ansi.StringWidth`, never `len()`. Notes are free text and may contain wide glyphs.
- `minNoteWidth = 8` cells. `noteCap = max(contentWidth/3, minNoteWidth)`.
- Arrow glyphs are `‹` and `›`, each rendered with a trailing space (2 cells each).

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/ui/center/model_info.go` (modify) | `SetWorkspace` no longer forces the Info tab when a note exists. |
| `internal/ui/center/model.go` (modify) | Add `tabScrollOffset int` field; add `tabHitNote` to `tabHitKind`. |
| `internal/ui/center/model_render_tabbar_layout.go` (create) | Pure layout arithmetic: `noteWidth`, `visibleTabs`. No `Model`, no lipgloss rendering. |
| `internal/ui/center/model_render_tabbar_layout_test.go` (create) | Table tests for the above. |
| `internal/ui/center/model_render_tabbar.go` (modify) | `renderTabBar()` composes zones; `handleTabBarClick()` / `dispatchTabHit()` handle arrows + note. |
| `internal/ui/center/model_render_tabbar_tabs.go` (create) | `renderInfoTab()` and `renderAgentTab()` — per-segment styling, lifted out of `renderTabBar` to stay under the 500-line gate. |
| `internal/ui/center/model_info_test.go` (create) | `SetWorkspace` regression tests. |

`tabHitPrev` and `tabHitNext` already exist in `model.go:219-220`, declared but referenced nowhere. Reuse them; do not add new names for the arrows.

---

### Task 1: Remove the note-based Info-tab default

**Files:**
- Modify: `internal/ui/center/model_info.go:33-38`
- Test: `internal/ui/center/model_info_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing new. Changes the behaviour of the existing `func (m *Model) SetWorkspace(ws *data.Workspace)`.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/center/model_info_test.go`:

```go
package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
)

// setWorkspaceModel builds a Model with one agent tab registered for ws,
// which is the precondition for the Info tab NOT being auto-selected.
func setWorkspaceModel(t *testing.T, ws *data.Workspace) *Model {
	t.Helper()
	m := newTestModel()
	m.tabsByWorkspace[ws.ID] = []*Tab{{ID: TabID("tab-1"), Name: "claude"}}
	m.activeTabByWorkspace[ws.ID] = 0
	return m
}

func TestSetWorkspaceWithNoteDoesNotSelectInfoTab(t *testing.T) {
	ws := &data.Workspace{ID: "ws-1", Note: "fix the auth redirect loop"}
	m := setWorkspaceModel(t, ws)

	m.SetWorkspace(ws)

	if m.infoTabActive {
		t.Fatal("workspace with a note must open on the agent tab, not Info")
	}
	if m.IsInfoTabActive() {
		t.Fatal("IsInfoTabActive() must be false when the workspace has agent tabs")
	}
}

func TestSetWorkspaceWithoutNoteDoesNotSelectInfoTab(t *testing.T) {
	ws := &data.Workspace{ID: "ws-2"}
	m := setWorkspaceModel(t, ws)

	m.SetWorkspace(ws)

	if m.infoTabActive {
		t.Fatal("workspace without a note must open on the agent tab")
	}
}

// The Info tab must still auto-activate when there are no agent tabs.
// This path lives in IsInfoTabActive(), not in the infoTabActive field.
func TestSetWorkspaceWithNoTabsStillShowsInfo(t *testing.T) {
	ws := &data.Workspace{ID: "ws-3", Note: "some note"}
	m := newTestModel() // no tabs registered

	m.SetWorkspace(ws)

	if !m.IsInfoTabActive() {
		t.Fatal("a workspace with no agent tabs must fall back to the Info tab")
	}
}
```

Check `newTestModel()` in `internal/ui/center/model_activity_test.go:11` — if it does not initialise `tabsByWorkspace` / `activeTabByWorkspace`, initialise those maps in `setWorkspaceModel` before assigning.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/center -run TestSetWorkspace -v`

Expected: `TestSetWorkspaceWithNoteDoesNotSelectInfoTab` FAILS with "workspace with a note must open on the agent tab, not Info". The other two PASS.

- [ ] **Step 3: Write minimal implementation**

In `internal/ui/center/model_info.go`, replace:

```go
// SetWorkspace sets the active workspace.
// If the workspace has a note, stay on the Info tab so the user sees it first.
func (m *Model) SetWorkspace(ws *data.Workspace) {
	m.workspace = ws
	m.infoCursor = 0
	m.infoTabActive = ws != nil && ws.Note != ""
```

with:

```go
// SetWorkspace sets the active workspace.
func (m *Model) SetWorkspace(ws *data.Workspace) {
	m.workspace = ws
	m.infoCursor = 0
	m.infoTabActive = false
```

Leave the rest of the function (the `reconcileTerminalSizes` block and its comment) exactly as it is.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/center -run TestSetWorkspace -v`
Expected: all three PASS.

Run: `go test ./internal/ui/center`
Expected: PASS. If an existing test asserted the old redirect, delete that assertion — it encoded the behaviour we are removing.

- [ ] **Step 5: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/ui/center/...
git add internal/ui/center/model_info.go internal/ui/center/model_info_test.go
git commit -m "feat: always open workspaces on the agent tab"
```

---

### Task 2: Pure layout arithmetic — `noteWidth`

**Files:**
- Create: `internal/ui/center/model_render_tabbar_layout.go`
- Test: `internal/ui/center/model_render_tabbar_layout_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func noteWidth(note string, contentWidth int) int` — returns the number of cells to reserve for the note, `0` when `note == ""`. Task 4 calls this.
- Produces: `const minNoteWidth = 8`.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/center/model_render_tabbar_layout_test.go`:

```go
package center

import "testing"

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/center -run TestNoteWidth -v`
Expected: FAIL to compile — `undefined: noteWidth`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/ui/center/model_render_tabbar_layout.go`:

```go
package center

import (
	"charm.land/lipgloss/v2"
)

// minNoteWidth is the fewest cells a non-empty note may be allocated before
// the tab viewport gets any width. It is a floor on the cap, not on the
// allocation: a three-cell note takes three cells, not eight.
const minNoteWidth = 8

// noteWidth returns the cells to reserve for the workspace note in the tab
// bar, or 0 when there is no note. A set note always receives at least
// min(displayWidth(note), minNoteWidth) cells so it is never fully hidden.
func noteWidth(note string, contentWidth int) int {
	if note == "" || contentWidth <= 0 {
		return 0
	}

	cap := contentWidth / 3
	if cap < minNoteWidth {
		cap = minNoteWidth
	}

	w := lipgloss.Width(note)
	if w > cap {
		return cap
	}
	return w
}
```

Note: `cap` shadows the builtin. golangci-lint may flag it under `predeclared`. If it does, rename the local to `maxWidth`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/center -run TestNoteWidth -v`
Expected: all subtests PASS.

If `"日本語版"` fails with `got 4`, `lipgloss.Width` is not doing what we need — switch to `ansi.StringWidth` from `github.com/charmbracelet/x/ansi` and re-run.

- [ ] **Step 5: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/ui/center/...
git add internal/ui/center/model_render_tabbar_layout.go internal/ui/center/model_render_tabbar_layout_test.go
git commit -m "feat: add tab bar note width allocation"
```

---

### Task 3: Pure layout arithmetic — `visibleTabs`

**Files:**
- Modify: `internal/ui/center/model_render_tabbar_layout.go`
- Test: `internal/ui/center/model_render_tabbar_layout_test.go`

**Interfaces:**
- Consumes: nothing from Task 2 (same file, independent function).
- Produces:

```go
type tabViewport struct {
    start, end int  // visible tab range, [start, end)
    showPrev   bool
    showNext   bool
}

func visibleTabs(widths []int, avail, offset, active int) tabViewport
```

Task 4 calls this. `widths[i]` is the rendered cell width of agent tab `i`. `avail` is the cells left for the viewport after pinned chrome and the note. `offset` is the current `m.tabScrollOffset`. `active` is `m.getActiveTabIdx()`, or `-1` when the Info tab is active.

Contract:
- Only whole tabs appear in `[start, end)`. A tab is never partially drawn.
- `showPrev == (start > 0)`, `showNext == (end < len(widths))`.
- When arrows show, they consume `arrowWidth` cells each *from `avail`*, so the returned range accounts for them.
- If `active >= 0`, the returned range always contains `active`.
- `offset` is clamped into `[0, len(widths)-1]` before use.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/center/model_render_tabbar_layout_test.go`:

```go
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
		name     string
		widths   []int
		avail    int
		offset   int
		active   int
		want     tabViewport
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
```

Verify the arithmetic in each `want` by hand before running — these numbers are the specification. Where a comment states the cell math, it is the derivation.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/center -run TestVisibleTabs -v`
Expected: FAIL to compile — `undefined: visibleTabs`, `undefined: tabViewport`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/ui/center/model_render_tabbar_layout.go`:

```go
// arrowWidth is the cells consumed by one scroll arrow plus its trailing space.
const arrowWidth = 2

// tabViewport describes which agent tabs are visible in the scrollable strip.
type tabViewport struct {
	start, end int // visible range, [start, end)
	showPrev   bool
	showNext   bool
}

// visibleTabs picks the largest run of whole tabs starting at offset that fits
// in avail cells, reserving arrowWidth for each arrow it decides to show.
//
// Tabs are never partially drawn: close-button hit regions are anchored from a
// tab's right edge, so a clipped tab would place a clickable × outside the
// viewport.
//
// When active >= 0 the returned range is guaranteed to contain it, which is
// what makes keyboard tab-cycling scroll the viewport into view.
func visibleTabs(widths []int, avail, offset, active int) tabViewport {
	n := len(widths)
	if n == 0 {
		return tabViewport{}
	}

	// Clamp the requested offset into range.
	if offset < 0 {
		offset = 0
	}
	if offset > n-1 {
		offset = n - 1
	}

	// Pull the viewport to the active tab. Scrolling left is a direct
	// assignment; scrolling right needs the run computed backwards from
	// active so that active is the last whole tab that fits.
	if active >= 0 && active < n {
		if active < offset {
			offset = active
		} else {
			for fitRun(widths, avail, offset, n) <= active {
				if offset >= active {
					break
				}
				offset++
			}
		}
	}

	end := fitRun(widths, avail, offset, n)

	return tabViewport{
		start:    offset,
		end:      end,
		showPrev: offset > 0,
		showNext: end < n,
	}
}

// fitRun returns the exclusive end index of the largest run of whole tabs
// starting at start that fits in avail, accounting for the arrows that such a
// run would require.
func fitRun(widths []int, avail, start, n int) int {
	budget := avail
	if start > 0 {
		budget -= arrowWidth // showPrev
	}

	// First pass: assume no next arrow.
	end := start
	used := 0
	for end < n && used+widths[end] <= budget {
		used += widths[end]
		end++
	}
	if end == n {
		return end
	}

	// A next arrow is needed. Re-fit against the smaller budget.
	budget -= arrowWidth
	end = start
	used = 0
	for end < n && used+widths[end] <= budget {
		used += widths[end]
		end++
	}
	return end
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/center -run TestVisibleTabs -v`
Expected: all subtests PASS.

Two properties worth understanding rather than debugging blind:

*`showNext = end < n` is correct even for an empty range.* `fitRun` returns `end == n` only when every tab from `start` onward fits. When nothing fits it returns `end == start < n`, so `showNext` is `true` — the user can still scroll.

*Over-scrolling is impossible through the UI.* `showNext` goes false exactly when the last tab becomes visible, so the `›` arrow disappears at the rightmost useful offset. The `offset > n-1` clamp is a safety net for programmatic offsets (e.g. a tab closing beneath a scrolled strip), not the mechanism that stops the user.

- [ ] **Step 5: Run the full package with the race detector**

Run: `go test -race ./internal/ui/center`
Expected: PASS. These are pure functions, but the package has goroutine-backed tabs and the race detector is the repo's gate.

- [ ] **Step 6: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/ui/center/...
git add internal/ui/center/model_render_tabbar_layout.go internal/ui/center/model_render_tabbar_layout_test.go
git commit -m "feat: add tab bar scroll viewport arithmetic"
```

---

### Task 4: Wire the layout into `renderTabBar`

**Files:**
- Modify: `internal/ui/center/model.go` (add `tabScrollOffset` field; add `tabHitNote` const)
- Modify: `internal/ui/center/model_render_tabbar.go` (rewrite `renderTabBar`)
- Modify: `internal/ui/center/model_info.go` (reset `tabScrollOffset` in `SetWorkspace`)

**Interfaces:**
- Consumes: `noteWidth(note string, contentWidth int) int` and `visibleTabs(widths []int, avail, offset, active int) tabViewport` from Tasks 2–3. `tabHitPrev` / `tabHitNext` from `model.go:219-220`.
- Produces: `m.tabScrollOffset int`; hit regions of kind `tabHitPrev`, `tabHitNext`, `tabHitNote` in `m.tabHits`. Task 5 handles clicks on them.

- [ ] **Step 1: Add the Model field and hit kind**

In `internal/ui/center/model.go`, in the `// Info tab (virtual tab for workspace info)` block around line 163, add below `infoCursor int`:

```go
	// Tab strip horizontal scroll (index of the leftmost visible agent tab)
	tabScrollOffset int
```

In the `tabHitKind` const block at `model.go:214-221`, append `tabHitNote` after `tabHitNext`:

```go
const (
	tabHitTab tabHitKind = iota
	tabHitClose
	tabHitPlus
	tabHitInfo
	tabHitPrev
	tabHitNext
	tabHitNote
)
```

Do not reorder the existing constants — `selection_test.go` logs `kind=%d`, and reordering silently changes what old failures mean.

- [ ] **Step 2: Reset scroll on workspace switch**

In `internal/ui/center/model_info.go`, in `SetWorkspace`, below `m.infoTabActive = false`:

```go
	m.tabScrollOffset = 0
```

- [ ] **Step 3: Write the failing test**

Append to `internal/ui/center/model_render_tabbar_layout_test.go`:

```go
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
			ID:        TabID("tab"),
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
```

Add imports `"github.com/Skowt/medusa/internal/data"` and `"github.com/Skowt/medusa/internal/vterm"` to the test file.

`newTestWorkspace(name, root string) *data.Workspace` already exists at `internal/ui/center/model_activity_test.go:21` and wraps `data.NewWorkspace(name, "", "", root, root)`. Reuse it; do not add a second workspace constructor.

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/ui/center -run TestRenderTabBar -v`
Expected: `TestRenderTabBarRegistersNoteHit` FAILS with "a workspace with a note must register a tabHitNote region".

- [ ] **Step 5: Rewrite `renderTabBar`**

Replace the body of `renderTabBar()` in `internal/ui/center/model_render_tabbar.go`. Keep the existing per-tab rendering logic (indicator selection, active/inactive styling, close-button geometry) verbatim — extract it into a helper `renderAgentTab(i int, tab *Tab, activeIdx int) string` that returns the rendered string, so `renderTabBar` can render each tab twice: once to measure its width, once for real. Measuring by rendering is correct here because the render is pure.

```go
// renderTabBar renders the tab bar: pinned Info tab, a horizontally
// scrollable strip of agent tabs, a pinned "+ New Agent" button, and the
// workspace note right-aligned at the content edge.
func (m *Model) renderTabBar() string {
	m.tabHits = m.tabHits[:0]
	currentTabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if m.infoTabActive {
		activeIdx = -1
	}
	width := m.contentWidth()

	// --- Zone 1: pinned Info tab ---
	infoRendered := m.renderInfoTab()
	infoWidth := lipgloss.Width(infoRendered)

	// --- Zone 3: pinned "+ New Agent" (omitted for archived workspaces) ---
	var plusRendered string
	if m.workspace == nil || !m.workspace.Archived() {
		plusRendered = m.styles.TabPlus.Render("+ New Agent")
	}
	plusWidth := lipgloss.Width(plusRendered)

	// --- Zone 4: note reservation ---
	var note string
	if m.workspace != nil {
		note = m.workspace.Note
	}
	nWidth := noteWidth(note, width)
	gap := 0
	if nWidth > 0 {
		gap = 1 // one cell between "+ New Agent" and the note
	}

	// --- Zone 2: whatever is left ---
	avail := width - infoWidth - plusWidth - nWidth - gap
	if avail < 0 {
		avail = 0
	}

	// Measure each agent tab, then pick the visible run.
	widths := make([]int, len(currentTabs))
	rendered := make([]string, len(currentTabs))
	for i, tab := range currentTabs {
		rendered[i] = m.renderAgentTab(i, tab, activeIdx)
		widths[i] = lipgloss.Width(rendered[i])
	}
	vp := visibleTabs(widths, avail, m.tabScrollOffset, activeIdx)
	m.tabScrollOffset = vp.start

	// --- Assemble left-to-right, tracking hit regions against x. ---
	var segments []string
	x := 0

	m.addHit(tabHitInfo, -1, x, infoWidth)
	segments = append(segments, infoRendered)
	x += infoWidth

	if vp.showPrev {
		arrow := m.styles.Muted.Render("‹ ")
		m.addHit(tabHitPrev, -1, x, arrowWidth)
		segments = append(segments, arrow)
		x += arrowWidth
	}

	for i := vp.start; i < vp.end; i++ {
		m.addHit(tabHitTab, i, x, widths[i])
		m.addCloseHit(i, x, widths[i])
		segments = append(segments, rendered[i])
		x += widths[i]
	}

	if vp.showNext {
		arrow := m.styles.Muted.Render("› ")
		m.addHit(tabHitNext, -1, x, arrowWidth)
		segments = append(segments, arrow)
		x += arrowWidth
	}

	if plusWidth > 0 {
		m.addHit(tabHitPlus, -1, x, plusWidth)
		segments = append(segments, plusRendered)
		x += plusWidth
	}

	// --- Right-align the note by padding out to the content edge. ---
	if nWidth > 0 {
		pad := width - x - nWidth
		if pad < 0 {
			pad = 0
		}
		segments = append(segments, strings.Repeat(" ", pad))
		x += pad

		noteStyle := lipgloss.NewStyle().Foreground(common.ColorPrimary)
		truncated := ansi.Truncate(note, nWidth, "…")
		m.addHit(tabHitNote, -1, x, nWidth)
		segments = append(segments, noteStyle.Render(truncated))
	}

	tabLine := lipgloss.JoinHorizontal(lipgloss.Bottom, segments...)

	separatorStyle := lipgloss.NewStyle().Foreground(common.ColorSurface2)
	separatorLine := separatorStyle.Render(strings.Repeat("─", width))

	return tabLine + "\n" + separatorLine
}

// addHit appends a single-row hit region at x of the given width.
func (m *Model) addHit(kind tabHitKind, index, x, w int) {
	if w <= 0 {
		return
	}
	m.tabHits = append(m.tabHits, tabHit{
		kind:   kind,
		index:  index,
		region: common.HitRegion{X: x, Y: 0, Width: w, Height: 1},
	})
}
```

`addCloseHit(i, x, renderedWidth)` moves the existing close-button geometry out of the old loop verbatim:

```go
// addCloseHit registers the × region, anchored from the tab's right edge.
func (m *Model) addCloseHit(i, x, renderedWidth int) {
	style := m.styles.Tab
	if i == m.getActiveTabIdx() && !m.infoTabActive {
		style = m.styles.ActiveTab
	}
	frameX, _ := style.GetFrameSize()
	leftFrame := frameX / 2

	closeWidth := lipgloss.Width(m.styles.Muted.Render("×")) + 1
	closeX := x + renderedWidth - leftFrame - closeWidth
	if closeX > x {
		m.tabHits = append(m.tabHits, tabHit{
			kind:   tabHitClose,
			index:  i,
			region: common.HitRegion{X: closeX, Y: 0, Width: renderedWidth - (closeX - x), Height: 1},
		})
	}
}
```

The two rendering helpers below are lifted from the current `renderTabBar` body with one change each, noted in their comments. Put them in `model_render_tabbar_tabs.go` (see Step 7 — the 500-line gate makes this near-certain anyway):

```go
package center

import (
	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/ui/common"
)

// renderInfoTab renders the pinned virtual Info tab.
func (m *Model) renderInfoTab() string {
	const infoLabel = "Info"
	indicator := common.Icons.Running + " "

	if m.infoTabActive {
		bg := common.ColorSurface2
		pad := lipgloss.NewStyle().Background(bg).Render(" ")
		indicatorPart := lipgloss.NewStyle().Foreground(common.ColorInfo).Background(bg).Render(indicator)
		namePart := lipgloss.NewStyle().Foreground(common.ColorForeground).Background(bg).Render(infoLabel)
		return pad + indicatorPart + namePart + pad
	}

	indicatorStyled := lipgloss.NewStyle().Foreground(common.ColorInfo).Render(indicator)
	return m.styles.Tab.Render(indicatorStyled + m.styles.Muted.Render(infoLabel))
}

// renderAgentTab renders a single agent tab.
//
// activeIdx is -1 when the Info tab is selected, so the active branch tests
// only `i == activeIdx` — the old `&& !m.infoTabActive` guard moved into the
// caller.
func (m *Model) renderAgentTab(i int, tab *Tab, activeIdx int) string {
	name := tab.Name
	if name == "" {
		name = tab.Assistant
	}

	tab.mu.Lock()
	tabDisconnected := tab.Detached || !tab.Running
	tab.mu.Unlock()

	var indicator string
	var tabActive bool
	isChat := m.isChatTab(tab)
	isScript := tab.Assistant == "script"
	if isChat || isScript {
		if tabDisconnected {
			indicator = common.Icons.Idle + " "
		} else {
			indicator = common.Icons.Running + " "
		}
		if isChat {
			tabActive = m.IsTabActive(tab)
		}
	}

	agentStyle := m.styles.AgentClaude
	switch {
	case isScript:
		agentStyle = m.styles.AgentScript
	case tab.Assistant != "claude":
		agentStyle = m.styles.AgentTerm
	}

	if i == activeIdx {
		bg := common.ColorSurface2
		pad := lipgloss.NewStyle().Background(bg).Render(" ")
		indicatorFg := agentStyle.GetForeground()
		if tabDisconnected {
			indicatorFg = common.ColorMuted
		}
		indicatorPart := lipgloss.NewStyle().Foreground(indicatorFg).Background(bg).Render(indicator)
		nameStyle := lipgloss.NewStyle().Foreground(common.ColorForeground).Background(bg)
		if tabDisconnected {
			nameStyle = nameStyle.Foreground(common.ColorMuted)
		}
		namePart := nameStyle.Render(name)
		space := lipgloss.NewStyle().Background(bg).Render(" ")
		closePart := lipgloss.NewStyle().Foreground(common.ColorMuted).Background(bg).Render("×")
		return pad + indicatorPart + namePart + space + closePart + pad
	}

	var nameStyled string
	switch {
	case tabDisconnected:
		nameStyled = m.styles.Muted.Render(name)
	case tabActive:
		nameStyled = lipgloss.NewStyle().Foreground(common.ColorPrimary).Bold(true).Render(name)
	default:
		nameStyled = m.styles.Muted.Render(name)
	}

	var indicatorStyled string
	if tabDisconnected {
		indicatorStyled = m.styles.Muted.Render(indicator)
	} else {
		indicatorStyled = agentStyle.Render(indicator)
	}

	closeLabel := m.styles.Muted.Render("×")
	return m.styles.Tab.Render(indicatorStyled + nameStyled + " " + closeLabel)
}
```

`renderAgentTab` is called twice per tab per frame (once to measure, once to place). It is pure — it reads `tab.mu`-guarded fields under the lock and touches no `Model` state — so calling it twice is safe. If profiling ever shows this on a hot path, cache the rendered strings in the slice, which the code in Step 5 already does.

Add imports to `model_render_tabbar.go`: `"github.com/charmbracelet/x/ansi"`.

Note the ordering constraint: close-button hits are appended *after* their tab's hit, and `handleTabBarClick` scans all `tabHitClose` regions in a first pass before anything else, so append order within `m.tabHits` does not matter for correctness. Do not "optimise" that two-pass scan away.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ui/center -run TestRenderTabBar -v`
Expected: both PASS.

Run: `go test ./internal/ui/center`
Expected: PASS, including `selection_test.go`. That test finds `+ New Agent` by scanning for `tabHitPlus` and clicks at `region.X + 1`; the button is still pinned and still registered, so its X may have shifted but the test computes X from the hit region rather than hardcoding it.

- [ ] **Step 7: Check the file-length gate**

Run: `wc -l internal/ui/center/model_render_tabbar.go internal/ui/center/model_render_tabbar_tabs.go`
Expected: both under 500.

- [ ] **Step 8: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/ui/center/...
git add internal/ui/center/model.go internal/ui/center/model_info.go \
        internal/ui/center/model_render_tabbar.go \
        internal/ui/center/model_render_tabbar_tabs.go \
        internal/ui/center/model_render_tabbar_layout_test.go
git commit -m "feat: render workspace note and scroll arrows in the tab bar"
```

---

### Task 5: Handle clicks on the arrows and the note

**Files:**
- Modify: `internal/ui/center/model_render_tabbar.go` (`handleTabBarClick`)
- Test: `internal/ui/center/model_render_tabbar_layout_test.go`

**Interfaces:**
- Consumes: `tabHitPrev`, `tabHitNext`, `tabHitNote` regions from Task 4.
- Produces: no new exported surface. Reuses `messages.ShowSetWorkspaceNoteDialog{Workspace: *data.Workspace}`, already declared in `internal/messages/messages.go:293` and already routed at `internal/app/app_input.go:298`. **Do not add a message type or an app-layer handler.**

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/center/model_render_tabbar_layout_test.go`:

```go
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
```

Add `"github.com/Skowt/medusa/internal/messages"` to the test imports.

`tabBarModel` and `findHit` are the helpers introduced in Task 4 Step 3, in the same test file. Reuse them; do not redefine them.

Note that `tabBarModel` calls `renderTabBar()` once before returning, so `m.tabHits` is already populated.

`dispatchTabHit(localX int) tea.Cmd` is a small test seam extracted in Step 3 — it is the region-matching half of `handleTabBarClick`, with the screen-to-content coordinate conversion left behind. Extracting it means these tests do not have to reconstruct border/padding/info-bar offsets, which is the fragile part of `selection_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/center -run 'TestClick|TestArrow' -v`
Expected: FAIL to compile — `undefined: m.dispatchTabHit`, `undefined: m.scrollTabs`.

- [ ] **Step 3: Write the implementation**

In `internal/ui/center/model_render_tabbar.go`, add:

```go
// scrollTabs moves the tab strip by delta tabs, clamped to the tab count.
func (m *Model) scrollTabs(delta int) {
	n := len(m.getTabs())
	if n == 0 {
		m.tabScrollOffset = 0
		return
	}
	off := m.tabScrollOffset + delta
	if off < 0 {
		off = 0
	}
	if off > n-1 {
		off = n - 1
	}
	m.tabScrollOffset = off
}
```

Split `handleTabBarClick` so the region matching is testable without coordinate maths:

```go
func (m *Model) handleTabBarClick(msg tea.MouseClickMsg) tea.Cmd {
	const (
		borderTop   = 1
		borderLeft  = 1
		paddingLeft = 1
	)

	tabBarY := borderTop + m.infoBarHeight()
	if msg.Y != tabBarY {
		return nil
	}
	localX := msg.X - m.offsetX - borderLeft - paddingLeft
	if localX < 0 {
		return nil
	}
	return m.dispatchTabHit(localX)
}

// dispatchTabHit resolves a tab-bar-local X coordinate to an action.
// Close buttons are checked first because they overlap tab regions.
func (m *Model) dispatchTabHit(localX int) tea.Cmd {
	const localY = 0

	for _, hit := range m.tabHits {
		if hit.kind == tabHitClose && hit.region.Contains(localX, localY) {
			idx := hit.index
			return func() tea.Msg { return messages.CloseTabAt{Index: idx} }
		}
	}

	for _, hit := range m.tabHits {
		if !hit.region.Contains(localX, localY) {
			continue
		}
		switch hit.kind {
		case tabHitPrev:
			m.scrollTabs(-1)
			return nil
		case tabHitNext:
			m.scrollTabs(1)
			return nil
		case tabHitNote:
			ws := m.workspace
			if ws == nil {
				return nil
			}
			return func() tea.Msg {
				return messages.ShowSetWorkspaceNoteDialog{Workspace: ws}
			}
		case tabHitPlus:
			if m.workspace != nil && m.workspace.Archived() {
				ws := m.workspace
				return func() tea.Msg {
					return messages.ShowArchivedWorkspaceDialog{Workspace: ws}
				}
			}
			return func() tea.Msg { return messages.ShowCustomizeTabDialog{} }
		case tabHitInfo:
			m.infoTabActive = true
			return nil
		case tabHitTab:
			if m.workspace != nil && m.workspace.Archived() {
				ws := m.workspace
				return func() tea.Msg {
					return messages.ShowArchivedWorkspaceDialog{Workspace: ws}
				}
			}
			m.infoTabActive = false
			m.setActiveTabIdx(hit.index)
			return m.tabSelectionChangedCmd()
		}
	}
	return nil
}
```

The `tabHitTab` and `tabHitPlus` bodies are copied unchanged from the current implementation.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/center -run 'TestClick|TestArrow' -v`
Expected: all PASS.

If `TestClickArrowsScrollViewport` fails at `t.Fatal("8 tabs in a 60-cell pane must produce a next arrow")`, the pane is not as narrow as assumed — log `m.contentWidth()` and the measured tab widths, then lower `SetSize` until overflow occurs. Do not weaken the assertion.

- [ ] **Step 5: Run the whole package with the race detector**

Run: `go test -race ./internal/ui/center`
Expected: PASS.

- [ ] **Step 6: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/ui/center/...
git add internal/ui/center/model_render_tabbar.go internal/ui/center/model_render_tabbar_layout_test.go
git commit -m "feat: click tab bar arrows to scroll and the note to edit it"
```

---

### Task 6: Verify against the real TUI and close out

**Files:** none modified unless a defect surfaces.

**Interfaces:**
- Consumes: everything from Tasks 1–5.
- Produces: a verified, lint-clean branch.

- [ ] **Step 1: Full gate**

Run: `make lint`

This runs `go test -race -v ./...`, then golangci-lint, then the 500-line check. Expected: exit 0.

- [ ] **Step 2: Headless render harness**

Run:

```bash
go run ./cmd/medusa-harness -mode center -frames 5 -warmup 1
go run ./cmd/medusa-harness -mode monitor -frames 5 -warmup 1
go run ./cmd/medusa-harness -mode sidebar -frames 5 -warmup 1
```

Expected: all three complete without panic. The center mode exercises the rewritten tab bar. A panic here most likely means a negative `strings.Repeat` count — check the `pad` and separator computations against `width`.

- [ ] **Step 3: Drive the real app**

Use the `verify` skill, or run `make run` directly, and confirm by observation:

1. Select a workspace **with a note** that has at least one agent tab → it opens on the **agent tab**, not Info. The note is visible, right-aligned, on the tab bar.
2. Click the note → the Set Note dialog opens, pre-filled.
3. Select a workspace **with no note** → tab bar right side is empty; clicking there does nothing.
4. Select a workspace with **no agent tabs** → Info tab is shown (unchanged fallback).
5. Open enough agents to overflow the bar → `›` appears. Click it; tabs scroll one at a time. `‹` appears once scrolled. Neither arrow scrolls past the ends.
6. With the strip scrolled right, press the next-tab keybinding to reach tab 0 → the viewport scrolls left on its own to reveal it.
7. Narrow the terminal until the bar is tiny → the note is still partially visible, ellipsised, never gone.
8. Set a note containing wide glyphs (e.g. `日本語版のテスト`) → the tab bar does not tear and the border is intact.

Step 8 is not paranoia: `163add0` fixed the compositor blanking wide glyphs and tearing the pane border. A width miscalculation in the note re-opens exactly that failure.

- [ ] **Step 4: Confirm the dead constants are now live**

Run: `grep -rn "tabHitPrev\|tabHitNext" --include="*.go" internal/`
Expected: declarations in `model.go` plus uses in `model_render_tabbar.go`. Before this change they were declared and never referenced.

- [ ] **Step 5: Commit the spec and plan**

```bash
git add docs/superpowers/specs/2026-07-10-tabbar-note-and-scroll-design.md docs/superpowers/plans/2026-07-10-tabbar-note-and-scroll.md
git commit -m "docs: add tab bar note and scroll design and plan"
```

---

## Notes for the implementer

**Do not add a `SetWorkspaceNote` message, handler, or dialog.** All three exist. `messages.ShowSetWorkspaceNoteDialog` (`internal/messages/messages.go:293`) is routed at `internal/app/app_input.go:298` to `handleShowSetWorkspaceNoteDialog`, which opens an input dialog pre-filled from `msg.Workspace.Note`. The Info tab's cursor-0 Enter already sends it from `model_info_keys.go:87`. The tab-bar note is simply a second sender of the same message.

**Do not touch `model_tabs_nav.go`.** Auto-scroll falls out of the `active`-clamping inside `visibleTabs`. If keyboard cycling does not scroll the viewport, the bug is in `visibleTabs`, not in nav.

**Do not truncate agent tab names.** They scroll out of view instead. Only the note truncates.

**`Workspace.Root()` is a value receiver** and copies the whole struct, so a goroutine calling it races with concurrent field writes. Nothing in this plan calls it, but `-race` failures elsewhere in the package have this cause.
