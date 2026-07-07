# Claude Fullscreen TUI as Default — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Launch every new/relaunched Claude agent in fullscreen TUI mode and forward the mouse to Claude, keeping medusa's own scroll/selection only as a fallback for pre-existing classic sessions.

**Architecture:** A single per-launch `Fullscreen` flag drives three consistent effects — the `CLAUDE_CODE_NO_FLICKER=1` env var (enable), the tmux `mouse on` + `@medusa_fullscreen` session tag (mark), and the in-memory `Tab.Fullscreen` used for mouse routing. The flag is forced `true` on unambiguously-fresh Claude launches (new tab, restart, auto-restart) and honored-from-persisted on restore/reattach, so a surviving classic session keeps medusa scrolling until it is explicitly restarted. When a tab is fullscreen, medusa SGR-encodes mouse events and writes them to the PTY instead of scrolling its own vterm.

**Tech Stack:** Go, Bubble Tea v2 (`charm.land/bubbletea/v2`), tmux, medusa's `vterm`/`pty` packages.

## Global Constraints

- Go. After each task: `make fmt` (gofmt + goimports); `golangci-lint run ./<touched-package>/...` exits 0; touched-package tests pass.
- No `.go` file may exceed **500 lines** (`make lint` enforces). Split by concern into sibling files.
- **End of development:** run `make lint` (race + golangci-lint + 500-line). The race detector is required.
- Commits are conventional-commit-lite: `feat:` / `fix:` / `refactor:` surface in release notes; `docs:` / `test:` / `chore:` do not.
- Fullscreen requires **Claude Code v2.1.89+**; older versions are an accepted, documented limitation (no kill switch).
- Enable and mark must always agree per launch — never emit the env var without the tmux mark, or vice versa.

## Design refinement vs. spec

The spec (§1) said the env var is applied *unconditionally* for Claude. This plan gates it on the per-launch `Fullscreen` flag instead, because a restored/reattached classic session must NOT receive the env var (else it would go fullscreen while medusa still scrolls its vterm — the original bug). The flag is `true` for every *fresh* Claude launch, so the user-visible behavior ("fullscreen is the default") is identical. Update the spec's §1 wording as part of Task 8.

## File Structure

- `internal/pty/agent.go` — add `AgentOptions.Fullscreen`; extract pure `buildAgentCommand`; gate env var; set `tags.Fullscreen`.
- `internal/pty/agent_command_test.go` *(new)* — unit tests for `buildAgentCommand`.
- `internal/tmux/tmux.go` — `SessionTags.Fullscreen`; per-session `mouse on`; `@medusa_fullscreen` tag.
- `internal/tmux/tmux_test.go` — extend tag/mouse assertions.
- `internal/data/workspace.go` — `TabInfo.Fullscreen`.
- `internal/data/workspace_tabinfo_test.go` *(new)* — JSON round-trip.
- `internal/ui/center/model.go` — `Tab.Fullscreen`.
- `internal/ui/center/model_tabs.go` — `ptyTabCreateResult.Fullscreen`; `createAgentTabWithSession` gains a `fullscreen bool` param; set `Tab.Fullscreen` in `handlePtyTabCreated`.
- `internal/ui/center/model_tabs_nav.go` — persist `Fullscreen` in both `GetTabsInfo*`.
- `internal/ui/center/model_tabs_session.go` — `addPlaceholderTab` reads `info.Fullscreen`; reattach passes `tab.Fullscreen`; restore call sites pass persisted value.
- `internal/ui/center/model_tabs_restart.go`, `model_input_lifecycle.go` — restart/auto-restart set `Fullscreen: assistant==claude`.
- `internal/ui/center/model_input_mouse.go` — thin forward guards in the 4 handlers.
- `internal/ui/center/model_input_mouse_forward.go` *(new)* — `activeTabForwardsMouse`, SGR encoder, `forwardMouse`.
- `internal/ui/center/mouse_forward_test.go` *(new)* — encoder + routing tests.
- `internal/app/app_tmux_discover.go` — read `@medusa_fullscreen` tag into discovered `TabInfo`.
- `CLAUDE.md` — version prereq + fullscreen-default note.

---

### Task 1: Extract pure `buildAgentCommand` (refactor, no behavior change)

**Files:**
- Modify: `internal/pty/agent.go` (command assembly inside `CreateAgentWithTags`, ~lines 108–176)
- Test: `internal/pty/agent_command_test.go` (create)

**Interfaces:**
- Produces: `func buildAgentCommand(agentType AgentType, command, sessionName, profileDir string, opts AgentOptions) string` — pure assembly of the env-prefixed shell command with Claude flags. No I/O, no side effects.

- [ ] **Step 1: Write the failing test**

Create `internal/pty/agent_command_test.go`:

