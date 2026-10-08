package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/hooks"
	"github.com/Skowt/medusa/internal/process"
	"github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/validation"
	"github.com/Skowt/medusa/internal/workspaceops"
)

// newFlags are `medusa workspace new`'s flags. Every agent flag left unset
// falls back to the settings the TUI saved; none of them is saved back.
type newFlags struct {
	repo, base, group, profile       string
	fromCurrent, noWorktree, copyIgn bool
	agent, prompt, promptFile        string
	noAgent, codexAuto, noFullscreen bool
	permissionMode, sandbox          string
	fullscreen                       optionalBool
	asJSON                           bool
}

func workspaceNewCommand() *command {
	return &command{
		name:    "new",
		summary: "Create a workspace and start an agent in it",
		usage:   "medusa workspace new <name> [flags]",
		long: "Creates a git worktree named <name> (and a branch of the same name) from\n" +
			"the repo you are in, then starts an agent in it. Agent settings you leave\n" +
			"out come from what the TUI last used: the assistant last used in the\n" +
			"profile, and the last permission mode, fullscreen and sandbox choices.\n" +
			"Flags never change those saved settings.",
		examples: []string{
			"medusa workspace new fix-login --prompt \"Fix the login redirect loop\"",
			"medusa ws new spike --profile Work --agent codex --sandbox read-only",
			"medusa ws new quick --no-worktree --no-agent",
			"cat task.md | medusa ws new task-42 --prompt-file - --json",
		},
		bind: bindNewFlags,
		flagGroups: []flagGroup{
			{"Where it is created", []string{"repo", "base", "from-current", "no-worktree", "copy-ignored", "group", "profile"}},
			{"Agent", []string{"agent", "no-agent", "prompt", "prompt-file", "permission-mode", "fullscreen", "no-fullscreen", "sandbox", "codex-auto"}},
			{"Output", []string{"json"}},
		},
	}
}

func bindNewFlags(fs *flag.FlagSet) func(c *cliContext, args []string) error {
	f := &newFlags{}
	fs.StringVar(&f.repo, "repo", "", "`path` of the git repo to branch from (default: the repo you are in)")
	fs.StringVar(&f.base, "base", "", "`branch` to cut the worktree from (default: the remote's main branch)")
	fs.BoolVar(&f.fromCurrent, "from-current", false, "cut the worktree from the branch the repo has checked out")
	fs.BoolVar(&f.noWorktree, "no-worktree", false, "use the repo's own checkout instead of creating a worktree")
	fs.BoolVar(&f.copyIgn, "copy-ignored", false, "copy gitignored files (.env and the like) into the worktree")
	fs.StringVar(&f.group, "group", "", "dashboard group `name` to put the workspace in")
	fs.StringVar(&f.profile, "profile", "", "medusa profile `name` (default: the profile last picked in the TUI)")
	fs.StringVar(&f.agent, "agent", "", "the `assistant` to start: claude or codex (default: the last one used in the profile)")
	fs.BoolVar(&f.noAgent, "no-agent", false, "create the workspace without starting an agent")
	fs.StringVar(&f.prompt, "prompt", "", "first `text` to send the agent")
	fs.StringVar(&f.promptFile, "prompt-file", "", "read the first prompt from `path` (\"-\" reads stdin)")
	fs.StringVar(&f.permissionMode, "permission-mode", "", "Claude only: starting `mode` (default, acceptEdits, plan, auto, bypassPermissions)")
	fs.Var(&f.fullscreen, "fullscreen", "Claude only: use Claude's fullscreen TUI")
	fs.BoolVar(&f.noFullscreen, "no-fullscreen", false, "Claude only: do not use Claude's fullscreen TUI")
	fs.StringVar(&f.sandbox, "sandbox", "", "Codex only: sandbox `policy` (read-only, workspace-write, danger-full-access)")
	fs.BoolVar(&f.codexAuto, "codex-auto", false, "Codex only: let Codex's reviewer approve requests (--approve-for-me)")
	fs.BoolVar(&f.asJSON, "json", false, "print the result as JSON")
	return func(c *cliContext, args []string) error {
		return runWorkspaceNew(c, f, args)
	}
}

// maxPromptBytes bounds a first prompt, which travels as a command-line
// argument and must stay well inside the system's argument-length limit.
const maxPromptBytes = 64 * 1024

var (
	permissionModes = map[string]bool{"default": true, "acceptEdits": true, "plan": true, "auto": true, "bypassPermissions": true}
	codexSandboxes  = map[string]bool{pty.CodexSandboxReadOnly: true, pty.CodexSandboxWorkspace: true, pty.CodexSandboxFullAccess: true}
)

