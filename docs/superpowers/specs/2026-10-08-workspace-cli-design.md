# Workspace CLI — design

Date: 2026-10-08

## Goal

Scripts and agents must be able to create, list and delete medusa workspaces
from the shell, and start an agent with a first prompt. The CLI must work when
the medusa TUI is not running. When the TUI *is* running, it must reload and
show what the CLI changed, with no user action.

## Decisions already made

- **The CLI always does the work itself (approach A).** It never delegates to
  the TUI. A script gets the same result, the same errors and the same JSON
  whether or not medusa is open.
- **The creation and deletion logic moves out of `App` into a shared package**,
  so the TUI and the CLI run one code path.
- **Agent settings use what is already saved.** No new config is added.
- **CLI flags never save settings.** A one-off `--permission-mode plan` in a
  script must not change what the user's next tab opens with.

## Commands

```
medusa workspace new <name> [flags]        (alias: medusa ws new)

  Where it's created
    --repo <path>          source repo (default: the git repo containing the cwd)
    --base <branch>        base branch to cut from (default: the remote's main branch)
    --from-current         cut from the branch the repo has checked out
    --no-worktree          use the repo's own checkout instead of a new worktree
    --copy-ignored         copy gitignored files into the worktree
    --group <name>         dashboard group
    --profile <name>       medusa profile

  Agent
    --agent claude|codex   which assistant to start
    --no-agent             create the workspace only
    --prompt <text>        first prompt for the agent
    --prompt-file <path>   same, read from a file ("-" reads stdin)
    --permission-mode <m>  Claude only: default|acceptEdits|plan|auto|bypassPermissions
    --[no-]fullscreen      Claude only
    --sandbox <policy>     Codex only: read-only|workspace-write|danger-full-access
    --codex-auto           Codex only: --approve-for-me

  Output
    --json

medusa workspace list [--profile <name>] [--group <name>] [--json]

medusa workspace delete <name|id> [--yes] [--json]
```

`workspace` and its alias `ws` are dispatched in `cmd/medusa/main.go` before
any TUI or logging setup, like `medusa skills`. The code lives in
`cmd/medusa/workspace*.go`, split by subcommand to stay under 500 lines.

### Help

Help works at every level, like most CLI tools:

- `medusa help`, `medusa '?'`, `medusa -h` and `medusa --help` print the
  top-level help: what medusa is, the command list, and global flags.
- `medusa workspace` (no subcommand), `medusa workspace help`,
  `medusa help workspace` and `medusa workspace --help` print the workspace
  commands.
- `medusa workspace new --help`, `medusa workspace new help`,
  `medusa help workspace new` and `medusa ws new -h` print that command's
  usage, flags and examples.
- `help`, `?`, `-h` and `--help` are recognised at any position in the
  command path. Help goes to stdout and exits 0. An unknown command prints
  the nearest help to stderr and exits 2.
- Running `medusa` with no arguments still starts the TUI. An unknown first
  argument is now an error, where it used to be ignored.
- `?` is a glob character in zsh and bash, so it has to be quoted. The README
  says so.

### README

The README gets a "Command line" section covering the three commands, the
defaults table in short form, `--json`, exit codes, scripting examples (create
with a prompt, pipe the JSON into `jq`), and how to get help.

Flags may come before or after the positional name
(`medusa workspace new test --profile Work` and
`medusa workspace new --profile Work test` are the same).

### Common behaviour

- Exit code `0` on success, `1` on failure, `2` for bad usage (unknown flag,
  conflicting flags, missing name).
- Errors and progress go to stderr, so `--json` output on stdout stays
  parseable.
- Usage errors: `--base` with `--from-current`; `--base`/`--from-current`
  with `--no-worktree` (a checkout workspace has no base); `--prompt` with
  `--prompt-file`; `--no-agent` with any agent flag; Claude-only flags
  with `--agent codex` and the reverse. The last case is checked after the
  assistant is resolved, so `--sandbox` alone on a profile whose saved
  assistant is Claude is an error too.
- The workspace name is validated with `validation.ValidateWorkspaceName`,
  the same check the dialog uses.