```go
package pty

import (
	"strings"
	"testing"
)

func TestBuildAgentCommandClaudeNewSession(t *testing.T) {
	got := buildAgentCommand(AgentClaude, "claude", "medusa-ws-1", "/cfg/profile", AgentOptions{
		ClaudeSessionID: "sess-123",
		PermissionMode:  "auto",
	})
	for _, want := range []string{
		"CLAUDE_CONFIG_DIR='/cfg/profile'",
		"MEDUSA_SESSION_NAME='medusa-ws-1'",
		"claude",
		"--session-id 'sess-123'",
		"--permission-mode 'auto'",
		"--enable-auto-mode",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("command missing %q\ngot: %s", want, got)
		}
	}
	if strings.Contains(got, "--resume") {
		t.Errorf("new session must not use --resume: %s", got)
	}
	if strings.Contains(got, "--settings") {
		t.Errorf("non-isolated must not pass --settings: %s", got)
	}
}

func TestBuildAgentCommandClaudeResumeAndIsolated(t *testing.T) {
	got := buildAgentCommand(AgentClaude, "claude", "s", "/cfg", AgentOptions{
		ClaudeSessionID:          "sess-9",
		Resume:                   true,
		Isolated:                 true,
		AllowUnsandboxedCommands: true,
	})
	if !strings.Contains(got, "--resume 'sess-9'") {
		t.Errorf("expected --resume: %s", got)
	}
	if strings.Contains(got, "--session-id") {
		t.Errorf("resume must not use --session-id: %s", got)
	}
	if !strings.Contains(got, "--settings ") {
		t.Errorf("isolated must pass --settings: %s", got)
	}
}

func TestBuildAgentCommandNonClaudeHasNoClaudeFlags(t *testing.T) {
	got := buildAgentCommand(AgentType("viewer"), "run.sh", "s", "", AgentOptions{
		ClaudeSessionID: "x",
		PermissionMode:  "auto",
	})
	for _, unwanted := range []string{"--session-id", "--resume", "--permission-mode", "--enable-auto-mode", "CLAUDE_CONFIG_DIR"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("non-claude must not contain %q: %s", unwanted, got)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pty/ -run TestBuildAgentCommand -v`
Expected: FAIL — `undefined: buildAgentCommand`.

- [ ] **Step 3: Add the pure helper**

In `internal/pty/agent.go`, add (near the other package functions):

```go
// buildAgentCommand assembles the env-prefixed shell command for an agent.
// It is pure: all filesystem side effects (profile dir setup, settings/hook
// injection) are performed by the caller, which passes the resolved profileDir.
func buildAgentCommand(agentType AgentType, command, sessionName, profileDir string, opts AgentOptions) string {
	cmd := fmt.Sprintf("MEDUSA_SESSION_NAME=%s %s", shellutil.Quote(sessionName), command)
	if agentType == AgentClaude && profileDir != "" {
		cmd = fmt.Sprintf("CLAUDE_CONFIG_DIR=%s %s", shellutil.Quote(profileDir), cmd)
	}
	if agentType == AgentClaude && opts.ClaudeSessionID != "" {
		if opts.Resume {
			cmd += " --resume " + shellutil.Quote(opts.ClaudeSessionID)
		} else {
			cmd += " --session-id " + shellutil.Quote(opts.ClaudeSessionID)
		}
	}
	if agentType == AgentClaude && opts.PermissionMode != "" {
		cmd += " --permission-mode " + shellutil.Quote(opts.PermissionMode)
	}
	if agentType == AgentClaude {
		cmd += " --enable-auto-mode"
	}
	if opts.Isolated && agentType == AgentClaude {
		cmd += " --settings " + shellutil.Quote(config.ClaudeSandboxSettingsJSON(opts.AllowUnsandboxedCommands))
	}
	return cmd
}
```

- [ ] **Step 4: Rewire `CreateAgentWithTags` to use it**

In `internal/pty/agent.go`, delete the inline command assembly currently at lines ~108, ~136, ~146–176 (the `agentCommand := ...`, the `CLAUDE_CONFIG_DIR` prepend, and the `--resume`/`--session-id`/`--permission-mode`/`--enable-auto-mode`/`--settings` appends). Keep every side-effecting block (profile mkdir/sync/inject, `InjectTrustedDirectory`, and the `bypassPermissions` → `InjectSkipPermissionPrompt` block). Replace the removed assembly with a single call placed AFTER all the side effects have run and `profileDir` is known:

```go
	// bypassPermissions still triggers Claude's confirmation dialog the first
	// time; suppress it for users who explicitly chose it from our launcher.
	if agentType == AgentClaude && opts.PermissionMode == "bypassPermissions" {
		_ = config.InjectSkipPermissionPrompt(profileDir)
	}

	agentCommand := buildAgentCommand(agentType, assistantCfg.Command, sessionName, profileDir, opts)
```

Confirm the existing `fullCommand := fmt.Sprintf("%s; stty sane; ...", agentCommand, shell)` line still wraps `agentCommand` unchanged.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/pty/ -run TestBuildAgentCommand -v && go test ./internal/pty/ -v`
Expected: PASS (new tests pass; no existing pty test regresses).

- [ ] **Step 6: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/pty/...
git add internal/pty/agent.go internal/pty/agent_command_test.go
git commit -m "refactor: extract pure buildAgentCommand from CreateAgentWithTags"
```