type newResult struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Root         string       `json:"root"`
	Branch       string       `json:"branch"`
	Base         string       `json:"base"`
	Worktree     bool         `json:"worktree"`
	Profile      string       `json:"profile"`
	Group        string       `json:"group"`
	Agent        *agentResult `json:"agent"`
	SetupWarning string       `json:"setup_warning"`
	TUINotified  bool         `json:"tui_notified"`
}

type agentResult struct {
	Assistant   string `json:"assistant"`
	TmuxSession string `json:"tmux_session"`
	SessionID   string `json:"session_id"`
}

func runWorkspaceNew(c *cliContext, f *newFlags, args []string) error {
	if len(args) != 1 {
		return usagef("expected exactly one workspace name, got %d arguments", len(args))
	}
	name := args[0]
	if err := validation.ValidateWorkspaceName(name); err != nil {
		return usagef("%v", err)
	}
	if err := f.checkConflicts(); err != nil {
		return err
	}
	prompt, err := f.readPrompt(c.stdin)
	if err != nil {
		return err
	}

	env, err := loadWorkspaceEnv()
	if err != nil {
		return err
	}
	ui := env.cfg.UI
	profile := firstNonEmpty(f.profile, ui.LastProfile, defaultProfile)
	worktree := ui.LastCreateWorktree
	if f.noWorktree {
		worktree = false
	}
	if f.base != "" || f.fromCurrent {
		worktree = true
	}
	startAgent := ui.AutoStartAgent
	switch {
	case f.noAgent:
		startAgent = false
	case f.agent != "" || prompt != "":
		startAgent = true
	}
	launch := workspaceops.ResolveLaunch(ui, profile, workspaceops.LaunchOverrides{
		Assistant:      f.agent,
		PermissionMode: f.permissionMode,
		Fullscreen:     f.fullscreenOverride(),
		CodexSandbox:   f.sandbox,
		CodexAuto:      boolOverride(f.codexAuto),
	})
	if startAgent {
		if err := f.checkAssistantFlags(launch.Assistant); err != nil {
			return err
		}
	}

	if err := checkProfile(env.cfg, profile); err != nil {
		return err
	}
	repo, err := resolveRepo(f.repo)
	if err != nil {
		return err
	}
	if exists, err := env.deps.NameExists(name); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("workspace '%s' already exists", name)
	}

	progress := c.stderr
	repos := []data.RepoRef{repo}
	bases, err := resolveBases(progress, repo, f, worktree)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(progress, "Creating workspace %s from %s\n", name, repo.Path)
	ws, err := env.deps.Create(workspaceops.CreateRequest{
		Name: name, Repos: repos, Bases: bases, Profile: profile, Group: f.group,
		CopyIgnored: f.copyIgn && worktree, Worktree: worktree,
	})
	if err != nil {
		return err
	}
	if ws.CopyIgnored {
		workspaceops.CopyIgnored(ws, repos)
	}

	res := newResult{
		ID: string(ws.ID()), Name: ws.Name, Root: ws.Root(), Branch: ws.Branch(),
		Base: bases[0], Worktree: ws.UsesWorktree(), Profile: profile, Group: ws.Group,
	}
	if ws.UsesWorktree() {
		_, _ = fmt.Fprintln(progress, "Running setup scripts")
		scripts := process.NewScriptRunner(env.cfg.PortStart, env.cfg.PortRangeSize)
		if err := scripts.RunSetup(ws); err != nil {
			res.SetupWarning = err.Error()
			_, _ = fmt.Fprintln(progress, "Warning:", err)
		}
	}

	if startAgent {
		started, err := env.deps.StartAgent(ws, launch, prompt)
		if err != nil {
			// The workspace exists and is usable; say so rather than leaving
			// the caller to guess whether to retry the whole create.
			hooks.NotifyWorkspacesChanged(env.cfg.Paths.HooksDir)
			return fmt.Errorf("workspace %s was created at %s, but the agent did not start: %w", ws.Name, ws.Root(), err)
		}
		res.Agent = &agentResult{Assistant: started.Assistant, TmuxSession: started.SessionName, SessionID: started.SessionID}
	}

	res.TUINotified = hooks.NotifyWorkspacesChanged(env.cfg.Paths.HooksDir)
	if f.asJSON {
		return printJSON(c.stdout, res)
	}
	_, _ = fmt.Fprintf(c.stdout, "Created %s at %s\n", res.Name, res.Root)
	if res.Agent != nil {
		_, _ = fmt.Fprintf(c.stdout, "Started %s in tmux session %s\n", res.Agent.Assistant, res.Agent.TmuxSession)
	}
	return nil
}

