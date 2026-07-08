# Design: medusa terminal-emulator fidelity fixes

Date: 2026-07-08
Branch: `tui-full-screen-mode`
Status: approved for planning
Reference implementation: **Amux** (`github.com/andyrewlee/amux`) — medusa was forked from/inspired by it; same stack (Bubble Tea v2, lipgloss v2, ultraviolet, creack/pty) and same architecture (`internal/vterm` emulator + `internal/ui/compositor`). Amux renders the same content correctly; medusa carries an older, lower-fidelity version of the emulator.

## Problem

medusa's own terminal emulator diverges from what a real terminal produces, causing two visible defects:

1. **Center pane right border breaks** near wide glyphs (emoji, CJK).
2. **Scrollback / history renders garbled** — both when scrolling a fullscreen (alt-screen) agent and when adopting an existing tmux session.

Evidence that the fault is medusa's emulation (not Claude, not tmux): the same agent renders correctly when run standalone (`claude`) and when the same tmux session is attached directly (`tmux attach`). Amux, with the same architecture, renders it correctly.

## Root causes (verified against both codebases)

### Bug 1 — single-rune cell model
`internal/vterm/cell.go` `Cell` stores only `Rune rune` (plus `Width`, `Style`). Combining marks and emoji modifiers (VS16 `U+FE0F`, ZWJ `U+200D`, skin-tone modifiers) arrive as separate width-0 runes; `putChar`'s width-0 branch is a no-op (`internal/vterm/ops.go:11-30`, literally `_ = cell // Currently no-op for combining characters`). So medusa emits a *different* glyph than tmux did, and its declared `Width` (from `mattn/go-runewidth v0.0.19`, stale tables) may not equal the emitted glyph's physical display width.

The center pane renders via the ultraviolet layer path (`internal/ui/compositor/vtermlayer.go` `cellToUVSnapshot`: `uvCell.Content = runeToString(cell.Rune)`, `uvCell.Width = cell.Width`). ultraviolet trusts the declared `Width` for cursor advance. When declared width ≠ physical width, the physical cursor drifts one column and the **next composited cell — the chrome border — prints at the wrong column**. Each wide glyph is an independent drift point → two wide glyphs → two broken spots.

The already-committed boundary clip (`vtermlayer.go` `DrawAt`, commit `e3dc798`) only handles a width-2 cell with no room for its continuation at the very last drawn column — a distinct mechanism (an over-wide row left by a resize-shrink) that does **not** address this width-drift. It will be **reverted**: Amux's live `DrawAt` has no such clip yet renders correctly with byte-identical resize row-preservation, so the case it guards is a transient the app's resize-redraw immediately overwrites in practice. The grapheme model is the real border fix; the sibling guard in the non-live `Canvas.DrawScreen` stays (both repos keep it).

### Bug 2a — fullscreen (alt-screen) scrollback is medusa's, not Claude's
Every agent terminal sets `AllowAltScreenScrollback = true` (`internal/ui/center/model_tabs.go`, `model_tabs_session.go`, `model_input_lifecycle.go`), so `scrollbackEnabled()` is true even in alt screen. medusa has none of Amux's alt-screen frame-capture/dedup machinery, so `scrollUp` (`internal/vterm/scroll.go`) captures raw partial alt-screen frames, and `renderWithScrollbackFrom` (`internal/vterm/render.go`) concatenates that garbage scrollback with the live screen → garble. Additionally, only the mouse **wheel** is forwarded to Claude for fullscreen tabs (`model_input_mouse_forward.go`); PgUp/PgDown (`model_input.go:252,271`, comment "these don't conflict with embedded TUIs") and drag-edge autoscroll (`model_input_mouse.go` `updateMouseMotion`, `ScrollView(±1)`) still drive medusa's own vterm scrollback.

