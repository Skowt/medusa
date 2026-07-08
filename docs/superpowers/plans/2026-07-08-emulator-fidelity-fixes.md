# Terminal-Emulator Fidelity Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make medusa's terminal emulator render faithfully — fix the wide-glyph/emoji border break, garbled text/selection, and broken fullscreen + adopted-session scrollback — by porting the relevant fidelity patterns from the Amux reference emulator.

**Architecture:** Adopt Amux's grapheme-cluster cell model (store + emit the full grapheme per cell so declared width matches physical width), defer fullscreen scroll to Claude (medusa keeps no vterm scrollback for fullscreen tabs and forwards all scroll input), fix the tmux-capture scrollback parse, and add two small robustness guards. No architecture change — medusa keeps its composited multi-pane UI and its own emulator.

**Tech Stack:** Go, Bubble Tea v2, lipgloss v2, `charmbracelet/ultraviolet`, `mattn/go-runewidth`, `rivo/uniseg`.

**Reference:** Amux is cloned at `/private/tmp/claude-501/-Users-skowt--medusa-workspaces-tui-full-screen-mode/de7ad296-4930-46ba-b9ed-7511378dd56a/scratchpad/amux`. If absent (fresh session), re-clone: `git clone --depth 1 https://github.com/andyrewlee/amux.git`. A detailed porting kit (exact before/after code per task) is at `…/scratchpad/porting-kit.md`.

## Global Constraints

