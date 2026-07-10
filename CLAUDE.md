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
make build            # builds `medusa`, `medusa-approve-compound`, `medusa-hook-emit`
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

### Workspace / worktree model: `internal/data`

A `Workspace` can span multiple repos (each with its own worktree) but shares a single branch. `Workspace.Root()` is the primary worktree root; `AllRoots()` / `PrimaryWorktreeRoot()` account for multi-repo layouts. Registry at `~/.medusa/workspaces.json` is the source of truth; `data.Registry` and `data.WorkspaceStore` are both guarded by `sync.Mutex` (saveLocked/deleteLocked pattern). Orphan handling has two flavors: `OrphanMetadata` (registry knows about a dir that's gone) and `OrphanDirectory` (dir on disk with no registry entry).

### Messages: `internal/messages`

Pure type declarations; no package may import app/ui code. Split into several files by concern (`messages_permissions.go`, `messages_actionbar.go`, `messages_sidebar.go`). When adding a message, check whether a concern-scoped file already exists before adding to `messages.go`.

### Entry points

- `cmd/medusa` — the TUI binary.
- `cmd/medusa-approve-compound` — standalone helper invoked as a Claude Code hook for permission prompts.
- `cmd/medusa-hook-emit` — standalone helper invoked as a Claude Code hook to forward lifecycle events to the activity socket.
- `cmd/medusa-harness` — headless render driver used by `make release-check` and benchmarks.

### Configuration

Per-repo workspace config lives at `.medusa/workspaces.json` (setup-workspace, run, archive). Environment variables passed to those commands include `$ROOT_WORKSPACE_PATH` plus an auto-allocated free port (`internal/process/env.go`).

## Commits & releases

Conventional-commit-lite — the `.goreleaser.yml` changelog filter depends on the prefix. See `.claude/skills/medusa-commits-and-releases/SKILL.md` for the full table and the release walkthrough.

Short version: `feat:` / `fix:` / `refactor:` / `perf:` surface in release notes; `docs:` / `test:` / `ci:` / `chore:` don't.

Local merges to `main`: `git merge --squash <branch>` then `git commit -m '<conventional subject>'`. Do not run bare `git commit` — its default template ("Squashed commit of the following:") becomes the commit message if you don't edit it. Plain `git merge` (no `--squash`) produces `Merge branch` commits that clutter release notes.