---

### Task 2: Enable fullscreen via `CLAUDE_CODE_NO_FLICKER=1`

**Files:**
- Modify: `internal/pty/agent.go` (`AgentOptions` struct + `buildAgentCommand`)
- Test: `internal/pty/agent_command_test.go`

**Interfaces:**
- Consumes: `buildAgentCommand` (Task 1).
- Produces: `AgentOptions.Fullscreen bool` — when true on a Claude launch, prefixes `CLAUDE_CODE_NO_FLICKER=1`.

- [ ] **Step 1: Write the failing test**

Append to `internal/pty/agent_command_test.go`:

```go
func TestBuildAgentCommandFullscreenEnvVar(t *testing.T) {
	on := buildAgentCommand(AgentClaude, "claude", "s", "/cfg", AgentOptions{ClaudeSessionID: "id", Fullscreen: true})
	if !strings.Contains(on, "CLAUDE_CODE_NO_FLICKER=1") {
		t.Errorf("fullscreen launch must set CLAUDE_CODE_NO_FLICKER=1: %s", on)
	}
	resume := buildAgentCommand(AgentClaude, "claude", "s", "/cfg", AgentOptions{ClaudeSessionID: "id", Resume: true, Fullscreen: true})
	if !strings.Contains(resume, "CLAUDE_CODE_NO_FLICKER=1") || !strings.Contains(resume, "--resume 'id'") {
		t.Errorf("fullscreen must apply on resume too: %s", resume)
	}
	off := buildAgentCommand(AgentClaude, "claude", "s", "/cfg", AgentOptions{ClaudeSessionID: "id", Fullscreen: false})
	if strings.Contains(off, "CLAUDE_CODE_NO_FLICKER") {
		t.Errorf("non-fullscreen launch must not set the env var: %s", off)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pty/ -run TestBuildAgentCommandFullscreen -v`
Expected: FAIL — `unknown field 'Fullscreen' in struct literal`.

- [ ] **Step 3: Add the field and the prefix**

In `internal/pty/agent.go`, add to `AgentOptions`:

```go
	// Fullscreen launches Claude in its fullscreen TUI renderer via
	// CLAUDE_CODE_NO_FLICKER=1 and marks the tmux session accordingly.
	Fullscreen bool
```

In `buildAgentCommand`, immediately before `return cmd`, add:

