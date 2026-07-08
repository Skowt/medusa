# Full-Width Workspace Titles with Wrap-on-Focus — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every workspace title the full pane width, and show the focused workspace's full name by wrapping it, moving its action controls to a text-label footer row.

**Architecture:** All changes live in `internal/ui/dashboard/`. A pure `wrapName` helper is the shared primitive for splitting a name into display lines; a `nameChunks` method wraps it with the row's selection/pending state so the renderer and `rowLineCount` compute the same number of lines from the same source. Action controls (`[dupe] [group] [archive]`) move from an inline icon slot on line 1 to a dedicated footer line on the selected row; their hit boxes are recorded as `{line, x0, x1}` so mouse routing stays correct as the footer's position shifts with the wrapped-name height.

**Tech Stack:** Go, Bubble Tea v2, lipgloss v2. Width is always measured with `lipgloss.Width` (grapheme-aware), never `len`.

## Global Constraints

- No `.go` file may exceed 500 lines (`make lint` enforces). Largest touched file is `dashboard_navigation.go` at 412; if a file approaches the limit, split by concern into a sibling file in the same package.
- Definition of done (repo `CLAUDE.md`): `make fmt`; `golangci-lint run ./internal/ui/dashboard/...` exits 0; touched-package tests pass; finish with `make lint` (race + lint + line check) and, because this is UI-adjacent, `go run ./cmd/medusa-harness -mode sidebar -frames 5 -warmup 1` (plus `monitor`/`center`) or `release-check`.
- All tests use the standard library `testing` package (the package already has `_test.go` files; match their style and the `mkWS` helper in `rebuild_rows_test.go`).
- Commit prefixes follow conventional-commit-lite: `feat:` for the user-visible behavior, `test:`/`refactor:` where apt.

---

### Task 1: `wrapName` pure helper

**Files:**
- Create: `internal/ui/dashboard/wrap.go`
- Test: `internal/ui/dashboard/wrap_test.go`

**Interfaces:**
- Produces:
  - `func wrapName(name string, width, maxLines int) []string` — splits `name` into ≤`maxLines` lines each ≤`width` display columns, preferring a break just after a hyphen near the wrap point; the last line ends with `…` if the name still overflows.
  - `func truncateRunes(runes []rune, width int) string` — longest prefix of `runes` fitting in `width` columns, appending `…` when truncated. Reused by the unselected single-line name.

- [ ] **Step 1: Write the failing tests**

```go
// internal/ui/dashboard/wrap_test.go
package dashboard

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestWrapName_ShortFitsOneLine(t *testing.T) {
	got := wrapName("alpha", 20, 3)
	if len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("want [alpha], got %q", got)
	}
}

func TestWrapName_WrapsAtWidth(t *testing.T) {
	got := wrapName("abcdefghij", 4, 3)
	if len(got) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(got), got)
	}
	for _, l := range got {
		if w := lipgloss.Width(l); w > 4 {
			t.Errorf("line %q width %d exceeds 4", l, w)
		}
	}
}

func TestWrapName_PrefersHyphenBoundary(t *testing.T) {
	// "no-ticket-x" at width 10 should break after a hyphen, not mid-token.
	got := wrapName("no-ticket-x", 10, 3)
	if !strings.HasSuffix(got[0], "-") {
		t.Errorf("expected first line to end at a hyphen, got %q", got[0])
	}
}

func TestWrapName_CapEllipsizesLastLine(t *testing.T) {
	got := wrapName("abcdefghijklmnop", 4, 2)
	if len(got) != 2 {
		t.Fatalf("want 2 lines (capped), got %d: %q", len(got), got)
	}
	if !strings.HasSuffix(got[1], "…") {
		t.Errorf("capped last line should end with ellipsis, got %q", got[1])
	}
	if w := lipgloss.Width(got[1]); w > 4 {
		t.Errorf("capped line width %d exceeds 4", w)
	}
}

func TestTruncateRunes_AppendsEllipsis(t *testing.T) {
	got := truncateRunes([]rune("abcdefgh"), 5)
	if lipgloss.Width(got) > 5 {
		t.Errorf("width %d exceeds 5: %q", lipgloss.Width(got), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("want trailing ellipsis, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/dashboard -run 'TestWrapName|TestTruncateRunes' -v`
