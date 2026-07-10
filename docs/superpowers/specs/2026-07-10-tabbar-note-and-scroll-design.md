# Tab bar: always-visible note, scrollable agent tabs

## Problem

Selecting a workspace that has a note lands the user on the Info tab instead of
their agent. The note is the reason for the redirect, but it is a single line of
text — it does not warrant displacing the agent the user came to look at.

Two changes follow from that:

1. A workspace always opens on its agent tab.
2. The note becomes visible from the agent tab, so nothing is lost by not
   redirecting.

Putting the note in the tab bar means the bar now has to share a fixed-width row
between the Info tab, a variable number of agent tabs, the `+ New Agent` button,
and the note. Agent tabs therefore become horizontally scrollable.

## Current behaviour

`Model.SetWorkspace` (`internal/ui/center/model_info.go:38`):

```go
m.infoTabActive = ws != nil && ws.Note != ""
```

`renderTabBar` (`internal/ui/center/model_render_tabbar.go`) walks left to right
accumulating an `x` cursor, appending rendered segments to `renderedTabs` and
hit regions to `m.tabHits`, then joins with `lipgloss.JoinHorizontal`. Every tab
is always drawn; there is no clipping and no scroll state. The right-hand side of
the bar is empty.

## Design

### 1. Remove the note-based default

`model_info.go:38` becomes:

```go
m.infoTabActive = false
```

The doc comment on `SetWorkspace` drops its "If the workspace has a note…"
sentence.

Explicitly unchanged:

- The Info tab still auto-activates when a workspace has no agent tabs.
  `IsInfoTabActive()` returns true when `len(m.getTabs()) == 0`, and
  `model_render.go:50` sets the flag on that path.
- `SelectInfoTab()` and the `tabHitInfo` click path.

The only behaviour removed is "workspace has a note ⇒ open on Info".

### 2. Tab bar layout

Four zones across `m.contentWidth()`:

```
 ● Info  ‹ ● tests ×  ● docs × ›  + New Agent        Fix the auth redirect…
└──────┘└──────────────────────┘└───────────┘      └────────────────────┘
 pinned      scroll viewport        pinned           note, right-aligned
```

No divider glyphs between zones. The active tab's background highlight already
provides the visual separation.

#### Width allocation

Applied in priority order. Pinned chrome is never sacrificed, and the note is
never fully hidden when one is set.

Measured segment widths, which every threshold below depends on: Info tab `8`,
`+ New Agent` `13`, one agent tab `12`, `arrowWidth` `2`. A viewport needs `14`
cells to show one tab plus the arrow that "not all tabs fit" implies.

1. **Pinned chrome.** The Info tab and `+ New Agent` take their natural width,
   plus a one-cell gap separating `+ New Agent` from the note.
   (`+ New Agent` is omitted for archived workspaces, as today.)
2. **Note floor.** If `ws.Note != ""`, the note reserves only its *minimum*
   before the viewport gets anything:

   ```
   noteCap  = max(contentWidth/3, minNoteWidth)   // minNoteWidth = 8
   noteFull = min(displayWidth(note), noteCap)    // its desired width
   reserved = min(noteFull, minNoteWidth)         // its floor, for now
   ```

   then clamped into `max(0, contentWidth - infoWidth - plusWidth - gap)`.

   The `minNoteWidth` cap floor matters on narrow panes: a third of a 20-cell
   bar is 6, which would squeeze the note below legibility.

3. **Tab viewport.** Everything remaining.

4. **Note yields to the first tab.** Chrome (21) + gap (1) + the note's 8-cell
   floor + one tab (12) + its arrow (2) needs **44** cells. Below that the floor
   and the first agent tab cannot both fit, and *the tabs win*: shrink the note,
   down to a floor of **one cell** (rendering as a lone `…`), until a tab fits.

   Shrink only when it actually buys a visible tab. At `contentWidth` 36 the
   widest viewport any visible note allows is 13 cells — one short of the 14 a
   tab needs — so shrinking there would cost legibility and gain nothing; the
   note keeps its full floor and no tab renders.

   **A set note is never hidden entirely.** That was the point of moving it into
   the tab bar: the user must know one exists.

5. **Note growth.** With the viewport satisfied, the note grows back toward
   `noteFull` using whatever is spare (`avail - sum(tabWidths)`). Growth is
   bounded by that spare, so a grown note can never push the tabs into needing
   arrows. Wide panes therefore render exactly as they would have under a
   naive "note takes its third first" rule.

