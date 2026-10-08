package main

import (
	"bufio"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/hooks"
	"github.com/Skowt/medusa/internal/tmux"
)

type deleteFlags struct {
	yes, asJSON bool
}

func workspaceDeleteCommand() *command {
	return &command{
		name:    "delete",
		summary: "Delete a workspace, its worktree and its agent sessions",
		usage:   "medusa workspace delete <name|id> [flags]",
		long: "Stops the workspace's agents, removes the worktree and branch medusa made,\n" +
			"and forgets the workspace. A workspace over a repo's own checkout never\n" +
			"removes the repo. Asks first on a terminal; scripts must pass --yes.",
		examples: []string{
			"medusa ws delete fix-login",
			"medusa ws delete fix-login --yes --json",
		},
		bind: bindDeleteFlags,
	}
}

func bindDeleteFlags(fs *flag.FlagSet) func(c *cliContext, args []string) error {
	f := &deleteFlags{}
	fs.BoolVar(&f.yes, "yes", false, "do not ask for confirmation (required without a terminal)")
	fs.BoolVar(&f.asJSON, "json", false, "print the result as JSON")
	return func(c *cliContext, args []string) error {
		return runWorkspaceDelete(c, f, args)
	}
}

// detachTimeout bounds how long delete waits for a running TUI to let go of
// the workspace's sessions.
const detachTimeout = 5 * time.Second

type deleteResult struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Deleted       bool   `json:"deleted"`
	BranchWarning string `json:"branch_warning"`
	TUINotified   bool   `json:"tui_notified"`
}

func runWorkspaceDelete(c *cliContext, f *deleteFlags, args []string) error {
	if len(args) != 1 {
		return usagef("expected exactly one workspace name or id, got %d arguments", len(args))
	}
	env, err := loadWorkspaceEnv()
	if err != nil {
		return err
	}
	ws, err := findWorkspace(env, args[0])
	if err != nil {
		return err
	}
	if !f.yes {
		if !c.stdinIsTTY {
			return usagef("refusing to delete %q without a terminal to confirm on; pass --yes", ws.Name)
		}
		if !confirm(c, ws) {
			return fmt.Errorf("not deleted")
		}
	}

	id := string(ws.ID())
	tags := map[string]string{"@medusa": "1", "@medusa_workspace": id}
	tmuxOpts := tmux.DefaultOptions()
	hooksDir := env.cfg.Paths.HooksDir
	// A TUI showing this workspace must drop its tabs before the sessions die,
	// or it reattaches to them — and a reattach that finds no session starts a
	// new agent in the directory being deleted.
	if hooks.NotifyWorkspaceClosing(hooksDir, id) {
		if !waitForDetach(tags, tmuxOpts, detachTimeout) {
			_, _ = fmt.Fprintln(c.stderr, "Warning: medusa is still attached to this workspace's sessions; deleting anyway")
		}
	}
	if _, err := tmux.KillSessionsMatchingTags(tags, tmuxOpts); err != nil {
		_, _ = fmt.Fprintln(c.stderr, "Warning: could not stop every agent session:", err)
	}
	res := deleteResult{ID: id, Name: ws.Name, Deleted: true}
	res.BranchWarning = env.deps.Delete(ws)
	// Backstop: a session a TUI recreated between the first kill and the
	// registry removal would otherwise outlive the workspace.
	_, _ = tmux.KillSessionsMatchingTags(tags, tmuxOpts)
	if res.BranchWarning != "" {
		_, _ = fmt.Fprintln(c.stderr, "Warning:", res.BranchWarning)
	}
	res.TUINotified = hooks.NotifyWorkspacesChanged(hooksDir)

	if f.asJSON {
		return printJSON(c.stdout, res)
	}
	_, _ = fmt.Fprintf(c.stdout, "Deleted %s\n", ws.Name)
	return nil
}

func confirm(c *cliContext, ws *data.Workspace) bool {
	what := ws.Root()
	if !ws.UsesWorktree() {
		what += " (the repo itself is kept)"
	}
	_, _ = fmt.Fprintf(c.stderr, "Delete workspace %q at %s? [y/N] ", ws.Name, what)
	answer, _ := bufio.NewReader(c.stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// waitForDetach polls until no tmux client is attached to any session with
// tags, reporting false if that does not happen within timeout.
func waitForDetach(tags map[string]string, opts tmux.Options, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		attached := 0
		sessions, _ := tmux.ListSessionsMatchingTags(tags, opts)
		for _, s := range sessions {
			if n, err := tmux.SessionClientCount(s, opts); err == nil {
				attached += n
			}
		}
		if attached == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