- After each task: `make fmt`; `golangci-lint run ./<touched-package>/...` exits 0; touched-package tests pass. End of development: `make lint` (race + golangci-lint + 500-line) and `make release-check` (render harness) both pass.
- No `.go` file may exceed **500 lines**; split by concern into sibling files.
- Commits: conventional-commit-lite (`feat:`/`fix:`/`refactor:`/`perf:` surface in release notes; `docs:`/`test:`/`chore:` don't).
- `go-runewidth` pinned to **v0.0.24**; `rivo/uniseg` a **direct** dependency.
- **Do NOT port Amux's alt-screen capture layer** (`alt_screen_capture*.go`, `alt_screen_restore_pending.go`, `altScreenCaptureState`, `LoadPaneCapture*`, `AppendScrollbackDelta*`, `syncViewOffsetDelta`/`syncPreserveViewport`, `invalidateAltScreenCapture`). It is out of scope (fullscreen scroll defers to Claude). Two Amux tests transitively depend on it and must be **skipped** when porting: `grapheme_test.go`'s `TestLinesEqualComparesGraphemeCluster`, and the `LoadPaneCapture`/delta cases in the prepend-scrollback tests.
- Keep `Canvas.DrawScreen`'s existing width-2 clip (`internal/ui/compositor/canvas.go:157-159`) — it is a separate, pre-existing guard.
- The `GraphemeCluster` invariant: `Rune` + `Width` still drive all layout/width logic; the cluster is only for emitted text. Never change `Width` when folding combining marks.

---

### Task 1: Revert the DrawAt boundary clip (superseded by the grapheme model)

**Files:**
- Modify: `internal/ui/compositor/vtermlayer.go` (remove the clip in `DrawAt`)
- Modify: `internal/ui/compositor/vtermlayer_test.go` (remove `TestVTermLayerClipsWideCharAtDrawBoundary`)

**Interfaces:**
- Produces: `DrawAt` matches Amux's live path (no width-2 boundary clip). The real border fix comes from Tasks 2–3.

- [ ] **Step 1: Revert the commit (stage only)**

Run: `git revert --no-commit e3dc798`
This removes exactly the 8-line clip block in `vtermlayer.go` `DrawAt` and the 31-line `TestVTermLayerClipsWideCharAtDrawBoundary` in `vtermlayer_test.go`. Confirm nothing else changed:
Run: `git diff --cached --stat`
Expected: only `internal/ui/compositor/vtermlayer.go` and `internal/ui/compositor/vtermlayer_test.go`, deletions only. `bufferScreen`/`.CellAt`/`.Bounds()` helpers remain (other tests use them); `Canvas.DrawScreen`'s clip is untouched.

- [ ] **Step 2: Verify the package still builds and tests pass**

Run: `go test ./internal/ui/compositor/ 2>&1 | tail -3`
Expected: `ok  github.com/Skowt/medusa/internal/ui/compositor`

- [ ] **Step 3: Format, lint, commit with a conventional message**

```bash
make fmt
golangci-lint run ./internal/ui/compositor/...
git commit -m "refactor: remove DrawAt wide-char boundary clip (superseded by grapheme model)"
```
(The revert was staged with `--no-commit`; this commit finalizes it with a release-note-friendly subject instead of the default "Revert …".)

---

### Task 2: Grapheme-cluster cell model in the vterm

**Files:**
- Modify: `internal/vterm/cell.go` (add `GraphemeCluster` field)
- Modify: `internal/vterm/ops.go` (`putChar` combining-mark fold)
- Modify: `internal/vterm/render.go` (add `writeCellContent` + `RenderableRune`; use them in `renderRow` and `renderWithScrollbackFrom`)
- Modify: `internal/vterm/selection.go` (`GetTextRange` emits the cluster)
- Modify: `go.mod` / `go.sum` (runewidth bump; uniseg direct)
- Create: `internal/vterm/grapheme_test.go` (ported)

**Interfaces:**
- Produces: `vterm.Cell.GraphemeCluster string`; `vterm.RenderableRune(r rune) rune` (exported, reused by the compositor in Task 3); `writeCellContent(buf *strings.Builder, cell Cell)` (package-internal). `putChar` folds width-0 runes into the base cell's `GraphemeCluster`.

- [ ] **Step 1: Write the failing tests (port from Amux)**

Create `internal/vterm/grapheme_test.go` by porting `amux/internal/vterm/grapheme_test.go` (adapt `package vterm`, imports). Include these cases (Amux file:line in parens), and **omit `TestLinesEqualComparesGraphemeCluster`** (depends on the unported capture layer):
- `TestGraphemeClusters` (`:11-77`): write `'e'` then combining `U+0301` → cell has `Rune=='e'`, `Width==1`, `GraphemeCluster=="é"`, and `CursorX==1` (cursor did not advance for the mark); a second combining mark accumulates onto the cluster; a subsequent plain rune has `GraphemeCluster==""`.
- `TestGraphemeSelectionCopy` (`:81-96`): after writing a clustered cell, `GetTextRange(0,0,0,0)` returns the full cluster string.
- `TestGraphemeRenderUsesCluster` (`:98-109`): `Render()` output contains the cluster.
- `TestGraphemeRenderUsesClusterInScrollback` (`:111-128`): with `ViewOffset>0`, `Render()` still emits the cluster.
- `TestGraphemeCombiningAtScreenStart` (`:148-169`): a combining mark written at cursor (0,0) with no prior cell must not panic and must not create a cluster.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vterm/ -run TestGrapheme -v`
Expected: FAIL — `cell.GraphemeCluster` undefined / clusters not populated.

- [ ] **Step 3: Add the `GraphemeCluster` field**

In `internal/vterm/cell.go`, change `Cell` to (append the field, keep everything else):

```go
// Cell represents a single character cell
type Cell struct {
	Rune  rune
	Style Style
	Width int // 1 normal, 2 wide, 0 continuation
	// GraphemeCluster, when non-empty, is the full grapheme (base rune plus
	// combining marks) for this cell. Empty means "use Rune". Readers that emit
	// text should prefer it; width/layout logic still uses Rune + Width.
	GraphemeCluster string
}
```
(`DefaultCell`, `MakeBlankLine`, `CopyLine` need no change — `CopyLine` copies by value.)

- [ ] **Step 4: Fold combining marks in `putChar`**

In `internal/vterm/ops.go`, replace the width-0 no-op branch with Amux's fold:

```go
	// Combining characters (width 0) attach to previous cell
	if width == 0 {
		prevX := v.CursorX - 1
		prevY := v.CursorY
		if prevX < 0 && prevY > 0 {
			prevY--
			prevX = v.Width - 1
		}
		// Step back over a wide-char continuation cell to its base cell.
		if prevY >= 0 && prevY < len(v.Screen) {
			line := v.Screen[prevY]
			if prevX > 0 && prevX < len(line) && line[prevX].Width == 0 {
				prevX--
			}
		}
		if prevY >= 0 && prevY < len(v.Screen) && prevX >= 0 && prevX < len(v.Screen[prevY]) {
			cell := &v.Screen[prevY][prevX]
			if cell.Rune != 0 { // never attach to a blank/continuation marker
				base := cell.GraphemeCluster
				if base == "" {
					base = string(cell.Rune)
				}
				cell.GraphemeCluster = base + string(r)
				v.markDirtyLine(prevY)
			}
		}
		return // Don't advance cursor for combining chars
	}
```

- [ ] **Step 5: Add render helpers and use them**

In `internal/vterm/render.go`, add:

```go
func writeCellContent(buf *strings.Builder, cell Cell) {
	if cell.Rune == 0 {
		buf.WriteRune(' ')
	} else if cell.GraphemeCluster != "" {
		buf.WriteString(cell.GraphemeCluster)
	} else {
		buf.WriteRune(cell.Rune)
	}
}

// RenderableRune substitutes a NUL rune (an unwritten cell) with a space so
// callers emit a blank instead of a zero byte.
func RenderableRune(r rune) rune {
	if r == 0 {
		return ' '
	}
	return r
}
```

Then in `renderRow` replace the emit (currently `if cell.Rune == 0 { buf.WriteRune(' ') } else { buf.WriteRune(cell.Rune) }`) with `writeCellContent(&buf, cell)`, and do the same in `renderWithScrollbackFrom`. (Keep the `if cell.Width == 0 { continue }` guards above each.)

- [ ] **Step 6: Emit the cluster in selection**

In `internal/vterm/selection.go` `GetTextRange`, add the cluster short-circuit before the rune emit:

```go
		if x < len(row) {
			if row[x].Width == 0 {
				continue
			}
			if g := row[x].GraphemeCluster; g != "" {
				result.WriteString(g)
				continue
			}
			r := row[x].Rune
			if r == 0 {
				r = ' '
			}
			result.WriteRune(r)
		}
```
(`GetSelectedText` delegates to `GetTextRange`; no change.)

- [ ] **Step 7: Run the grapheme tests to verify they pass**

Run: `go test ./internal/vterm/ -run TestGrapheme -v`
Expected: PASS.

- [ ] **Step 8: Bump runewidth and reconcile width tests**

```bash
go get github.com/mattn/go-runewidth@v0.0.24
go mod tidy
```
Confirm `require github.com/rivo/uniseg v0.4.7` is no longer `// indirect` in `go.mod`.
Run: `go test ./internal/vterm/ 2>&1 | tail -20`
Expected: PASS. If any `vterm_widechar_test.go` expectation shifted due to v0.0.24's updated tables, reconcile it (the new value matches modern terminals — update the expectation, don't pin the old lib). Note in the commit body which expectations changed, if any.