6. **Arrows.** If the agent tabs do not all fit in the remainder, `‹` and `›`
   appear inside the viewport and consume 2 cells each. When
   `arrowCells > avail`, both are suppressed rather than drawn unbudgeted.

Resulting behaviour across widths (4 tabs, a 25-cell note):

| `contentWidth` | note cells | tabs |
| --- | --- | --- |
| 24 | 2 | 0 |
| 30–36 | 8 | 0 |
| 37 | 1 | 1 |
| 40 | 4 | 1 |
| 44–52 | 8 | 1 |
| 56 | 8 | 2 |
| 116 | 25 | 4 |

#### Narrow panes

`noteWidth` caps against the *full* `contentWidth` and cannot know how many
cells the pinned chrome already claimed. The renderer must therefore clamp the
note a second time, into the space the chrome actually left:

```
room     = max(0, contentWidth - infoWidth - plusWidth - gap)
reserved = min(reserved, room)
```

Without this, a 26-cell bar still grants the note its 8-cell minimum on top of
~21 cells of chrome and assembles a 31-cell line — wider than its own pane.
That tears the compositor border, the same failure class fixed in `163add0`.
This is not a degenerate case: a `contentWidth` of 26–32 is an ordinary pane
with the sidebar open.

Likewise, when `avail` collapses to zero, `visibleTabs` still reports
`showNext: true` (tabs exist that it cannot show). The renderer must suppress
both arrows when `arrowCells > avail`, or it draws a 2-cell affordance it never
budgeted for.

The note shrinks into whatever remains and disappears only when zero cells are
left. The Info tab, the agent tabs, and `+ New Agent` are never sacrificed for
it.

**Known limitation, pre-existing and out of scope:** the pinned chrome is ~21
cells and does not narrow responsively, so a `contentWidth` below that still
overflows — by exactly the chrome width, with the note and arrows contributing
nothing. Before this change the same bar overflowed by 12–28 cells at those
widths, because the old renderer had no width awareness at all.

#### Truncation

The note is truncated to its allocated width with a trailing `…` using
`ansi.Truncate` from `github.com/charmbracelet/x/ansi` (already a direct
dependency). `len()` is wrong here: notes are free text and may contain wide
glyphs or combining marks, and the compositor is sensitive to width
miscalculation.

Agent tab names are **not** truncated. Tabs that do not fit scroll out of view
instead.

#### Whole tabs only

The viewport renders the largest run of *complete* tabs starting at
`tabScrollOffset` that fits in the available width. No tab is ever half-drawn.

This is a correctness property, not an aesthetic one: the close-button hit
region is computed by anchoring from a tab's right edge
(`closeX = x + renderedWidth - leftFrame - closeWidth`). A partially clipped tab
would place that region off the end of the viewport, so clicking near the right
arrow would close a tab the user cannot see.

#### Scroll state

Two fields on `Model`:

```go
tabScrollOffset      int   // index of the leftmost visible agent tab
lastRenderedActiveID TabID // active tab's identity at the previous render
```

- `tabScrollOffset` resets to `0` and `lastRenderedActiveID` to `""` in
  `SetWorkspace`.
- Every render clamps `tabScrollOffset` so it never scrolls past the last tab.

**Pull the viewport to the active tab only when the active tab has changed.**
`renderTabBar` runs on every `View()`, which Bubble Tea calls after nearly
every message — PTY output ticks, animations, resizes. An unconditional pull
would therefore undo a manual arrow scroll on the very next repaint, making
the arrows inert for anyone sitting on an agent tab. That is the primary user,
since removing the note redirect is what puts them there.

So `renderTabBar` passes `active = -1` to `visibleTabs` when the active tab is
unchanged, and the real index when it changed. `visibleTabs` keeps its simple
contract ("pull when `active >= 0`"); the caller decides when to ask.

**Compare by identity, not index.** `Tab.ID` is documented to survive slice
reordering. Closing the active tab slides a different tab into the same index —
that is a changed active tab and must pull, which an index comparison misses.
Conversely, closing a tab to the left shifts the active tab's index without
changing which tab is active, and must not pull.

**Do not record an empty identity.** While the Info tab is active, `activeIdx`
is `-1` and the identity is `""`. Writing that back would make the return to
the *same* agent tab look like a change, force-pulling and discarding the
user's scroll. Record only a non-empty ID; `""` then doubles as the
fresh-workspace sentinel, since `generateTabID` never produces it.

