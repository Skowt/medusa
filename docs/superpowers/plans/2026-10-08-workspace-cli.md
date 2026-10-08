# Workspace CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `medusa workspace new|list|delete` creates, lists and deletes workspaces headlessly, starts an agent with an optional first prompt, and makes a running TUI show the change.

**Architecture:** Creation/deletion logic moves from `internal/app` into a new `internal/workspaceops` package that both the TUI and the CLI call. The CLI starts agents in detached tmux sessions recorded in `OpenTabs`, which the TUI adopts on reload. A running TUI is told to reload (and, before a delete, to drop tabs) over the existing hooks Unix socket. The registry and recents files gain a cross-process `flock` and atomic writes.

**Tech Stack:** Go, std `flag`, `syscall.Flock`, tmux, Bubble Tea v2.

**Spec:** `docs/superpowers/specs/2026-10-08-workspace-cli-design.md`

## Global Constraints

- No `.go` file over 500 lines (`make lint` enforces it).
- `make fmt`, `golangci-lint run` on touched packages, tests pass; `make lint` at the end.
- `internal/workspaceops` must not import `internal/app` or `internal/ui`.
- `CLAUDE_CODE_NO_FLICKER` is always set explicitly (`=1` / `=0`): unchanged, `buildAgentCommand` keeps doing it.
- CLI flags never write `config.json`.
- Exit codes: 0 success, 1 failure, 2 usage. Errors/progress on stderr, results on stdout.
- Builds only for darwin/linux (`.goreleaser.yml`), so `syscall.Flock` needs no build tags.
- Commit prefixes per `.claude/skills/medusa-commits-and-releases`: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`.

## Review Focus

1. **Flags after the positional name** (`medusa workspace new test --profile Work`): std `flag` stops at the first non-flag, so without interspersed parsing every flag is silently ignored. Test in Task 6.
2. **A prompt starting with `-`** or containing quotes/newlines: must reach the agent verbatim, not be parsed as a flag. Test in Task 3 (`--` separator + shell quoting).
3. **Profile typo** (`--profile wrok`): must fail listing the real profiles, not silently create a new empty profile. Test in Task 7.
4. **Delete while the TUI is attached**: sessions must not be respawned by auto-reattach. Covered by the closing handshake (Task 5) and its test.
5. **Name collision with an archived workspace or a leftover branch**: must fail before anything is created, with a clear message. Test in Task 7.

---

### Task 1: Cross-process registry and recents safety

**Files:**
- Create: `internal/data/filelock.go`, `internal/data/filelock_test.go`
- Modify: `internal/data/registry.go`, `internal/data/recents.go`

**Interfaces:**
- Produces: `withFileLock(path string, exclusive bool, fn func() error) error`, `writeFileAtomic(path string, data []byte, perm os.FileMode) error`, `(*Registry).Has(id string) (bool, error)`.

- [ ] **Step 1: Failing tests** — `TestRegistryConcurrentWritersLoseNothing`: 8 goroutines, each with its *own* `NewRegistry(samePath)` (separate mutexes, so only the flock protects them — flock on darwin/linux is per open file description, so this models two processes), each adding 20 distinct IDs; assert 160 entries. `TestRegistryReaderNeverSeesPartialFile`: writer loop + reader loop for 300ms, reader's `ListWorkspaces` must never return an error. `TestRegistryHas`. Same concurrency test for `RecentsStore` is not needed (capped at 5 entries); instead `TestRecentsWriteIsAtomic` asserts no `.tmp` leftovers and valid JSON after concurrent `Add`.
- [ ] **Step 2: Run, verify fail** — `go test ./internal/data -run 'Registry|Recents' -race`.
- [ ] **Step 3: Implement**

```go
// filelock.go
func withFileLock(path string, exclusive bool, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	// write, Chmod(perm), Close, Rename; remove tmp on any failure
}
```

`Registry.ListWorkspaces` wraps `loadLocked` in `withFileLock(r.path, false, …)`; `modifyWorkspaces` wraps load+fn+save in `withFileLock(r.path, true, …)`; `saveLocked` uses `writeFileAtomic`. `RecentsStore.Add` wraps its read-modify-write in an exclusive lock, `List` in a shared lock (split an unlocked `list()` so `Add` does not deadlock), `save` uses `writeFileAtomic`.

- [ ] **Step 4: Run, verify pass** (with `-race`).
- [ ] **Step 5: Commit** — `fix: lock the registry across processes and write it atomically`

### Task 2: `internal/workspaceops` — shared create, delete and launch resolution

**Files:**
- Create: `internal/workspaceops/doc.go`, `base.go`, `create.go`, `delete.go`, `launch.go`, and tests `create_test.go`, `delete_test.go`, `launch_test.go`
- Move (git mv + package rename): `internal/app/copy_ignored_files.go` → `internal/workspaceops/copy_ignored.go` (and its test if present)
- Modify: `internal/app/app_base_branch.go`, `app_operations.go`, `assistants.go`, `permission_modes.go`, `app_dialog_new_tab.go`, plus call sites of moved helpers

**Interfaces:**
- Produces:
```go
type Deps struct {
	Config   *config.Config
	Registry *data.Registry
	Store    *data.WorkspaceStore
	Recents  *data.RecentsStore
}
// base.go
func RemoteBases(repos []data.RepoRef) ([]string, error)
func CheckedOutBases(repos []data.RepoRef) ([]string, error)
func CustomBases(repos []data.RepoRef, branch string) (bases, fallbackRepos []string, err error)
func CheckoutBranch(repoPath string) (branch, pullNotice string) // pulls first (PullBeforeOpen)
func PullBeforeOpen(repoPath string) string
// create.go
type CreateRequest struct {
	Name        string
	Repos       []data.RepoRef
	Bases       []string
	Profile     string
	Group       string
	CopyIgnored bool
	Worktree    bool
}
func (d Deps) NameExists(name string) (bool, error)          // registry names
func (d Deps) Create(req CreateRequest) (*data.Workspace, error) // validates, makes worktree, saves, registers; ws may be non-nil on error
func CopyIgnored(ws *data.Workspace, repos []data.RepoRef)
// delete.go
func RemovableWorktrees(ws *data.Workspace) []git.RepoSpec
func (d Deps) Delete(ws *data.Workspace) (branchWarning string)
// launch.go
const AssistantClaude, AssistantCodex = "claude", "codex"
type Launch struct {
	Assistant                string
	Isolated                 bool
	AllowUnsandboxedCommands bool
	PermissionMode           string
	Fullscreen               bool
	CodexSandbox             string
	CodexAuto                bool
}
type LaunchOverrides struct {
	Assistant      string // "" = saved
	PermissionMode string
	Fullscreen     *bool
	CodexSandbox   string
	CodexAuto      *bool
}
func DefaultAssistant(last string) string
func DefaultCodexSandbox(last string) string
func DefaultCodexStartingMode(last string) string
func DefaultPermissionMode(last string) string
func ResolveLaunch(ui config.UISettings, profile string, o LaunchOverrides) Launch
```

- [ ] **Step 1: Failing tests**
  - `launch_test.go`: table over the spec's defaults rows — profile with saved assistant codex → codex with `LastCodexSandbox`/`LastCodexStartingMode`; profile with no entry falls back to `LastAssistant`; Claude picks up `LastPermissionMode`/`LastFullscreen`/`LastIsolated`/`LastAllowUnsandboxedCommands`; empty `LastPermissionMode` → `"auto"`; overrides win; Claude result has empty Codex fields and vice versa.
  - `create_test.go`: temp git repo (init, commit) + temp `WorkspacesRoot`/metadata/registry. Worktree create → dir exists, branch exists, registry has entry with profile, store loads it. Checkout create (`Worktree:false`) → root is repo, nothing created. Duplicate name → error, nothing changed. Existing branch → error. `NameExists` true after create.
  - `delete_test.go`: delete of worktree workspace removes dir, branch, store dir, registry entry; delete of checkout workspace leaves the repo intact (file still there, `.git` still a dir).
- [ ] **Step 2: Run, verify fail** — `go test ./internal/workspaceops/...`
- [ ] **Step 3: Implement** — move bodies verbatim from `app_base_branch.go` (`fetch*` loops), `createWorkspace`/`registerWorkspace`/`waitForGitFile`, `deleteWorkspace` (minus `goHome`), `removableWorktrees`, `joinStrings` use → `strings.Join`, `lastUsedLaunch` + `stickyAssistant` logic, the four `default*` helpers. `Create` first runs `validation.ValidateWorkspaceName`, `NameExists`. In `internal/app`, replace bodies with thin wrappers that call `workspaceops` and map results to the same messages (`WorkspaceFetchDone`, `WorkspaceCreateFailed`, `WorkspaceWorktreeDone`, `WorkspaceCreated`, `WorkspaceDeleted`). `a.workspaceNameExists` stays as the TUI's extra in-memory check (covers placeholders). `lastUsedLaunch(ws, assistant)` becomes `ResolveLaunch(a.config.UI, ws.Profile, LaunchOverrides{Assistant: assistant})` converted to `messages.LaunchAgent`.
- [ ] **Step 4: Run** `go test ./internal/workspaceops/... ./internal/app/... -race` — new and existing tests pass (existing app tests for creation/deletion/checkout/orphan are the regression net).
- [ ] **Step 5: Commit** — `refactor: move workspace create and delete into workspaceops`

### Task 3: Headless agent preparation, first prompt, detached tmux sessions

**Files:**
- Create: `internal/pty/agent_prepare.go`, `internal/pty/agent_prompt_test.go`, `internal/tmux/detached.go`, `internal/tmux/detached_test.go`
- Modify: `internal/pty/agent.go` (`AgentOptions.InitialPrompt`, `buildAgentCommand`, `CreateAgentWithTags` uses `PrepareAgent`), `internal/pty/agent_codex.go`, `internal/tmux/client_command.go` (split session settings out)

**Interfaces:**
- Produces:
```go
// pty
type PreparedAgent struct {
	SessionName string
	Command     string   // full wrapped command (agent; stty sane; … exec $SHELL -l)
	Env         []string // WORKSPACE_ROOT, WORKSPACE_NAME, LINES=, COLUMNS=, COLORTERM
	Fullscreen  bool     // Claude && opts.Fullscreen
	Config      config.AssistantConfig
}
func PrepareAgent(cfg *config.Config, ws *data.Workspace, agentType AgentType, sessionName string, opts AgentOptions) (PreparedAgent, error)
// AgentOptions gains: InitialPrompt string // fresh launch only; never persisted
// tmux
func StartDetachedSession(sessionName, workDir, command string, env []string, opts Options, tags SessionTags) error
func SessionClientCount(sessionName string, opts Options) (int, error)
```

- [ ] **Step 1: Failing tests**
  - `agent_prompt_test.go`: Claude fresh with `InitialPrompt: "-x it's \"q\"\nline2"` → command ends with `" -- " + shellutil.Quote(prompt)`; Claude with `Resume: true` → no prompt; Codex fresh → ends with `-- <quoted>`; Codex resume → no prompt; empty prompt → no `--`. Round-trip: run `sh -c 'printf %s "$@"' x <args after -->` is overkill — instead assert via `shellutil.Quote` equality.
  - `detached_test.go` (skip if `tmux` missing; uses a private `-L medusa-test-<pid>` server, killed in cleanup): `StartDetachedSession` with tags → `SessionTagValue(@medusa_workspace)` matches, `@medusa_fullscreen` set when Fullscreen, session runs `sleep 30`; second call with the same name fails; `SessionClientCount` = 0.
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement**
  - `buildAgentCommand`: after all Claude flags, `if opts.InitialPrompt != "" && !opts.Resume { cmd += " -- " + shellutil.Quote(opts.InitialPrompt) }`. Same at the end of `buildCodexCommand` (after policy args; `codexSessionArgs` sets resume, so gate on `!opts.Resume`).
  - `PrepareAgent`: lift lines from `CreateAgentWithTags` (profile check, assistant config lookup, `NextUniqueSessionName` if empty, `EnsureAvailable`, env, profileDir/`prepareCodexHome`/`InjectTrustedDirectory`, `opts.Cwd`, `buildAgentCommand`, `fullCommand`). `CreateAgentWithTags` becomes `p, err := PrepareAgent(...)`; `tags.Fullscreen = p.Fullscreen`; `ClientCommandWithTags(p.SessionName, ws.Root(), p.Command, …)`; `NewWithSize(termCommand, ws.Root(), p.Env, rows, cols)`.
  - `client_command.go`: extract `sessionSettings(base, session string, opts Options, tags SessionTags) string` (everything between create and attach). `clientCommand` = create && settings + attach (unchanged output — existing tests guard it). `detached.go`: `create := base + " new-session -ds <s> -c <dir> sh -lc <cmd>"`; run `sh -c create + " && " + settings` with `cmd.Env = append(os.Environ(), env...)`, 10s timeout, include output in the error. `SessionClientCount`: `tmux list-clients -t =<s> -F x`, count lines; a missing session returns 0, nil.
- [ ] **Step 4: Run** `go test ./internal/pty/... ./internal/tmux/... -race`.
- [ ] **Step 5: Commit** — `feat: start agents in detached tmux sessions with a first prompt`

### Task 4: Headless agent start in workspaceops

**Files:**
- Create: `internal/workspaceops/agent.go`, `internal/workspaceops/agent_test.go`

**Interfaces:**
- Consumes: `pty.PrepareAgent`, `tmux.StartDetachedSession`, `Launch`.
- Produces:
```go
type StartedAgent struct {
	Assistant   string
	SessionName string
	SessionID   string // Claude only
	Tab         data.TabInfo
}
func (d Deps) StartAgent(ws *data.Workspace, l Launch, prompt string) (StartedAgent, error)
```

- [ ] **Step 1: Failing test** (skip without tmux; private tmux server via `MEDUSA_TMUX_SERVER`; a config whose `Assistants["claude"].Command` is `sleep 30` so nothing real launches): after `StartAgent`, session exists with `@medusa_workspace = ws.ID()`, `@medusa_type=agent`; `ws.OpenTabs` saved in the store with `Status "running"`, `Name "claude"`, `ClaudeSessionID` non-empty and equal to `SessionID`, `PermissionMode`/`Fullscreen` from the launch; prompt never appears in the stored JSON.
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement** — `config.InjectHooks(profileDir, HooksDir, ResolveHookEmitBinary())` for Claude (TUI does it for all profiles at start; a headless run may precede that); build `pty.AgentOptions` from `Launch` (Claude: `ClaudeSessionID = pty.GenerateSessionID()`); `PrepareAgent`; `StartDetachedSession` with `SessionTags{WorkspaceID, TabID: "cli-" + random, Type: "agent", Assistant, CreatedAt: now, Fullscreen: p.Fullscreen}`; append `TabInfo`; `d.Store.Save(ws)`. On save failure kill the session and return the error.
- [ ] **Step 4: Run, verify pass.**
- [ ] **Step 5: Commit** — `feat: start a workspace agent without the TUI`

### Task 5: Telling the running TUI

**Files:**
- Create: `internal/hooks/control.go`, `internal/hooks/control_test.go`, `internal/app/app_external_control.go`, `internal/app/app_external_control_test.go`
- Modify: `internal/hooks/server.go`, `internal/hooks/event.go`, `internal/app/app_hooks.go`, `internal/app/app_msgpump.go`, `internal/app/app_input_workspace_create.go`, `internal/app/app_persistence.go`, `internal/app/app_input_dispatch*.go` (route new msg)

**Interfaces:**
- Produces:
```go
// hooks
const EventWorkspacesChanged EventType = "medusa:workspaces-changed"
const EventWorkspaceClosing  EventType = "medusa:workspace-closing"
func IsControlEvent(e EventType) bool
func NotifyWorkspacesChanged(hooksDir string) bool             // false = no TUI listening
func NotifyWorkspaceClosing(hooksDir, workspaceID string) bool
// HookEvent gains: WorkspaceID string (json "workspace")
// app
type workspaceClosingMsg struct{ WorkspaceID string }
```

- [ ] **Step 1: Failing tests**
  - `control_test.go`: start `NewServer` on a temp socket; `NotifyWorkspacesChanged` → callback gets `EventWorkspacesChanged` with empty session; `NotifyWorkspaceClosing(dir,"abc")` → `WorkspaceID "abc"`; no socket → returns false quickly (<1s); a non-control event with empty session is still dropped.
  - `app_external_control_test.go`: App test fixture (see existing `app_operations_test.go` helpers) with a loaded workspace W that has center tabs; `handleWorkspaceClosing(W.ID)` → `center` has no tabs for W, W not in `dirtyWorkspaces`, `activeWorkspace` nil if it was W. `handleWorkspacesLoaded` with W missing (and no workspace named W.Name) → same cleanup; with W missing but a workspace of the same name under a new ID → no cleanup. Persist: `persistSnapshotAllowed(registry, id)` false when the registry lacks the id, true when the registry file is unreadable. `isCriticalExternalMsg(messages.RefreshDashboard{})` true.
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement**
  - `server.go`: parse `Workspace string json:"workspace"`; `if raw.Event == "" || (raw.Session == "" && !IsControlEvent(EventType(raw.Event))) { return }`.
  - `control.go`: `notify(hooksDir, payload)` = `net.DialTimeout("unix", SocketPath(dir), 500ms)`, write JSON + `\n`, close; true on success.
  - `app_hooks.go` `onEvent`: `switch he.Event { case hooks.EventWorkspacesChanged: a.enqueueExternalMsg(messages.RefreshDashboard{}); return; case hooks.EventWorkspaceClosing: a.enqueueExternalMsg(workspaceClosingMsg{he.WorkspaceID}); return }`.
  - `isCriticalExternalMsg`: add `messages.RefreshDashboard` and `workspaceClosingMsg`.
  - `app_external_control.go`: `handleWorkspaceClosing(id)` → find ws; `a.dropWorkspaceTabs(ws)` = `center.CleanupWorkspace(ws)`, `goHome()` if active, `delete(a.dirtyWorkspaces, id)`, `dashboard.SetWorkspaceDeleting(id, true)`. `dropVanishedWorkspaces(old, new)` applies `dropWorkspaceTabs` to each non-orphan old ws whose ID and name are both absent from new; called at the top of `handleWorkspacesLoaded` before `a.allWorkspaces` is replaced.
  - `app_persistence.go`: inside the save cmd, `if !persistSnapshotAllowed(a.registry, snap.ID()) { continue }` where it calls `registry.Has` and returns true on error.
  - Route `workspaceClosingMsg` in the dispatch layer that handles `hookActivityEvent`.
- [ ] **Step 4: Run** `go test ./internal/hooks/... ./internal/app/... -race`.
- [ ] **Step 5: Commit** — `feat: let a running medusa follow workspace changes from outside`

### Task 6: CLI command tree and help

**Files:**
- Create: `cmd/medusa/cli.go` (command tree, dispatch, help), `cmd/medusa/cli_flags.go` (interspersed parsing, optional bool, usage error), `cmd/medusa/cli_test.go`
- Modify: `cmd/medusa/main.go` (dispatch), `cmd/medusa/skills.go` (expose flagset for help)

**Interfaces:**
- Produces:
```go
type command struct {
	name     string
	aliases  []string
	summary  string
	usage    string            // "medusa workspace new <name> [flags]"
	long     string            // description + examples
	flags    func(*flag.FlagSet) // registers flags (for help output)
	run      func(ctx *cliContext, args []string) error
	children []*command
}
type cliContext struct{ stdout, stderr io.Writer; stdin io.Reader; stdinIsTTY bool }
type usageError struct{ msg string }
func runCLI(args []string, ctx *cliContext) (handled bool, code int)
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error)
type optionalBool struct{ set, value bool } // flag.Value + IsBoolFlag
```

- [ ] **Step 1: Failing tests** (`cli_test.go`, using a test-only tree plus the real root):
  - `runCLI(nil)` → handled false (TUI starts).
  - `help`, `?`, `-h`, `--help` → code 0, stdout contains "Usage:" and "workspace".
  - `workspace`, `workspace help`, `help workspace`, `ws --help` → lists `new`, `list`, `delete`.
  - `workspace new --help`, `workspace new help`, `help workspace new`, `ws new -h` → contains `--prompt` and an example; code 0.
  - `bogus` → code 2, stderr contains `unknown command "bogus"` and the top-level usage. `workspace bogus` → code 2 with workspace help.
  - `parseInterspersed`: `["test","--profile","Work","--json"]` and `["--profile","Work","test","--json"]` → positional `["test"]`, flags set; `["--", "-weird"]` → positional `["-weird"]`.
  - `optionalBool`: `--fullscreen` → set/true; `--fullscreen=false` → set/false; absent → unset.
  - `--version` keeps working (handled in main before runCLI; covered by existing `main_test.go`).
- [ ] **Step 2: Run** `go test ./cmd/medusa -run CLI -v` — fail.
- [ ] **Step 3: Implement** — root command `medusa` with children `workspace` (alias `ws`; children `new`, `list`, `delete` from Task 7; Tasks 6 and 7 share one commit), `skills` (`run` delegates to `runSkillsCommand`; help prints its flagset via `skillsFlagSet()`), `help`. Dispatch walks args: at each level, if arg ∈ {help, ?, -h, --help} print help for the current node (or, for `help`, for the node named by the remaining args); a node with children and no matching child prints its help (code 0 when no arg, 2 with an unknown arg); a leaf runs with the remaining args, and its own `-h/--help/help` anywhere in them prints the leaf help. Help format:

```
<summary>