Expected: FAIL — `undefined: wrapName` / `undefined: truncateRunes`.

- [ ] **Step 3: Write the implementation**

```go
// internal/ui/dashboard/wrap.go
package dashboard

import "charm.land/lipgloss/v2"

const (
	// maxNameLines caps how many lines a wrapped workspace name may occupy on
	// the selected row.
	maxNameLines = 3
	// hyphenLookback is how far back from a hard wrap we look for a hyphen to
	// break on, so slug names break at "-" rather than mid-token.
	hyphenLookback = 8
)

// wrapName splits name into at most maxLines display lines, each no wider than
// width columns. It prefers to break just after a hyphen within the last
// hyphenLookback columns of a line. If the name still overflows the last
// allowed line, that line is ellipsized. Width is measured with lipgloss.Width.
func wrapName(name string, width, maxLines int) []string {
	if width < 1 {
		width = 1
	}
	if maxLines < 1 {
		maxLines = 1
	}
	runes := []rune(name)
	var lines []string
	for len(runes) > 0 {
		if len(lines) == maxLines-1 {
			lines = append(lines, truncateRunes(runes, width))
			return lines
		}
		cut := lineCut(runes, width)
		lines = append(lines, string(runes[:cut]))
		runes = runes[cut:]
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}
	return lines
}

// lineCut returns how many leading runes fit within width columns, preferring a
// hyphen boundary within the last hyphenLookback columns of the fitted span.
func lineCut(runes []rune, width int) int {
	n := 0
	for n < len(runes) && lipgloss.Width(string(runes[:n+1])) <= width {
		n++
	}
	if n == 0 {
		n = 1 // always make progress
	}
	if n >= len(runes) {
		return len(runes)
	}
	for i := n - 1; i >= 0 && i >= n-hyphenLookback; i-- {
		if runes[i] == '-' {
			return i + 1
		}
	}
	return n
}

// truncateRunes returns the longest prefix of runes fitting within width
// columns, appending an ellipsis when truncation occurs.
func truncateRunes(runes []rune, width int) string {
	if lipgloss.Width(string(runes)) <= width {
		return string(runes)
	}
	const ell = "…"
	budget := width - lipgloss.Width(ell)
	if budget < 0 {
		budget = 0
	}
	n := 0
	for n < len(runes) && lipgloss.Width(string(runes[:n+1])) <= budget {
		n++
	}
	return string(runes[:n]) + ell
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/dashboard -run 'TestWrapName|TestTruncateRunes' -v`
Expected: PASS (all 5).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/dashboard/wrap.go internal/ui/dashboard/wrap_test.go
git commit -m "feat: add name-wrapping helper for dashboard titles"
```

---

### Task 2: Full-width names, wrap-on-select, and the button footer

This is one atomic change: the button-slot fields, the renderer, `rowLineCount`, and click routing all turn on the same struct fields and must move together to keep the package compiling and hit-testing consistent.

**Files:**
- Modify: `internal/ui/dashboard/model.go` (replace icon-X fields with a button-hit slice + types)
- Modify: `internal/ui/dashboard/dashboard_render_lines.go` (`renderWorkspaceLine1` → full-width wrapped name; add footer + `nameChunks`)
- Modify: `internal/ui/dashboard/dashboard_render.go` (`renderWorkspaceRow` assembles lines + records hits; orphan/archived record their delete hit)
- Modify: `internal/ui/dashboard/dashboard_navigation.go` (dynamic `rowLineCount`; `rowIndexAt` returns within-row line offset)
- Modify: `internal/ui/dashboard/model_update.go` (click routing loops over recorded hits with a line + X check)
- Modify: `internal/ui/dashboard/rebuild_rows_test.go`, `internal/ui/dashboard/click_routing_test.go` (existing tests assert old geometry)

**Interfaces:**
- Consumes: `wrapName`, `truncateRunes`, `maxNameLines` (Task 1).
- Produces:
  - `type wsButtonAction int` with `btnDuplicate`, `btnGroup`, `btnArchive`.
  - `type wsButtonHit struct { action wsButtonAction; line, x0, x1 int }` — `line` is the row-relative display line; `x0..x1` is the content-relative X range `[x0, x1)`.
  - `Model.wsButtonHits []wsButtonHit` — buttons of the currently selected row.
  - `func (m *Model) nameChunks(ws *data.Workspace, selected bool, contentWidth int) []string`
  - `func (m *Model) rowIndexAt(screenX, screenY int) (idx, lineWithinRow int, ok bool)` (signature changes: adds `lineWithinRow`).
  - `const nameIndent = 3`, `const footerIndent = 2`.

- [ ] **Step 1: Update the model fields and add button types**

In `internal/ui/dashboard/model.go`, replace the three icon-X fields (lines 76-78):

```go
	deleteIconX     int             // X position of delete "x" icon for currently selected row
	duplicateIconX  int             // X position of the "+" duplicate icon for the currently selected row
	groupIconX      int             // X position of the "#" group-edit icon for the currently selected row