`model_tabs_nav.go` continues to move `activeIdx` and nothing else; the next
render pulls the viewport to follow. **No changes to the nav code.**

The consequence a reader should expect: a manual arrow scroll persists across
repaints and may push the active tab off-screen, exactly as an editor tab strip
behaves.

### 3. Hit regions and interaction

`tabHitKind` (`internal/ui/center/model.go:215`) already declares `tabHitPrev`
and `tabHitNext`. They are currently dead — declared, referenced nowhere. Reuse
them for the arrows rather than introducing new names.

One genuinely new kind is needed:

```go
tabHitNote
```

Behaviour:

| Hit | Action |
| --- | --- |
| `tabHitPrev` | `tabScrollOffset--`, return `nil` |
| `tabHitNext` | `tabScrollOffset++`, return `nil` |
| `tabHitNote` | return `messages.ShowSetWorkspaceNoteDialog{Workspace: ws}` |

Arrow clicks mutate view state only and emit no message, matching how
`tabHitInfo` already sets `m.infoTabActive` and returns `nil`.

The note click reuses the exact message `infoTabNote()` emits
(`model_info_keys.go:87`). `app_input.go:298` already routes it to
`handleShowSetWorkspaceNoteDialog`. **No new message type and no new app-layer
handler.**

The `tabHitNote` region is registered only when a note exists, so a workspace
without a note has no clickable dead zone on the right of its tab bar.

Arrow and note regions do not overlap any other region, so they slot into the
second pass of `handleTabBarClick` unchanged. The existing first pass (close
buttons, which overlap tab regions) is untouched.

## Code structure

`model_render_tabbar.go` is 261 lines and holds both `renderTabBar()` and
`handleTabBarClick()`. Adding viewport clamping, arrow geometry, and note
allocation to a function that already handles indicator selection, active
styling, close-button geometry, and hit tracking would push it toward the repo's
500-line ceiling and make the layout arithmetic untestable without rendering.

Extract the arithmetic into a sibling file, `model_render_tabbar_layout.go`,
containing pure functions over integers — no `Model`, no lipgloss:

```go
// noteWidth returns the cells to reserve for the note, 0 when there is none.
func noteWidth(note string, contentWidth int) int

type tabViewport struct {
    start, end int  // visible tab range, [start, end)
    showPrev   bool
    showNext   bool
}

// visibleTabs picks the largest run of whole tabs that fits.
func visibleTabs(widths []int, avail, offset, active int) tabViewport
```

`renderTabBar()` becomes a composer: render the pinned zones, call
`visibleTabs`, render that slice, right-align the note.

The scroll bugs worth worrying about here are all off-by-one arithmetic. This
shape lets them be table-tested without a terminal.

## Testing

`visibleTabs`, table-driven:

- no tabs
- one tab, exact fit
- all tabs fit (no arrows)
- overflow right only (offset 0)
- overflow left only (scrolled to end)
- overflow both sides
- active tab left of `start` ⇒ viewport pulled left
- active tab right of `end` ⇒ viewport pulled right
- offset past the last tab ⇒ clamped
- `avail` smaller than the narrowest tab ⇒ empty range, arrows still shown

`noteWidth`:

- empty note ⇒ 0
- short note, wide bar ⇒ natural width
- short note (< `minNoteWidth`), narrow bar ⇒ natural width, not padded to 8
- long note, wide bar ⇒ capped at `contentWidth/3`
- long note, narrow bar ⇒ `minNoteWidth`, not dropped
- note containing wide glyphs ⇒ display width, not byte length

Integration:

- `SetWorkspace` with a note ⇒ `infoTabActive == false`
- `SetWorkspace` on a workspace with no tabs ⇒ Info still active
- `selection_test.go:153` locates `+ New Agent` by scanning `m.tabHits` for
  `tabHitPlus`. The button stays pinned, so the assertion holds — verify its
  expected X coordinate has not shifted.

## Out of scope

- Mouse-wheel scrolling over the tab strip. Would require carving the tab-bar
  row out of `model_input_mouse_forward.go`, which forwards wheel events to
  fullscreen agents.
- A keybinding to scroll the viewport independently of the active tab.
- Changes to how the note renders on the Info tab itself
  (`app_view_content.go:106`).
