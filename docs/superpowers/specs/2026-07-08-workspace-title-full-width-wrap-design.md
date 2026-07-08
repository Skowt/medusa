# Design: full-width workspace titles with wrap-on-focus

Date: 2026-07-08
Branch: `longer-workspace-names`
Status: approved for planning

## Problem

Workspace names in the left dashboard pane get cut off. The pane is a fixed
35-column width (`internal/ui/layout/manager.go:57`, `startupLeftWidth: 35`), so
after the border and padding a name has only ~31 content columns, and
`renderWorkspaceLine1` reserves 7 of those on **every** row for action icons that
only render when the row is selected (`internal/ui/dashboard/dashboard_render_lines.go:17`,
`rightSlotWidth = 7`). An unselected name gets ~20 columns and is right-truncated
with `…`.

The user's goal (chosen during brainstorming): **read the full name, at least for
the workspace currently under the cursor.**

Two observations shaped the design:

1. **Selected rows already expand.** A selected multi-repo workspace already grows
   from 2 to 3 lines to list its repos (`renderWorkspaceLine3`, gated on
   `selected && len(ws.Repos) >= 2`). Wrapping the name on the selected row is the
   same idiom, not a new one.
2. **The reserved 7 columns are wasted on unselected rows.** The `+ ≡ ×` icons only
   appear when selected, yet every row pays for them.

## Locked decisions

- **Name takes the full content width in both states.** No columns are reserved for
  buttons on the name's line, selected or not.
- **Unselected row:** single name line at full width; right-truncate with `…` only
  if the name is longer than the whole pane. (No "smart"/middle truncation — the goal
  is full-name-on-focus, and unselected rows already show much more once the reserved
  columns are reclaimed.)
- **Selected row:** the name wraps to show in full, capped at **3 lines** (`…` on the
  last line past the cap). Action controls move to their own **footer row** below the
  metadata.
- **Controls become text labels, not icons:** `[dupe] [group] [archive]`. On a
  dedicated row there is width for legible, discoverable labels.
- **Label parity with today** — three controls, same actions. No new actions (e.g.
  `[rename]`) in this change.
- **Scope: active workspace rows only.** Archived rows (`renderArchivedRow`, 1 line)
  and orphan rows (`renderOrphanRow`) keep their current layout; they are only touched
  to stop reserving width they don't need (see §6).

### Label accuracy (verified)

The footer labels map to the existing handlers and describe what they actually do:

| Label       | Handler (`dashboard_navigation.go`) | Effect |
|-------------|-------------------------------------|--------|
| `[dupe]`    | `handleDuplicate` (:385)            | `DuplicateWorkspace` |
| `[group]`   | `handleSetGroup` (:370)             | `ShowSetWorkspaceGroupDialog` |
| `[archive]` | `handleDelete` (:257)               | active workspace → `ShowArchiveWorkspaceDialog` |

The footer only shows on a selected **active** workspace, and for an active
workspace `handleDelete` opens the **archive** dialog (`dashboard_navigation.go:272`),
not a hard delete — so `[archive]` is truthful. (Archived/orphaned rows hard-delete,
but they render via a different path and expose no footer.)

## Design

### Layout

Unselected active row (name at full width):
```
○ PE-37895-place-to-place-migrat…
  └ 3 Repos · Work · Clean · Tue
```

Selected active row (full name wrapped; controls as a footer):
```
● PE-37895-place-to-place-migration
  -spike
  └ 3 Repos · Work · Clean · Tue
  └ cargo-core, e2e-tests, mgmt-app
  [dupe] [group] [archive]
```

The `[name → metadata]` block is byte-identical in structure to the unselected row;
selection only *appends* the repo-list line (when multi-repo, as today) and the button
footer. Moving the cursor on/off a row therefore does not shift the part you were
reading.

### 1. Shared line model (the correctness lynchpin)

Today `rowLineCount` (`dashboard_navigation.go:63`) hardcodes heights (1 archived,
3 selected-multi-repo, 2 otherwise) and the renderer independently joins lines with
`"\n"`. Once the name wraps, the height depends on the name, the width, and selection —
the renderer and `rowLineCount` **must not** compute it independently or scrolling and
mouse hit-testing will disagree.

Introduce one helper that is the single source of truth, e.g.:

```go
// workspaceRowLines returns the ordered display lines for an active workspace row.
func (m *Model) workspaceRowLines(ws *data.Workspace, selected bool, contentWidth int) []string
```

- The renderer joins the returned slice with `"\n"`.
- `rowLineCount` returns `len(...)` of the same slice for active workspace rows.

Both call the same wrap routine, so they cannot drift. `rowLineCount` keeps its
existing fast paths for archived rows and non-workspace row types.

### 2. Name wrapping

- Wrap width = `contentWidth - 2` (the 2-column indicator/hanging indent). The first
  line is `"● " + chunk`; continuation lines are `"  " + chunk`, so every name line has
  the same usable width and continuations align under the first character of the name.
- Width is measured with `lipgloss.Width` (grapheme-cluster aware, consistent with the
  emulator work in commit `8d7e8f5`), never `len`.
- Break at the wrap width. **Preference:** if a hyphen falls within the last few
  columns of a line, break just after it (slug names like
  `no-ticket-prompt-injection-hardening-pass` read better broken at `-`). This is a
  nicety; a hard width break is the fallback and is acceptable on its own if the hyphen
  logic proves fiddly.