- [ ] **Step 9: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/vterm/...
git add internal/vterm/ go.mod go.sum
git commit -m "feat: add grapheme-cluster cell model to vterm"
```

---

### Task 3: Emit the grapheme cluster from the compositor

**Files:**
- Modify: `internal/ui/compositor/vtermlayer.go` (`cellToUVSnapshot`)
- Modify: `internal/ui/compositor/canvas.go` (`Canvas.Render`)
- Modify: `internal/ui/compositor/vtermlayer_test.go` (add a cluster test)

**Interfaces:**
- Consumes: `vterm.Cell.GraphemeCluster`, `vterm.RenderableRune` (Task 2).
- Produces: the ultraviolet layer path (the live center render) emits the full cluster with a declared `Width` that matches its physical width — the load-bearing border/emoji fix.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/compositor/vtermlayer_test.go` (uses the existing `bufferScreen`/`CellAt` helpers):

```go
func TestVTermLayerEmitsGraphemeCluster(t *testing.T) {
	term := vterm.New(3, 1)
	// 'e' + combining acute → one width-1 cell whose cluster is "é".
	term.Screen[0][0] = vterm.Cell{Rune: 'e', Width: 1, GraphemeCluster: "é"}
	snap := NewVTermSnapshot(term, false)
	if snap == nil {
		t.Fatalf("expected snapshot")
	}
	layer := &PositionedVTermLayer{VTermLayer: NewVTermLayer(snap), PosX: 0, PosY: 0, Width: 3, Height: 1}
	screen := &bufferScreen{Buffer: uv.NewBuffer(3, 1)}
	layer.Draw(screen, screen.Bounds())

	cell := screen.CellAt(0, 0)
	if cell == nil || cell.Content != "é" {
		t.Fatalf("expected cell content to be the full cluster %q, got %+v", "é", cell)
	}
	if cell.Width != 1 {
		t.Fatalf("cluster cell width must stay 1, got %d", cell.Width)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/compositor/ -run TestVTermLayerEmitsGraphemeCluster -v`