```

with:

```go
	wsButtonHits    []wsButtonHit   // clickable action buttons of the currently selected workspace row
```

Add near the `Row` type (after line 39):

```go
// wsButtonAction identifies a workspace action button.
type wsButtonAction int

const (
	btnDuplicate wsButtonAction = iota
	btnGroup
	btnArchive
)

// wsButtonHit is the clickable hit box of one action button on the selected
// workspace row. line is the row-relative display line; x0..x1 is the
// content-relative X range [x0, x1).
type wsButtonHit struct {
	action     wsButtonAction
	line       int
	x0, x1     int
}
```

- [ ] **Step 2: Rewrite the name/footer rendering in `dashboard_render_lines.go`**

Replace the whole `renderWorkspaceLine1` function and the `rightSlotWidth` const block. Keep the indicator/status/style computation verbatim (current lines 27-99 — status text, indicator switch, hook overrides, unread override, iconStyle, `prefix`); only the name and right-slot handling change. Add `nameChunks`, the footer helpers, and constants:

```go
const (
	nameIndent   = 3 // width of the leading " ● " prefix on line 1
	footerIndent = 2 // left indent of the action button footer
)

// wsButtonDefs is the ordered set of action buttons and their labels.
var wsButtonDefs = []struct {
	action wsButtonAction
	label  string
}{
	{btnDuplicate, "[dupe]"},
	{btnGroup, "[group]"},
	{btnArchive, "[archive]"},
}

// workspacePending reports whether the workspace is mid-create or mid-delete
// (its row shows a spinner + status text instead of wrapping the name).
func (m *Model) workspacePending(ws *data.Workspace) bool {
	if m.deletingWorkspaces[ws.Root()] {
		return true
	}
	_, creating := m.creatingWorkspaces[ws.Root()]
	return creating
}

// nameChunks returns the display lines of the workspace name (text only, no
// styling). An unselected or pending row gets a single ellipsized line; a
// selected row wraps up to maxNameLines. Both the renderer and rowLineCount
// call this so their line counts cannot drift.
func (m *Model) nameChunks(ws *data.Workspace, selected bool, contentWidth int) []string {
	width := contentWidth - nameIndent
	if width < 1 {
		width = 1
	}
	if !selected || m.workspacePending(ws) {
		return []string{truncateRunes([]rune(ws.Name), width)}
	}
	return wrapName(ws.Name, width, maxNameLines)
}

// footerButtonHits returns each button's footer-relative hit box (line 0).
func footerButtonHits() []wsButtonHit {
	x := footerIndent
	hits := make([]wsButtonHit, 0, len(wsButtonDefs))
	for _, b := range wsButtonDefs {
		w := lipgloss.Width(b.label)
		hits = append(hits, wsButtonHit{action: b.action, line: 0, x0: x, x1: x + w})
		x += w + 1 // one space between buttons
	}
	return hits
}

