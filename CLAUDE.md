# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Definition of done

Before declaring any Go change complete:

1. `make fmt` — formats with `gofmt` + `goimports`.
2. `golangci-lint run` — must exit 0. Run on just the touched package for speed (e.g. `golangci-lint run ./internal/app/...`).
3. No `.go` file may exceed **500 lines**. `make lint` enforces this; if you hit it, split by concern into sibling files in the same package rather than inflating a single file.
4. Tests pass for the touched package.
5. **At the end of development**, run `make lint` — it mirrors CI by running `go test -race -v ./...` followed by golangci-lint and the 500-line check. The race detector catches issues plain `go test` misses (e.g. value-receiver methods like `Workspace.Root()` copy the whole struct, so a goroutine calling them races with any concurrent field write). `make test-race` is the same race run without lint, handy for reproducing a CI failure in isolation.

Don't batch these up for a later cleanup pass — fix as you go.

## Common commands

```bash
make build            # builds `medusa` and `medusa-hook-emit`
make run              # build + run the TUI
make dev              # hot-reload via air
make test             # go test -v ./...
make test-race        # go test -race -v ./... (mirrors CI; slower)
make lint             # full gate: test-race + golangci-lint + 500-line check
make fmt              # gofmt + goimports
make bench            # compositor render benchmarks
```

Run a single test:
```bash
go test ./internal/app -run TestCopyIgnoredFiles -v
```

Headless render/perf harness (no human, no terminal):
```bash
go run ./cmd/medusa-harness -mode monitor -frames 5 -warmup 1
go run ./cmd/medusa-harness -mode center  -frames 5 -warmup 1
go run ./cmd/medusa-harness -mode sidebar -frames 5 -warmup 1
```

`release-check` runs tests + all three harness modes — use it to validate UI-adjacent changes without a real terminal.

## Architecture

Medusa is a Bubble Tea v2 TUI that orchestrates multiple Claude Code (or other agent) sessions, each pinned to its own git worktree and its own tmux session.

### The root model: `internal/app`

`app.App` is the single `tea.Model`. Every `tea.Msg` flows through `App.Update` → `App.update` in `internal/app/app_input.go`. Routing order matters:

1. **Dialog results** (`common.DialogResult`) are handled first by `handleDialogResult`.
2. **Help overlay** consumes input if visible.
3. **`routeOverlayInput`** (`app_input_overlays.go`) runs the chain of modal overlays (dialog, file picker, settings, theme, sound, permissions, sandbox rules editor, profile manager). If any is visible, it consumes the message and the main switch is skipped.
4. **Main `switch msg.(type)`** handles window-size, mouse, keypress, and a small set of high-frequency messages.
5. **`default`** dispatches to `routePTYMsg` (tab / PTY / tmux tick messages) then `routeSystemMsg` (permissions, updates, action-bar, file watcher), and finally forwards unknown messages to `center.Update`.

When adding a new message handler, decide first which of these layers owns it — handlers in the wrong layer can be shadowed by an overlay or fire before the active workspace is set.

### Center pane: per-tab PTY + virtual terminal

`internal/ui/center.Model` owns the workspace tab bar and all running agents. Each `Tab` holds:

- a tmux session name (session-per-tab is what survives restarts),
- an `internal/pty.Terminal` + `internal/vterm.VTerm` pair (the PTY reader goroutine writes raw bytes; VTerm renders them),
- a pending-output buffer with debounced flushes (`model_input_pty.go`: `updatePTYOutput` → `updatePTYFlush`).

PTY readers run as long-lived goroutines. They capture the workspace ID at start
and embed it in every message they emit, which is why a workspace's ID must not
change under them. It no longer can: the ID is minted once at creation and
stored (`data.Workspace.StableID`), so nothing recomputes it from anything that
could move. The redirect map that existed to reroute stale messages is gone
with it.

### Fullscreen TUI mode

**On by default**, opt-out. The "Fullscreen TUI" checkbox in the New Claude Tab
dialog sets `config.UI.LastFullscreen` (persisted to `~/.medusa/config.json` as
`last_fullscreen`), which rides on `messages.LaunchAgent.Fullscreen` →
`pty.AgentOptions.Fullscreen` → `CLAUDE_CODE_NO_FLICKER` plus the
`@medusa_fullscreen` tmux tag and `mouse on`.

**`CLAUDE_CODE_NO_FLICKER` must always be set explicitly — `=1` on, `=0` off —
never omitted.** Claude's `/tui` command persists the user's choice as `"tui"`
in `settings.json`, and medusa launches agents with `CLAUDE_CONFIG_DIR` pointing
at the *profile* dir, so that persisted setting lives in
`~/.medusa/profiles/<name>/settings.json` and wins whenever the env var is
absent. Omitting the var for an unchecked tab therefore still launched
fullscreen for any profile where a session had ever run `/tui fullscreen` — the
checkbox could turn fullscreen on but not off (`buildAgentCommand` in
`internal/pty/agent.go`).

The checkbox is a sticky "last used" value: it defaults to on when
`last_fullscreen` is absent, and a stored value (including `false`) wins
thereafter.

Fullscreen is per-tab state, not a property of the agent type: it is persisted
in `data.TabInfo.Fullscreen` and every restore/reattach/restart path must read
`tab.Fullscreen` rather than re-deriving it from `assistant == claude`.

For fullscreen tabs medusa forwards wheel/click/drag/release to the PTY instead
of scrolling its own vterm, so vterm scroll/select is unavailable there. Requires
**Claude Code v2.1.89+** — older versions ignore the env var and mouse behavior
degrades, so leave the checkbox off on those.