Expected: FAIL — content is `"e"` (base rune only), not the cluster.

- [ ] **Step 3: Emit the cluster in `cellToUVSnapshot`**

In `internal/ui/compositor/vtermlayer.go`, replace the `r := cell.Rune; if r == 0 { r = ' ' }` + `uvCell.Content = runeToString(r)` block with:

```go
	content := cell.GraphemeCluster
	if content == "" {
		content = runeToString(vterm.RenderableRune(cell.Rune))
	}

	uvCell := getCell()
	uvCell.Content = content
	uvCell.Style = vtermStyleToUV(style)
	uvCell.Width = cell.Width
	return uvCell
```
(Leave the underline-suppression check above it — it reads `cell.Rune` and stays correct.)

- [ ] **Step 4: Emit the cluster in `Canvas.Render` (harness/monitor path, for consistency)**

In `internal/ui/compositor/canvas.go` `Render`, replace `r := cell.Rune; if r == 0 { r = ' ' }; b.WriteRune(r)` with:

```go
			if cell.GraphemeCluster != "" {
				b.WriteString(cell.GraphemeCluster)
			} else {
				b.WriteRune(vterm.RenderableRune(cell.Rune))
			}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/compositor/ -run TestVTermLayer -v && go test ./internal/ui/compositor/`
Expected: PASS (new cluster test passes; existing layer/canvas tests unaffected).

- [ ] **Step 6: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/ui/compositor/...
git add internal/ui/compositor/
git commit -m "fix: emit grapheme clusters from compositor so wide glyphs don't break the border"
```

---

### Task 4: Fullscreen scroll gate — defer scroll to Claude

**Files:**
- Modify: `internal/ui/center/model_input.go` (gate PgUp/PgDown on `!tab.Fullscreen`)
- Modify: `internal/ui/center/model_input_mouse.go` (gate drag-edge autoscroll)
- Modify: `internal/ui/center/tab_actor.go`, `internal/ui/center/model_input_pty.go` (same drag-autoscroll guard, for consistency)
- Modify: `internal/ui/center/model_tabs.go`, `model_tabs_session.go`, `model_input_lifecycle.go` (`AllowAltScreenScrollback = !fullscreen`)
- Test: `internal/ui/center/` (behavioral test)

**Interfaces:**
- Consumes: `tab.Fullscreen`, `data.TabInfo.Fullscreen`, `ptyTabCreateResult.Fullscreen` / reattach `msg.Fullscreen` (all from the merged fullscreen feature).
- Produces: fullscreen tabs keep no vterm scrollback and never scroll their own vterm on PgUp/PgDown/drag; those inputs flow to Claude's PTY.

- [ ] **Step 1: Write the failing behavioral test**

Append to `internal/ui/center/mouse_forward_test.go` (reuse its Model/Tab constructor):

```go
func TestFullscreenTabDoesNotScrollOnPgUp(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = true
	before := tab.Terminal.ViewOffset
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if tab.Terminal.ViewOffset != before {
		t.Fatalf("fullscreen tab must not scroll its vterm on PgUp (offset %d -> %d)", before, tab.Terminal.ViewOffset)
	}
}

func TestClassicTabScrollsOnPgUp(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = false
	before := tab.Terminal.ViewOffset
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if tab.Terminal.ViewOffset == before {
		t.Fatalf("classic tab must scroll its vterm on PgUp")
	}
}
```
(If `m.Update` isn't the right entry for a keypress in the test harness, call the same handler the center uses for key input — mirror how `mouse_forward_test.go` drives `updateMouseWheel`. Verify the exact entry when implementing.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/center/ -run 'TestFullscreenTabDoesNotScrollOnPgUp|TestClassicTabScrollsOnPgUp' -v`
Expected: the fullscreen case FAILS (it currently scrolls).