Usage:
  <usage>

Commands:            (nodes with children)
  new        Create a workspace and start an agent
  ...

Flags:               (leaves; fs.PrintDefaults with "--" style)
  --profile <name>   ...

Examples:            (from long)

Run 'medusa help <command>' for more about a command.
```

  `main.go`: after `--version`, `if handled, code := runCLI(os.Args[1:], defaultCLIContext()); handled { os.Exit(code) }`. Remove the direct skills dispatch.
- [ ] **Step 4: Run tests, verify pass.**
- [ ] **Step 5: Commit** (with Task 7) — see Task 7.

### Task 7: `workspace new`, `list`, `delete`

**Files:**
- Create: `cmd/medusa/workspace_common.go` (deps from config, repo resolution, profile validation, JSON/table printing, workspace lookup), `cmd/medusa/workspace_new.go`, `cmd/medusa/workspace_list.go`, `cmd/medusa/workspace_delete.go`, `cmd/medusa/workspace_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: JSON shapes from the spec (`newResult`, `listEntry`, `deleteResult` structs with the spec's json tags).

- [ ] **Step 1: Failing tests** (`MEDUSA_HOME` = temp dir, temp git repo with one commit and an `origin`-less setup, so `--base`/`--from-current` or `--no-worktree` are used; `--no-agent` everywhere except one tmux-gated test):
  - `new test --repo <r> --from-current --no-agent --json` → code 0, JSON parses, `worktree:true`, root exists, `tui_notified:false`.
  - `new test --profile Work` where profile dirs are only `Default` → code 1, stderr lists `Default`. `Default` passes even if its dir is missing.
  - Usage conflicts → code 2: `--base x --from-current`; `--no-worktree --base x`; `--prompt a --prompt-file f`; `--no-agent --prompt a`; `--agent codex --permission-mode plan`; `--agent claude --sandbox read-only`; `--prompt-file` > 64 KiB.
  - Duplicate name → code 1, "already exists". Leftover branch named `test2` → code 1 mentions the branch.
  - `list --json` after two creates → two entries with names, `live:0`; table output has a header row.
  - `delete test` with no TTY and no `--yes` → code 2, workspace still listed. `delete test --yes --json` → code 0, worktree dir gone, `list` no longer shows it, source repo intact. `delete nope --yes` → code 1.
  - Checkout workspace: `new co --repo <r> --no-worktree --no-agent`, then `delete co --yes` → repo dir and its `.git` still present.
  - tmux-gated: `new withagent --from-current --prompt "hi"` with config `assistants.claude.command = "sleep 30"` (written into temp `config.json`) and private tmux server → JSON `agent.tmux_session` exists in tmux with `@medusa_workspace` = id; `delete withagent --yes` kills it.
  - Stdin prompt: `--prompt-file -` reads from `ctx.stdin`.
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement**
  - `workspace_common.go`: `loadDeps()` → `config.DefaultConfig()`, `applyTmuxEnv(cfg)` (copy of `applyTmuxEnvFromConfig`'s non-force branch: set `MEDUSA_TMUX_SERVER`/`CONFIG` if non-empty and unset in env), `cfg.Paths.EnsureDirectories()`, build `workspaceops.Deps`. `resolveRepo(path)` = `git rev-parse --show-toplevel` → `data.RepoRef{Name: filepath.Base, Path}`. `validateProfile(cfg, name)`. `findWorkspace(deps, nameOrID)` → registry entry by ID then name → `Store.Load` + profile from entry. `printJSON(w, v)`.
  - `workspace_new.go`: flags per spec; `parseInterspersed`; exactly one positional; conflict checks (return `usageError`); resolve settings (profile → `LastProfile` → `Default`; worktree → `LastCreateWorktree`; agent on/off; `ResolveLaunch` then conflict check of assistant-specific flags against the resolved assistant); bases via `workspaceops.RemoteBases`/`CheckedOutBases`/`CustomBases` (print fallback warning) or `CheckoutBranch` for no-worktree (print pull notice); `deps.Create`; `CopyIgnored`; setup: `process.NewScriptRunner(cfg.PortStart, cfg.PortRangeSize).RunSetup(ws)` when `ws.UsesWorktree()`, error → `setup_warning`; `StartAgent` if on; `hooks.NotifyWorkspacesChanged`; print. Progress lines (`Fetching base…`, `Created …`) on stderr unless `--json`, where only warnings go to stderr.
  - `workspace_list.go`: registry entries → load each (mark `metadata missing` instead of failing); live count via `tmux.ListSessionsMatchingTags({"@medusa":"1","@medusa_workspace":id})` (0 on error); filters; `text/tabwriter` table or JSON.
  - `workspace_delete.go`: find; confirm (TTY prompt `Delete workspace "x" at <root>? [y/N]`, or require `--yes`); `if hooks.NotifyWorkspaceClosing(...) { waitForDetach(id, 5s) }` polling `SessionClientCount` for each session from `ListSessionsMatchingTags` every 100ms; `tmux.KillSessionsMatchingTags`; `deps.Delete(ws)`; `NotifyWorkspacesChanged`; print (branch warning to stderr and in JSON).
- [ ] **Step 4: Run** `go test ./cmd/medusa/... -race -v`.
- [ ] **Step 5: Commit** — `feat: add medusa workspace new, list and delete with help at every level`

### Task 8: README and CLAUDE.md

**Files:** Modify `README.md` (new "Command line" section after "Quick start"), `CLAUDE.md` (Entry points: list `medusa workspace`; new short section "Workspace CLI" with the load-bearing properties: shared `workspaceops`, flock registry, closing handshake before kill, prompt only on fresh launch, flags never save).

- [ ] **Step 1:** Write README section: commands, defaults table (short), `--json` + `jq` example, exit codes, `medusa help` / quoting `'?'`, live refresh note, limitations (Codex resume without TUI; `run` scripts not started).
- [ ] **Step 2:** Verify every example in the README by running it against a temp `MEDUSA_HOME`.
- [ ] **Step 3: Commit** — `docs: document the workspace command line`

### Task 9: Full verification

- [ ] `make fmt`, `golangci-lint run ./...`, `make lint` (race tests + lint + 500-line check).
- [ ] `make build`; smoke against a temp `MEDUSA_HOME` and a temp repo: `./medusa help`, `./medusa '?'`, `./medusa ws new -h`, create/list/delete with `--no-agent`.
- [ ] Live TUI check: run the built TUI in a detached tmux (private `MEDUSA_HOME`), run `medusa workspace new x --no-agent` from outside, capture the TUI pane and confirm `x` appears; delete it and confirm it disappears.
- [ ] Final whole-branch review by a fresh reviewer; fix findings.