### Bug 2b — adopted-session scrollback parsed as a raw PTY stream
`PrependScrollback` (`internal/vterm/vterm.go:393`) does `tmp := New(...); tmp.Write(data)` on tmux `capture-pane` output. That output is newline-delimited rows, but the PTY parser treats `\n` as line-feed/scroll and lets full-width rows double-advance → mis-wrapped, garbled restored history when adopting an existing tmux session.

## Locked decisions

- **Keep the composited multi-pane UI; fix the emulator** (no native-tmux passthrough rearchitecture — that would sacrifice the always-visible sidebar/panes, which is medusa's core UX).
- **Fullscreen scrollback defers to Claude (minimal):** for fullscreen tabs medusa keeps no vterm scrollback and forwards *all* scroll input to Claude. We do **not** port Amux's alt-screen capture layer.
- Full scope: grapheme-cluster cell model + fullscreen scroll gate + adopted-session parse fix + two small robustness fixes.

## Design

### 1. Grapheme-cluster cell model (fixes Bug 1 + garbled text/selection)
Adopt Amux's approach:
- Add `GraphemeCluster string` to `vterm.Cell` (`cell.go`). It holds the full base+combining/emoji cluster; empty means "just `Rune`".
- In `putChar` (`ops.go`), replace the width-0 no-op with Amux's fold (`amux/internal/vterm/ops.go:18-35`): step back over any continuation cell to the base cell, initialize its `GraphemeCluster` to the base rune if empty, and append the width-0 rune. Never advance the cursor, never change `Width`.
- Emit the cluster (fall back to `Rune`) at **every** serialization site — all must change together:
  - `internal/ui/compositor/vtermlayer.go` `cellToUVSnapshot` (live/center path).
  - `internal/ui/compositor/canvas.go` `Render` (harness/RenderTerminal path).
  - `internal/vterm/render.go` `renderRow` and `renderWithScrollbackFrom`.
  - `internal/vterm/selection.go` `GetSelectedText` / `GetTextRange` (fixes copy of emoji/accented text — gap #5).
- Bump `mattn/go-runewidth` to `v0.0.24` (match Amux's tables). Promote `rivo/uniseg` to a direct dependency only if the port needs it; the fold itself is codepoint-level and does not require full segmentation.
- Port Amux's `grapheme_test.go` and the relevant `vterm_widechar_test.go` cases (cluster accumulation, `Width` unchanged, render + scrollback + selection use the cluster, combining mark at the row/col-0 boundary — gap #8).

### 2. Fullscreen scroll gate — defer to Claude (fixes Bug 2a)
For fullscreen tabs (`tab.Fullscreen`):
- Do **not** enable medusa's alt-screen scrollback: set `AllowAltScreenScrollback = false` for fullscreen agent terminals (so `scrollUp` captures nothing and the scrolled view has nothing to garble).
- Forward **all** scroll input to Claude, not just the wheel:
  - **PgUp/PgDown** (`model_input.go:252,271`): gate the scrollback interception on `!tab.Fullscreen`. For fullscreen tabs, do not intercept — let the keys fall through to medusa's normal key→PTY forwarding (the same path all other keystrokes take), so Claude receives PgUp/PgDown.
  - **Drag-edge autoscroll** (`model_input_mouse.go` `updateMouseMotion`, `ScrollView(±1)`): gate on `!tab.Fullscreen` (for fullscreen tabs the drag is already forwarded to the PTY as SGR motion via `forwardMouse`, so medusa must not also scroll its own vterm).
  - The mouse **wheel** is already forwarded for fullscreen tabs (`model_input_mouse_forward.go`); no change there.
- Net effect: a fullscreen tab behaves like a direct `tmux attach` — Claude's renderer owns history.

Classic (non-fullscreen) tabs keep medusa's existing scrollback behavior unchanged.

### 3. Adopted-session scrollback parse fix (fixes Bug 2b)
Replace the raw-PTY parse in `PrependScrollback` with a width-aware capture parse, porting Amux's `PrependScrollbackWithSize` / `parseCaptureWithSize` (`amux/internal/vterm/vterm_scroll.go`): treat the capture as newline-delimited rows, resetting to column 0 on each LF so full-width rows don't double-advance. Thread the pane width/height through from the caller (`model_tabs.go` `PrependScrollback(msg.ScrollbackCapture)`).

### 4. Sync-stall failsafe (robustness gap #4)
medusa freezes `RenderBuffers()` on DEC-2026 synchronized-output begin (`render.go`, `sync.go`) with no timeout — a writer dying mid-frame freezes the pane. Port Amux's `SyncStallTimeout` (~2s) force-release, checked on both `Write` and `RenderBuffers`.

### 5. Alt-screen-exit cursor clamp (robustness gap #6)
`exitAltScreen` (`internal/vterm/cursor.go`) omits `clampCursor()` (and any capture invalidation). After a resize while in alt screen the restored cursor can be out of range. Add `clampCursor()` on exit, mirroring Amux (`amux/internal/vterm/cursor.go:137-149`).

## Relationship to prior work
- **Revert the boundary clip (`e3dc798`).** Amux's live `DrawAt` omits it despite identical resize row-preservation, confirming it is unnecessary; the grapheme model is the real border fix. Revert removes the `DrawAt` clip and its test only — the sibling guard in `Canvas.DrawScreen` (predating it, kept by both repos) is untouched.
- This builds directly on the fullscreen-TUI feature already merged: `tab.Fullscreen`, `activeTabForwardsMouse`, `forwardMouse`, and the wheel forwarding are reused for the scroll gate.

## Out of scope
- Amux's full alt-screen frame-capture/dedup subsystem (`alt_screen_capture*.go`, `alt_screen_restore_pending.go`, `vterm_scrollback_delta.go`) and its chat-history-only scrolled view (`model_scrolled_history.go`). Redundant once fullscreen defers to Claude.
- `IgnoreCursorVisibilityControls` (gap #7, cosmetic flicker) — deferred.

## Testing
- **Unit (vterm):** ported grapheme tests (cluster fold, width unchanged, boundary combining mark); serialization emits the cluster at each site; selection returns the cluster; `PrependScrollback` width-aware parse produces correctly-wrapped rows for a newline-delimited capture; sync-stall force-release after timeout; `exitAltScreen` clamps the cursor.
- **Unit (compositor):** existing wide-char tests plus a case that a combining/emoji cell serializes to the full cluster with correct declared width, so column accounting (and thus the border) stays intact.
- **Behavioral (center):** fullscreen tab does not scroll medusa's vterm on PgUp/PgDown/drag (forwarded instead); classic tab still scrolls its vterm.
- **Gate:** `make lint` (race + golangci-lint + 500-line) and `make release-check` (render harness) both pass.
- **Manual:** emoji/CJK at the right edge no longer breaks the border; scrolling a fullscreen Claude tab shows Claude's own history (matches a direct `tmux attach`); adopting an existing tmux session restores non-garbled history.

## Files touched (estimate)
- `internal/vterm/cell.go` (field), `ops.go` (grapheme fold), `render.go` (emit cluster + sync failsafe), `selection.go` (cluster), `cursor.go` (exit clamp), `sync.go` (timeout), `vterm.go`/`vterm_scroll.go` (width-aware PrependScrollback).
- `internal/ui/compositor/vtermlayer.go`, `canvas.go` (emit cluster).
- `internal/ui/center/model_input.go`, `model_input_mouse.go`, `model_input_mouse_forward.go` (fullscreen scroll gate + PgUp/drag forwarding), tab-creation sites for `AllowAltScreenScrollback` on fullscreen tabs.
- `go.mod` / `go.sum` (runewidth bump).
- Ported/added tests across `internal/vterm` and `internal/ui/compositor` and a center behavioral test.