```go
	if agentType == AgentClaude && opts.Fullscreen {
		cmd = "CLAUDE_CODE_NO_FLICKER=1 " + cmd
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pty/ -run TestBuildAgentCommand -v`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/pty/...
git add internal/pty/agent.go internal/pty/agent_command_test.go
git commit -m "feat: enable Claude fullscreen renderer via CLAUDE_CODE_NO_FLICKER"
```

---

### Task 3: tmux per-session mouse-on + `@medusa_fullscreen` tag

**Files:**
- Modify: `internal/tmux/tmux.go` (`SessionTags`, `clientCommand`, `appendSessionTags`)
- Modify: `internal/pty/agent.go` (`CreateAgentWithTags`: set `tags.Fullscreen`)
- Test: `internal/tmux/tmux_test.go`

**Interfaces:**
- Consumes: `AgentOptions.Fullscreen` (Task 2).
- Produces: `SessionTags.Fullscreen bool` → drives `set-option mouse on` and `set-option @medusa_fullscreen 1` for that session.

- [ ] **Step 1: Write the failing test**

Append to `internal/tmux/tmux_test.go`:

```go
func TestClientCommandFullscreenSession(t *testing.T) {
	opts := Options{ServerName: "s", ConfigPath: "/dev/null", HideStatus: true, DisableMouse: true, DefaultTerminal: "xterm-256color"}
	fs := ClientCommandWithTags("sess", "/tmp", "claude", opts, SessionTags{WorkspaceID: "ws", TabID: "t", Type: "agent", Assistant: "claude", Fullscreen: true})
	if !strings.Contains(fs, "mouse on") {
		t.Errorf("fullscreen session must enable mouse: %s", fs)
	}
	if strings.Contains(fs, "mouse off") {
		t.Errorf("fullscreen session must not disable mouse: %s", fs)
	}
	if !strings.Contains(fs, "@medusa_fullscreen 1") {
		t.Errorf("fullscreen session must set @medusa_fullscreen tag: %s", fs)
	}

	classic := ClientCommandWithTags("sess", "/tmp", "claude", opts, SessionTags{WorkspaceID: "ws", TabID: "t", Type: "agent", Assistant: "claude", Fullscreen: false})
	if !strings.Contains(classic, "mouse off") {
		t.Errorf("classic session must disable mouse: %s", classic)
	}
	if strings.Contains(classic, "@medusa_fullscreen") {
		t.Errorf("classic session must not set fullscreen tag: %s", classic)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tmux/ -run TestClientCommandFullscreen -v`
Expected: FAIL — `unknown field 'Fullscreen'`.

- [ ] **Step 3: Add the field**

In `internal/tmux/tmux.go`, add to `SessionTags`:

```go
	Fullscreen bool // Claude fullscreen renderer: enables tmux mouse + @medusa_fullscreen tag.
```

- [ ] **Step 4: Emit mouse-on and the tag**

In `clientCommand`, replace the current mouse block:

```go
	if opts.DisableMouse {
		fmt.Fprintf(&settings, "%s set-option -t %s mouse off 2>/dev/null; ", base, session)
	}
```

with:

```go
	if tags.Fullscreen {
		// Fullscreen Claude owns the mouse; tmux must forward mouse events to it.
		fmt.Fprintf(&settings, "%s set-option -t %s mouse on 2>/dev/null; ", base, session)
	} else if opts.DisableMouse {
		fmt.Fprintf(&settings, "%s set-option -t %s mouse off 2>/dev/null; ", base, session)
	}
```

In `appendSessionTags`, add before the closing brace (and include `tags.Fullscreen` in the early-return guard's OR chain so a tags value with only `Fullscreen` set still emits):

```go
	if tags.Fullscreen {
		fmt.Fprintf(settings, "%s set-option -t %s @medusa_fullscreen 1 2>/dev/null; ", base, session)
	}
```

Update the guard at the top of `appendSessionTags` to:

```go
	if tags.WorkspaceID == "" && tags.TabID == "" && tags.Type == "" && tags.Assistant == "" && tags.CreatedAt == 0 && !tags.Fullscreen {
		return
	}
```

- [ ] **Step 5: Set `tags.Fullscreen` in `CreateAgentWithTags`**

In `internal/pty/agent.go`, immediately before the `termCommand := tmux.ClientCommandWithTags(...)` line, add:

```go
	// Fullscreen mark and the CLAUDE_CODE_NO_FLICKER env var must agree.
	tags.Fullscreen = opts.Fullscreen
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tmux/ -run TestClientCommand -v && go test ./internal/tmux/ ./internal/pty/ -v`
Expected: PASS.

- [ ] **Step 7: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/tmux/... ./internal/pty/...
git add internal/tmux/tmux.go internal/tmux/tmux_test.go internal/pty/agent.go
git commit -m "feat: enable tmux mouse and fullscreen tag for fullscreen sessions"
```

---

### Task 4: Persist and thread `Fullscreen` through tabs

**Files:**
- Modify: `internal/data/workspace.go` (`TabInfo`)
- Test: `internal/data/workspace_tabinfo_test.go` (create)
- Modify: `internal/ui/center/model.go` (`Tab`), `model_tabs.go` (`ptyTabCreateResult`, `createAgentTab*`, `handlePtyTabCreated`), `model_tabs_nav.go` (persist), `model_tabs_session.go` (placeholder + reattach + restore), `model_tabs_restart.go`, `model_input_lifecycle.go`

**Interfaces:**
- Consumes: `AgentOptions.Fullscreen` (Task 2).
- Produces: `TabInfo.Fullscreen bool`, `Tab.Fullscreen bool`, `ptyTabCreateResult.Fullscreen bool`, and `createAgentTabWithSession(..., fullscreen bool)` — all carrying the same per-launch decision. `Tab.Fullscreen` is the value the mouse router reads (Task 6).

- [ ] **Step 1: Write the failing test (persistence round-trip)**

Create `internal/data/workspace_tabinfo_test.go`:

```go
package data

import (
	"encoding/json"
	"testing"
)

func TestTabInfoFullscreenRoundTrip(t *testing.T) {
	in := TabInfo{Assistant: "claude", Name: "claude", Fullscreen: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out TabInfo
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Fullscreen {
		t.Errorf("Fullscreen did not round-trip: %s", b)
	}

	// Legacy JSON without the field defaults to false (fallback).
	var legacy TabInfo
	if err := json.Unmarshal([]byte(`{"assistant":"claude","name":"claude"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Fullscreen {
		t.Errorf("legacy tab must default to non-fullscreen")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/data/ -run TestTabInfoFullscreen -v`
Expected: FAIL — `unknown field 'Fullscreen'`.

- [ ] **Step 3: Add `TabInfo.Fullscreen`**

In `internal/data/workspace.go`, add to `TabInfo` (after `PermissionMode`):

```go
	Fullscreen bool `json:"fullscreen,omitempty"` // Claude launched in fullscreen TUI mode.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/data/ -run TestTabInfoFullscreen -v`
Expected: PASS.

- [ ] **Step 5: Add the in-memory + result fields**

In `internal/ui/center/model.go`, add to `Tab` (after `PermissionMode`):

```go
	Fullscreen bool // Claude fullscreen TUI mode: mouse is forwarded to Claude.
```

In `internal/ui/center/model_tabs.go`, add to `ptyTabCreateResult` (after `PermissionMode`):

```go
	Fullscreen bool
```

- [ ] **Step 6: Thread `fullscreen` through `createAgentTabWithSession`**

In `internal/ui/center/model_tabs.go`:

Change the signature to add a trailing `fullscreen bool`:

```go
func (m *Model) createAgentTabWithSession(assistant string, ws *data.Workspace, sessionName string, displayName string, activate bool, claudeSessionID string, isolated, allowUnsandboxed bool, permissionMode string, fullscreen bool) tea.Cmd {
```

In `createAgentTab`, pass fullscreen = Claude:

```go
func (m *Model) createAgentTab(assistant string, ws *data.Workspace, isolated, allowUnsandboxed bool, permissionMode string) tea.Cmd {
	fullscreen := appPty.AgentType(assistant) == appPty.AgentClaude
	return m.createAgentTabWithSession(assistant, ws, "", "", true, "", isolated, allowUnsandboxed, permissionMode, fullscreen)
}
```

Inside `createAgentTabWithSession`, set the option and the result field. In the `agentOpts := appPty.AgentOptions{...}` literal add `Fullscreen: fullscreen,`. In the returned `ptyTabCreateResult{...}` add `Fullscreen: fullscreen,`.

- [ ] **Step 7: Set `Tab.Fullscreen` on creation**

In `internal/ui/center/model_tabs.go`, in `handlePtyTabCreated`, add to the `tab := &Tab{...}` literal (after `PermissionMode`):

```go
		Fullscreen:               msg.Fullscreen,
```

- [ ] **Step 8: Persist `Fullscreen`**

In `internal/ui/center/model_tabs_nav.go`, in BOTH `data.TabInfo{...}` literals (~lines 160 and 202), read the field under the existing `tab.mu.Lock()` block (add `fullscreen := tab.Fullscreen` next to `permMode := tab.PermissionMode`) and add to each literal:

```go
			Fullscreen: fullscreen,
```

- [ ] **Step 9: Restore/reattach paths carry the persisted value**

In `internal/ui/center/model_tabs_session.go`:

- `addPlaceholderTab`: add to the `tab := &Tab{...}` literal:
  ```go
  		Fullscreen:               info.Fullscreen,
  ```
- Restore call site (~line 292): append `, tab.Fullscreen` to the `createAgentTabWithSession(...)` call.
- `AddTabsFromWorkspace` call site (~line 355): append `, tab.Fullscreen`.
- Reattach (~line 156): change `appPty.AgentOptions{}` to `appPty.AgentOptions{Fullscreen: tab.Fullscreen}` (read `tab.Fullscreen` under the existing lock alongside `sessionName`/`claudeSessionID`).

- [ ] **Step 10: Restart / auto-restart force fullscreen for Claude**

In `internal/ui/center/model_tabs_restart.go` (~line 122) and `internal/ui/center/model_input_lifecycle.go` (~line 238), add to each `appPty.AgentOptions{...}` literal:

```go
				Fullscreen: appPty.AgentType(assistant) == appPty.AgentClaude,
```

- [ ] **Step 11: Build the whole tree**

Run: `go build ./... && go test ./internal/data/ ./internal/ui/center/ -v`
Expected: builds clean; existing center tests still pass.

- [ ] **Step 12: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/data/... ./internal/ui/center/...
git add internal/data/workspace.go internal/data/workspace_tabinfo_test.go internal/ui/center/
git commit -m "feat: persist and thread per-tab fullscreen flag through agent tabs"
```

---

### Task 5: SGR mouse encoder (pure)

**Files:**
- Create: `internal/ui/center/model_input_mouse_forward.go`
- Test: `internal/ui/center/mouse_forward_test.go` (create)

**Interfaces:**
- Produces:
  - `func sgrMouseButton(b tea.MouseButton, motion bool) (code int, ok bool)` — maps a Bubble Tea button to an SGR button code; `ok` is false for buttons that should not be forwarded.
  - `func encodeSGRMouse(code, col1, row1 int, release bool) []byte` — builds `ESC [ < code ; col ; row (M|m)` with 1-based coordinates.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/center/mouse_forward_test.go`:

```go
package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEncodeSGRMouse(t *testing.T) {
	if got := string(encodeSGRMouse(0, 10, 5, false)); got != "\x1b[<0;10;5M" {
		t.Errorf("press encoding wrong: %q", got)
	}
	if got := string(encodeSGRMouse(0, 10, 5, true)); got != "\x1b[<0;10;5m" {
		t.Errorf("release encoding wrong: %q", got)
	}
	if got := string(encodeSGRMouse(64, 1, 1, false)); got != "\x1b[<64;1;1M" {
		t.Errorf("wheel-up encoding wrong: %q", got)
	}
}

func TestSGRMouseButton(t *testing.T) {
	cases := []struct {
		b      tea.MouseButton
		motion bool
		want   int
		ok     bool
	}{
		{tea.MouseLeft, false, 0, true},
		{tea.MouseMiddle, false, 1, true},
		{tea.MouseRight, false, 2, true},
		{tea.MouseWheelUp, false, 64, true},
		{tea.MouseWheelDown, false, 65, true},
		{tea.MouseLeft, true, 32, true}, // drag: motion bit (+32)
		{tea.MouseNone, false, 0, false},
	}
	for _, c := range cases {
		got, ok := sgrMouseButton(c.b, c.motion)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("button %v motion=%v => (%d,%v), want (%d,%v)", c.b, c.motion, got, ok, c.want, c.ok)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/center/ -run 'TestEncodeSGRMouse|TestSGRMouseButton' -v`
Expected: FAIL — undefined `encodeSGRMouse` / `sgrMouseButton`.

- [ ] **Step 3: Implement the encoder**

Create `internal/ui/center/model_input_mouse_forward.go`:

```go
package center

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// sgrMouseButton maps a Bubble Tea mouse button to an SGR (mode 1006) button
// code. motion adds the 32 "button-event" bit for drag reporting. ok is false
// for buttons we never forward (e.g. MouseNone).
func sgrMouseButton(b tea.MouseButton, motion bool) (int, bool) {
	var code int
	switch b {
	case tea.MouseLeft:
		code = 0
	case tea.MouseMiddle:
		code = 1
	case tea.MouseRight:
		code = 2
	case tea.MouseWheelUp:
		code = 64
	case tea.MouseWheelDown:
		code = 65
	default:
		return 0, false
	}
	if motion {
		code += 32
	}
	return code, true
}

// encodeSGRMouse builds an SGR mouse report. col1/row1 are 1-based. A release
// uses the final 'm'; press/motion/wheel use 'M'.
func encodeSGRMouse(code, col1, row1 int, release bool) []byte {
	final := byte('M')
	if release {
		final = 'm'
	}
	return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", code, col1, row1, final))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/center/ -run 'TestEncodeSGRMouse|TestSGRMouseButton' -v`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/ui/center/...
git add internal/ui/center/model_input_mouse_forward.go internal/ui/center/mouse_forward_test.go
git commit -m "feat: add SGR mouse encoding for fullscreen forwarding"
```

---

### Task 6: Route mouse to the PTY for fullscreen tabs

**Files:**
- Modify: `internal/ui/center/model_input_mouse_forward.go` (add `activeTabForwardsMouse`, `forwardMouse`)
- Modify: `internal/ui/center/model_input_mouse.go` (guards in the 4 handlers)
- Test: `internal/ui/center/mouse_forward_test.go`

**Interfaces:**
- Consumes: `Tab.Fullscreen` (Task 4), `sgrMouseButton`/`encodeSGRMouse` (Task 5), existing `m.screenToTerminal`, `m.focused`, `m.hasActiveAgent`, `m.getTabs`, `m.getActiveTabIdx`, `tab.Agent.Terminal.Write`.
- Produces:
  - `func (m *Model) activeTabForwardsMouse() (*Tab, bool)` — returns the active tab and true when mouse should be forwarded (focused, active live-agent view, not info tab, not diff viewer, `tab.Fullscreen`).
  - `func (m *Model) forwardMouse(tab *Tab, code, screenX, screenY int, release bool)` — translates to pane coords and writes the encoded event to the PTY; no-op if out of bounds or the agent terminal is unavailable.

- [ ] **Step 1: Write the failing behavioral test**

Append to `internal/ui/center/mouse_forward_test.go` (mirror the Model/Tab construction used in `selection_test.go`):

```go
func TestFullscreenTabDoesNotScrollVterm(t *testing.T) {
	m := newTestModelWithAgentTab(t) // helper below
	tab := m.getTabs()[m.getActiveTabIdx()]
	// Give the vterm scrollback so ScrollView would visibly move it.
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = true
	before := tab.Terminal.ViewOffset

	m.updateMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})

	if tab.Terminal.ViewOffset != before {
		t.Errorf("fullscreen tab must not scroll medusa vterm (offset %d -> %d)", before, tab.Terminal.ViewOffset)
	}
}

func TestClassicTabScrollsVterm(t *testing.T) {
	m := newTestModelWithAgentTab(t)
	tab := m.getTabs()[m.getActiveTabIdx()]
	for i := 0; i < 50; i++ {
		tab.Terminal.Write([]byte("line\r\n"))
	}
	tab.Fullscreen = false
	before := tab.Terminal.ViewOffset

	m.updateMouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})

	if tab.Terminal.ViewOffset == before {
		t.Errorf("classic tab must scroll medusa vterm (offset unchanged at %d)", before)
	}
}
```

If `selection_test.go` has no shared constructor, add a small helper `newTestModelWithAgentTab(t)` in `mouse_forward_test.go` that builds a `Model` with one focused agent tab whose `Terminal = vterm.New(80,24)`, `Agent = nil`, `Running = true`, `focused = true`, and the workspace/active-index maps set — copy the construction pattern from `TestSelectionLifecycle` in `selection_test.go`. Setting `Agent = nil` makes `forwardMouse` a safe no-op while still exercising the routing branch (the assertion is only about whether the vterm scrolled).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/center/ -run 'TestFullscreenTabDoesNotScroll|TestClassicTabScrolls' -v`
Expected: FAIL — `undefined: m.activeTabForwardsMouse` (once guards are referenced) or the fullscreen case scrolls (before guards exist).

- [ ] **Step 3: Add routing helpers**

Append to `internal/ui/center/model_input_mouse_forward.go`:

```go
// activeTabForwardsMouse reports whether mouse input for the active tab should
// be forwarded to Claude instead of handled by medusa. True only for a focused,
// live, fullscreen agent terminal view — never the info tab or diff viewer.
func (m *Model) activeTabForwardsMouse() (*Tab, bool) {
	if !m.focused || !m.hasActiveAgent() || m.infoTabActive {
		return nil, false
	}
	tabs := m.getTabs()
	idx := m.getActiveTabIdx()
	if idx < 0 || idx >= len(tabs) {
		return nil, false
	}
	tab := tabs[idx]
	if tab == nil || !tab.Fullscreen {
		return nil, false
	}
	if m.getDiffViewer(tab) != nil {
		return nil, false // diff viewer is medusa-rendered
	}
	return tab, true
}

// forwardMouse writes an SGR mouse report to the tab's PTY. It is a no-op when
// the event is outside the terminal content region or the agent terminal is
// unavailable (e.g. during tests or a dead session).
func (m *Model) forwardMouse(tab *Tab, code, screenX, screenY int, release bool) {
	termX, termY, inBounds := m.screenToTerminal(screenX, screenY)
	if !inBounds {
		return
	}
	if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
		return
	}
	seq := encodeSGRMouse(code, termX+1, termY+1, release)
	if _, err := tab.Agent.Terminal.Write(seq); err != nil {
		logging.Warn("mouse forward write failed for tab %s: %v", tab.ID, err)
	}
}
```

Add `"github.com/Skowt/medusa/internal/logging"` to the file's imports.

- [ ] **Step 4: Add guards to the four handlers**

In `internal/ui/center/model_input_mouse.go`:

- `updateMouseWheel`: after the `dispatchDiffInput` block and before the `delta` computation, insert:
  ```go
  	if tab2, ok := m.activeTabForwardsMouse(); ok {
  		code, cok := sgrMouseButton(msg.Button, false)
  		if cok {
  			m.forwardMouse(tab2, code, msg.X, msg.Y, false)
  		}
  		return m, nil
  	}
  ```
  (`tea.MouseWheelMsg` is `type MouseWheelMsg Mouse`, so `msg.X`/`msg.Y`/`msg.Button` are available — same as `MouseClickMsg`. Confirmed against the vendored bubbletea v2.)

- `updateMouseClick`: after the chrome handlers (tab-bar, action-bar, info-content) and the `dispatchDiffInput` block, before the double-click/selection logic, insert:
  ```go
  	if tab2, ok := m.activeTabForwardsMouse(); ok {
  		if code, cok := sgrMouseButton(msg.Button, false); cok {
  			m.forwardMouse(tab2, code, msg.X, msg.Y, false)
  		}
  		return m, nil
  	}
  ```

- `updateMouseMotion`: after `dispatchDiffInput`, before the selection-drag logic, insert (motion=true for the drag bit):
  ```go
  	if tab2, ok := m.activeTabForwardsMouse(); ok {
  		if code, cok := sgrMouseButton(msg.Button, true); cok {
  			m.forwardMouse(tab2, code, msg.X, msg.Y, false)
  		}
  		return m, nil
  	}
  ```

- `updateMouseRelease`: after `dispatchDiffInput`, before the copy-selection logic, insert (release=true):
  ```go
  	if tab2, ok := m.activeTabForwardsMouse(); ok {
  		if code, cok := sgrMouseButton(msg.Button, false); cok {
  			m.forwardMouse(tab2, code, msg.X, msg.Y, true)
  		}
  		return m, nil
  	}
  ```

Note: keep the existing early `!m.focused || !m.hasActiveAgent()` guards; `activeTabForwardsMouse` re-checks them so the ordering is safe.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/center/ -run 'TestFullscreenTabDoesNotScroll|TestClassicTabScrolls|TestSelection' -v`
Expected: PASS (fullscreen forwards / does not scroll; classic scrolls; existing selection tests still pass).

- [ ] **Step 6: Check the 500-line limit**

Run: `wc -l internal/ui/center/model_input_mouse.go`
Expected: ≤ 500. If over, move the `handleInfoContentClick`/`handleActionBarClickFromMsg` helpers into a sibling file (e.g. `model_input_mouse_click.go`) — do NOT inflate the file.

- [ ] **Step 7: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/ui/center/...
git add internal/ui/center/model_input_mouse.go internal/ui/center/model_input_mouse_forward.go internal/ui/center/mouse_forward_test.go
git commit -m "feat: forward mouse events to Claude in fullscreen tabs"
```

---

### Task 7: Read the fullscreen tag for discovered tmux sessions

**Files:**
- Modify: `internal/app/app_tmux_discover.go`

**Interfaces:**
- Consumes: `tmux.SessionsWithTags` (existing), `@medusa_fullscreen` tag (Task 3).
- Produces: discovered `TabInfo.Fullscreen` set from the live session tag, so adopted sessions route the mouse correctly.

- [ ] **Step 1: Request the tag**

In `discoverWorkspaceTabsFromTmux`, change the `SessionsWithTags` keys slice from:

```go
	rows, err := tmux.SessionsWithTags(match, []string{"@medusa_assistant"}, opts)
```

to:

```go
	rows, err := tmux.SessionsWithTags(match, []string{"@medusa_assistant", "@medusa_fullscreen"}, opts)
```

- [ ] **Step 2: Populate the field**

In the row loop, when building `data.TabInfo{...}`, add:

```go
			Fullscreen:  strings.TrimSpace(row.Tags["@medusa_fullscreen"]) == "1",
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./internal/app/ -v`
Expected: builds clean; app tests pass. (Discovery hits a live tmux server, so the fullscreen tag path is verified manually in Task 9's `make run`; this step only confirms compilation and no regression.)

- [ ] **Step 4: Format, lint, commit**

```bash
make fmt
golangci-lint run ./internal/app/...
git add internal/app/app_tmux_discover.go
git commit -m "feat: carry fullscreen mode for adopted tmux sessions"
```

---

### Task 8: Document the version prerequisite and default

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/superpowers/specs/2026-07-07-tui-fullscreen-default-design.md` (§1 wording)

- [ ] **Step 1: Add a note to `CLAUDE.md`**

Under the "Architecture" section (near the "Center pane" description), add a short subsection:

```markdown
### Fullscreen TUI default

New and relaunched Claude agents run in Claude's fullscreen renderer
(`CLAUDE_CODE_NO_FLICKER=1`), and their tmux session is set to `mouse on` so
mouse events reach Claude. medusa forwards wheel/click/drag/release to the PTY
for those tabs instead of scrolling its own vterm; medusa's vterm scroll/select
remains only for pre-existing classic sessions until they are restarted.
Requires **Claude Code v2.1.89+** — older versions ignore the env var and mouse
behavior degrades (there is no kill switch).
```

- [ ] **Step 2: Reconcile the spec**

In the design doc, change §1's "unconditionally prefixes" wording to note the env var is gated on the per-launch `Fullscreen` flag (true for every fresh Claude launch), matching this plan's "Design refinement vs. spec" section.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-07-07-tui-fullscreen-default-design.md
git commit -m "docs: document fullscreen TUI default and v2.1.89 prerequisite"
```

---

### Task 9: Full gate + manual verification

**Files:** none (verification only).

- [ ] **Step 1: Run the full CI-mirroring gate**

Run: `make lint`
Expected: `go test -race -v ./...` passes, `golangci-lint run` exits 0, and the 500-line check passes. Fix any failure before proceeding — do not batch fixes.

- [ ] **Step 2: Render harness sanity**

Run: `make release-check`
Expected: tests + all three harness modes (monitor/center/sidebar) complete without error.

- [ ] **Step 3: Manual smoke test (`make run`)**

1. Start medusa (`make run`) and open a new Claude agent tab.
2. Confirm Claude launches in fullscreen (fixed input box, alt-screen look).
3. Scroll the mouse wheel up over the conversation — Claude's own viewport scrolls (not medusa's). Click to expand a tool call; drag to select text (Claude copies it).
4. If you have a pre-existing classic session from before the change, reattach it and confirm medusa's own scrollback still scrolls (fallback intact) until you restart it, after which it becomes fullscreen.

- [ ] **Step 4: Final commit (if any fixes were needed)**

```bash
git add -A
git commit -m "fix: address lint/test findings for fullscreen TUI default"
```
(Skip if steps 1–3 required no changes.)

---

## Self-Review

- **Spec coverage:** §Enable → Tasks 1–2; §Mark/tmux mouse → Task 3; §persist/thread → Task 4; §mouse forwarding → Tasks 5–6; §discovery/adopted sessions → Task 7; §version prereq + native-selection docs → Task 8; §testing → per-task tests + Task 9. §"remove medusa scrolling = forward, don't delete" is honored (Task 6 keeps the classic path).
- **Type consistency:** `Fullscreen bool` is the name across `AgentOptions`, `SessionTags`, `TabInfo`, `Tab`, `ptyTabCreateResult`; `createAgentTabWithSession(..., fullscreen bool)` trailing param used consistently at all call sites; encoder/router names (`sgrMouseButton`, `encodeSGRMouse`, `activeTabForwardsMouse`, `forwardMouse`) match between Tasks 5, 6, and their tests.
- **Placeholder scan:** none — every code step shows full code. One implementation-time note remains (Task 6 Step 1: reuse `selection_test.go`'s Model constructor pattern for the behavioral test helper); the `tea.MouseWheelMsg` coordinate question is resolved (it embeds `Mouse`, so `X`/`Y`/`Button` are present).