- Cap at 3 name lines; if the name still overflows, the third line ends with `…`.
- Unselected: exactly one line, right-truncated with `…` at the full wrap width (this
  is the existing truncation, just without the `rightSlotWidth` subtraction).

### 3. Reclaim the reserved columns

- Delete the unconditional `rightSlotWidth` reservation from the name's width budget
  (`dashboard_render_lines.go:110`).
- Drop the 7-space `rightSlot` string on unselected rows and the inline
  `" + ≡ × "` slot on selected rows (`dashboard_render_lines.go:101-105,129`) — controls
  now live only in the footer.

### 4. Button footer

- Rendered only for a selected active workspace, as the last line of the row.
- Content: `[dupe] [group] [archive]`, indented 2 columns to align with the name/metadata
  text. Selected background fill via the existing `padWithBg` (`dashboard_render.go:89`).
- At render time, store each label's hit box as **line offset within the row + X range**
  (`{line, x0, x1}`), not just an X. Replace the current single-X icon fields
  `duplicateIconX` / `groupIconX` / `deleteIconX` (`model.go:76-78`) with these. Storing
  the line makes hit-testing correct whether the footer is one line or (narrow pane)
  wrapped across several.
- **Narrow-pane fallback:** at the default width (35) the footer fits on one line
  (`[dupe] [group] [archive]` ≈ 24 cols + indent ≤ 31). If `contentWidth` cannot fit all
  three labels (only possible near `minDashboardWidth = 20`), wrap the footer
  button-by-button onto additional lines; because line count comes from
  `workspaceRowLines` (§1), scrolling and hit-testing stay correct automatically.

### 5. Click hit-testing (add a line check)

`model_update.go:37-56` currently checks only the click's **X** against the icon
positions, relying on the icons being on line 1. With the controls on a footer sub-line,
a click on the metadata line at a matching X would misfire. Fix:

- Extend `rowIndexAt` (`dashboard_navigation.go:89`) to also return the **line offset
  within the row** (`lineWithinRow = rowY - visLine`, computed in the existing walk at
  :147 and the archived walk at :159). Update its one caller in `model_update.go:25`.
- A control click requires `idx == m.cursor` and a label whose stored `{line, x0, x1}`
  matches `lineWithinRow` and `contentX`. Otherwise fall through to normal row
  activation. (Matching the stored line — rather than assuming "the last line" — is what
  keeps this correct if the footer wraps on a narrow pane.)

### 6. Stale-position resets and other row types

- The reset path that guards against stale `deleteIconX` (`dashboard_render.go:179-186`)
  and the orphan reset (`:133-134`) must set the new range fields to an empty/no-hit
  value on rows that expose no footer, so a click never resolves to a control on those
  rows.
- Orphan rows (`renderOrphanRow`, `dashboard_render.go`) render `ws.Name` **untruncated**
  today (`dashboard_render.go:124`) and reserve a 3-column delete slot. Reuse the
  full-width truncation for the orphan name and reclaim the slot when unselected. No
  wrap/footer for orphan rows (out of scope).
- Archived rows stay single-line.

## Files touched (all under `internal/ui/dashboard/`)

- `dashboard_render_lines.go` — `renderWorkspaceLine1` full-width name + wrap; new footer
  renderer; drop `rightSlotWidth`.
- `dashboard_render.go` — `renderWorkspaceRow` joins the shared line slice; footer append;
  orphan name truncation; range-field resets.
- `dashboard_navigation.go` — `workspaceRowLines` helper; dynamic `rowLineCount`;
  `rowIndexAt` returns within-row line offset.
- `model_update.go` — control-click detection uses line offset + label X ranges.
- `model.go` — replace icon-X fields with label X-range fields.

None of these files is near the 500-line limit (largest is `dashboard_navigation.go` at
412); the shared helper adds a modest amount. If any file approaches the limit, split by
concern into a sibling file in the same package rather than inflating one file.

## Testing

- **Line-count agreement (core invariant):** table test asserting `rowLineCount(i)` equals
  `strings.Count(rendered, "\n") + 1` for the rendered row, across: short unselected name,
  long name selected wrapping to 2 and 3 lines, over-cap name (`…`), selected single-repo,
  selected multi-repo (repo-list line present), archived, orphan. Extend
  `rebuild_rows_test.go`.
- **Control clicks:** clicking each label's X on the footer line dispatches the right
  handler; the same X on the metadata line and on an unselected row does **not**. Extend
  `click_routing_test.go`.
- **Reclaimed width:** an unselected name that previously truncated now shows more (assert
  the rendered name is longer / no longer truncated at a representative width).
- **Wrap correctness:** name wraps at the width boundary, respects the 3-line cap with a
  trailing `…`, and hyphen-preference break (if implemented) lands after a `-`.

## Definition of done (repo `CLAUDE.md`)

1. `make fmt`.
2. `golangci-lint run ./internal/ui/dashboard/...` exits 0.
3. No `.go` file exceeds 500 lines.
4. Touched-package tests pass.
5. At the end: `make lint` (race + lint + line check). Because this is UI-adjacent,
   also run `release-check` (or `go run ./cmd/medusa-harness` in each mode) to validate
   rendering without a terminal.

## Out of scope

- Widening or making the dashboard pane resizable.
- Smart/middle truncation to disambiguate shared prefixes.
- Shortening workspace names at the source (branch-name generation).
- New actions in the footer (e.g. `[rename]`, `[profile]`).