**Never use `vterm.AltScreen` to decide what the agent is doing.** A center
tab's vterm reads from a `tmux attach` client, and a tmux client enters the
alternate screen at attach no matter what runs in the pane — so `AltScreen` is
true for *every* tab and carries no information about the agent. (Fullscreen
Claude does not even enter the pane's alt screen; it repaints in place, so
tmux's own `#{alternate_on}` is 0 for it too.) The signal that tracks the agent
is **mouse reporting**: tmux replays an app's mouse modes to its clients, so it
survives attach and adoption. `tabAppOwnsScreen` (`model_input_mouse_forward.go`)
is that predicate — `tab.Fullscreen || Terminal.MouseReporting()` — and it gates
mouse forwarding, PgUp/PgDn, and scrollback capture alike.

Consequently center vterms set `AllowAltScreenScrollback = true` (the alt screen
they see is tmux's, not the agent's) and `AppFullscreen` from `tab.Fullscreen`;
`vterm.appPaintsFrames` suppresses scrollback capture while the agent owns the
screen, so frame fragments never land in history. Gating scrollback on
`AltScreen` instead silently disables it for every tab — `ScrollView` then
clamps to an empty buffer and default-mode tabs cannot scroll at all.
Regression cover: `internal/e2e/default_scroll_e2e_test.go` (default mode must
scroll) and `internal/e2e/fullscreen_scroll_e2e_test.go` (fullscreen must not).

### Key forwarding

`common.KeyToBytes` (`internal/ui/common/keys.go`) is the single encoder from a
decoded key event back to the bytes a real terminal would have sent; the center
tabs, the monitor grid, and the sidebar terminal all go through it. Every
modifier it drops is a shortcut the agent can never see, and the failure is
silent — flattening `shift+enter` to a bare CR submits the prompt the user was
trying to break onto a new line.

Two properties are load-bearing:

1. **Alt keys must be rebuilt from `Key.Code`.** Both decoders (legacy
   ESC-prefix and Kitty) clear `Key.Text` whenever a modifier beyond shift is
   held, so `alt+b` / `alt+f` (word motion) arrive with empty text and cannot be
   forwarded as text. Requiring non-empty text dropped them outright.
2. **Enter is the one key that does not use the CSI form.** Modified special
   keys go out as `CSI 1;<mod><final>` (`ctrl+left` → `CSI 1;5D`), which tmux
   forwards. `shift+enter` only reaches medusa as the Kitty `CSI 13;2u` form,
   which tmux would strip since medusa never enables `extended-keys`, so
   `shift+enter` and `alt+enter` both become **ESC CR** — the meta+enter
   sequence Claude Code binds to "insert newline", and what its own
   `/terminal-setup` installs for shift+enter.

Regression cover: `internal/ui/common/keys_test.go`.

Note that `ctrl+a` never reaches the agent: it is medusa's tmux-style prefix
key. Press it twice to send a literal `ctrl+a` (`sendPrefixToTerminal`).

### OSC 8 hyperlinks

Agents print links as shorthand text wrapped in an OSC 8 sequence carrying the
real URL (`services/protos!1638` → the GitLab MR). Keeping them clickable needs
**both** halves of the chain, and losing either one is silent — the outer
terminal falls back to guessing a URL from the visible text, so an MR link opens
as `http://services/protos!1638`.

1. **tmux must forward it.** tmux only sends hyperlinks to a client whose
   terminal advertises the `hyperlinks` feature. Medusa attaches with
   `TERM=xterm-256color` (`tmux.ClientTerm`, consumed by `pty.NewWithSize`),
   whose terminfo says nothing about hyperlinks, so tmux strips the URI unless
   `terminal-features` advertises it (`appendHyperlinkFeature` in
   `internal/tmux/client_command.go`). That option is a server-wide array set on
   every attach, so the append is guarded — a bare append duplicates per tab.
2. **The vterm must keep it.** `Parser.executeOSC` parses `OSC 8 ; params ; URI`
   and `VTerm.CurrentLink` rides onto each written cell as `Cell.Link`, an
   interned ID (scrollback holds `MaxScrollback × width` cells, so a URI string
   per cell is far too heavy). `Cell.Link` is deliberately **not** part of
   `Style`: SGR — including a full reset, which agents emit inside a link — must
   not end a hyperlink. `cellToUVSnapshot` resolves the ID to `uv.Cell.Link`,
   and ultraviolet re-emits the OSC 8 to the real terminal.

Regression cover: `internal/e2e/hyperlink_e2e_test.go` drives the real client
command through tmux + PTY + vterm, so dropping either half fails it.

### Activity detection & notifications

Per-workspace busy/ready/needs-input state comes from Claude Code hooks
delivered over a Unix socket. `cmd/medusa-hook-emit` (injected into every
profile's settings.json by `config.InjectHooks`; legacy printf|nc shell hooks
are the fallback when the binary is missing) parses each hook payload and
forwards one JSON line. The state machine (`internal/app/app_hooks.go`) is
payload-driven, not event-counted: a `Stop` reads the payload's
`background_tasks` count to decide ready vs. still-working (`SubagentWait`),
and `SubagentStop` is deliberately inert — Claude Code fires phantom
SubagentStop events after Stop (upstream #59719/#70151), so nothing may treat
it as a busy signal. Sounds/highlights fire only on explicit ready or
needs-input transitions (`notifyWorkspaceAttention`), never from a workspace
"leaving the active set". The idle_prompt notification is outstanding-aware:
Claude fires it ~60s after the REPL goes quiet even while background agents
work, so it only clears/pings when the last authoritative Stop/SubagentStop
reported no live background tasks (`hookOutstanding`, assignment-only — never
counted). A reconciler (`app_hooks_reconcile.go`) silently clears busy states
with no hook event for 3 minutes. Background-task awareness
needs **Claude Code v2.1.145+** (`background_tasks` in Stop payloads); older
versions degrade to ping-on-Stop.

**Not every `background_tasks` entry is the agent working.** Each carries a
`type`, and a `monitor` is a live-update subscription — publishing an Artifact
auto-arms one for it — which reports `status: "running"` for as long as the
session stays subscribed, i.e. until the user stops it. Counting one parked
every Stop in `SubagentWait`, so the workspace spun forever and only the
3-minute reconciler ended it, minutes after the watch was finally killed.
`nonWorkTaskTypes` (`internal/hooks/emit.go`) excludes it, alongside the
sibling `session_crons` list, which is excluded by never being parsed: both
stay "running" while Claude sits waiting for input. Types are **denied, not
allowed**, matching the rule for statuses — an unrecognised type still counts,
because over-counting self-heals on the next Stop while under-counting fires a
false "ready" ping mid-work.

### Codex tabs

A tab's assistant is picked in the New Tab dialog's "Assistant" cycler, which
is **sticky per profile** (`config.UI.LastAssistantByProfile`, falling back to
the global `LastAssistant` for a profile that has never launched one) and drives
every no-dialog launch path too. Always resolve it through `stickyAssistant(ws)`
rather than reading `LastAssistant`, or a Work profile on Claude opens on Codex
because a Default workspace used it last. Cycling it rebuilds the dialog: Claude's permission modes and Codex's
sandbox policies share no values, so the fields below the assistant belong to
one of them and to no other (`app_dialog_new_tab.go`). Per-tab Codex policies
persist in `data.TabInfo`, and `agentTabOptions.forAssistant` strips the other
assistant's settings on every create, restore, and restart — `codex` exits on
an unknown flag rather than ignoring it, which drops the tab to a bare shell.

Four properties are load-bearing:

1. **`CODEX_HOME` is the profile boundary.** Codex keeps auth, config, hooks
   and session rollouts under it, so each profile gets
   `~/.medusa/profiles/<name>/codex` — the role `CLAUDE_CONFIG_DIR` plays for
   Claude. Credentials are not copied from `~/.codex`: the profile's first
   Codex tab opens on a login prompt so every profile authenticates independently.
2. **The worktree must be pre-trusted.** Codex refuses to start in an
   untrusted directory ("Not inside a trusted directory and
   --skip-git-repo-check was not specified"), which is every fresh worktree, so
   `InjectCodexTrustedDirectory` appends `[projects."<root>"] trust_level =
   "trusted"` to the home's `config.toml`. The append is text, not a TOML
   round-trip: Codex writes its own state into that file — hook trust hashes
   among it — and reserializing would reformat a file it owns.
3. **Hooks work unchanged, behind a one-time trust gesture.** Codex's payloads
   use the same field names Claude Code's do (`session_id`, `cwd`,
   `hook_event_name`) and it runs `type = "command"` hooks through `$SHELL -lc`,
   so `medusa-hook-emit` and its session-name guard need no Codex branch;
   `InjectCodexHooks` writes them to `<CODEX_HOME>/hooks.json`, which Codex
   discovers alongside `config.toml`. But Codex hashes each hook and **skips
   untrusted ones silently**, so the first Codex tab in a profile opens on
   "Hooks need review" and has no activity detection until the user picks
   "Trust all and continue".

   That hash covers the **command string**, which is why every rule points at
   `<CODEX_HOME>/medusa-hook.sh` instead of naming `medusa-hook-emit` directly.
   Naming the binary put its absolute path in the hashed string, so the trust
   prompt came back for every medusa that lived somewhere new — a `make run`
   build, an `air` rebuild, an upgrade, or a PATH lookup that missed and fell
   back to the shell pipeline. The shim is rewritten on every launch and holds
   everything that varies (binary path, socket) plus both guards, so the hashed
   string is constant and trust is asked once per profile. This does mean an
   upgrade changes what runs without re-asking; medusa owns `CODEX_HOME`
   outright, so trusting a rule that names its shim is the same act as trusting
   one that names its binary. Note `timeout` there is **seconds**, where Claude
   Code's `settings.json` reads milliseconds.
4. **Codex mints its own session ids.** There is no `--session-id` to
   pre-assign one, so a tab only learns its id from the SessionStart hook —
   another reason the trust prompt matters. Restart resumes with
   `codex resume <id>`, guarded by a rollout-file existence check
   (`sessions/<y>/<m>/<d>/rollout-*-<id>.jsonl`): resuming an unknown id exits
   1 rather than degrading, which would drop the tab to a shell.

Degradations to expect: Codex has no Notification event, so no idle_prompt
ping, and its Stop payload carries no `background_tasks`, so the outstanding
count stays unknown and every Stop reads as ready. It has no `StopFailure`
either, so a turn that dies on an API error leaves the tab busy until the
3-minute reconciler clears it.

Codex's eleven hook events are a subset of Claude Code's, matching name for
name — `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse`,
`PermissionRequest`, `PostToolUse`, `SubagentStart`, `SubagentStop`, `Stop`,
`PreCompact`, `PostCompact`. Medusa subscribes to all but the compaction pair
and `SessionEnd`, which say nothing about whether a workspace wants attention.
Everything else Claude Code offers (the `Notification` family, `StopFailure`,
`Elicitation`, the file/config/task events) has no Codex counterpart at all.

That leaves a Codex tab **two** needs-input signals, both of which Claude Code
also has, so both go through one path:

1. **A question the agent puts on screen** arrives as a plain `PreToolUse` —
   Codex's `request_user_input` goes through its generic function-tool dispatch
   with no hook-name override, so nothing but the tool name separates it from a
   file read. `hooks.IsQuestionTool` holds both assistants' names (Claude
   Code's is `AskUserQuestion`); missing one leaves the tab spinning as busy
   while it waits for an answer nobody knows it wants.
2. **An approval** arrives as `PermissionRequest` — but the two assistants do
   not mean the same thing by it, and reading it the same way on both is what
   would break. Claude Code fires it *only* when it is about to prompt a human;
   a call its permission rules, `bypassPermissions`, or auto mode already
   settled never reaches it. Codex fires it **before it picks a reviewer**, so
   under `--approve-for-me` it also covers approvals its automatic reviewer
   then resolves with no prompt shown. `tabAutoReviewer` is the discriminator,
   and it is read from the tab's launch options because the payload cannot
   answer it: `permission_mode` reflects the approval policy alone, never who
   reviews. Ungated, an Auto tab would ping on every sandbox escape it makes —
   and Auto is Medusa's default Codex mode.

Whichever way it arrives, a needs-input signal is stored as
`EventNotificationElicitation`, so the dashboard and the persisted
`ActivityState` have one value to reason about. An auto-reviewed
`PermissionRequest` is stored under its own name and counts as **busy**: the
review is work in progress.

`PermissionRequest` is also the one event either assistant lets a hook *answer*,
which makes the Codex shim's tail load-bearing. Codex reads exit 2 plus a stderr
message as a **denial**, so the shim discards stderr and exits 0 rather than
`exec`ing: a Go runtime panic in `medusa-hook-emit` exits 2 and prints a stack
trace, which would otherwise block the agent's command and hand it Medusa's
crash as the reason. (Claude Code takes its decision as stdout JSON, which
`medusa-hook-emit` never writes — it must not, since stdout from a Stop hook is
fed back as context.) `TestCodexHooksNeverDecidePermissions` guards the tail.

Medusa does not intercept or share either assistant's permission decisions.
The Codex New Agent dialog exposes a Starting Mode: Auto adds
`--approve-for-me`, while Default leaves approvals to Codex. Web search is
always enabled with `--search`; the selected Codex sandbox is passed through
with `--sandbox`.

### Workspaces pane ordering

Workspaces and group sections in the dashboard are reorderable by dragging a
row. Both halves of the order are persisted, and both are deliberately additive
over the ordering that existed before them:

1. **`data.Workspace.SortKey` is the position within a group, and 0 means
   "never placed by hand".** `sortWorkspacesForDisplay` puts keyed workspaces
   first in key order, then unkeyed ones oldest-first. Treating 0 as unplaced
   rather than as position zero is what makes a registry that predates manual
   ordering sort exactly as it did, and what makes a newly created workspace
   land at the bottom of an already-ordered group instead of jumping to its top.
   Every drop renumbers the whole target group (`sortKeyStride`), so the
   half-ordered case never has to be reasoned about.
2. **Group order lives in `config.UI.GroupOrder`**, keyed by label alone — the
   same property the alphabetical fallback has, and for the same reason:
   ordering sections by their members' timestamps made a group's position
   depend on which members were live, so archiving a group's oldest workspace
   reshuffled the pane. `sectionOrder` emits manually-ordered keys first and
   falls back to the old rule (alphabetical) for every group never dragged, so
   an empty `GroupOrder` reproduces the previous layout exactly. **Ungrouped is
   pinned to the bottom** and never participates: it is not a real group, so a
   manual position for it would only hide where the ungrouped workspaces are.
   It is not draggable and is never written to `GroupOrder`. It *is* emitted
   with no members whenever any group exists, because an empty section still has
   to be a drop target and a header that vanishes when its last workspace leaves
   cannot be one.

**The drag renders its own outcome.** There is no separate drop-target
highlight to keep in sync with the pending order: `dragState` holds a projected
placement, `rebuildRows` applies it, and the release commits what is on screen.
A dragged workspace moves among the rows; a dragged section moves as a block,
members and collapse state intact, marked only by a grip on its header
(`Row.DragLifted`).

**Dropping on "New group" creates one.** A workspace drag emits a `RowNewGroup`
target at the bottom of the section list, above Ungrouped. A drop there emits a
single `CreateGroupForWorkspace`, which the app expands into three steps in
order: move the workspace into a group named by `group_names.go`, pin that group
where it was dropped, then open the naming dialog on it. The generated name is a
placeholder the group can exist under — a group *is* the label its members share,
so it needs one before anything can be persisted or shown — and it is the
dialog's placeholder text, not its value, so the input starts empty and the user
just types. Cancelling keeps the generated name (`r` renames later); submitting
empty puts the workspace back in Ungrouped, which is what an empty rename has
always meant.

It is one message rather than a batch of the three steps because the order is
load-bearing and batched commands arrive in none: the dialog renames whatever
`dialogDefaultName` points at, so opening it before the move lands would cascade
a rename over a group with no members. The handler also skips the dialog when the
move did not stick — offering to rename a group whose creation just failed to
save is worse than silence.

Pinning matters for the same reason ordering does elsewhere: without it the new
group falls back to the alphabetical order every undragged group uses, and a
generated name starting with an "a" leaps to the top of the pane the instant it
is created at the bottom.

The target is shown for the whole drag, not only once the pointer nears it — a
drop target you cannot see until you are on it is one you cannot aim for — and
it is not keyboard-selectable, since `g` already groups a workspace by name.
Because inserting it reflows every row below, **promotion rebuilds the rows
before the pointer is resolved against them**; resolving against the
pre-promotion layout landed drops a row or two from where the user was looking.
It is separated from the Ungrouped header below it by a spacer, so it does not
read as that section's own header.

**Rows and sections resolve the pointer differently, and have to.** Both would
jitter under the other's rule:

- **A workspace resolves to an index in the *displayed* order** — never against
  a target row's identity. Rendering the projection puts the dragged row under
  the pointer, so identity-based resolution ("the row I am over is not the one I
  am dragging") flips between placed and unplaced on alternating events. An index
  read off the list that still contains the dragged item is a fixed point:
  re-reading the same position resolves to the same index. This holds because
  the dragged row is the cursor row, hence the tallest, so after the move the
  pointer is always still inside it. `TestMoveToIndex_IsAFixedPoint` and
  `TestDrag_PreviewIsStableWhilePointerHolds` guard it.
- **A section moves one place at a time, once the pointer passes the midpoint of
  the neighbour it would displace** (`updateGroupProjection`). Sections are tall
  and unequal, so they have no such fixed point: resolving to the hovered
  section landed the dragged one somewhere the pointer was no longer inside, the
  next event resolved to whatever took its place, and it flipped above and below
  its neighbour forever. Half of the *displaced* neighbour is the threshold that
  makes the two directions disjoint — displacing a neighbour of height `e`
  downward needs the pointer `e/2` past this section's end, while coming back up
  would need it more than `e/2` above the section's new start, and those bands
  cannot both hold. `TestDrag_GroupSweepIsMonotone` sweeps the pane a line at a
  time and asserts the index never goes backwards.

Three further properties are load-bearing
(`internal/ui/dashboard/dashboard_drag.go`):
1. **A press on a draggable row defers that row's action to the release.**
   Activating a workspace or toggling a group on press cannot coexist with
   dragging it — the drag would also open what it was carrying. Rows that are
   not draggable (`+ New Workspace`, archived, orphaned, Ungrouped's header)
   still act on press.
2. **The drag stores roots and group labels, never row indices.** `rebuildRows`
   runs on every workspace update, hook event and spinner tick; an index would
   come to mean whichever row had moved into its place. For the same reason it
   re-anchors a section-header cursor by label, not just a workspace cursor by
   root — a cursor landing on a workspace row makes that row taller and shifts
   everything below it.
3. **Drag and hover markers never change a row's height.** A dragged section
   keeps its members and its chevron for this reason too: withholding them made
   dragging a group look like collapsing it, and left the pointer resolving
   against a one-line stand-in for a section many lines tall.

   A row's height depends on whether it is the cursor row (`activeRowLineCount` via
   `nameChunks`) and `rowLineCount` decides that from the cursor alone, so the
   lifted marker is foreground-only and the hover handle right-aligns *within*
   the row's existing width. Anything that changed a row's height under the
   pointer would move the drop target out from under it.

   This is why `nameChunks` reserves `handleGutter` on **every** workspace row,
   hovered or not, rather than making room when the handle appears: re-wrapping
   a name on hover would change the row's height, and a name that already filled
   the width had nowhere to put the handle — it silently vanished on selected
   rows (selection fills the row) and on long names (truncated to the full
   width), which is exactly where it was most needed. Group labels get no such
   reservation and are clipped via `MaxWidth` instead — ANSI-aware, since these
   lines carry styling that plain slicing would cut mid-sequence.

Hover handles (the `⠿` at a row's right edge) are painted in the **accent**
color, never a `Surface` one. Surface tokens are background tiers: `Surface3`
against a dark theme's background is `#292e42` on `#1a1b26`, so the handle
rendered exactly where it should and could not be seen at all. The glyph is also
sparse — six braille dots rather than a solid block — which costs it more
perceived contrast than its nominal ratio suggests.
`TestHover_HandleIsNotPaintedInASurfaceColor` guards the tier.

The dashboard observes hover motion **before** the center pane does, so its
handles cannot be starved by anything on the center's path.

**Every hover affordance depends on the terminal actually reporting pointer
motion, and medusa has to nudge the mode to get it.** Bubbletea writes the
mouse-enable sequence only when the mode a view requests differs from the last
frame's, and it writes it *before* entering the alternate screen — so on a
terminal that scopes DEC private modes to the screen buffer, the single
all-motion enable medusa asks for at startup lands on the primary screen and is
lost on the way in. Nothing changes the mode afterwards, so motion reporting
stays off for the whole run and every hover affordance silently does nothing;
the symptom is hover that only starts working once something unrelated happens
to re-establish the modes. `App.mouseMode` therefore requests cell-motion for
one short phase after the first window size and all-motion after it, purely so
the mode changes once with the alt screen already up. `routeMouseMotion` also
logs the first motion event it sees, which is the only way to tell "the terminal
never reported motion" apart from "the app ignored it".

Hover handles need hover motion to reach the pane whether or not it holds focus — clicking is what takes focus, so a
focus-gated affordance could never advertise itself. `routeMouseMotion` lets the
dashboard observe button-less motion alongside the center, the same way the
center's copy affordances work.

Regression cover: `dashboard_drag_test.go` and `dashboard_drag_preview_test.go`
(both drive real mouse messages through `rowIndexAt`'s geometry),
`dashboard_order_test.go`, and `app_input_messages_reorder_test.go`.

### Workspace / worktree model: `internal/data`

A `Workspace` is built from **one** repo. It can still *span* several — the
model keeps `Repos` and `Worktrees` as parallel slices, `AllRoots()` /
`PrimaryWorktreeRoot()` account for that layout, and workspaces created before
the New Workspace flow was narrowed keep working — but nothing creates a new
multi-repo one, and quick-duplicate refuses a multi-repo source rather than
cloning it into another. `Workspace.Root()` is the primary worktree root.
Registry at `~/.medusa/workspaces.json` is the source of truth; `data.Registry`
and `data.WorkspaceStore` are both guarded by `sync.Mutex`
(saveLocked/deleteLocked pattern). Orphan handling has two flavors:
`OrphanMetadata` (registry knows about a dir that's gone) and `OrphanDirectory`
(dir on disk with no registry entry).

**A worktree is optional, and whether there is one decides what may be deleted.**
The "Create a git worktree" checkbox in the New Workspace dialog is sticky
(`config.UI.LastCreateWorktree`, default on). Ticked, creation cuts a branch
named after the workspace and puts a worktree under the workspaces root, which
is what the base-branch step and its fetch exist to serve. Unticked, **nothing
is created at all**: the workspace's root *is* the source repo, it opens on
whatever branch that repo is already on, and the base-branch step is skipped
along with the fetch. `Runtime` records which (`RuntimeLocalCheckout` vs
`RuntimeLocalWorktree`), and `UsesWorktree()` derives the same answer from the
paths so a workspace written before the option existed still answers correctly.

**A checkout workspace pulls before it opens** (`pullBeforeOpen`). Opening an
agent on a repo a week behind is the common way to start work on stale code, and
the worktree path already fetches for the same reason. Three conditions gate it,
and each is a case where pulling would be worse than being out of date: a **dirty
working tree** is left alone and the user is told, since the alternative is
medusa moving their in-progress files around; a branch with **no upstream** (and
a detached HEAD) is skipped silently, since there is nothing to pull and warning
every time is noise; and the pull itself is `--ff-only`, so a **diverged branch**
is declined rather than having a merge commit written into the user's history or
conflicts left for them to resolve before they can start. The notice rides back
on `WorkspaceFetchDone.PullNotice` and surfaces as a toast. Regression cover:
`workspace_checkout_test.go`.

Three further consequences are load-bearing:

1. **Delete must not remove what medusa did not make.** `deleteWorkspace` runs
   `removableWorktrees` first, which drops any worktree whose directory is the
   source repo, and — as a backstop for a stored path that has come to mean
   something else — any path still on disk that is not a worktree
   (`git.IsWorktree`, a `.git` *file* rather than a directory). Branch deletion
   is bound to the same test, since a checkout workspace records the branch the
   repo happened to be on rather than one of its own. The final
   `os.RemoveAll(ws.Root())` is gated on `UsesWorktree()`. Getting this wrong
   deletes the user's repository.
2. **Setup scripts are skipped for a checkout workspace.** `setup-workspace`
   commands exist to make a *fresh* worktree usable — installing dependencies,
   copying env files in — and the repo the user already works in is already set
   up. `runSetupAsync` still emits `WorkspaceSetupComplete`, since the `run`
   scripts hang off it.
3. **The workspace ID is minted at creation, not derived from paths.** It used
   to hash repo path plus root, and for a checkout those are the same path for
   every workspace over that repo — so the second one collided with the first
   and overwrote its store entry. `data.Workspace.StableID` is written once by
   `mintWorkspaceID` (repo, root and *name*) and never recomputed, so a rename
   cannot move it either. The path-derived hash survives as the fallback in
   `ID()` for every workspace stored before the field existed, but the fallback
   is never trusted for long: **`WorkspaceStore.Load` pins `StableID` to the
   directory the metadata was found in.** That directory is the identity — it is
   what the registry entry points at and what every other store call addresses —
   while the derived hash stops reproducing it the moment the root moves, which
   the flat-layout migration did to plenty of them. A workspace then answered to
   an ID nothing on disk held, so deleting it as a metadata orphan removed
   neither its store directory nor its registry entry and reported success
   anyway: the orphan came back on the next load and survived every restart.
   `Save` still pins on write, for a workspace that was built rather than
   loaded. The mint is a pure function rather than a random value because the
   dashboard renders a placeholder workspace while creation is in flight, and
   the placeholder has to carry the ID the finished workspace will have.

   **An orphan cleanup verifies that the entry is actually gone**
   (`registryStillLists`, `app_operations_orphan.go`). Removing a registry ID
   that is not there succeeds, and so does deleting a store directory that is
   not there, so the failure above had no symptom but the toast. The check is by
   name as well as by ID, since a mismatched ID is the one thing that would slip
   past an ID-only check. Regression cover:
   `internal/app/app_orphan_delete_test.go` and
   `internal/data/workspace_store_test.go`.

   **The consequence is that a root no longer identifies a workspace**, and
   anything that keys off one is wrong the moment two workspaces share a repo.
   Converted with the mint: the dashboard's creating/deleting maps, its active
   row, cursor re-anchoring, drag and hover state, `ReorderWorkspaces` /
   `CreateGroupForWorkspace`, the pending auto-launch and profile-launch
   handoffs, `ScriptRunner.running`, and `PortAllocator` — two checkout
   workspaces sharing a port range meant their run scripts fought over it.
   Roots are still the right key for genuinely path-scoped things: the git
   status cache, the file watcher, and the orphan-directory scan.
   Regression cover: `internal/ui/dashboard/dashboard_shared_root_test.go`,
   `internal/app/workspace_checkout_test.go`,
   `internal/process/env_test.go`.

   **The monitor grid's project filter is a third kind of key again** — the
   source repo path, so one chip covers every workspace over that repo. Both
   readers must resolve it through `monitorProjectKeyLabel`, never against a
   root: `filterMonitorTabs` does, but `tmuxSyncWorkspaces` compared
   `ws.Root()`, which matches only a checkout workspace. Picking any chip
   therefore left the tmux tick polling nothing at all, and the grid froze on
   the tab state it last had — silently, since the tiles keep rendering.
   Regression cover: `internal/app/app_monitor_filter_test.go`.

**A registry entry's ID is not a guarantee that the metadata is still there, so
`loadWorkspaces` heals the entry instead of dropping the workspace**
(`app_operations_heal.go`). `WorkspaceStore.Save` rehomes a workspace whenever
`ws.ID()` disagrees with the directory it was loaded from and deletes the
directory it moved off, without telling the registry. Current medusa never
changes an ID it has minted — the three callers that can change one
(`loadWorkspaces`'s flat-layout migration, rename, add-repos) all repoint the
registry — but **a build predating `StableID` recomputes the repo-plus-root
hash instead of reading the stored `id`**, and self-hosting on medusa makes
running one easy: any worktree sitting on an older commit builds one, and it
manages the same `~/.medusa`. That silently moved a workspace's metadata to the
derived hash and stranded the registry at the minted one.

The workspace then disappeared *twice*: the load skipped it, and the worktree it
owns resurfaced as an `OrphanDirectory` — "no metadata (directory orphan)" —
whose only offered action is deleting the user's work. Recovery is only a
registry repoint, which is why it is done rather than reported.

Three properties keep the heal from doing harm of its own:

1. **Only a missing file is second-guessed** (`os.IsNotExist`). Any other error
   means the metadata is there and unreadable, where adopting some other
   directory is a guess rather than a repair.
2. **Name is the key, and it is not sufficient alone.** It is the only thing the
   registry records that survives an ID change, and it is sound because two live
   workspaces may not share a name — but the store keeps every directory the
   registry has ever dropped, so a name is easily shared with a workspace that
   died long ago. A candidate is adopted only when it is the single *unclaimed*
   one left; ambiguity is narrowed by which worktree still exists on disk, and
   anything still ambiguous is left stranded. Adopting the wrong metadata would
   attach the entry to another workspace's worktree.
3. **It heals stranded entries only, never unregistered store directories.** The
   store holds far more directories than the registry has entries, so adopting
   those would resurrect every workspace the user has ever deleted.

A healed entry whose worktree is *also* gone becomes an ordinary
`OrphanMetadata` — visible and deletable — rather than the invisible dangling
entry it was before. Regression cover: `app_operations_test.go`'s
`TestLoadWorkspaces_HealsRehomedMetadata`, `…_HealRefusesAnAmbiguousName`, and
`…_HealDoesNotStealClaimedMetadata`; the first drives the real rehoming through
`WorkspaceStore.Save` rather than faking the drift.

**Rename changes a label and nothing else.** It does not move directories, does
not rename branches, and does not restart agents. Moving the worktree to match
the new name used to be the whole implementation, and it changed the workspace's
root — hence its ID, every agent's working directory, and the meaning of every
path anything had already resolved. On a checkout workspace it would have moved
the user's repo. Nothing on disk moves now, and the ID is stored rather than
derived, so a rename cannot move it either and nothing has to be migrated; the
tmux sessions are renamed only because their names are built from the workspace
name, and `renameWorkspaceSessions` updates the in-memory records for exactly
the sessions tmux confirmed, so the two cannot drift. There
is no longer any restriction on *which* workspaces can be renamed — the old
guards against renaming a primary checkout or a main/master branch existed
because of what the rename did on disk. Regression cover:
`app_rename_permissions_test.go`, `workspace_checkout_test.go`.

**Auto-start applies to every workspace**, including one whose root is the
source repo: a workspace exists because the user made it, and pointing it at a
repo checkout is a choice they made in the New Workspace dialog. The e2e
fixtures set `auto_start_agent: false` because they create their agent tabs
explicitly and assert on the exact set of tmux sessions.

### Messages: `internal/messages`

Pure type declarations; no package may import app/ui code. Split into several files by concern. When adding a message, check whether a concern-scoped file already exists before adding to `messages.go`.

### Entry points

- `cmd/medusa` — the TUI binary.
- `cmd/medusa-hook-emit` — standalone helper invoked as a Claude Code hook to forward lifecycle events to the activity socket.
- `cmd/medusa-harness` — headless render driver used by `make release-check` and benchmarks.

Subcommands of `cmd/medusa` run instead of the TUI and are dispatched in
`main()` before any TUI or logging setup, so they work headlessly:
`medusa skills` (`cmd/medusa/skills.go`). The separate helper binaries above are
separate only because Claude Code invokes them as external hook commands — a
subcommand cannot serve that role.

### Skill-usage tracking: `internal/skillstats`

Skill usage — which skills were invoked, grouped by providing plugin, in hourly
/ daily / weekly views per profile. Two front ends over one package:

- **`[U]` in the dashboard toolbar** (next to `[?] [M] [S]`) opens it in the
  browser. `skillstats.Service` starts on that first press and stays up for the
  session on an **ephemeral loopback port**, so it can never collide with a
  standalone `medusa-skills` nor be reachable off the machine. The first press
  does a cold scan, so `handleOpenSkillUsage` runs the whole thing in a
  `tea.Cmd` — never on the UI thread. Adding a toolbar item requires nothing
  else: `columns` is `len(toolbarItems())` and navigation is already
  length-driven, but `toolbarHeight` hard-codes one row, so items must fit it
  (`internal/ui/dashboard/toolbar_test.go` guards that).
- **`medusa skills`** (or `make skills`) serves the same dashboard headlessly on
  `127.0.0.1:7788`, plus `-scan` and `-report -gran week -profile Work` for
  terminal output. Both front ends share `internal/skillstats` and the same
  store, so either one's scan benefits the other.

The source is **Claude Code's own transcripts**, not a hook: every skill
invocation is a `Skill` tool_use block carrying `{"skill": "plugin:name"}`, and
transcripts already record the timestamp, session, and cwd. That makes tracking
retroactive over sessions that already happened and keeps it off the hook path
entirely — nothing about activity detection changes. The profile comes from the
transcript's location (`~/.medusa/profiles/<profile>/projects/...`, via
`CLAUDE_CONFIG_DIR`). `~/.claude/projects` is scanned as well — Claude Code run
outside Medusa, where `CLAUDE_CONFIG_DIR` was never set — and reported under
`skillstats.ClaudeProfileLabel` (`~/.claude`), which appears in the profile list
only once it has an invocation, like any other profile.

Two properties are load-bearing:

1. **Scanned events are copied into `~/.medusa/skill-usage/events.jsonl`.**
   Claude Code deletes session files older than `cleanupPeriodDays` (default
   **30**, minimum 1) at startup, while the weekly view spans a quarter — so the
   durable log is the only reason history older than a month exists. This is not
   theoretical: a cleanup sweep during development pruned ~580 of 1110
   transcripts, and 63 recorded invocations then existed only in the log. Dedup
   is by transcript entry uuid (plus a `#n` suffix when one assistant message
   invokes several skills), which makes any rescan — incremental or full —
   idempotent, so pruning can never double-count on the way back.

   The log is itself bounded at `RetentionMonths` (**6**, calendar months). The
   cutoff is enforced on **both** paths — dropped at load, and refused at commit
   — because the transcript for an aged-out event can still be on disk, so a
   commit-side check is the only thing stopping a full rescan (after a lost
   `scan-state.json`) from resurrecting what was just pruned. Pruning rewrites
   the log via temp+rename, and pruned uuids leave the `seen` set since the
   commit cutoff already bars their return.
2. **Scans are incremental.** `scan-state.json` holds size+mtime+offset per
   transcript, so unchanged files are skipped and a grown file is read from its
   tail: ~1s for a cold pass over 400 MB, ~40 ms in steady state. The offset only
   ever advances past newline-terminated lines — a transcript caught mid-write
   must reparse its partial tail, not skip it.

Skills invoked as `/slash` commands are **not** counted: the harness injects the
skill directly and no `Skill` tool call is emitted, so there is nothing in the
transcript to attribute. Skill names with no `plugin:` prefix are bucketed as
`personal` / `project` / `built-in` by locating the skill on disk.

### Git reviews in the browser: `internal/gitreview`

`[Review Changes]`, right-aligned on the info bar's first row, opens a live diff
review for the active workspace in the browser: file tree on the left, diff on
the right, comments on a line or a dragged range, and a submit that pastes the
whole batch into the agent that made the changes. Threads are
collapsible, and a resolved one starts collapsed -- the point of resolving is to
get it out of the way. Collapse state is held per thread id in the page, not as a
flag rebuilt from the server: the diff repaints every couple of seconds, and a
server-derived flag would snap every card open under the reader's cursor.

It is a web page rather than a TUI pane because a diff review wants a browser's
width, mouse selection and text input, none of which a terminal pane does well.
An earlier split-pane version of this feature was removed for that reason; what
survived it and is reused here is `center.SendToAgentSession` (with its
bracketed-paste helper, which is what keeps a multi-line review one prompt
instead of a dozen half-sentences) and `git.DiffLine.OldLine/NewLine`.

**The diff renders unified or side by side**, chosen by a toggle in the top bar
and remembered like the other review settings. `splitRows` does the pairing: a
unified hunk is runs of context, removals and additions, so context belongs to
both sides at once, and a run of removals with the run of additions that follows
it is one edit seen twice -- zipped index-wise, which is what puts a rewritten
line *opposite* the line it replaced rather than below it. The longer run leaves
the other side blank, striped rather than empty so it reads as "nothing on this
side" and not as a blank line.

Five properties:

1. **A side carries the line's index in the unified list, not the line.**
   Everything already keys off that index -- the highlight cache, range
   selection -- so pairing changes what is on screen without giving any of them a
   second representation to understand.
2. **`table-layout: fixed`, with the two code columns left unsized.** Under
   fixed layout the columns with no width share what is left equally, which is
   the only way to get halves that stay equal; asked for `width: 50%` under the
   default auto layout they came out 551px and 508px, because auto layout treats
   a width as a hint and gives the wider content more -- so the two sides of one
   change wrapped at different points. The gutters are pinned by a `<colgroup>`.
3. **Tints go on cells, not rows.** The two halves of a split row are usually a
   removal and its replacement, so a row-level colour could only ever be right
   about one of them. Same for the comment bar, which marks the side the comment
   is on, and for the drag highlight (`picked-left` / `picked-right`).
4. **Dragging is per side**, because a range on the left is about removed lines
   and the two sides do not share line numbers. Indices are per hunk, so the
   hunk header travels on the row with them -- without it a drag in one hunk
   highlighted the same index in every other.
5. **Cards still span the whole width** (`colSpan` 5 unified, 8 split). A comment
   is about a place in the code, not about a column, and one confined to a half
   would be a third of the width of the same card unified.

The tab is named `<workspace> - <file> Changes` (`renderTitle`), following the
selection as the reader navigates -- a review left in a background tab is
otherwise indistinguishable from any other. The base name rather than the path,
since the page already shows the path and a title long enough to be clipped
identifies nothing, and assigned only when it differs, because render runs on
every poll.

**The theme is the reader's to choose, and light is not a new palette -- it was
always the base one.** The page followed `prefers-color-scheme` and had no way
to disagree with it; a ☀/☾ toggle in the top bar now does, remembered with the
other review settings as `last_review_theme`.

Three properties:

1. **Light on bare `:root`, dark twice.** The dark block appears under
   `@media (prefers-color-scheme: dark)` *guarded as*
   `:root:not([data-theme="light"])`, and again under `:root[data-theme="dark"]`.
   Without the guard the toggle could turn dark on but never off on a dark
   machine. It is written twice because a media query and an attribute selector
   cannot be combined into one rule; keeping **every** colour the page uses in
   those blocks -- the syntax tokens included, which used to be literals spread
   over a dozen `.hljs-` rules -- is what holds the duplication to one list.
2. **No choice means no attribute**, rather than stamping whatever the OS
   currently says. Absent is what lets the media query answer, so the page keeps
   following the system as it changes through the day. The buttons still show
   which theme is *in effect*, asking `matchMedia` directly, so neither is lit
   falsely -- and a system change while no choice is in force relights them.
3. **`ParseTheme` normalises anything it does not recognise to "follow the OS"**
   rather than refusing it. A hand-edited config would otherwise have no theme
   at all, and falling back to a fixed one would override a preference the reader
   expressed in their system settings.

**Diffs are syntax highlighted by a vendored highlight.js** (v11.11.1, the
common bundle, BSD-3-Clause; provenance and upgrade notes in
`highlight.min.js.README`). It is vendored and embedded rather than loaded from
a CDN because the page is served from an ephemeral loopback port inside Medusa's
own binary: a review that has to reach the internet to colour a diff is blank on
a plane, and asking the network for the code that renders a local file is not a
dependency this tool should have. It is served from its own token-scoped route
so the browser caches 127KB once, instead of being inlined into a page that is
re-served on every open.

Four properties are load-bearing:

1. **Each side of a hunk is lexed as one block, not each row on its own.** A
   hunk's *rows* are not source -- they interleave a removed line with the line
   that replaced it, so joining them hands the lexer text that never existed in
   any file. The post-image rows (context and added) are contiguous source, and
   so are the pre-image rows (context and removed), so those are the two strings
   that get highlighted. That is what makes a multi-line construct inside the
   hunk come out right -- a Python docstring, a JS template literal, a C block
   comment -- and it costs two calls per hunk rather than one per line. A
   construct that opens *before* the hunk is still read wrong; expanding the
   context is the answer, which is why expanded rows are lexed with the rest.
2. **`splitHighlighted` reopens spans across newlines.** highlight.js nests
   spans and leaves them open across a line break, so cutting its output at
   newlines would put an unclosed span in one table cell and a stray closing tag
   in another. What is open is closed at each break and reopened on the next
   line. The scan only has to understand `<span>` and `</span>`, because that is
   the whole of highlight.js's output.
3. **The language comes from an explicit extension map** (`languageFor`), never
   from `highlightAuto`: auto-detection on a fragment of a diff picks the wrong
   language often enough to be worse than no colour, and does it silently.
   Anything unmapped, or mapped to a language the bundle does not carry, renders
   as plain text -- which is also why the `hljs.getLanguage` check is there,
   since highlight.js *raises* on an unregistered language rather than falling
   back, and that would take the diff pane down.
4. **`innerHTML` receives highlight.js output and nothing else.** hljs escapes
   its input on the way through, so the line's own text can never be treated as
   markup; nothing else is interpolated into the cell.

The result is memoised per file, hunk and expansion state, and dropped with the
rest of a file's cached context when its digest moves (`dropStaleContext`) --
without it the same lines are lexed several times a second, since the pane is
rebuilt on every poll, selection and comment. Measured on this repo: ~1ms for a
300-line Go file, 71ms for the 127KB minified bundle, ~0ms on every repaint
after. `MAX_HL_BYTES` bounds one pass, because a file under git's 2MB "large"
threshold can still be a single minified line of half a megabyte, and nobody
reads a minified bundle for its keywords anyway.

The token palette is **GitHub's**, in both themes, because those values are the
ones proven legible on exactly these three backgrounds -- panel, the added tint
and the removed tint -- which a palette picked against white alone is not. A
highlighted added or removed row drops its green or red ink: with tokens
coloured, the text that is *not* a token would be the only green left on the
line. The background tint and the sign column still say which side it is. There
are no italics: this is a monospace table, and a slanted comment run changes the
glyph advance just enough to stop columns of code lining up.

**Every changed file is on one page, and the navigator reports where the reader
is rather than choosing what exists.** That is what a review actually is: you
scroll through the change, and clicking a file scrolls to it
(`selectFile` → `scrollToFile`) instead of swapping the pane.

Only the files near the viewport are built. This repo's own diff is sixty files
and twelve thousand lines; building all of it took the better part of a second,
and the pane is rebuilt on every poll, so it would have spent half its life
laying out code nobody was looking at. Five properties hold it together:

1. **An unbuilt file is a spacer of its own height** -- estimated from its row
   count until it has been built once, and its measured height every time after.
   Without the estimate every section starts at zero height, they all stack at
   the top of the pane, they all count as near the viewport, and the whole diff
   mounts on the first render: precisely what the arrangement exists to avoid.
2. **A built file is left alone unless something about it changed**
   (`sectionStamp`: digest, its threads, the draft form, the expansion, the
   layout and theme). A poll that rebuilt four screens of rows every two seconds
   would also be rebuilding the reply box the reader is typing in.
3. **The page order is the navigator's order**, taken from the same tree walk
   (`orderedFiles` → `collectFiles`), because a page whose order differs from its
   own index is worse than either order alone. `state.file` falls back to the
   *tree's* first file, not the snapshot's -- taking the snapshot's had the
   navigator highlighting one file while the page opened on another.
4. **The spy names the last file whose head has reached the top of the pane**,
   not the one nearest the middle, which flickers between neighbours on a short
   file. It is muted while a click-scroll is in flight, or it would rename the
   current file for every file the scroll passes over. It updates the navigator
   and the title only -- a full render would rebuild rows on every frame.
5. **A row's identity now includes its path.** A `@@` header is unique only
   within one file, and every file is on the same page, so a drag keyed on the
   header alone would have highlighted the same index in every file that happened
   to share one.

**The navigator is a directory tree, not a path list.** A flat list repeats the
directory on every row, so on a change spanning several packages the part of each
row that says where the file is scrolls past as noise. `buildTree` reconstructs
the structure from the paths, and three properties keep it readable:

- **A run of single-child directories folds into one row** (`collapseChain`), so
  a Go repository reads as `internal/gitreview` rather than four nested folders
  each holding nothing but the next. The row is keyed by the *deepest* path in
  the chain, since that is the one whose contents its twisty shows.
- **Collapse state lives in the page, and it is the collapsed set** — the same
  shape, and for the same reason, as thread collapse: the navigator is rebuilt on
  every poll, so anything derived from the snapshot would spring every folder
  open under the reader. Collapsed rather than expanded is what makes a directory
  the page has never seen start open; a tree that hides the changes on first load
  defeats the point of it. Selecting a file opens the folders above it
  (`revealFile`), or a link into a folded-away path would leave the navigator
  showing no selection at all.
- **A folder shows its counts only while it is shut.** Open, they duplicate the
  rows immediately below them; shut, they are the only sign that anything in
  there changed or has a comment waiting.

**A file can be marked read, and the mark is pinned to a digest, never to a
boolean.** `File.Digest` fingerprints one file's diff, `Session.viewed` maps a
path to the digest that was on screen when the reader ticked it, and
`ViewedPaths` reports only the marks that still match. So the agent touching a
file takes its tick away with it -- a live review page still ticking a file that
has since been rewritten tells the reader they have seen code they have never
seen, which is worse than never having offered the tick. Invalidation is per
file, or one edit anywhere would wipe the reader's whole progress
(`session_viewed_test.go`).

Four smaller decisions around it:

1. **The server resolves what counts as read, not the page.** The digest
   comparison has one home, and the page never holds a value it could get wrong.
   `Event.Viewed` therefore has **no `omitempty`**: "nothing is read" is a real
   state to render and would otherwise be indistinguishable from "this event says
   nothing about it". Every event is built through `Session.withState` for that
   reason, so an event about threads cannot blank the ticks and vice versa.
2. **An unknown path is refused rather than recorded** (`SetViewed`,
   `Session.HasFile`). That keeps the map to real files, and it is *also* what
   makes a traversal guard unnecessary on the endpoints: the only paths that get
   in are ones the snapshot itself produced.
3. **Marks persist beside the comments**, for the comments' own reason -- a
   review spanning two sittings that forgets what was already read is
   untrustworthy. Nothing prunes them: entries are added only by an explicit
   click and keyed by path, so re-ticking a file overwrites rather than grows.
   That is a different shape from threads, which accumulate prose daily and do
   need `maxPersistedThreads`.
4. **The tick sits in the navigator's twisty column**, where folders keep their
   chevron, so ticks line up down the left edge instead of competing with the
   comment badge on the right -- which means something else entirely. The row is
   dimmed as well, because a tick alone is easy to miss in a column of forty rows
   and what the reader is looking for is the work that is *left*.

**The context around a hunk can be opened up, and the lines come from the
working tree.** `▲`/`▼` on each hunk header call `GET /api/lines`, which
`readFileLines` answers from disk. Disk is right because neither scope passes
`--cached`, so the `+` side of every hunk *is* the file as it is now; a read from
anywhere else would renumber the gap the reader is trying to fill.

Four properties are load-bearing (`snapshot_lines.go`, and the expansion half of
`review.html`):

1. **A gap is shared by the hunks on either side of it, so each accounts for the
   other's expansion.** `roomAbove`/`roomBelow` subtract the neighbour's opened
   lines, which is what stops expanding down from hunk *n* and up from hunk *n+1*
   from rendering the same lines twice. `roomBelow(i)` and `roomAbove(i+1)`
   therefore measure the same run of lines, which is what decides where the bars
   go: **one bar per gap, and none where the code is continuous.**

   - **One bar per gap, drawn at the gap** (`gapBar`). The two directions are
     the same run of hidden lines seen from its two ends, so one bar offers
     both, and each arrow reveals what lies in the direction it points --
     counted from the nearest visible line on the *far* side of the gap. At a bar
     between line 24 and line 142, `▲` shows what comes before 142 and `▼` shows
     what comes after 24. It was the other way round first, which reads just as
     coherently written down and not at all when you are looking at it; the
     tooltips now name the line ("Show the lines before 142"), because an arrow
     in the middle of a gap has two plausible readings and a number has one.
   - That is the fix for two things being wrong at once. The downward arrow used
     to live in a hunk's *header*, at the top of the hunk, while the lines it
     revealed appeared at the bottom -- off screen for any hunk taller than the
     viewport. And a header is only drawn while something is hidden above its
     hunk, so opening the gap above a hunk took away the arrow that opened the
     gap below it.
   - The bar is dropped when its gap closes, label and all. The `@@` line marks a
     discontinuity -- "the next thing you see is line N, not the line after the
     one above" -- so with nothing hidden there it would sit in the middle of
     continuous code announcing a jump that is not there. The gap after the last
     hunk gets a bar with no label, there being no hunk below it to name.
   - `sizeLineColumns` publishes the file's widest line number as a custom
     property the gutter cells size from. Sized per table, two hunks expanded
     until they touch would step sideways exactly where the bar that used to
     separate them has just been dropped.
2. **Expanded lines are spliced into one flat list with the hunk's own**
   (`effectiveLines`), because range selection indexes into that list. Rendered
   as a separate block they would be uncommentable, or would need a second code
   path for picking that could drift from the first.
3. **A gap's length is the same in both images**, since by definition nothing in
   it changed -- which is what lets an expanded line carry a correct *old* and
   *new* number without asking git anything.
4. **The file's length is not in the snapshot, and is not guessed either.**
   Counting it would mean reading every changed file on every two-second poll,
   against a refresh budget of ~145ms. Assuming a gap until proven otherwise --
   which is what it used to do -- put a bar at the foot of every diff whose last
   hunk already reached the end of the file, an arrow that did nothing. So an
   unknown length reads as "nothing below", and `ensureLineTotal` asks once per
   file per digest, for the file on screen only, quietly (a deleted file must not
   toast an error for having been opened). Untracked files are skipped outright:
   their diff is the whole file already. `readFileLines` clamps a range past the
   end and returns `Total` rather than refusing, so an expansion click is never
   wasted either.

**Expanded context survives both a reload and the file changing underneath it.**
It used to survive neither -- a reload lost it with the page, and any edit to the
file snapped every opened region shut. Three parts:

- **The counts and the lines are separated.** `dropStaleContext` drops a changed
  file's cached *lines*, which really are stale, but keeps how far the reader had
  opened it: "twenty lines above this hunk" is still exactly what they asked for.
  `refillExpanded` then re-reads what the counts need, in one request spanning
  the lot -- two reads of one file cost more than one read of a few extra lines,
  and the extra lines are ones the reader is likely to open next anyway.
- **`clampExpansion` fits the counts to the diff as it is now**, before anything
  builds lines from them. A count measured against hunks that have since moved,
  or restored from a previous visit, can overshoot its gap and render lines
  belonging to the hunk above: the same line twice, in two places, with nothing
  to say anything is wrong. A gap is shared, so the two counts opening into it
  are also held to its size together. The last hunk's downward count is left
  alone while the file's length is unknown, since `gapBelow` answers 0 then and
  clamping to it would throw a good expansion away.
- **A reload restores them from `localStorage`**, keyed by this review's own
  token, so an entry dies with the review it belongs to. Not through the host:
  every click would be an HTTP round trip and a config write, for something only
  worth keeping until the page closes. Entries other reviews left behind are
  pruned on load, and every access is guarded -- a private window throws on the
  accessor itself rather than returning empty.

The highlight memo is keyed on the **line count** as well as the expansion
counts. Those used to be the same thing, because a count only ever moved after
its lines had been fetched; a restored expansion arrives before them, so the
first render sees the counts with none of the lines, and keyed on the counts
alone that render's short array was handed back to the render after it --
painting the hunk's own text onto the expanded rows above it, line 1 reading
"line 077".

**Typing survives a repaint, and so does the caret.** The pane is rebuilt every
couple of seconds while an agent works, and a textarea whose text lives only in
the DOM loses it -- which is exactly what happened to a comment being written
when the agent touched the file it was about. Both textareas are now rendered
from state (`form.draft`, `state.replies[threadID]`), and the reply box in
particular became part of the render rather than something appended on click.

Keeping the words is not sufficient on its own. `captureTyping` /
`restoreTyping` carry focus and the selection across the rebuild, keyed by the
textarea's identity (`data-typing`) rather than the element, which is gone by the
time it has to be found again: a repaint that restores the text but drops the
caret to the start, or drops focus so the next keystroke goes nowhere, is only
marginally better than losing the comment.

Three consequences:

1. **A card being replied to is never collapsed** (`isCollapsed` checks for an
   open reply draft first), or folding it would hide text being typed.
2. **Navigating away no longer discards a draft.** The form names its own path
   and renders only on that file, so switching files -- or scopes -- and coming
   back keeps what was typed.
3. **A form whose anchor has left the diff renders in the orphan block**, with
   the threads whose lines are also gone. The text is safe in state either way,
   but a form nowhere on screen cannot be submitted or read back, which is the
   same loss by another route.

**A comment marks every line it covers, and its card renders after the last of
them.** The bar runs down the outermost column, clear of the hover pencil in the
gutter, and a single-line comment gets the same bar as a twenty-line one -- the
question the reader asks of the margin is "which lines is this about", and it has
the same answer either way. `placeThreads` walks the rendered rows in order and
keeps the *last* one each comment covers, which is what makes a range spanning
two hunks land at its true end and what identifies the comments with no row left
at all (they go to the orphan list). Rendering the card **after** the range
rather than before it is the point: above, a card is separated from its subject
by however many lines it covers and the reader has to guess downwards; below, the
marked lines read as the quote and the card as the response. The draft form is
marked the same way while it is being typed (`formCovers`), since losing the
marking the moment the drag ended made a multi-line comment look like it had
landed on one line.

**The drag repaints, it does not re-render** (`paintPick`). Rebuilding the table
mid-drag silently loses the drag: the mousedown lands on a cell, the re-render
destroys that cell, and Chrome then has nowhere to deliver the matching mouseup,
so the document handler that commits the range never runs. The reader releases
the mouse, no comment form opens, and `state.dragging` is left armed -- so their
*next* click anywhere finishes the stale drag and opens a form on lines they are
no longer looking at. A real mouse hid this most of the time, because any
movement after the last re-render re-targets the pointer at a live cell;
releasing without that final jiggle did not. Rows carry `data-i`, `data-hunk` and `data-file`
so the selection can be shown by toggling a class on the rows already there,
which is also simply cheaper than rebuilding every row in the hunk per row
crossed.

**And the range follows the pointer's row, not the gutter cell it started in**
(`armDrag` / `extendPick`). Tracking entry into the cell -- which is what this
replaced -- meant the range stopped following the moment the pointer drifted out
of a 22px column, and a comment card, being the full width of the pane,
guaranteed it did: no range could span one. Distance to the nearest row carries
the selection straight over the card. The rows are measured once at mousedown, in
scroll-invariant pane coordinates, which is only safe because the drag no longer
re-renders: from mousedown to release, nothing but classes changes.

This is also why `NewComment.normalize` only swaps two *real* line numbers. An
omitted `EndLine` is zero, and swapping that in anchored a single-line comment
from line 0 -- invisible while the range was only ever printed, and a marked run
from the top of the file once the page began drawing it.

**The diff's `+`/`-` is a column of its own** (`td.sign`, split off by
`splitMarker`). Git puts it in the first character of the line, and rendered that
way inside the code cell it reads as part of the file's text — which is how a
reviewer comes to believe an added line begins with a plus sign. Anything else in
that position is passed through whole rather than trimmed: git's
`\ No newline at end of file` is not a line of the file and has no marker to
strip.

Serving follows `internal/skillstats`: ephemeral loopback port, started on first
press, `Close` on shutdown. Each session's URL additionally carries a random
token, because the composed review hands that URL to the agent — a guessable one
would let anything else on the machine post replies as the agent. An unknown
token answers **404, not 403**: a wrong token is indistinguishable from a review
that has been closed, and "exists but forbidden" tells a guesser they found a
real session.

**The round trip is asynchronous, and has to be.** The page posts a submit to an
HTTP goroutine; pasting into a tab's PTY belongs to whatever owns the tabs. So
the handler calls a `send` hook that enqueues `messages.GitReviewSubmitted`
through `enqueueExternalMsg`, the app pastes it on the UI thread, and
`Service.ConfirmSend` reports back over the live stream. Two consequences:

1. **Threads stay `Sent: false` until the host confirms.** A failed paste — no
   agent tab open — surfaces on the page and leaves the comments pending, so the
   user retries instead of retyping. Marking them sent at submit time loses them.
2. **`GitReviewSubmitted` is a critical external message.** The non-critical
   queue drops under load, and a review is written by hand and submitted once:
   dropping it loses the user's comments with nothing to retry from and leaves
   the page reporting "sending" forever.

**Claude answers comments over HTTP.** `Compose` puts the `curl` for
`POST /api/reply` **before** the notes, and tags every note with its thread id.
Both halves are load-bearing: an agent that reads the notes first has already
decided what to do by the time it reaches the tail of a long review and will not
go back for a protocol it did not know it needed, and without an id it can reply
but not say which comment it is answering. Replies render under their comment
with a `claude` pill.

Two things about that preamble are deliberate, and both are omissions:

1. **It is short.** Every line of it is read before the agent sees a single
   comment, so protocol pushes the review itself further down. One curl, one
   sentence on where the thread id comes from, and the reply style -- nothing
   else.
2. **Resolving is never mentioned.** The endpoint still accepts `resolved` and
   the page still offers Resolve/Reopen, but the composed message says nothing
   about either: closing a comment is the user's judgement on whether their
   point was met, and an agent told it may resolve closes things it has not
   really dealt with. `TestComposeSaysNothingAboutResolving` guards the
   omission, so a later edit cannot reintroduce it as a helpful aside.

The reply style is stated because leaving it unsaid produces several paragraphs
in answer to a one-line comment, which is unreadable in a card the width of a
diff: simple, direct English (ASD-STE100 style), one or two sentences, three at
the very most (`TestComposeAsksForShortRepliesInPlainEnglish`).

**"Send as I comment" delivers each comment as it is written, and the second
message is not the first one again.** The toggle in the top bar is off by
default; with it on, the request that saves a comment also submits, so there is
no window in which the comment exists as a draft a reload could present as
unsent. `ComposeFollowUp` is what an agent mid-task gets: the file, the lines,
the comment and the thread id, and none of the framing -- repeating the curl and
the reply style costs it context and tells it nothing it does not already know.
Separators are drawn only between several comments, since one at a time is what
auto-send produces.

Four decisions hold it up:

1. **The mode is session state, not something the page remembers.** It decides
   where the reader's comments go, so a page that quietly forgot it -- on a
   reload, or in a second tab -- would leave them queueing while the reader
   believed each one had gone. It *is* remembered across sessions, but by the
   host rather than here (see **Sticky review settings** below).
2. **`handleComments` reads the mode from the session, never from the request.**
   A page holding a stale toggle would otherwise queue a comment the review
   considers already sent (`TestAutoSendIsReadFromTheSessionNotTheRequest`).
3. **The question is "has *this* agent been told how to reply", not "have we
   sent before".** A review page outlives the tab it was opened against, and
   `Retarget` can point it at another agent; one that never saw the curl cannot
   answer, and a follow-up that assumes it did fails silently. So
   `Session.needsProtocol` compares against the agent session, and a changed one
   gets the instructions in full again.
4. **The protocol counts as delivered only when the paste lands.** It is recorded
   in `MarkSent`'s success path, not at compose time: a review that failed to
   paste -- no agent tab open -- taught nobody anything, and recording it would
   make the retry a follow-up to a message the agent never received
   (`TestNeedsProtocolUntilASendActuallyLands`).

**A card header is one line, always, and a resolved one wears a single pill.**
Everything in it is fixed-size or ellipsizes: a pill allowed to wrap broke "1
reply" across two lines and made a shut card taller than an open one's first
line, and the anchor did the same by splitting a line range down the middle. Two
details are less obvious than they look:

- **The anchor does not shrink at all** (`flex: 0 0 auto`, capped by
  `max-width`). Allowed to, it ellipsized a range that fitted perfectly: the
  header is letter-spaced, so the last glyph carries a fraction of trailing
  space, the content measured a sub-pixel over an integer box, and
  `text-overflow` duly cut the `:31-48` off the end. The preview takes a shrink
  factor of 100 and absorbs the slack instead -- it is a convenience, and the
  anchor is the answer to "where".
- **Resolved replaces both the sent pill and the reply count.** How many times a
  thread was discussed and whether the agent was told are both answers to "what
  is left to do here", and the answer is nothing; `resolved` is filled rather
  than outlined because it is now the only thing on the row worth reading.
  `draft` survives being resolved, since a comment closed without ever being
  sent is worth knowing about in a way that "sent" is not.

**A reply is pending work in its own right.** `Reply.Sent` exists for the same
reason `Thread.Sent` does: a thread delivered an hour ago is no longer pending,
so with the flag only on the thread, a reply written under one reached nobody --
not on submit, which only looked at unsent threads, and not under auto-send,
which had hidden the submit bar anyway. `PendingReplies` reports the user's
unsent replies and `submitPending` sends them with everything else.

Five details:

1. **Only replies on threads the agent *has* are announced separately.** A reply
   under a comment that is itself still a draft is folded into that comment's
   note (`composeNotes`, as an `also:` line) and marked delivered with it --
   announcing a reply to something the agent has never seen tells it about half a
   conversation, and not marking it would re-announce it on the next submit.
2. **An agent's own reply is never pending**: it came from there, so `AddReply`
   stores it already sent (`TestAnAgentsOwnReplyIsNeverSentBack`).
3. **Threads and replies share one id space.** Both are minted by `newID` with
   different prefixes, so `MarkSent` takes one list and looks up either kind --
   `SendRequest.ThreadIDs` and `ConfirmSend` did not have to grow a second list,
   and the host stayed out of it.
4. **One paste, not two.** New comments and replies are composed separately and
   joined, because each send is a separate prompt: splitting them would interrupt
   the agent twice for one gesture, and let it start on the first half before it
   had read the rest.
5. **`storeVersion` went to 2.** A v1 file has no `sent` on its replies, so every
   one of them decodes as unsent and the first submit after an upgrade would
   announce the entire reply history. Loading a v1 file marks them all sent,
   reading them as the past -- which loses the delivery state of a reply written
   just before a restart, and that is much the smaller failure
   (`TestUpgradingTheStoreDoesNotReAnnounceOldReplies`).

`ComposeReplies` names its thread twice over, by id and by file and lines, and
quotes the comment being replied to: a reply on its own names no place at all,
and an agent several turns into the task may no longer have the original in
front of it.

**Under auto-send there is no queue, so there is no submit bar.** It is hidden
outright rather than shown with its buttons removed: a bar reporting comments
with no way to act on them is worse than no bar. That is also why turning the
mode *on* flushes whatever was already queued (`handleAutoSend` →
`submitPending`) -- with nothing left to press, a comment left behind would sit
there unreachable. The flush is reported to the reader rather than done quietly,
since they flipped a toggle and several comments left as a result. If it fails
-- no agent tab -- the error is toasted and turning the mode back off brings the
bar, and those comments, straight back; that is what makes "off" the recovery
path (`TestTurningAutoSendOffSendsNothing`).

The toggle sits left of the scope tabs, so the header reads outward from what the
review *is* toward how it is being run.

**Every event goes through `Session.withState`, and building one by hand is a
bug.** The page cannot tell an absent JSON field from a false one, so an event
that omits the read ticks or the send mode does not leave them alone -- it clears
them. `handleLive`'s opening event was built by hand and did exactly that: a page
loaded its state correctly from `/api/snapshot` and then had the ticks and the
toggle wiped the instant the live stream connected. Regression cover:
`TestTheOpeningLiveEventCarriesTheWholeState`.

**Sticky review settings live in `config.UI`, and travel through the host.**
`last_review_scope`, `last_review_autosend` and `last_review_split` join the
other "last used" values (`last_fullscreen`, `last_assistant`,
`last_create_worktree`): the reader ticks "Send as I comment", switches to Since
base or picks the split view once, and every review after that opens the same
way. The layout is carried through the session even though nothing on the server
reads it, because the page has nowhere durable of its own -- each review is
served from a fresh ephemeral port, so anything the browser stored would be lost
on the next restart. `internal/gitreview` deliberately knows nothing about a
config file -- it reports a `Preferences` value through the `OnPreferences` hook
and the host decides what remembering means.

Four properties:

1. **The save happens on the UI thread.** The change is noticed on an HTTP
   goroutine, and `config.UI` belongs to the UI thread, so it rides the message
   pump as `GitReviewPrefsChanged` exactly as a submitted review does. Writing it
   from the handler is a data race the detector would find. Unlike a submitted
   review it is **not** critical: a dropped preference costs a re-tick, where a
   dropped review costs the user's comments.

   Both hooks are connected in `wireGitReview` rather than inline in `App.New`,
   because the second one is easy to lose: registered in the wrong scope it still
   compiles, and the symptom is a preference that silently never saves -- which
   is exactly what happened, the hook having landed *inside* the send callback,
   where it would not have run until a review was submitted. Named, one test
   covers the wiring (`TestWiringRegistersBothReviewHooks`, which fails if the
   registration moves back inside).
2. **Both settings are reported together**, read off the session rather than
   taken as arguments, so a caller that has just changed one cannot overwrite the
   other with a stale value.
3. **A no-op is not a change.** The page asks for its scope on every load, so
   reporting unconditionally would rewrite the config file on every refresh
   (`TestAskingForTheScopeAlreadyShownReportsNothing`, and the handler compares
   before saving).
4. **Auto-send is now remembered across restarts, which it deliberately was
   not.** The old reasoning was that a mode chosen in an earlier sitting should
   never silently send comments; as an explicit sticky preference, alongside the
   others, that is exactly what the reader is asking for. A *fresh* config still
   defaults it off (`TestReviewSettingsRoundTrip` asserts both defaults), and a
   blank stored scope falls back to working rather than becoming the scope.

An existing session keeps the scope, mode and layout its open page is already
showing rather than being re-seeded on reopen -- which is the same value anyway,
since every change is reported back.

The chain has two ends and both are covered: `TestOpenSeedsTheReviewFromThe`
`RememberedPrefs` drives `handleOpenGitReview` over a real repo and asserts the
session opens with all three, because saving a preference is pointless if the
next press of Review Changes does not open with it.

**Untracked files are enumerated with `ls-files --others --exclude-standard`,
never `git.GetStatus`.** Status runs `--untracked-files=normal` (deliberately —
see `internal/git/status.go`), which stops at untracked-directory boundaries, so
a new package arrives as the single entry `internal/thing/`. That is right for
the dashboard's change indicator, which only needs a count, and fatal here
twice: the directory row has no diff to show, so its files are unreviewable, and
because that row's content never changes the snapshot digest does not move, so
editing anything inside it never repaints an open page. An agent creating a new
package is the common case. `--exclude-standard` is what keeps this affordable
on every poll by respecting `.gitignore`; `maxUntrackedFiles` caps the
pathological repo and says so on the page. Regression cover:
`TestBuildListsFilesInsideANewDirectory` and
`TestDigestMovesWhenAFileInANewDirectoryChanges`.

**Live refresh has two signals, and neither replaces the other.**
`Service.NotifyRootChanged`, called from `handleFileWatcherEvent`, makes git
operations repaint at once. It cannot be the only signal: Medusa's file watcher
watches a workspace's **`.git` directory** (see `internal/git/watcher.go`), so it
fires on commits, staging, checkouts and ref updates and **not at all** when an
agent simply edits a file in the working tree -- which is the review page's main
case. So the tick stays, and dropping it would freeze a page for as long as an
agent works without committing. Both paths refresh on the one poll goroutine, so
a burst of pokes cannot fan out into concurrent git runs over the same repo, and
the poke is a non-blocking send because it comes from the UI thread. Every git
read passes `--no-optional-locks`, which is also what stops the review's own
polling from touching `.git` and waking itself in a loop.

Polling only runs while a page is open (`Session.watched`), so a review left in a
background tab does not run git over the repo forever. The **digest covers every
hunk's content**, not just the file list and line counts: an agent rewriting a
line in place leaves both of those identical, and a page that does not repaint
for that is showing code that is no longer there. A scope change repaints
regardless, or two scopes producing the same diff would leave the page labelled
with the one it is not showing.

**A refresh is two git invocations, not one per file.** The obvious shape --
list the files, then `git diff -- <path>` for each -- measured ~800ms on a
33-file change, which at a 2s tick is a 40% duty cycle of git subprocesses. Now
one `--name-status` gives the authoritative list and one bulk diff is split at
its `diff --git` boundaries (`snapshot_diff.go`), which is ~145ms for the same
change. Two things make that safe:

- **Sections are attributed by order, then verified.** Paths in diff headers may
  be C-quoted, and misreading one attaches the wrong patch to the wrong file, so
  nothing is parsed out of them: git emits `--name-status` and a plain diff in
  the same order, each section is checked against the entry it was matched with,
  and *any* mismatch abandons the whole fast path for per-file calls rather than
  guessing. A quoted header is simply not decoded, so it falls back.
- **A rename must be asked for by both paths.** `git diff <rev> -- <newpath>`
  cannot see the rename pair and reports the file as newly added, so a pure
  rename came back as every line added. The bulk diff is not path-limited and
  gets this right; `diffAgainst` passes `oldPath` too so the fallback agrees.
  `TestBulkDiffMatchesPerFileDiff` compares the two paths file by file, hunk by
  hunk, and is what caught this.

New files are read from disk rather than diffed (`untrackedDiff`), since an
agent's work is mostly new files and that was the rest of the cost. Reading them
directly means this code owns what git used to absorb: NUL bytes mean binary, a
size check comes before the read, and anything that is not a regular file is
refused rather than followed -- a symlink's target is not this workspace's
business. `TestUntrackedDiffMatchesGit` holds it equivalent to what git produced.

**`broadcast` holds the session lock across its sends.** Sending on a closed
channel is a panic and `unsubscribe` closes, so copying the subscriber list and
releasing the lock — the obvious shape — meant a review tab closed while the
poller broadcast took the whole TUI process down. Holding it is affordable
precisely because every send is non-blocking: the critical section is bounded by
the number of open pages, never by how fast any of them reads. Dropping an event
is safe because each one carries the whole state it describes and the poller
guarantees a next. Callers must therefore not hold the lock, and must resolve
arguments like `Threads()` before calling. Regression cover:
`TestBroadcastRacesUnsubscribe` (needs `-race`).

**Both assistants can be reviewed, but only one can always answer.** The button,
the diff and the send path are assistant-agnostic -- `isAgentAssistant` admits
anything that is not a script or a shell -- so a review reaches a Codex tab
exactly as it reaches a Claude one. The **reply path does not survive Codex's
default sandbox**: under `workspace-write` Seatbelt denies network, so the
loopback POST cannot connect, and it fails as an ordinary "couldn't connect"
rather than as a permission error. Undetected, the agent concludes the review
server is down and sends the user hunting for a server that is running perfectly
well.

So the capability is resolved before the page says anything about replies.
`reviewAgentProfile` (`app_git_review_agent.go`) maps the tab's assistant and
sandbox to a `gitreview.AgentProfile`, which rides in on `OpenRequest`, is
stamped onto every snapshot, and drives three things: the page names the agent
(a Codex tab labelled "Submit to Claude" is simply wrong), a banner states that
replies are unavailable and gives the exact fix, and `Compose` **omits the reply
protocol entirely** rather than handing the agent instructions that will fail.

Four details are load-bearing:

1. **`workspace-write` is checked against the profile's config, not assumed.**
   `config.CodexNetworkAccessEnabled` reads `<CODEX_HOME>/config.toml` for
   `sandbox_workspace_write.network_access`, so a user who has already enabled it
   is not told replies are off. The file is **read and never rewritten**, for the
   same reason `InjectCodexTrustedDirectory` appends text: Codex owns it and
   keeps its own state there.
2. **An unknown or unset sandbox reads as "cannot reply".** Over-warning costs a
   dismissible banner; under-warning leaves the user waiting for replies that can
   never arrive.
3. **The fix names the profile's `CODEX_HOME`**, not `~/.codex/config.toml`.
   Medusa points `CODEX_HOME` at the profile directory, so the global file is the
   one Codex ignores and following that advice would change nothing. It also says
   that enabling this grants network access generally, since it does.
4. **Claude is treated as reply-capable.** True for every tab in practice, though
   a tab launched with the Sandboxed toggle may be subject to the same
   restriction -- what Claude Code's own sandbox does to loopback traffic has not
   been established, and claiming either way without knowing would be worse than
   the status quo. It is one more case in the same switch when it is.

Deliberately *not* done: a second, file-based reply channel. `$TMPDIR` and the
worktree are both writable under `workspace-write`, so a drop box would work --
but silently routing around a sandbox the user chose is not this feature's call
to make, and writing into the worktree would show up in the review's own diff.

Five smaller properties:

1. **The page opens on Uncommitted, and the branch scope diffs the merge base
   rather than the base branch tip.** Uncommitted is the work the user opened the
   review to look at -- what the agent has just done. The branch view is the step
   back from that, and it has to be the merge base: diffing the tip attributes
   every commit made on main since the workspace forked to the workspace, burying
   the actual change. It covers committed *and* uncommitted work, because an
   agent that commits as it works would otherwise have a review that hides all of
   it. `ParseScope` defaults to working, so a page that asks for no scope gets the
   same view as one that has never been touched
   (`TestParseScopeDefaultsToUncommitted`).

   **The base is re-resolved on every refresh, against the newest of two
   candidate refs** (`snapshot_base.go`). Both halves matter, and each was a bug:

   - *Newest of two.* `git.GetBaseBranch` returns a branch **name**, so "main"
     resolves to the local branch -- which in a worktree may not have moved since
     the workspace was cut, while the rebase went onto `origin/main`. The merge
     base against that stale ref is the old fork point, so every commit the
     rebase brought in showed up as the workspace's own change. The local branch
     can equally be the newer of the two, when the user has pulled it and not the
     remote ref, so neither is right on its own: `newestMergeBase` takes the merge
     base furthest forward in history, because of two candidate fork points the
     later one excludes more history the user did not write.
   - *Every refresh.* Resolved once at open time, a rebase mid-session would leave
     the review measuring from a fork point that no longer exists.

   `BaseRev` is part of the snapshot digest, because a rebase can move the fork
   point while leaving the resulting diff byte-for-byte identical, and a page that
   does not repaint for that goes on naming a commit the review is no longer
   measured from. The top bar prints the base's short hash and subject next to
   the branch names for the same reason the field exists: the base is derived
   rather than chosen and it moves under the reader, so a bar reading only "main"
   gives them no way to tell which fork point they are looking at. Regression
   cover: `snapshot_base_test.go` builds a rebased worktree with a stale local
   base and asserts the upstream commit stays out of the review.
2. **The button is not gated on a dirty worktree**, unlike the one it replaces.
   The branch scope reviews committed work, so a clean tree is exactly when the
   old button vanished and the new one is most needed.
3. **Comments anchor to `OldLine`/`NewLine` and quote their lines.** The
   rendered row index names no real place in a file, so it cannot be reported to
   an agent; the quote is what lets the agent find the place when the line number
   has since moved. A path from the browser is checked with `staysInWorkspace`, so a
   comment on `../../.ssh/config` is never quoted back as a file in this
   workspace.
4. **Threads persist to `<MetadataRoot>/<workspaceID>/review-comments.json`**
   (`store.go`), beside that workspace's `workspace.json`. They are outstanding
   work: a sent thread is something an agent was told about and may still be
   acting on, and a draft is something the user typed and has not sent -- losing
   either to a restart makes the page untrustworthy for any review spanning more
   than one sitting. Written temp-plus-rename, so a crash cannot leave a
   half-written file where the comments were; a corrupt or hand-edited file
   degrades to "no history" and is **left on disk** rather than deleted, so it
   stays recoverable. `maxPersistedThreads` prunes resolved threads before
   unresolved ones, because an unresolved comment is the last thing that should
   quietly disappear. An empty metadata dir disables persistence instead of
   failing, which is what tests and a Service built without one get.
5. **The navigator badge shows only what is outstanding**, and is not the read
   tick, which lives on the other side of the row. That means a count of
   *unresolved* comments and nothing at all for a file whose comments are all
   resolved: a navigator marked up with finished work competes with the work
   that is left, which is the only thing the column is for. Alongside it, a dot
   marks a file where the agent has replied and the reader has not looked --
   `state.unread`, page state because "have I read this" is a fact about the
   person looking and a second tab is a second reader. It is set by
   `announceAgentReplies` (which already had to notice a reply arriving on a file
   other than the current one) and cleared by `selectFile`. Clearing is not
   enough on its own: `readUnread` also **expands** the threads that carried the
   replies, since a resolved thread starts collapsed and would otherwise be
   marked read by a glance that could not possibly have included it. A shut
   folder carries both marks over everything inside it (`badgeHTML` is shared),
   so folding a directory away cannot hide either.

### Configuration

Per-repo workspace config lives at `.medusa/workspaces.json` (setup-workspace, run, archive). Environment variables passed to those commands include `$ROOT_WORKSPACE_PATH` plus an auto-allocated free port (`internal/process/env.go`).

Profiles used to share **one** skills/plugins tree: `profiles/<name>/skills` and
`profiles/<name>/plugins` were symlinked to `profiles/shared`. That is gone, and
`config.HealSharedProfileLinks` (run once per start, `app.New`) migrates anyone
still on it — it copies the shared tree into each linked profile and drops the
symlink. Only symlinks that resolve to `profiles/shared` are touched; a profile
with real directories, or a link the user made to somewhere else, is left alone.

**The copy is not enough on its own — the plugin store records absolute paths.**
`known_marketplaces.json` and `installed_plugins.json` name where each
marketplace and plugin lives (`installLocation`, `installPath`), and while the
store was shared, whichever profile wrote an entry wrote *its own* path in. So
Work's copy could say `profiles/Default/plugins/marketplaces/...`, and Claude
would then read a marketplace out of a directory that profile does not own —
which breaks the moment the other profile changes it. `repointPluginPaths`
rewrites just the profile segment of every such path, in the JSON files at the
top of the plugins dir only: the marketplace and cache checkouts below it are
repository content, not the store's bookkeeping.

`profiles/shared` and the `skills_backup` / `plugins_backup` directories the old
sync left behind are the user's data, so the heal leaves both on disk.

## Commits & releases

Conventional-commit-lite — the `.goreleaser.yml` changelog filter depends on the prefix. See `.claude/skills/medusa-commits-and-releases/SKILL.md` for the full table and the release walkthrough.

Short version: `feat:` / `fix:` / `refactor:` / `perf:` surface in release notes; `docs:` / `test:` / `ci:` / `chore:` don't.

Local merges to `main`: `git merge --squash <branch>` then `git commit -m '<conventional subject>'`. Do not run bare `git commit` — its default template ("Squashed commit of the following:") becomes the commit message if you don't edit it. Plain `git merge` (no `--squash`) produces `Merge branch` commits that clutter release notes.
