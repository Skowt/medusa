# Design: Claude fullscreen TUI as the default rendering mode

Date: 2026-07-07
Branch: `tui-full-screen-mode`
Status: approved for planning

## Problem

When Claude Code runs inside medusa in its fullscreen TUI mode, mouse-wheel scroll-up
stops working. Two facts combine to cause this:

1. **medusa consumes the mouse itself.** `center/model_input_mouse.go:updateMouseWheel`
   always scrolls medusa's own `vterm` scrollback (`tab.Terminal.ScrollView`) and never
   forwards mouse events to the PTY. medusa deliberately runs tmux with `mouse off`
   (`tmux/tmux.go`, the `DisableMouse` option) so it can own the mouse at the Bubble Tea
   layer.
2. **A fullscreen (alt-screen) app doesn't feed medusa's scrollback.** In classic mode
   Claude prints the conversation as ordinary output, so lines scroll off the top and get
   captured into `vterm` scrollback (`vterm/scroll.go:scrollUp`) — which medusa's wheel
   handler then walks. In fullscreen mode Claude repaints a fixed viewport on the
   alternate screen and manages its own scroll internally, so nothing is ever captured to
   scrollback. Wheel-up therefore has nothing useful to scroll, and Claude — the only
   thing that could scroll its viewport — never receives the event.

## Goal

Make Claude's fullscreen renderer the default for all new and relaunched agents, and hand
the mouse (scroll, selection, clicks) to Claude in those tabs. Keep medusa's own
scroll/selection behavior only as a fallback for classic sessions that predate this change,
until they are restarted or resumed.

## Decisions (locked)

- **Enable mechanism:** environment variable `CLAUDE_CODE_NO_FLICKER=1` (there is no
  `--tui` CLI flag). It rides along on both new (`--session-id`) and resumed (`--resume`)
  launches.
- **Mouse handling in fullscreen tabs:** full hand-off. medusa forwards wheel, click,
  drag, and release to Claude. Claude owns scrolling, selection (with automatic copy),
  click-to-expand, and URL clicks. medusa's own center-pane copy-on-select no longer
  applies in those tabs — Claude provides it.
- **Forced, not a preference:** every new/relaunched Claude agent is fullscreen. No
  per-agent toggle and no global kill switch.
- **Legacy tabs upgrade on next restart/resume:** the medusa-scroll fallback serves only
  currently-live classic processes. The moment a legacy tab is restarted or resumed by the
  updated medusa, it relaunches in fullscreen.

## Key mechanism facts (verified)

From the official Claude Code fullscreen docs (`https://code.claude.com/docs/en/fullscreen.md`):

- Fullscreen rendering is enabled by `CLAUDE_CODE_NO_FLICKER=1` at startup, `"tui":
  "fullscreen"` in `settings.json`, or `/tui fullscreen` in-session.
- It uses the terminal's alternate screen buffer, renders only visible messages, keeps the
  input box pinned, and adds full mouse support (wheel scroll, click, selection).
- Mouse wheel scrolling **through tmux requires tmux mouse mode** (`set -g mouse on`);
  without it, wheel events go to tmux, not Claude.
- It is a **research preview requiring Claude Code v2.1.89+**.

From the medusa codebase:

- The launch command is assembled in `internal/pty/agent.go:CreateAgentWithTags`, which
  already prefixes env vars (`CLAUDE_CONFIG_DIR=`, `MEDUSA_SESSION_NAME=`) and appends
  flags (`--permission-mode`, unconditional `--enable-auto-mode`, `--settings`).
- tmux options are built in `internal/tmux/tmux.go:clientCommand`; `DefaultOptions()` sets
  `DisableMouse: true`, and the code only ever emits `set-option mouse off` — it never
  emits `mouse on`. Enabling requires actively emitting `set-option -t <session> mouse on`.