### `new`: defaults when a flag is absent

| Setting | Source, in order |
|---|---|
| Profile | `--profile`, then `config.UI.LastProfile`, then `Default` |
| Worktree | `--no-worktree`, then `config.UI.LastCreateWorktree` (default on) |
| Base | `--base` / `--from-current`, then remote main (`BranchModeRemoteMain`) |
| Start an agent | `--no-agent` forces off; `--agent`, `--prompt` or `--prompt-file` forces on; otherwise `config.UI.AutoStartAgent` |
| Assistant | `--agent`, then `config.UI.AssistantFor(profile)`: the profile's saved assistant, falling back to the global `LastAssistant` |
| Claude options | each flag, then the global `LastPermissionMode`, `LastFullscreen`, `LastIsolated`, `LastAllowUnsandboxedCommands` |
| Codex options | each flag, then the global `LastCodexSandbox`, `LastCodexStartingMode` |

The two agent rows are exactly what the TUI's `stickyAssistant` and
`lastUsedLaunch` do today. Both move into the shared package and the TUI calls
them from there, so the CLI and the TUI's no-dialog launches cannot disagree.

### `new`: steps

1. Resolve the repo (`git rev-parse --show-toplevel` from `--repo` or the cwd)
   and every setting above. Fail on a usage error before touching anything.
2. Fail if the name is taken (checked against the registry, not an in-memory
   list) or, for a worktree, if the branch already exists.
3. Fetch and resolve the base. These are the same functions the TUI uses:
   `fetchRemoteBase`, `fetchCheckedOutBase` and `fetchCustomBase`. A checkout
   workspace runs `pullBeforeOpen`, and its notice is printed to stderr.
4. Create the worktree, save the metadata and add the registry and recents
   entries. This is the existing `createWorkspace` + `registerWorkspace`,
   rollback included.
5. Copy ignored files if asked.
6. Run `setup-workspace` scripts synchronously, streaming output to stderr.
   They are skipped for a checkout workspace, as in the TUI. A setup failure
   is a warning, as in the TUI: the workspace is kept and the command still
   exits 0, but the JSON output reports `"setup_warning"`.
   `run` scripts are not started: in the TUI they open as tabs, and the CLI
   has no tabs.
7. Start the agent (see below) and record it in the workspace's `OpenTabs`.
8. Notify a running TUI (see below).
9. Print the result.

JSON output for `new`:

```json
{
  "id": "…", "name": "test", "root": "/…/workspaces/test",
  "branch": "test", "base": "origin/main", "worktree": true,
  "profile": "Work", "group": "",
  "agent": {"assistant": "claude", "tmux_session": "medusa-test-1", "session_id": "…"},
  "setup_warning": "",
  "tui_notified": true
}
```

`agent` is `null` when no agent was started. `session_id` is empty for Codex,
which mints its own (see Degradations).

### `list`

`list` reads the registry and the store. Default output is a table:
NAME, PROFILE, GROUP, BRANCH, ROOT and LIVE. LIVE is the number of medusa tmux
sessions tagged with the workspace ID. `--json` prints an array of the same
fields plus `id` and `worktree`. Archived and orphaned entries are included and
marked, so a script can see why a name is taken.

### `delete`

1. Look up the workspace by exact ID, then by exact name. Fail if there is no
   match.
2. Ask for confirmation on a TTY. With no TTY, `--yes` is required, or the
   command exits 2.
3. If the TUI is running, have it drop the workspace's tabs first (see
   "Delete while the TUI has the workspace open"). Then kill its tmux
   sessions: `tmux.KillSessionsMatchingTags({@medusa: 1,
   @medusa_workspace: <id>})`, the same as `cleanupWorkspaceTmuxSessions`.
4. Run the shared delete. This is today's `deleteWorkspace` minus its
   `activeWorkspace`/`goHome` part, which stays in `App`. It keeps every
   safety check unchanged: `removableWorktrees` never removes the source repo
   or a path that is not a worktree, branch deletion is tied to the same test,
   and `os.RemoveAll(root)` runs only when `UsesWorktree()`.
5. Notify a running TUI and print the result.