// resolveBases picks the base each repo's worktree is cut from, or, for a
// checkout workspace, records the branch the repo is on (pulling it first).
func resolveBases(progress io.Writer, repo data.RepoRef, f *newFlags, worktree bool) ([]string, error) {
	repos := []data.RepoRef{repo}
	switch {
	case !worktree:
		branch, notice := workspaceops.CheckoutBranch(repo.Path)
		if notice != "" {
			_, _ = fmt.Fprintln(progress, notice)
		}
		return []string{branch}, nil
	case f.fromCurrent:
		return workspaceops.CheckedOutBases(repos)
	case f.base != "":
		bases, fallback, err := workspaceops.CustomBases(repos, f.base)
		if len(fallback) > 0 {
			_, _ = fmt.Fprintf(progress, "Branch %q not found in %s; using the default branch\n", f.base, strings.Join(fallback, ", "))
		}
		return bases, err
	default:
		_, _ = fmt.Fprintln(progress, "Fetching the default branch")
		return workspaceops.RemoteBases(repos)
	}
}

func (f *newFlags) checkConflicts() error {
	switch {
	case f.base != "" && f.fromCurrent:
		return usagef("--base and --from-current both pick the base; use one")
	case f.noWorktree && (f.base != "" || f.fromCurrent):
		return usagef("--no-worktree uses the repo as it stands, so it takes no base")
	case f.noWorktree && f.copyIgn:
		return usagef("--copy-ignored needs a worktree to copy into")
	case f.prompt != "" && f.promptFile != "":
		return usagef("--prompt and --prompt-file both set the prompt; use one")
	case f.fullscreen.set && f.noFullscreen:
		return usagef("--fullscreen and --no-fullscreen contradict each other")
	case f.agent != "" && f.agent != workspaceops.AssistantClaude && f.agent != workspaceops.AssistantCodex:
		return usagef("--agent must be claude or codex, not %q", f.agent)
	case f.permissionMode != "" && !permissionModes[f.permissionMode]:
		return usagef("unknown --permission-mode %q", f.permissionMode)
	case f.sandbox != "" && !codexSandboxes[f.sandbox]:
		return usagef("unknown --sandbox %q", f.sandbox)
	}
	if f.noAgent && (f.agent != "" || f.prompt != "" || f.promptFile != "" || f.permissionMode != "" ||
		f.fullscreen.set || f.noFullscreen || f.sandbox != "" || f.codexAuto) {
		return usagef("--no-agent cannot be combined with agent flags")
	}
	return nil
}

// checkAssistantFlags refuses flags that belong to the other assistant. It
// runs after the assistant is resolved, so "--sandbox" on a profile whose
// saved assistant is Claude is caught too.
func (f *newFlags) checkAssistantFlags(assistant string) error {
	if assistant == workspaceops.AssistantCodex && (f.permissionMode != "" || f.fullscreen.set || f.noFullscreen) {
		return usagef("--permission-mode and --fullscreen are Claude flags, and the agent is codex")
	}
	if assistant == workspaceops.AssistantClaude && (f.sandbox != "" || f.codexAuto) {
		return usagef("--sandbox and --codex-auto are Codex flags, and the agent is claude")
	}
	return nil
}

func (f *newFlags) readPrompt(stdin io.Reader) (string, error) {
	prompt := f.prompt
	if f.promptFile != "" {
		r := stdin
		if f.promptFile != "-" {
			file, err := os.Open(f.promptFile)
			if err != nil {
				return "", err
			}
			defer func() { _ = file.Close() }()
			r = file
		}
		raw, err := io.ReadAll(io.LimitReader(r, maxPromptBytes+1))
		if err != nil {
			return "", err
		}
		prompt = string(raw)
	}
	if len(prompt) > maxPromptBytes {
		return "", usagef("the prompt is over %d KiB; put the detail in a file in the repo and point the prompt at it", maxPromptBytes/1024)
	}
	return prompt, nil
}

func (f *newFlags) fullscreenOverride() *bool {
	if f.noFullscreen {
		v := false
		return &v
	}
	return f.fullscreen.ptr()
}

func boolOverride(set bool) *bool {
	if !set {
		return nil
	}
	return &set
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