// renderFooterLine renders the action button row for a selected active row.
func (m *Model) renderFooterLine() string {
	bg := lipgloss.NewStyle().Background(common.ColorSelection)
	btnStyle := lipgloss.NewStyle().Foreground(common.ColorSecondary).Background(common.ColorSelection).Bold(true)
	parts := []string{bg.Render(strings.Repeat(" ", footerIndent))}
	for i, b := range wsButtonDefs {
		if i > 0 {
			parts = append(parts, bg.Render(" "))
		}
		parts = append(parts, btnStyle.Render(b.label))
	}
	return strings.Join(parts, "")
}

// renderWorkspaceNameLines renders the styled name line(s): the indicator +
// first name chunk (plus any status text), then indented continuation lines.
func (m *Model) renderWorkspaceNameLines(ws *data.Workspace, selected bool, contentWidth int) []string {
	// --- BEGIN verbatim indicator/status/style computation from the old
	// renderWorkspaceLine1 (statusText, indicator switch, hook overrides,
	// unread override, iconStyle, renderedIndicator, style, prefix). ---
	// ... (copied unchanged; produces: statusText string, style lipgloss.Style,
	//      prefix string with lipgloss.Width(prefix) == nameIndent) ...
	// --- END verbatim block ---

	chunks := m.nameChunks(ws, selected, contentWidth)
	contPrefixStyle := lipgloss.NewStyle()
	if selected {
		contPrefixStyle = contPrefixStyle.Background(common.ColorSelection)
	}
	contPrefix := contPrefixStyle.Render(strings.Repeat(" ", nameIndent))

	lines := make([]string, 0, len(chunks))
	lines = append(lines, prefix+style.Render(chunks[0])+statusText)
	for _, c := range chunks[1:] {
		lines = append(lines, contPrefix+style.Render(c))
	}
	return lines
}
```

Delete `rightSlotWidth`, the `rightSlot` string, the inline `" + ≡ × "` slot, and the `duplicateIconX/groupIconX/deleteIconX` assignments from the old `renderWorkspaceLine1`; that function is replaced by `renderWorkspaceNameLines`. Keep `renderWorkspaceLine2` and `renderWorkspaceLine3` unchanged.

- [ ] **Step 3: Rewrite `renderWorkspaceRow` in `dashboard_render.go`**

```go
// renderWorkspaceRow renders an active workspace entry: wrapped name line(s),
// metadata, optional repo list, and (when selected) an action-button footer.
func (m *Model) renderWorkspaceRow(row Row, selected bool) string {
	ws := row.Workspace
	if ws == nil {
		return ""
	}

	contentWidth := m.width - 3
	if contentWidth < 1 {
		contentWidth = 1
	}

	if ws.IsOrphaned() {
		return m.renderOrphanRow(ws, selected, contentWidth)
	}
	if ws.Archived() {
		return m.renderArchivedRow(ws, selected, contentWidth)
	}

	if selected {
		m.wsButtonHits = nil
	}

	lines := m.renderWorkspaceNameLines(ws, selected, contentWidth)
	lines = append(lines, m.renderWorkspaceLine2(ws, selected, contentWidth))
	if selected && len(ws.Repos) >= 2 {
		if l3 := m.renderWorkspaceLine3(ws, contentWidth); l3 != "" {
			lines = append(lines, l3)
		}
	}
	if selected {
		footerLine := len(lines)
		for _, h := range footerButtonHits() {
			h.line += footerLine
			m.wsButtonHits = append(m.wsButtonHits, h)
		}
		lines = append(lines, m.renderFooterLine())
	}

	if selected {
		bg := lipgloss.NewStyle().Background(common.ColorSelection)
		for i := range lines {
			lines[i] = padWithBg(lines[i], contentWidth, bg)
		}
	}
	return strings.Join(lines, "\n")
}
```

In `renderOrphanRow` and `renderArchivedRow`, replace the `m.duplicateIconX = 0; m.groupIconX = 0` (and the `m.deleteIconX = ...` in archived) side-effects with recording a single archive/delete hit into `wsButtonHits`. Both rows are single-line, so `line: 0`. For the archived row, compute the `×` X range from the rendered widths already available there:

```go
	if selected {
		m.wsButtonHits = nil
		x0 := lipgloss.Width(prefix) + lipgloss.Width(name) + 1 // skip the leading space in " × "
		m.wsButtonHits = append(m.wsButtonHits, wsButtonHit{action: btnArchive, line: 0, x0: x0, x1: x0 + lipgloss.Width(common.Icons.Close)})
		bgStyle := lipgloss.NewStyle().Background(common.ColorSelection)
		line = padWithBg(line, contentWidth, bgStyle)
	}
