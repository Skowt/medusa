package process

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/safego"
	"github.com/Skowt/medusa/internal/shellutil"
)

// ScriptType identifies the type of script
type ScriptType string

const (
	ScriptSetup   ScriptType = "setup"
	ScriptRun     ScriptType = "run"
	ScriptArchive ScriptType = "archive"
)

const configFilename = "workspaces.json"

// RunCommand is a named command for a run script tab.
type RunCommand struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// WorkspaceConfig holds per-project workspace configuration
type WorkspaceConfig struct {
	SetupWorkspace []string     `json:"setup-workspace"`
	RunCommands    []RunCommand `json:"-"` // parsed from "run" (string or array)
	RunScript      string       `json:"-"` // kept for RunScript() backward compat
	ArchiveScript  string       `json:"archive"`
}

// ScriptRunner manages script execution for workspaces
type ScriptRunner struct {
	mu            sync.Mutex
	portAllocator *PortAllocator
	envBuilder    *EnvBuilder
	running       map[string]*exec.Cmd // workspace root -> running process
}

// NewScriptRunner creates a new script runner
func NewScriptRunner(portStart, portRange int) *ScriptRunner {
	ports := NewPortAllocator(portStart, portRange)
	return &ScriptRunner{
		portAllocator: ports,
		envBuilder:    NewEnvBuilder(ports),
		running:       make(map[string]*exec.Cmd),
	}
}

// LoadConfig loads the workspace configuration from the repo
func (r *ScriptRunner) LoadConfig(repoPath string) (*WorkspaceConfig, error) {
	configPath := filepath.Join(repoPath, ".medusa", configFilename)

	fileData, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return &WorkspaceConfig{}, nil
	}
	if err != nil {
		return nil, err
	}

	// Parse with raw "run" field to handle string or array
	var raw struct {
		SetupWorkspace []string        `json:"setup-workspace"`
		Run            json.RawMessage `json:"run"`
		Archive        string          `json:"archive"`
	}
	if err := json.Unmarshal(fileData, &raw); err != nil {
		return nil, err
	}

	config := &WorkspaceConfig{
		SetupWorkspace: raw.SetupWorkspace,
		ArchiveScript:  raw.Archive,
	}

	// "run" can be a string or an array of {name, command} objects
	if len(raw.Run) > 0 {
		var single string
		if err := json.Unmarshal(raw.Run, &single); err == nil {
			config.RunScript = single
			config.RunCommands = []RunCommand{{Command: single}}
		} else {
			var multi []RunCommand
			if err := json.Unmarshal(raw.Run, &multi); err == nil {
				config.RunCommands = multi
				if len(multi) == 1 {
					config.RunScript = multi[0].Command
				}
			}
		}
	}

	return config, nil
}

// RunSetup runs the setup scripts for a workspace.
// For multi-repo workspaces it loads each repo's .medusa/workspaces.json and
// runs that repo's setup-workspace commands inside the matching worktree.
func (r *ScriptRunner) RunSetup(ws *data.Workspace) error {
	env := r.envBuilder.BuildEnv(ws)

	for i, repo := range ws.Repos {
		if i >= len(ws.Worktrees) {
			break
		}
		config, err := r.LoadConfig(repo.Path)
		if err != nil {
			return err
		}
		worktreeRoot := ws.Worktrees[i].Root
		for _, cmdStr := range config.SetupWorkspace {
			cmd := exec.Command("sh", "-c", cmdStr)
			cmd.Dir = worktreeRoot
			cmd.Env = env

			var stderr bytes.Buffer
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				return fmt.Errorf("setup command failed in %s: %s: %s", repo.Name, cmdStr, stderr.String())
			}
		}
	}

	return nil
}

// RunScript runs a script for a workspace
func (r *ScriptRunner) RunScript(ws *data.Workspace, scriptType ScriptType) (*exec.Cmd, error) {
	config, err := r.LoadConfig(ws.PrimaryRepo().Path)
	if err != nil {
		return nil, err
	}

	var cmdStr string
	switch scriptType {
	case ScriptRun:
		cmdStr = config.RunScript
		if cmdStr == "" {
			cmdStr = ws.Scripts.Run
		}
	case ScriptArchive:
		cmdStr = config.ArchiveScript
		if cmdStr == "" {
			cmdStr = ws.Scripts.Archive
		}
	}

	if cmdStr == "" {
		return nil, fmt.Errorf("no %s script configured", scriptType)
	}

	// Check for existing process in non-concurrent mode
	if ws.ScriptMode == "nonconcurrent" {
		_ = r.Stop(ws)
	}

	env := r.envBuilder.BuildEnv(ws)

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = ws.PrimaryWorktreeRoot()
	cmd.Env = env
	SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Capture the workspace root up-front so the monitor goroutine doesn't
	// race with concurrent mutations of ws (Workspace.Root is a value receiver,
	// so calling it copies every field — including ones callers may write).
	wsRoot := ws.Root()

	r.mu.Lock()
	r.running[wsRoot] = cmd
	r.mu.Unlock()

	// Monitor in background
	safego.Go("process.script_wait", func() {
		_ = cmd.Wait()
		r.mu.Lock()
		delete(r.running, wsRoot)
		r.mu.Unlock()
	})

	return cmd, nil
}