- `internal/pty/terminal.go:Terminal.Write([]byte)` is the raw PTY write path (already used
  for interrupts and keystroke input) — this is how forwarded mouse bytes reach the PTY.
- Per-tab state persists in `internal/data/workspace.go:TabInfo` (`Isolated`,
  `PermissionMode`, `ClaudeSessionID`, …). tmux sessions are tagged via `SessionTags`
  (`@medusa_*` options), read back on discovery in `internal/app/app_tmux_discover.go`.
- Because tmux sits between medusa's `vterm` and Claude, medusa **cannot** reliably detect
  Claude's fullscreen/mouse state by watching escape sequences — tmux abstracts them. The
  fullscreen state must be marked explicitly at launch.

## Design

### 1. Enable fullscreen for Claude launches

In `agent.go`, prefix `CLAUDE_CODE_NO_FLICKER=1` on the agent command for `AgentClaude`
when the per-launch `Fullscreen` flag is true (which is true for every fresh Claude launch),
in the same place as the already-unconditional `--enable-auto-mode`. Because it is an env
prefix on the whole command, it applies to both the `--session-id` and `--resume` branches
with no extra branching. The `Fullscreen` flag added for the mark (§2) doubles as the
enable gate — it is `true` for every fresh Claude launch, so fullscreen is the default.

### 2. Mark the session as fullscreen (drives mouse routing, not enabling)

The mark exists only so the mouse handler knows whether the live process is fullscreen
(forward) or a legacy classic session (fall back to medusa scroll).

- Add a `Fullscreen bool` field (json tag `fullscreen,omitempty`) to `data.TabInfo`, and a
  `Fullscreen` field to the in-memory center `Tab`.
- Add a `@medusa_fullscreen` tmux session tag via `SessionTags`, set at launch.
- Set both true whenever *this* medusa launches or resumes a Claude agent.
- On medusa restart:
  - Sessions discovered/adopted from tmux are identified by the `@medusa_fullscreen` tag
    (authoritative for the running process); tabs reconstructed from the registry use
    persisted `TabInfo.Fullscreen`. Both are set at launch, so they agree.
  - Sessions launched by a pre-feature medusa have neither → `Fullscreen == false` →
    fallback (medusa scroll).
- "Upgrade on next restart/resume" falls out for free: a legacy tab flips to `true` the
  moment the updated medusa relaunches it.

### 3. tmux mouse mode per session (`tmux/tmux.go`)

Fullscreen agent sessions need `mouse on` so tmux forwards mouse events to Claude once
Claude enables tracking. Everything else (sidebar shell, viewers, legacy sessions) stays
`mouse off`.