```

Apply the same pattern to `renderOrphanRow` (its `deleteSlot` is `" × "` after `" "+"⚠ "+name`), recording one `btnArchive` hit at the `×` column. (`handleDelete` hard-deletes for archived/orphan workspaces — that is the existing, intended behavior; `btnArchive` is just the routing key to `handleDelete`.)

- [ ] **Step 4: Make `rowLineCount` dynamic and extend `rowIndexAt` in `dashboard_navigation.go`**

Replace the `RowWorkspace` arm of `rowLineCount` (lines 68-76):

```go
	case RowWorkspace:
		ws := m.rows[idx].Workspace
		if ws == nil {
			return 2
		}
		if ws.Archived() {
			return 1
		}
		if ws.IsOrphaned() {
			return 2
		}
		return m.activeRowLineCount(ws, idx == m.cursor)
```

Add:

```go
// activeRowLineCount returns the display-line count of an active workspace row.
// It mirrors renderWorkspaceRow exactly by reusing nameChunks and the same
// selection conditions, so scroll math and hit-testing never disagree.
func (m *Model) activeRowLineCount(ws *data.Workspace, selected bool) int {
	contentWidth := m.width - 3
	if contentWidth < 1 {
		contentWidth = 1
	}
	lines := len(m.nameChunks(ws, selected, contentWidth)) // name (1 if unselected)
	lines++                                                // metadata
	if selected && len(ws.Repos) >= 2 {
		lines++ // repo list
	}
	if selected {
		lines++ // footer
	}
	return lines
}
```

Change `rowIndexAt` to also return the within-row line offset. Update its signature and the two `return i, true` sites plus the final `return`:

```go
func (m *Model) rowIndexAt(screenX, screenY int) (int, int, bool) {
	// ... unchanged setup ...
	// main-rows walk (was line 147):
			if rowY >= visLine && rowY < visLine+rowLines {
				return i, rowY - visLine, true
			}
	// archived walk (was line 159):
			if rowY >= aLine && rowY < aLine+rowLines {
				return i, rowY - aLine, true
			}
	// bottom:
	return -1, 0, false
}
```

Replace the early `return -1, false` guards (lines 102, 105, 108, 119) with `return -1, 0, false`.

- [ ] **Step 5: Route clicks through `wsButtonHits` in `model_update.go`**

Update the caller (line 25) and the icon-click block (lines 36-56):

```go
			idx, lineWithinRow, ok := m.rowIndexAt(msg.X, msg.Y)
			if !ok {
				return m, nil
			}
			if idx < 0 || idx >= len(m.rows) {
				return m, nil
			}
			if !isSelectable(m.rows[idx]) {
				return m, nil
			}

			// Click on an action button of the selected row?
			if idx == m.cursor && m.rows[idx].Type == RowWorkspace {
				borderLeft := 1
				paddingLeft := 0
				contentX := msg.X - borderLeft - paddingLeft
				for _, h := range m.wsButtonHits {
					if lineWithinRow == h.line && contentX >= h.x0 && contentX < h.x1 {
						m.toolbarFocused = false
						switch h.action {
						case btnDuplicate:
							return m, m.handleDuplicate()
						case btnGroup:
							return m, m.handleSetGroup()
						case btnArchive:
							return m, m.handleDelete()
						}
					}
				}
			}