## Shared core: `internal/workspaceops`

The package takes its dependencies explicitly: `*config.Config`,
`*data.Registry`, `*data.WorkspaceStore`, the recents store and a
`*process.ScriptRunner`. It never imports `internal/app` or `internal/ui`.
It exposes:

- `Create(ctx, CreateRequest) (*data.Workspace, error)`, covering steps 2–5.
  The TUI keeps its async shape by wrapping this in a `tea.Cmd` and mapping
  errors to `WorkspaceCreateFailed`. The base-fetch functions move with it.
- `Delete(ws) error`, today's `deleteWorkspace` body.
- `ResolveLaunch(cfg, ws, overrides) LaunchSettings`, today's
  `stickyAssistant` + `lastUsedLaunch`. `App` keeps thin wrappers so call
  sites barely change.
- `NameExists(registry, store, name) bool`, replacing the `a.allWorkspaces`
  lookup in `workspaceNameExists` for the CLI. The TUI keeps its in-memory
  check as well, because it also covers placeholder workspaces still being
  created.

Setup scripts stay in `process.ScriptRunner`. The CLI calls `RunSetup`
directly. The TUI keeps `runSetupAsync`.

## Starting the agent headlessly

The side effects in `AgentManager.CreateAgentWithTags` move into an exported
`pty.PrepareAgent(cfg, ws, opts) (PreparedAgent, error)`: the profile dir,
`InjectTrustedDirectory`, `prepareCodexHome` and the wrapped command with its
`stty sane; exec $SHELL -l` tail. `CreateAgentWithTags` calls it, so the TUI's
behaviour does not change.

The CLI then:

1. Picks a session name with `tmux.NextUniqueSessionName`, using
   `tmux.DefaultOptions()` after applying the config's tmux env, as
   `applyTmuxEnvFromConfig` does.
2. For Claude, pre-mints the session ID, as `claudeSessionArgs` does today, so
   a restart can `--resume` it.
3. Runs `tmux new-session -d -s <name> -c <root> sh -lc <cmd>` with the PTY
   env the TUI sets (`WORKSPACE_ROOT`, `WORKSPACE_NAME`, `COLORTERM`), then
   sets the same tags `clientCommand` sets (`@medusa`, `@medusa_workspace`,
   `@medusa_tab`, `@medusa_type=agent`, `@medusa_assistant`,
   `@medusa_created_at`, `@medusa_fullscreen`, plus `mouse on` when
   fullscreen). The tag-setting code is factored out of `clientCommand` so
   both callers share it.
4. Appends a `data.TabInfo` to `ws.OpenTabs` with `Assistant`, `Name`,
   `SessionName`, `Status: "running"`, `ClaudeSessionID` and the launch flags,
   then saves the workspace.

On its next `loadWorkspaces`, the TUI adopts the session through the existing
`RestoreTabsFromWorkspace` path. Its `new-session -A` attaches to the running
session and ignores its own command. If the TUI is not running, the session
keeps working in tmux and is adopted the next time medusa starts.

### First prompt

`pty.AgentOptions` gains `InitialPrompt string`. When it is set, it is
shell-quoted and appended as the positional prompt argument, for both
`claude` and `codex`. It is used **only on a fresh launch**: it is never
written to `TabInfo`, so restore, reattach and restart paths cannot replay it.
A prompt read from `--prompt-file` is passed the same way. A prompt longer
than 64 KiB is rejected, to stay well inside argument-length limits.

## Telling a running TUI

The CLI sends one line to the hooks socket (`<HooksDir>/medusa.sock`):

```json
{"event": "medusa:workspaces-changed"}
```

- If there is no socket, or nothing listens on it, the TUI is not running.
  The CLI skips the step silently and reports `"tui_notified": false`.
- `hooks.Server` currently drops lines with an empty `session`. This event is
  exempt.
- The `onEvent` closure in `app_hooks.go` maps it to `messages.RefreshDashboard{}`
  instead of `hookActivityEvent`.
- `RefreshDashboard` is added to `isCriticalExternalMsg`, so it cannot be
  dropped under load.

