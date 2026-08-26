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

### Change review window: `internal/ui/review`

`[Review Changes]` in the center info bar (or `ctrl+a v`) opens a split-pane
overlay — changed files left, the selected file's diff right — where the user
annotates lines, edits files in place, and sends the result back to the agent.
It is a self-contained sub-model like `internal/ui/diff`: it imports no app
code and hands back a single `review.Result`.

Four properties are load-bearing:

1. **The review is sent as a bracketed paste, and the Enter goes separately.**
   Written raw, every newline in the message is an Enter, so the agent gets the
   first line as a prompt and each following line as its own — a review arrives
   as a dozen half-sentences and the agent answers the first before reading the
   rest. `bracketedPaste` (`internal/ui/center/model_tabs_send.go`) wraps it in
   `ESC[200~`/`ESC[201~`; the submitting `\r` must follow *after* the closing
   sequence, since inside the brackets it is pasted text and nothing is sent at
   all. `SendToAgentSession` resolves the tab **by session name**, not the
   active tab: the user can switch tabs while the window is open, and the review
   belongs to the agent whose changes it describes.
2. **An edit buffer refuses to write over a file that moved under it.**
   The agent is often still running, so `editBuffer` stamps size+mtime at open
   and re-stats before writing (`internal/ui/review/edit.go`). A refused write
   aborts the **whole** send: the message names the files the user edited by
   hand, and sending it while one of those edits sits unwritten would point the
   agent at a file that still holds its own version. Silently clobbering the
   agent's work is the worst thing this feature could do, and it would be
   invisible. Regression cover: `TestEditBufferStalenessGuard`.
3. **Comments anchor to real file lines, which is why `git.DiffLine` carries
   them.** `parseDiff` seeds `OldLine`/`NewLine` from each `@@` header rather
   than counting from the top of the diff, so the second hunk numbers correctly.
   Two rows reach the context branch that are not content and must not consume a
   number — the empty string `strings.Split` leaves after a trailing newline,
   and `\ No newline at end of file` — or every row after them reports one line
   past where it lives. A deleted row has no post-image number, so `anchorLine`
   falls back to its old one: a comment on `:0` names nothing.
4. **Editing a unified diff is not possible**, so `e` swaps the pane to the
   working-tree file, keeping the gutter marks the diff supplies
   (`changedLines`). That is what the line numbers buy beyond comments.

**The editor uses `textarea` as its model but draws itself** (`view_edit.go`).
Two things force that, and both are properties of the widget rather than
preferences: it has no per-token styling hook (its `StyleState` applies to the
whole field), so its rendering cannot carry syntax colour; and its viewport only
takes content during `View`, so any scroll set before the first render is
clamped to zero — which left the caret parked on line 120 with the pane still
showing line 1. `editScrollTop` therefore owns scrolling and centres the caret,
and the textarea is read back through `Value`, `Line` and `LineInfo`.

**Both textareas must have `MaxHeight` and `MaxWidth` cleared.** The widget
defaults them to 99 rows and 500 columns, and once a value reaches `MaxHeight`
it makes Enter a **silent** no-op — no error, no bell. Every source file worth
reviewing is longer than 99 lines, so Enter simply appeared not to be wired up,
and a short test fixture cannot see it (`TestEnterWorksPastTheWidgetsHeightCeiling`
uses 400 lines for exactly that reason). `MaxWidth` truncates long lines just as
quietly, which would shorten a minified file on save. Read the caret column from
`Column()`, not `LineInfo().ColumnOffset`: the latter is the offset within the
*visual* row, so on a soft-wrapped line it draws the caret at the start of the
wrap rather than where the user is typing.

**The widget rewrites every tab as four spaces on load, and cannot be told not
to** — its sanitizer is an unexported field of an internal package. Saving its
value verbatim would reindent whole files: a spurious 400-line diff for Go, and
for a Makefile a silent break, since there the tabs are syntax. So `editBuffer`
keeps both `original` (raw) and `shown` (tab-expanded), and:

- `Dirty` compares against **`shown`**, or every tab-indented file reads as
  edited the moment it is opened and lands in the review as "I edited this".
- `merged` writes a line the user did not touch back **byte-identical** from
  `original`, and folds leading spaces back to tabs only on lines that changed,
  only when the file is tab-indented (`usesTabIndent`), and only in the leading
  run — spaces aligning a trailing comment are the author's.

Regression cover: `edit_tabs_test.go`.

**Syntax colouring is `internal/syntax`**, a shape-based tokenizer rather than
real grammars. **chroma was tried and reverted**: it does the job properly, but
its lexers add **4.7MB** to the binary and take ~30ms on a few-hundred-line file
— thirty times this — which is felt as scroll lag in a view that redraws per
keystroke. A terminal palette shows six or seven colours, and getting those
right does not need a parser.

`Highlight` takes the **block about to be drawn**, not a line: a comment or
string spanning lines is invisible to anything fed one line at a time, and its
body then colours as though it were code. `state` carries between lines within
the block. A window opening *inside* such a construct still lexes from the wrong
state — bounded and local.

`wordKind` looks one token ahead for `(`, which is the single structural fact
worth a colour of its own: without it a line is a wall of one colour, and that
distinction was most of what made highlighting worth having. An unrecognised
extension tokenizes as plain text rather than being guessed at. The load-bearing
test is that highlighting never changes the text.

**The view's two derived structures are cached, and have to be.** The line
marks (`editBuffer.cached`, keyed on content) and the row list
(`Model.rowsCache`, keyed on path + diff pointer + `commentsRev`) are each read
several times per frame — by the gutter, the header, the rebuilt diff, the
cursor and every hit test. Uncached, a single wheel event ran **four to five
full diffs of the file**, which is what scroll lag felt like.
`TestViewIsCheapEnoughToScroll` holds a frame under 8ms; it is ~1.4ms.
Any change to the notes must bump `commentsRev`, or the rows go stale.

**The editor's textarea is pinned to `noWrapWidth` so the model never wraps.**
`CursorDown` moves by *visual* row, so with wrapping on, stepping to a line
counts wrapped rows and stops short — jumping to line 120 of a file with long
comments landed on line 13 — and `Column()` stops being the logical column.
Wrapping is a display concern and this view clips.

**The diff pane's cursor moves over `paneRows`, not over `DiffResult.Lines`**
(`rows.go`). Notes are drawn between diff lines, so a cursor that only knew
about diff lines could never land on one — the comments were visible and
unreachable, with no way to edit or delete one once written. `renderDiffRows`
walks the same list, so what is drawn and what is selectable cannot drift apart.
`e` on a note reopens it (removing the original first, so attaching does not
duplicate), `d` deletes it, and an emptied note is a deletion — the same meaning
an empty rename has elsewhere in medusa.

**`ctrl+enter` is the one submit gesture**: it attaches the note being typed,
and from anywhere else sends the review. `alt+enter` is bound alongside it
because ctrl+enter is not deliverable everywhere — a terminal without the Kitty
keyboard protocol sends a bare CR for both enter and ctrl+enter, so the binding
would be dead there with nothing to suggest why. Enter stays a newline in both
text panes.

**Click targets are recorded while drawing, never recomputed** (`rowMap` in
`mouse.go`). A comment wraps to as many rows as its text needs, a removal draws
rows of its own, and the editor inserts lines the buffer does not have — so
screen-row-to-item is a product of rendering, and a second implementation of it
would be wrong the first time either renderer changed. Clicks arrive in screen
coordinates and the window works in its own, so **the app rebases them**
(`localizeReviewMouse`): it is the only thing that knows where it centred the
window, and duplicating that arithmetic in the window is how the two drift.