```

- [ ] **Step 6: Update the existing tests that assert old geometry**

In `rebuild_rows_test.go`:
- `TestRowLineCount_SelectedMultiRepo`: selected multi-repo is now `name(1)+meta(1)+repos(1)+footer(1) = 4`; unselected stays 2. Change `want 3` → `want 4`.
- `TestRenderWorkspaceRow_MultiRepoSelected_ShowsThreeLines`: now 4 lines. Rename to `...ShowsFourLines`, assert `len(lines) == 4`, keep the `billing` check on `lines[2]`, and add `strings.Contains(lines[3], "[archive]")`.
- `TestRenderWorkspaceRow_SingleRepoSelected_StaysTwoLines`: now 3 lines (name+meta+footer). Rename to `...StaysThreeLines`, assert `len(lines) == 3` and `strings.Contains(lines[2], "[dupe]")`.

Rewrite `click_routing_test.go`'s `TestClickRouting_DuplicateIconTriggersDuplicate`:

```go
func TestClickRouting_ButtonHitsRecordedOnFooter(t *testing.T) {
	m := New()
	m.workspaces = []*data.Workspace{
		mkWS("alpha", "", []string{"medusa"}, time.Unix(1, 0)),
	}
	m.width = 40
	m.height = 20
	m.rebuildRows()

	wsIdx := -1
	for i, r := range m.rows {
		if r.Type == RowWorkspace {
			wsIdx = i
			break
		}
	}
	if wsIdx == -1 {
		t.Fatal("expected a workspace row")
	}
	m.cursor = wsIdx
	_ = m.renderWorkspaceRow(m.rows[wsIdx], true)

	if len(m.wsButtonHits) != 3 {
		t.Fatalf("expected 3 button hits, got %d", len(m.wsButtonHits))
	}
	// All three sit on the same footer line, ordered dupe < group < archive.
	line := m.wsButtonHits[0].line
	for _, h := range m.wsButtonHits {
		if h.line != line {
			t.Errorf("button %d on line %d, expected all on %d", h.action, h.line, line)
		}
	}
	if m.wsButtonHits[0].action != btnDuplicate || m.wsButtonHits[2].action != btnArchive {
		t.Errorf("unexpected button order: %+v", m.wsButtonHits)
	}
	if m.wsButtonHits[0].x0 >= m.wsButtonHits[2].x0 {
		t.Errorf("dupe should be left of archive: %+v", m.wsButtonHits)
	}
	// Footer is the last line of a selected single-repo row (name+meta+footer).
	if line != m.rowLineCount(wsIdx)-1 {
		t.Errorf("footer line %d, expected last line %d", line, m.rowLineCount(wsIdx)-1)
	}
}
```

- [ ] **Step 7: Add the line-count agreement guardrail test**

Append to `rebuild_rows_test.go`:

```go
func TestActiveRowLineCount_MatchesRender(t *testing.T) {
	cases := []struct {
		name     string
		wsName   string
		repos    []string
		selected bool
	}{
		{"short-unselected", "alpha", []string{"medusa"}, false},
		{"short-selected", "alpha", []string{"medusa"}, true},
		{"long-selected-wraps", "no-ticket-prompt-injection-hardening-pass", []string{"medusa"}, true},
		{"multirepo-selected", "PE-37895-place-to-place-migration-spike", []string{"a", "b", "c"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := New()
			m.width = 35
			m.workspaces = []*data.Workspace{mkWS(c.wsName, "", c.repos, time.Unix(1, 0))}
			m.rebuildRows()
			wsIdx := -1
			for i, r := range m.rows {
				if r.Type == RowWorkspace {
					wsIdx = i
					break
				}
			}
			if c.selected {
				m.cursor = wsIdx
			} else {
				m.cursor = -1
			}
			rendered := m.renderRow(m.rows[wsIdx], c.selected)
			wantLines := strings.Count(rendered, "\n") + 1
			if got := m.rowLineCount(wsIdx); got != wantLines {
				t.Errorf("rowLineCount=%d but render produced %d lines:\n%s", got, wantLines, rendered)
			}
		})
	}
}
```

- [ ] **Step 8: Run the dashboard test suite**

Run: `go test ./internal/ui/dashboard -v`
Expected: PASS, including the updated geometry tests and the new agreement test. If `TestActiveRowLineCount_MatchesRender` fails, the renderer and `activeRowLineCount` disagree — reconcile them (both must use `nameChunks` and the identical selection conditions) before continuing.

- [ ] **Step 9: Format, lint, and verify no callers were missed**

Run:
```bash
make fmt
grep -rn "duplicateIconX\|groupIconX\|deleteIconX\|rightSlotWidth\|renderWorkspaceLine1\b" internal/ | grep -v _test
golangci-lint run ./internal/ui/dashboard/...
```
Expected: the grep returns nothing (all old references removed); golangci-lint exits 0.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/dashboard/
git commit -m "feat: full-width workspace titles with wrap-on-focus and text action footer"
```

