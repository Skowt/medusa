package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Skowt/medusa/internal/data"
)

// TeardownCommandTimeout bounds each archive/teardown command run when a
// workspace is deleted. Teardown runs synchronously ahead of worktree removal,
// so a hung command (a stuck `podman rm`) must not stall the delete forever.
const TeardownCommandTimeout = 60 * time.Second

// teardownWaitDelay is how long Wait keeps reading output after the process
// group is killed, in case a grandchild still holds the stderr pipe.
const teardownWaitDelay = time.Second

// runProfileSetup runs the workspace profile's setup commands in the primary
// worktree with the same env as the repo setup scripts.
func (r *ScriptRunner) runProfileSetup(ws *data.Workspace, env []string) error {
	cfg, err := LoadProfileConfig(r.profilesRoot, ws.Profile)
	if err != nil {
		return fmt.Errorf("profile %s: %w", ws.Profile, err)
	}
	for _, cmdStr := range cfg.Setup {
		cmd := exec.Command("sh", "-c", cmdStr)
		cmd.Dir = ws.PrimaryWorktreeRoot()
		cmd.Env = env

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("profile %s setup command failed: %s: %s", ws.Profile, cmdStr, stderr.String())
		}
	}
	return nil
}

// RunTeardown runs each repo's archive script and then the profile's teardown
// commands, synchronously, before a workspace's worktrees are removed. Every
// command runs even when an earlier one fails; all failures are returned, each
// naming its command and stderr, for the caller to log. Deletion must proceed
// regardless.
func (r *ScriptRunner) RunTeardown(ws *data.Workspace) []error {
	env := r.envBuilder.BuildEnv(ws)
	steps, errs := r.archiveSteps(ws)
	for _, step := range steps {
		if err := r.runBounded(step.command, step.dir, env); err != nil {
			errs = append(errs, fmt.Errorf("archive script: %w", err))
		}
	}
	cfg, err := LoadProfileConfig(r.profilesRoot, ws.Profile)
	if err != nil {
		return append(errs, fmt.Errorf("profile %s: %w", ws.Profile, err))
	}
	for _, cmdStr := range cfg.Teardown {
		if err := r.runBounded(cmdStr, ws.PrimaryWorktreeRoot(), env); err != nil {
			errs = append(errs, fmt.Errorf("profile %s teardown: %w", ws.Profile, err))
		}
	}
	return errs
}

type scriptStep struct {
	command string
	dir     string
}

// archiveSteps collects each repo's archive script with the worktree it runs
// in, mirroring RunSetup. The workspace-level Scripts.Archive is the fallback
// when no repo configures one, matching RunScript.
func (r *ScriptRunner) archiveSteps(ws *data.Workspace) ([]scriptStep, []error) {
	var steps []scriptStep
	var errs []error
	for i, repo := range ws.Repos {
		if i >= len(ws.Worktrees) {
			break
		}
		config, err := r.LoadConfig(repo.Path)
		if err != nil {
			errs = append(errs, fmt.Errorf("load %s workspace config: %w", repo.Name, err))
			continue
		}
		if config.ArchiveScript == "" {
			continue
		}
		steps = append(steps, scriptStep{command: config.ArchiveScript, dir: ws.Worktrees[i].Root})
	}
	if len(steps) == 0 && ws.Scripts.Archive != "" {
		steps = append(steps, scriptStep{command: ws.Scripts.Archive, dir: ws.PrimaryWorktreeRoot()})
	}
	return steps, errs
}

// runBounded runs cmdStr via sh -c, killing its whole process group once
// teardownTimeout elapses.
func (r *ScriptRunner) runBounded(cmdStr, dir string, env []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.teardownTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = existingDir(dir)
	cmd.Env = env
	SetProcessGroup(cmd)
	cmd.Cancel = func() error { return KillProcessGroup(cmd.Process.Pid, KillOptions{}) }
	cmd.WaitDelay = teardownWaitDelay

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s: timed out after %s: %s", cmdStr, r.teardownTimeout, strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		return fmt.Errorf("%s: %w: %s", cmdStr, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// existingDir returns dir when it exists, else "" (the current directory): a
// teardown such as `podman rm` must still run for a workspace whose worktree
// is already gone from disk.
func existingDir(dir string) string {
	if dir == "" {
		return ""
	}
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}