- [ ] **Step 3: Gate PgUp/PgDown on `!tab.Fullscreen`**

In `internal/ui/center/model_input.go`, wrap the existing PgUp/PgDown `switch` (currently ~lines 250-289) in `if !tab.Fullscreen { … }`, unchanged body inside:

```go
	// PgUp/PgDown scroll medusa's own scrollback ONLY for classic tabs.
	// Fullscreen/alt-screen agents (Claude) own their scrollback, so these
	// keys fall through to the key→PTY forwarder below and reach the app.
	if !tab.Fullscreen {
		switch msg.Key().Code {
		case tea.KeyPgUp:
			// ...existing body unchanged...
			return m, nil
		case tea.KeyPgDown:
			// ...existing body unchanged...
			return m, nil
		}
	}
```
For a fullscreen tab the skipped switch falls through to the existing "Forward ALL keys to terminal" block (`model_input.go` ~309-333, `common.KeyToBytes(msg)` → `tabEventSendInput`/`SendString`), so PgUp/PgDown bytes reach Claude's PTY.

- [ ] **Step 4: Gate drag-edge autoscroll**

In `internal/ui/center/model_input_mouse.go` `updateMouseMotion`, guard the two `ScrollView` calls:

```go
			if termY < 0 {
				if !tab.Fullscreen {
					tab.Terminal.ScrollView(1)
				}
				termY = 0
			} else if termY >= termHeight {
				if !tab.Fullscreen {
					tab.Terminal.ScrollView(-1)
				}
				termY = termHeight - 1
			}
```
Apply the same `!tab.Fullscreen` guard to the duplicate autoscroll paths in `internal/ui/center/tab_actor.go` (the `selectionScrollDir` block) and `internal/ui/center/model_input_pty.go` (the selection-autoscroll tick).

- [ ] **Step 5: Disable alt-screen scrollback for fullscreen tabs**

- `internal/ui/center/model_tabs.go`: `term.AllowAltScreenScrollback = !msg.Fullscreen`
- `internal/ui/center/model_tabs_session.go`: `term.AllowAltScreenScrollback = !info.Fullscreen`
- `internal/ui/center/model_input_lifecycle.go`: `tab.Terminal.AllowAltScreenScrollback = !msg.Fullscreen`

