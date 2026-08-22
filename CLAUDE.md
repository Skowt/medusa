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

PTY readers run as long-lived goroutines. They capture the workspace ID at start; when a workspace is renamed, `MigrateWorkspaceTabs` populates `wsIDRedirects` so stale messages resolve to the new ID instead of killing the readers.

Workspace rename: do NOT restart PTY readers — the redirect map handles routing and restarting races with the blocked read goroutine.

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

### Codex tabs

A tab's assistant is picked in the New Tab dialog's "Assistant" cycler, which
is **sticky** (`config.UI.LastAssistant`) and drives every no-dialog launch
path too. Cycling it rebuilds the dialog: Claude's permission modes and Codex's
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

Degradations to expect: Codex has no Notification event, so no idle_prompt or
permission_prompt ping, and its Stop payload carries no `background_tasks`, so
the outstanding count stays unknown and every Stop reads as ready.

Medusa does not intercept or share either assistant's permission decisions.
The Codex New Agent dialog exposes a Starting Mode: Auto adds
`--approve-for-me`, while Default leaves approvals to Codex. Web search is
always enabled with `--search`; the selected Codex sandbox is passed through
with `--sandbox`.

### Workspace / worktree model: `internal/data`

A `Workspace` can span multiple repos (each with its own worktree) but shares a single branch. `Workspace.Root()` is the primary worktree root; `AllRoots()` / `PrimaryWorktreeRoot()` account for multi-repo layouts. Registry at `~/.medusa/workspaces.json` is the source of truth; `data.Registry` and `data.WorkspaceStore` are both guarded by `sync.Mutex` (saveLocked/deleteLocked pattern). Orphan handling has two flavors: `OrphanMetadata` (registry knows about a dir that's gone) and `OrphanDirectory` (dir on disk with no registry entry).

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

### Configuration

Per-repo workspace config lives at `.medusa/workspaces.json` (setup-workspace, run, archive). Environment variables passed to those commands include `$ROOT_WORKSPACE_PATH` plus an auto-allocated free port (`internal/process/env.go`).

## Commits & releases

Conventional-commit-lite — the `.goreleaser.yml` changelog filter depends on the prefix. See `.claude/skills/medusa-commits-and-releases/SKILL.md` for the full table and the release walkthrough.

Short version: `feat:` / `fix:` / `refactor:` / `perf:` surface in release notes; `docs:` / `test:` / `ci:` / `chore:` don't.

Local merges to `main`: `git merge --squash <branch>` then `git commit -m '<conventional subject>'`. Do not run bare `git commit` — its default template ("Squashed commit of the following:") becomes the commit message if you don't edit it. Plain `git merge` (no `--squash`) produces `Merge branch` commits that clutter release notes.