**A note's text starts in the same column as the code it annotates**, with its
bar in the diff's marker column (`commentRow` mirrors `renderDiffLine`'s
layout). Indented anywhere else it reads as stray text dropped into the middle
of the file rather than as a note about a line. The comment editor is drawn the
same way and for the same reason the file editor is: the widget paints its own
prompt column and highlights the caret line across the whole field, which put a
stray bar and a black band through the diff.

**The line diff is Myers, via `go-udiff`** (`linediff.go`), not something local.
It replaced a hand-rolled quadratic LCS with an area budget, which past the cap
gave up and marked the whole changed window as modified — so an ordinary file
with two edits ninety lines apart drew every line between them as changed
(91×91 exceeds a 4000 cap). A real diff has no such cliff, and no constant to
tune. `TestDiffIsFastOnALargeFile` keeps it inside a keystroke on 20k lines,
since the marks are recomputed every frame.

**`cancelIdentical` is load-bearing.** An edit script is not unique, and the
line-level conversion readily emits `delete b, insert b, insert NEW` for what
the user experienced as inserting one line — taken at face value that reports
two changed lines. Trimming the lines identical at both ends of a change leaves
only what differs, and is also what makes the rewrite pairing safe: everything
reaching `addedOrModified` genuinely differs. Deletions are carried forward in
`carry` rather than written to `marks` eagerly, because the line they attach to
has not been emitted yet and writing ahead of it is overwritten the moment it
is. Deletions have no
row of their own left in the buffer, so they attach to the line that closed over
them, and removals with no following line attach to the last — dropped instead,
deleting a block was the one edit that left no trace at all.

**Both marker tiers are live diffs over the buffer's current lines** — nothing
reads the git diff's line numbers, which are fixed when the window opens. Using
them meant an unsaved insertion left every marker below it pointing at whatever
slid into its slot: the indicator stayed with the *number* instead of with the
line. `editBuffer` holds the committed content (`git.FileAtHead`, read once at
open) so `BaseMarks` can diff against it every frame without touching git;
`Marks` diffs against what was opened, for the user's own edits.

**An unsaved buffer rebuilds its file's diff** (`live.go`). A git diff describes
what is on disk, so a buffer typed into and not saved is invisible in it —
pressing esc showed the diff exactly as it was before the user started, which
reads as their work having been thrown away. `displayDiff` is the single
accessor every view must use: the pane, its row list and its header have to
agree, or the cursor navigates one diff while another is drawn under it.

`lineMark.Replaced` is why a rewrite can be both things at once: **one** `~` in
the editor's gutter, and `-old` / `+new` in a diff view. Dropping it made a
rewritten line read as a pure insertion with the replaced text simply gone.

**The gutter reads as a diff, and `lineMark.RemovedBefore` carries the removed
*text*, not a count.** `+` is a line that was not there, `−` one that is gone,
`~` one that was rewritten — the vocabulary the reader already has, where a bar
said only *that* something changed. A deleted line is drawn where it used to be,
with its text and a blank number column: a count ("1 line removed") names
nothing the reader can look up, since the line is no longer in the buffer to
scroll back to, and numbering it with its neighbour's position would be a lie.
The user's own edits are bright and win the column; a line the agent added that
they have not touched is the same `+` muted — two tiers rather than two glyphs,
so the meaning of `+` stays single.

**A removal adjacent to an addition is paired into one modification**
(`addedOrModified`). An LCS has no notion of "changed": rewriting a line is a
delete plus an insert, so fixing a typo drew a green bar *and* a red "1 line
removed" rule — two rows and two colours for one keystroke. Counts follow the
same rule (`editCounts`: `~` modified, `+` added, `−` removed, non-zero parts
only), because reporting a rewrite as both doubles every edit.

The editor header counts lines **the user** has changed, recomputed each frame
from the buffer (`DirtyLineCount`). It used to report the diff's added-line
count, which never moved however much was typed: a number that does not respond
to editing is worse than no number, since it reads as a broken live value.

Opening a file lands the cursor on the first row *of* the hunk while the
viewport starts at the `@@` header: a header is not a line of the file, so
opening on it makes the very first `c` or `e` fail for a reason the user could
not have anticipated.