---

### Task 3: Full-gate verification

**Files:** none (verification only).

- [ ] **Step 1: Race + lint + line-count gate**

Run: `make lint`
Expected: `go test -race ./...` passes, golangci-lint exits 0, no `.go` file exceeds 500 lines.

- [ ] **Step 2: Headless render harness (UI-adjacent change)**

Run:
```bash
go run ./cmd/medusa-harness -mode sidebar -frames 5 -warmup 1
go run ./cmd/medusa-harness -mode monitor -frames 5 -warmup 1
go run ./cmd/medusa-harness -mode center  -frames 5 -warmup 1
```
Expected: each exits 0 with no panic. (Equivalent: `make release-check`.)

- [ ] **Step 3: Manual smoke (optional, if a terminal is available)**

Run `make run`, focus the dashboard, and confirm: unselected titles use the full width; the selected long title wraps to its full name; the `[dupe] [group] [archive]` footer appears under the selected row; clicking each label triggers duplicate / set-group / archive; clicking the metadata line does not misfire a button.

---

## Self-Review

**Spec coverage:**
- Full-width name both states → Task 2 Steps 2-3 (`nameChunks` drops the reserved slot; `renderWorkspaceNameLines`). ✓
- Wrap-on-select, 3-line cap, `…` → Task 1 (`wrapName`, `maxNameLines`). ✓
- Text button footer `[dupe] [group] [archive]` → Task 2 Steps 2-3. ✓
- Labels verified against handlers → footer routes `btnDuplicate→handleDuplicate`, `btnGroup→handleSetGroup`, `btnArchive→handleDelete` (Task 2 Step 5). ✓
- Single shared line source → `nameChunks` used by both renderer and `activeRowLineCount`; guarded by `TestActiveRowLineCount_MatchesRender` (Task 2 Steps 2,4,7). ✓
- Click hit-test line + X, correct as footer position shifts → `wsButtonHit{line,x0,x1}` + extended `rowIndexAt` (Task 2 Steps 1,4,5). ✓
- Reclaim reserved columns; reset stale positions on non-footer rows → `wsButtonHits = nil` on selected render; orphan/archived record only their delete hit (Task 2 Steps 3). ✓
- Orphan name full-width truncation → Task 2 Step 3 (orphan uses `truncateRunes`). *Note:* the plan records the orphan/archived delete hit but keeps their existing single-line name; apply `truncateRunes` to the orphan name in Step 3 to honor the spec's reclaim intent.
- Archived rows single-line → unchanged in `rowLineCount` (Task 2 Step 4). ✓
- Out of scope (widen pane, smart truncation, source renaming, new actions) → not implemented. ✓

**Placeholder scan:** The only `...` is the explicitly-marked verbatim indicator block in `renderWorkspaceNameLines` (copied from the current `renderWorkspaceLine1`, lines 27-99) and the "unchanged setup" in `rowIndexAt`; both cite exact source line ranges. No TBD/TODO.

**Type consistency:** `wsButtonHit{action,line,x0,x1}` is defined in Task 2 Step 1 and used identically in Steps 3, 5, 6. `nameChunks(ws, selected, contentWidth)` signature matches across Steps 2, 4. `rowIndexAt` new 3-value return is updated at its sole caller (Step 5). `wsButtonAction` constants (`btnDuplicate/btnGroup/btnArchive`) match between definition and the click switch.