// GetRunCommands returns the run commands, environment map, and any warnings
// produced while normalizing tab names for a workspace. For multi-repo
// workspaces it collects commands from every repo's .medusa/workspaces.json,
// wraps each command with `cd <worktree>` so it executes inside the right
// worktree (the tmux session cwd is ws.Root(), which is the parent dir for
// multi-repo), and prefixes tab names with the repo name to keep them
// distinguishable. Falls back to ws.Scripts.Run if no per-repo commands are
// defined.
func (r *ScriptRunner) GetRunCommands(ws *data.Workspace) ([]RunCommand, map[string]string, []string, error) {
	wsRoot := ws.Root()
	multiRepo := ws.IsMultiRepo()

	var allCmds []RunCommand
	for i, repo := range ws.Repos {
		if i >= len(ws.Worktrees) {
			break
		}
		config, err := r.LoadConfig(repo.Path)
		if err != nil {
			return nil, nil, nil, err
		}
		worktreeRoot := ws.Worktrees[i].Root
		for _, rc := range config.RunCommands {
			cmd := rc
			if worktreeRoot != "" && worktreeRoot != wsRoot {
				cmd.Command = "cd " + shellutil.Quote(worktreeRoot) + " && " + rc.Command
			}
			if multiRepo {
				if name := strings.TrimSpace(rc.Name); name != "" {
					cmd.Name = repo.Name + ": " + name
				} else {
					cmd.Name = repo.Name
				}
			}
			allCmds = append(allCmds, cmd)
		}
	}

	if len(allCmds) == 0 && ws.Scripts.Run != "" {
		allCmds = []RunCommand{{Command: ws.Scripts.Run}}
	}
	if len(allCmds) == 0 {
		return nil, nil, nil, fmt.Errorf("no run script configured")
	}
	cmds, warnings := normalizeRunCommandNames(allCmds)
	envMap := r.envBuilder.BuildEnvMap(ws)
	return cmds, envMap, warnings, nil
}

// deriveScriptTabName returns a display name for a run-script tab: the
// caller-supplied name when set, otherwise the command (trimmed, and truncated
// with an ellipsis if longer than 24 runes). Falls back to "dev server" only
// when neither is available.
func deriveScriptTabName(rc RunCommand) string {
	if name := strings.TrimSpace(rc.Name); name != "" {
		return name
	}
	cmd := strings.TrimSpace(rc.Command)
	if cmd == "" {
		return "dev server"
	}
	const maxLen = 24
	const truncTo = 21
	if utf8.RuneCountInString(cmd) <= maxLen {
		return cmd
	}
	return string([]rune(cmd)[:truncTo]) + "…"
}

// normalizeRunCommandNames fills in empty names from the command and appends
// " (N)" suffixes to any duplicates. Returns the updated list and a list of
// human-readable warnings describing the renamings.
func normalizeRunCommandNames(cmds []RunCommand) ([]RunCommand, []string) {
	if len(cmds) == 0 {
		return cmds, nil
	}
	taken := make(map[string]struct{}, len(cmds))
	out := make([]RunCommand, len(cmds))
	var warnings []string
	for i, rc := range cmds {
		base := deriveScriptTabName(rc)
		final := base
		for n := 2; ; n++ {
			if _, clash := taken[final]; !clash {
				break
			}
			final = fmt.Sprintf("%s (%d)", base, n)
		}
		if final != base {
			warnings = append(warnings, fmt.Sprintf("run script name %q already used; renamed to %q", base, final))
		}
		taken[final] = struct{}{}
		rc.Name = final
		out[i] = rc
	}
	return out, warnings
}

// Stop stops the running script for a workspace
func (r *ScriptRunner) Stop(ws *data.Workspace) error {
	r.mu.Lock()
	cmd, ok := r.running[ws.Root()]
	r.mu.Unlock()

	if !ok {
		return nil
	}

	if cmd.Process != nil {
		return KillProcessGroup(cmd.Process.Pid, KillOptions{})
	}

	return nil
}

// IsRunning checks if a script is running for a workspace
func (r *ScriptRunner) IsRunning(ws *data.Workspace) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[ws.Root()]
	return ok
}

// StopAll stops all running scripts
func (r *ScriptRunner) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, cmd := range r.running {
		if cmd.Process != nil {
			_ = KillProcessGroup(cmd.Process.Pid, KillOptions{})
		}
	}
	r.running = make(map[string]*exec.Cmd)
}