**Every row of a pane must be exactly the pane's width** (`joinPaneRows`).
`lipgloss.JoinHorizontal` sizes a block to its widest line, and the outer frame
then wraps every joined line that no longer fits — which is how the comment
editor's box ended up drawn across the file list with the window twice its
height. For the same reason the editor is prefixed rows rather than a nested
bordered box: a lipgloss border inside hand-built rows is re-measured by the
frame's own `Width()`. Note `Width()` is the *total* rendered width, border and
padding included, so the panes get `bodyWidth() = m.width - 4`.

Two rendering details that were wrong first time and are cheap to get wrong
again: `---`/`+++` file headers open with the same characters a diff marker
does, so `trimDiffMarker` must be applied only to add/delete/context rows; and
the file list's counts use `−` (U+2212, three bytes, one column), so its widths
must be measured with `lipgloss.Width`, never `len`. The same byte-vs-column
trap applies to any test that locates a button with `strings.Index` — the info
bar contains `←` and `│`, which puts a byte offset four columns right of the
glyph.

The window opens on each file's **first hunk**, not row zero: a diff starts with
`diff --git`/`index`/`---`/`+++`, which repeat what the window header already
says. `gitDirty` gates the button and is pushed from the app
(`center.SetGitDirty`) wherever the sidebar's status is set — including the
workspace-switch paths, or a previous workspace's button sits on a clean one.

The button is **right-aligned in the info bar**, and the left side (branch,
path, IDE) is what gives way when the two collide: the path is already
abbreviated and can lose a little more, whereas a button pushed past the edge is
simply gone — the failure hardest to notice, since nothing is left to hint it
should be there. `TestReviewButtonIsFlushRight` sweeps widths down to 40 columns.

**Neither git-status path may gate on the sidebar.** `handleGitStatusTick` and
`handleFileWatcherEvent` both used to skip the active workspace's refresh while
`layout.SidebarHidden()`, since the sidebar was the only consumer. It no longer
is: the button and the live review window are gated on that same status, so with
the sidebar collapsed neither could ever turn on and the button was simply
absent. `TestGitStatusRefreshIsNotGatedOnTheSidebar` guards it.

**The view is live, and merges rather than replaces** (`refresh.go`). It tracks
the agent off `messages.GitStatusResult` — the *debounced* signal — never off
raw `FileWatcherEvent`s: a busy agent touches files far faster than a full
re-diff of the workspace runs. `handleFileWatcherEvent` requests that status
when the review window is open even if the sidebar is hidden, or the window
freezes on the diff as it stood when it was opened. `Refresh` drops a request
that arrives mid-read and records `refreshPending` instead, so a burst collapses
into one follow-up read rather than a queue the window is permanently behind.

Everything the user has produced has to survive a refresh, and a reload knows
about none of it:

- **Comments and cursor re-anchor by text, not by line number.** Both hold the
  content they were placed against, and `findQuotedLine` moves them to the
  *nearest* match — nearest because files repeat lines (a closing brace, a blank
  line) and the first textual match is often not the one the user chose.
  Restoring by number instead leaves the reader staring at whatever the agent
  just pushed into that slot, and drifts a line further on every insertion.
- **A note whose quoted line is gone is kept and marked `Stale`**, rendered
  muted and sent with "(you have since changed this line)". Dropping it loses
  the user's typing; sending it silently makes the agent hunt for text that is
  not there.
- **A file the agent reverts stays in the list**, marked `Gone`, and its pane
  shows its orphaned comments — a row held open purely to preserve notes has to
  show them, or keeping it is indistinguishable from having dropped them.
- **Edit buffers are keyed by path and never touched**, and `markEditConflicts`
  flags one whose file the agent has rewritten (`!` on the row). The write is
  refused at save time regardless; the marker is what stops the user typing
  another paragraph into a buffer that can no longer be written.

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
