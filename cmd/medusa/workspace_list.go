package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/tmux"
)

type listFlags struct {
	profile, group string
	asJSON         bool
}

func workspaceListCommand() *command {
	return &command{
		name:    "list",
		summary: "List workspaces",
		usage:   "medusa workspace list [flags]",
		long: "Lists every registered workspace, including archived ones and ones whose\n" +
			"directory has gone. LIVE counts the agent sessions running for it.",
		examples: []string{
			"medusa ws list",
			"medusa ws list --profile Work --json | jq -r '.[].root'",
		},
		bind: bindListFlags,
	}
}

func bindListFlags(fs *flag.FlagSet) func(c *cliContext, args []string) error {
	f := &listFlags{}
	fs.StringVar(&f.profile, "profile", "", "only workspaces in profile `name`")
	fs.StringVar(&f.group, "group", "", "only workspaces in group `name`")
	fs.BoolVar(&f.asJSON, "json", false, "print the result as JSON")
	return func(c *cliContext, args []string) error {
		return runWorkspaceList(c, f, args)
	}
}

type listEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Profile  string `json:"profile"`
	Group    string `json:"group"`
	Branch   string `json:"branch"`
	Root     string `json:"root"`
	Worktree bool   `json:"worktree"`
	Live     int    `json:"live"`
	// State is "ok", "archived", "missing" (the directory is gone) or
	// "no-metadata" (the registry names a workspace whose metadata is gone).
	State string `json:"state"`
}

func runWorkspaceList(c *cliContext, f *listFlags, args []string) error {
	if len(args) > 0 {
		return usagef("unexpected argument %q", args[0])
	}
	env, err := loadWorkspaceEnv()
	if err != nil {
		return err
	}
	entries, err := env.deps.Registry.ListWorkspaces()
	if err != nil {
		return fmt.Errorf("reading the registry: %w", err)
	}
	tmuxOpts := tmux.DefaultOptions()
	list := []listEntry{}
	for _, e := range entries {
		item := listEntry{ID: e.ID, Name: e.Name, Profile: e.Profile, State: "ok"}
		if ws, err := env.deps.Store.Load(data.WorkspaceID(e.ID)); err != nil {
			item.State = "no-metadata"
		} else {
			item.Group, item.Branch, item.Root, item.Worktree = ws.Group, ws.Branch(), ws.Root(), ws.UsesWorktree()
			if ws.Archived() {
				item.State = "archived"
			} else if _, err := os.Stat(ws.Root()); err != nil {
				item.State = "missing"
			}
		}
		if (f.profile != "" && item.Profile != f.profile) || (f.group != "" && item.Group != f.group) {
			continue
		}
		if sessions, err := tmux.ListSessionsMatchingTags(map[string]string{"@medusa": "1", "@medusa_workspace": e.ID}, tmuxOpts); err == nil {
			item.Live = len(sessions)
		}
		list = append(list, item)
	}

	if f.asJSON {
		return printJSON(c.stdout, list)
	}
	tw := tabwriter.NewWriter(c.stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tPROFILE\tGROUP\tBRANCH\tLIVE\tSTATE\tROOT")
	for _, item := range list {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			item.Name, dash(item.Profile), dash(item.Group), dash(item.Branch), item.Live, item.State, dash(item.Root))
	}
	return tw.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