- Add a way to request mouse-on for a session (e.g. an `EnableMouse` option, or thread the
  session's fullscreen flag through `clientCommand`).
- For fullscreen sessions, emit `set-option -t <session> mouse on 2>/dev/null` instead of
  the `mouse off` line. (Skipping the off line is not enough — with `ConfigPath=/dev/null`
  tmux's built-in default is mouse off.)

### 4. Forward mouse to Claude for fullscreen tabs (`center/model_input_mouse.go`)

- Add a guard `activeTabForwardsMouse()` → true when: focused, an active agent exists, the
  active view is the **live agent terminal** (not the diff viewer or info tab), and
  `tab.Fullscreen`.
- In `updateMouseWheel`, `updateMouseClick`, `updateMouseMotion`, `updateMouseRelease`:
  when forwarding, translate screen coords to pane coords with the existing
  `screenToTerminal`, SGR-encode the event (mode 1006), and `Terminal.Write` the bytes to
  the PTY. Return without touching medusa's `vterm` scroll or selection.
  - SGR encoding (1-based col/row): press `ESC [ < b ; x ; y M`, release `ESC [ < b ; x ; y m`.
    Buttons: left=0, middle=1, right=2; wheel-up=64, wheel-down=65; motion sets the +32 bit.
  - Only forward when `screenToTerminal` reports in-bounds (inside the terminal content
    region).
- **Chrome stays with medusa.** Tab-bar, action-bar, and info-tab click handling run
  *before* the forward check (as they do today), and diff-viewer routing
  (`dispatchDiffInput`) stays ahead of it too. Only mouse inside the live terminal content
  is forwarded.
- Legacy tabs (`tab.Fullscreen == false`): the current code path is untouched — that is the
  fallback.
- To respect the 500-line rule, the SGR encoder and forwarding helper likely live in a new
  sibling file, e.g. `center/model_input_mouse_forward.go`.

### 5. Scope of "remove medusa's scrolling"

Nothing is deleted. The `vterm` scrollback machinery is still used by legacy fallback tabs,
the sidebar shell terminal, viewers, and the diff viewer. For fullscreen tabs, medusa
simply forwards instead of scrolling its own buffer.

### 6. Known limitation: Claude version

Fullscreen requires Claude Code **v2.1.89+**. On an older Claude the env var is ignored
(no fullscreen), but medusa still flips tmux to `mouse on` and forwards mouse events;
classic Claude has no mouse tracking, so tmux would swallow the wheel into copy-mode
(janky). Per the locked decisions there is no kill switch — the v2.1.89 requirement is
documented as a hard prerequisite in `CLAUDE.md` and release notes, and the old-Claude
behavior is an accepted known limitation.

### 7. Native text selection

While Claude holds the mouse in a fullscreen tab, the outer terminal's native
copy-on-select is suppressed. The standard workaround is the terminal's modifier key
(Option in iTerm2, Fn in Terminal.app, Shift elsewhere), which Claude documents. No medusa
work is required; this is noted for user documentation.

## Error handling

- A `Terminal.Write` failure while forwarding a mouse event is logged and ignored (never
  panics the UI loop).
- Out-of-bounds coordinates from `screenToTerminal` are not forwarded.
- The tmux `set-option mouse on` line uses `2>/dev/null` like the other session options, so
  an older/edge tmux can't break session creation.

## Testing

- **Unit:** SGR encoding table (wheel/click/drag/release at known coords);
  `activeTabForwardsMouse` matrix (fullscreen vs legacy vs diff/info view); `agent.go`
  command includes `CLAUDE_CODE_NO_FLICKER=1` for both new and resume; `clientCommand`
  emits `mouse on` only for fullscreen sessions and `mouse off` otherwise.
- **Regression:** existing `center/selection_test.go` must still pass (legacy path intact).
- **Render:** `make release-check` (tests + all three harness modes) for render regressions.
- **Manual:** real mouse-in-tmux fullscreen scroll verified via `make run` — mouse routing
  through tmux to a live Claude cannot be fully automated.

## Files touched (estimate)

- `internal/pty/agent.go` — unconditional `CLAUDE_CODE_NO_FLICKER=1` for Claude; set the
  fullscreen tmux tag.
- `internal/tmux/tmux.go` — per-session `mouse on` for fullscreen sessions; `@medusa_fullscreen`
  tag in `SessionTags`.
- `internal/data/workspace.go` — `TabInfo.Fullscreen`.
- `internal/ui/center/model_input_mouse.go` (+ new `model_input_mouse_forward.go`) — mouse
  forwarding.
- `internal/ui/center/model.go` / `model_tabs*.go` — `Tab.Fullscreen` field, set + persist
  on launch/resume.
- `internal/app/app_tmux_discover.go` — read the `@medusa_fullscreen` tag for adopted
  sessions.
- Tests for the above, and a `CLAUDE.md` note on the v2.1.89 prerequisite.

## Out of scope

- Per-agent fullscreen toggle or global kill switch.
- Automatic Claude version detection / conditional fallback.
- Any change to the sidebar shell terminal, viewers, or diff-viewer input handling.
- `settings.json` injection of `"tui": "fullscreen"` (env var chosen instead).