(Leave the sidebar terminals' `AllowAltScreenScrollback = true` — they aren't Claude fullscreen agents.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ui/center/ -run 'TestFullscreenTabDoesNotScrollOnPgUp|TestClassicTabScrollsOnPgUp|TestSelection|TestFullscreenTabDoesNotScrollVterm|TestClassicTabScrollsVterm' -v`
Expected: PASS (fullscreen tab no longer scrolls on PgUp; classic still does; existing selection/wheel tests intact).

- [ ] **Step 7: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/ui/center/...
git add internal/ui/center/
git commit -m "fix: defer fullscreen scrollback to Claude (gate PgUp/PgDown/drag + disable alt-screen scrollback)"
```

---

### Task 5: Fix adopted-session scrollback parse (tmux capture is row-delimited)

**Files:**
- Modify: `internal/vterm/vterm.go` (`TreatLFAsCRLF` field; rewrite `PrependScrollback` body)
- Modify: `internal/vterm/parser.go` (honor `TreatLFAsCRLF` on LF)
- Create: `internal/vterm/vterm_capture.go` (`parseCaptureWithSize`, `captureLines`, `captureRowCount`, `trimCaptureTrailingNewline`)
- Modify: `internal/vterm/prepend_scrollback_test.go` (add row-count assertion)

**Interfaces:**
- Produces: `VTerm.TreatLFAsCRLF bool`; capture helpers; `PrependScrollback` parses newline-delimited capture rows correctly (no double-advance/mis-wrap).

- [ ] **Step 1: Write the failing test**

Add to `internal/vterm/prepend_scrollback_test.go`:

```go
func TestPrependScrollbackFullWidthRows(t *testing.T) {
	v := New(4, 3)
	// Three capture rows, each exactly Width(4) chars, newline-delimited.
	data := []byte("AAAA\nBBBB\nCCCC\n")
	v.PrependScrollback(data)
	if got := len(v.Scrollback); got != 3 {
		t.Fatalf("expected 3 scrollback rows, got %d (raw PTY parse double-advances full-width rows)", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vterm/ -run TestPrependScrollbackFullWidthRows -v`
Expected: FAIL — more than 3 rows (auto-wrap + newline double-advances).

- [ ] **Step 3: Add the `TreatLFAsCRLF` mode flag and honor it**

In `internal/vterm/vterm.go`, add near the other mode flags:

```go
	// TreatLFAsCRLF makes a bare LF also return to column 0. Used when parsing
	// tmux capture-pane history (newline-delimited rows) rather than a PTY stream.
	TreatLFAsCRLF bool
```

In `internal/vterm/parser.go`, the LF case (currently `case b == '\n': p.vt.newline()`):

```go
	case b == '\n': // LF
		if p.vt.TreatLFAsCRLF {
			p.vt.carriageReturn()
		}
		p.vt.newline()
```

- [ ] **Step 4: Add the capture helpers**

Create `internal/vterm/vterm_capture.go` (port from `amux/internal/vterm/vterm_capture.go`; medusa already has `isBlankLine` and `CopyLine`, so do not duplicate them):

```go
package vterm

import "bytes"

func parseCaptureWithSize(data []byte, width, height int) *VTerm {
	if len(data) == 0 {
		return nil
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	tmp := New(width, height)
	tmp.TreatLFAsCRLF = true
	tmp.Write(trimCaptureTrailingNewline(data))
	return tmp
}

func captureRowCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	trimmed := trimCaptureTrailingNewline(data)
	if len(trimmed) == 0 {
		return 1
	}
	return bytes.Count(trimmed, []byte{'\n'}) + 1
}

func captureLines(data []byte, tmp *VTerm) [][]Cell {
	if tmp == nil {
		return nil
	}
	lines := make([][]Cell, 0, len(tmp.Scrollback)+len(tmp.Screen))
	for _, line := range tmp.Scrollback {
		lines = append(lines, CopyLine(line))
	}
	screenRows := captureRowCount(data) - len(tmp.Scrollback)
	if screenRows <= 0 {
		return lines
	}
	if screenRows > len(tmp.Screen) {
		screenRows = len(tmp.Screen)
	}
	for i := 0; i < screenRows; i++ {
		lines = append(lines, CopyLine(tmp.Screen[i]))
	}
	return lines
}

func trimCaptureTrailingNewline(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	if bytes.HasSuffix(data, []byte("\r\n")) {
		return data[:len(data)-2]
	}
	if data[len(data)-1] == '\n' || data[len(data)-1] == '\r' {
		return data[:len(data)-1]
	}
	return data
}
```

- [ ] **Step 5: Rewrite `PrependScrollback` body (no caller changes)**

Replace the body of `PrependScrollback` in `internal/vterm/vterm.go` with:

```go
func (v *VTerm) PrependScrollback(data []byte) {
	if len(data) == 0 {
		return
	}
	tmp := parseCaptureWithSize(data, v.Width, v.Height)
	if tmp == nil {
		return
	}
	lines := captureLines(data, tmp)
	if len(lines) == 0 {
		return
	}
	newScrollback := make([][]Cell, 0, len(lines)+len(v.Scrollback))
	newScrollback = append(newScrollback, lines...)
	newScrollback = append(newScrollback, v.Scrollback...)
	v.Scrollback = newScrollback
	v.trimScrollback()
}
```
(The vterm is created at the current pane size immediately before each `PrependScrollback` call, so reusing `v.Width`/`v.Height` is correct — no message/caller plumbing needed.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/vterm/ -run 'TestPrependScrollback' -v && go test ./internal/vterm/`
Expected: PASS (new full-width-row test passes; existing prepend-scrollback tests intact).

- [ ] **Step 7: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/vterm/...
git add internal/vterm/
git commit -m "fix: parse tmux capture-pane scrollback as rows, not a PTY stream"
```

---

### Task 6: Sync-stall failsafe

**Files:**
- Modify: `internal/vterm/vterm.go` (`import "time"`; `syncStartedAt` field; `maybeReleaseStaleSync` call in `Write`)
- Modify: `internal/vterm/sync.go` (`SyncStallTimeout`, `syncNow`, `maybeReleaseStaleSync`; set `syncStartedAt` on begin)
- Modify: `internal/vterm/render.go` (`maybeReleaseStaleSync` in `RenderBuffers`)
- Modify: `internal/vterm/accessors.go` (`maybeReleaseStaleSync` in `Version`)
- Create: `internal/vterm/sync_stall_test.go` (ported)

**Interfaces:**
- Produces: an open synchronized-output region auto-releases after `SyncStallTimeout` (2s) on the next `Write`/`RenderBuffers`/`Version`, so a writer dying mid-frame can't freeze the pane. `syncNow` (package var) is stubbable in tests.

- [ ] **Step 1: Write the failing test (port from Amux)**

Create `internal/vterm/sync_stall_test.go` porting `amux/internal/vterm/sync_stall_test.go`: begin a sync region, stub `syncNow` to advance past `SyncStallTimeout`, then assert `RenderBuffers()` returns the live (not frozen) buffers and `syncActive` is false after the next `Write`/`RenderBuffers`/`Version`. Restore `syncNow` with `defer`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vterm/ -run TestSyncStall -v`
Expected: FAIL — `SyncStallTimeout`/`syncNow`/`maybeReleaseStaleSync` undefined.

- [ ] **Step 3: Add the field and import**

In `internal/vterm/vterm.go`: add `"time"` to imports; add near the sync fields:

```go
	// syncStartedAt is when the open sync region began; used by the stall
	// failsafe (SyncStallTimeout).
	syncStartedAt time.Time
```

- [ ] **Step 4: Add the failsafe to `sync.go` and stamp the start time**

In `internal/vterm/sync.go` add `import "time"` and:

```go
const SyncStallTimeout = 2 * time.Second

// syncNow returns the current time for sync stall tracking; tests may stub it.
var syncNow = time.Now

func (v *VTerm) maybeReleaseStaleSync() {
	if v.syncActive && syncNow().Sub(v.syncStartedAt) > SyncStallTimeout {
		v.setSynchronizedOutput(false)
	}
}
```
In `setSynchronizedOutput`, on the `active` branch, add `v.syncStartedAt = syncNow()` right after `v.syncActive = true`. Keep the rest of medusa's body (including `v.invalidateRenderCache()`).

- [ ] **Step 5: Call the failsafe at the three entry points**

- `internal/vterm/vterm.go` `Write`: `v.maybeReleaseStaleSync()` as the first line.
- `internal/vterm/render.go` `RenderBuffers`: `v.maybeReleaseStaleSync()` as the first line.
- `internal/vterm/accessors.go` `Version`: `v.maybeReleaseStaleSync()` as the first line.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/vterm/ -run TestSyncStall -v && go test ./internal/vterm/`
Expected: PASS.

- [ ] **Step 7: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/vterm/...
git add internal/vterm/
git commit -m "fix: release a stalled synchronized-output region after a timeout"
```

---

### Task 7: Clamp the cursor on alt-screen exit

**Files:**
- Modify: `internal/vterm/cursor.go` (`exitAltScreen`)
- Modify: `internal/vterm/cursor_test.go` (add exit-clamp assertion)

**Interfaces:**
- Produces: after exiting alt screen, the restored cursor is clamped in range even if a resize shrank the screen while in alt screen.

- [ ] **Step 1: Write the failing test**

Add to `internal/vterm/cursor_test.go`:

```go
func TestExitAltScreenClampsCursor(t *testing.T) {
	v := New(80, 24)
	v.enterAltScreen()
	v.CursorX = 40
	v.CursorY = 20
	// Save these as the alt-cursor via a main-screen cursor before exit:
	v.altCursorX = 40
	v.altCursorY = 20
	v.Resize(10, 5) // shrink while in alt screen
	v.exitAltScreen()
	if v.CursorX >= v.Width || v.CursorY >= v.Height {
		t.Fatalf("cursor not clamped after alt-screen exit: (%d,%d) in %dx%d", v.CursorX, v.CursorY, v.Width, v.Height)
	}
}
```
(When implementing, confirm the exact fields `altCursorX`/`altCursorY` and how `enterAltScreen` records them; adjust the setup to make `exitAltScreen` restore an out-of-range cursor. Mirror the alt-screen cases already in `cursor_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vterm/ -run TestExitAltScreenClampsCursor -v`
Expected: FAIL — restored cursor out of range.

- [ ] **Step 3: Add the clamp**

In `internal/vterm/cursor.go` `exitAltScreen`, add `v.clampCursor()` before `v.invalidateRenderCache()`:

```go
	v.AltScreen = false
	v.Screen = v.altScreenBuf
	v.altScreenBuf = nil
	v.CursorX = v.altCursorX
	v.CursorY = v.altCursorY
	v.clampCursor()
	v.invalidateRenderCache()
```
Do **not** add `invalidateAltScreenCapture()` (capture layer is out of scope).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vterm/ -run TestExitAltScreenClampsCursor -v`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/vterm/...
git add internal/vterm/
git commit -m "fix: clamp cursor when exiting the alt screen"
```

---

### Task 8: Full gate + manual verification

**Files:** none (verification only).

- [ ] **Step 1: Full CI-mirroring gate**

Run: `make lint`
Expected: `go test -race ./...` passes, golangci-lint 0 issues, 500-line check passes. Fix any failure before proceeding (in particular any residual `vterm_widechar_test.go` drift from the runewidth bump).

- [ ] **Step 2: Render harness**

Run: `make release-check`
Expected: tests + monitor/center/sidebar harness modes all pass.

- [ ] **Step 3: Manual smoke test (`make build` + restart medusa)**

1. `make build`, restart medusa (the running binary must be rebuilt to include these fixes).
2. Open a Claude agent tab (fullscreen default). Print emoji/CJK at the right edge (e.g. a message ending in an emoji) — the right border no longer breaks.
3. Scroll (wheel + PgUp/PgDown) in the fullscreen tab — Claude's own history scrolls, matching a direct `tmux attach`; medusa shows no garbled scrollback.
4. Adopt/restore an existing tmux session — restored history is correctly wrapped, not garbled.
5. Select and copy text containing an emoji/accent — the clipboard has the full grapheme.

- [ ] **Step 4: Final commit (only if the gate required fixes)**

```bash
git add -A
git commit -m "fix: reconcile width-test expectations after runewidth bump"
```
(Skip if steps 1–2 required no changes.)

---

## Self-Review

- **Spec coverage:** grapheme model → Tasks 2–3 (Bug 1 + garbled text/selection, spec §1); revert clip → Task 1 (spec "Relationship to prior work"); fullscreen scroll gate → Task 4 (Bug 2a, spec §2); adopted-session parse → Task 5 (Bug 2b, spec §3); sync failsafe → Task 6 (spec §4); exitAltScreen clamp → Task 7 (spec §5); testing/gate → Task 8 + per-task tests. Out-of-scope items (capture layer, `IgnoreCursorVisibilityControls`) are excluded per spec.
- **Type consistency:** `GraphemeCluster` (Task 2) consumed by `writeCellContent`/`cellToUVSnapshot`/`Canvas.Render`/`GetTextRange` (Tasks 2–3); `RenderableRune` defined in Task 2, used in Task 3; `TreatLFAsCRLF` + capture helpers defined and used within Task 5; `syncStartedAt`/`syncNow`/`maybeReleaseStaleSync`/`SyncStallTimeout` defined and called within Task 6; `tab.Fullscreen`/`msg.Fullscreen`/`info.Fullscreen` reused from the merged fullscreen feature (Task 4).
- **Placeholder scan:** implementation steps carry exact code. Three steps flag an implementation-time confirmation against the real harness (Task 4 Step 1 key-input entry point; Task 7 Step 1 alt-cursor field setup) and one reconcile pass (Task 2 Step 8 / Task 8 widechar drift) — each names exactly what to confirm and where. Ported test files (grapheme, sync-stall) reference the Amux source file:line + the required assertions, with the skip-list called out.