## Two writers: registry safety

Today `data.Registry` locks only within one process and writes with a plain
`os.WriteFile`. With a CLI and a TUI both writing, one change can be lost, and
a reader can see a half-written file and show an empty dashboard.

- **Cross-process lock.** Every read-modify-write in `Registry` takes an
  exclusive `flock` on `workspaces.json.lock`, in addition to the existing
  mutex. Plain reads take a shared lock.
- **Atomic write.** `saveLocked` writes to a temp file in the same directory
  and renames it.
- **The recents store** gets the same two changes, since `registerWorkspace`
  writes it too.
- `WorkspaceStore.Save` already writes temp-plus-rename. The CLI only writes
  metadata for a workspace it just created or is deleting, so it does not
  race the TUI's debounced persist except in the delete case below.

## Delete while the TUI has the workspace open

Killing a tmux session that the TUI is attached to is not safe on its own. The
TUI sees its PTY stop and auto-reattaches or auto-restarts the tab, and its
`new-session -A` would then start a **new** agent in a worktree that is being
deleted. The TUI's own delete avoids this by dropping the tabs first
(`handleDeleteWorkspace`). The CLI must get the TUI to do the same before it
kills anything:

1. The CLI sends `{"event": "medusa:workspace-closing", "workspace": "<id>"}`
   over the hooks socket.
2. The TUI handles it as the first half of its own delete. It calls
   `center.CleanupWorkspace`, goes home if that workspace was active, drops it
   from the persist queue and marks the dashboard row as deleting.
3. The CLI waits until no tmux client is attached to any of the workspace's
   sessions, polling `list-clients` for up to 5 seconds. If clients remain
   after that, it carries on with a warning on stderr.
4. The CLI kills the sessions and runs the shared delete.
5. The CLI sends `workspaces-changed`, and the TUI reloads without the
   workspace.

Two backstops cover a TUI that missed step 1:
- On `WorkspacesLoaded`, the TUI compares the new list with `a.allWorkspaces`.
  For any live workspace that has disappeared, and whose *name* is not in the
  new list either (so an ID migration is not mistaken for a delete), it does
  the cleanup from step 2.
- Persistence refuses to save a workspace whose ID is not in the registry, in
  case a persist is already in flight when the reload lands. If the registry
  cannot be read, it saves anyway, so a read error never loses tab state.

## Degradations

- **Codex has no resumable ID without the TUI.** Codex learns its session ID
  from the SessionStart hook. If the TUI is not running when Codex starts,
  that event reaches nobody, and the tab cannot `codex resume` after a
  restart. It still runs and is still adopted.
- **The setup port is not reserved.** `PortAllocator` lives in memory, so the
  port a CLI-run setup script sees may differ from the one the TUI later hands
  that workspace's `run` scripts. This matches what already happens across a
  TUI restart.
- **No creation spinner.** The TUI shows the workspace once the CLI notifies
  it, not while it is being created.

## Out of scope

- `run` scripts from the CLI.
- A first prompt in the TUI's New Tab dialog.
- Rename, archive, or adding tabs to an existing workspace from the CLI.
- Saving settings from CLI flags.

## Testing

- `workspaceops`: create/delete against temp git repos (worktree and checkout).
  Rollback on save failure. `ResolveLaunch` for each row of the defaults table,
  including a profile with no saved assistant.
- Registry: two processes adding entries concurrently lose nothing (a
  test-binary helper process). A reader never sees a partial file.
- `pty.PrepareAgent`: `InitialPrompt` is quoted and present on a fresh launch,
  and absent on resume and restart.
- Hooks server: the `workspaces-changed` event is accepted with no session and
  becomes `RefreshDashboard`.
- App: a workspace that disappears on reload loses its tabs and is never
  persisted again.
- e2e (`internal/e2e`): `medusa workspace new --prompt …` with no TUI running
  creates a tagged tmux session. A TUI then started adopts it as a normal
  tab. `delete` kills the sessions and removes the worktree but never the
  source repo.
- CLI: flag conflicts exit 2. `delete` with no TTY and no `--yes` exits 2.
  `--json` output parses.
