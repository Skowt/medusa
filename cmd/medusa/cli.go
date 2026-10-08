package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// command is one node of the CLI: a group with children, or a leaf that runs.
// Subcommands run instead of the TUI and are dispatched before any TUI or
// logging setup, so they stay usable headlessly (scripts, CI, no terminal).
type command struct {
	name     string
	aliases  []string
	summary  string   // one line, shown in the parent's command list
	usage    string   // "medusa workspace new <name> [flags]"
	long     string   // paragraphs shown under the summary in this command's help
	examples []string // shown under "Examples:"
	children []*command

	// bind registers a leaf's flags on fs and returns the function that runs
	// it with the remaining positional arguments. Help calls it only to list
	// the flags.
	bind func(fs *flag.FlagSet) func(c *cliContext, args []string) error
	// flagGroups orders a leaf's flags under headings. Flags in no group are
	// listed alphabetically under "Flags:".
	flagGroups []flagGroup
}

type flagGroup struct {
	title string
	names []string
}

// cliContext carries the streams a command uses, so tests can capture them.
type cliContext struct {
	stdout     io.Writer
	stderr     io.Writer
	stdin      io.Reader
	stdinIsTTY bool
}

func defaultCLIContext() *cliContext {
	tty := false
	if info, err := os.Stdin.Stat(); err == nil {
		tty = info.Mode()&os.ModeCharDevice != 0
	}
	return &cliContext{stdout: os.Stdout, stderr: os.Stderr, stdin: os.Stdin, stdinIsTTY: tty}
}

// usageError is a mistake in how a command was invoked. It exits 2 and points
// at the command's help; any other error exits 1.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return usageError{msg: fmt.Sprintf(format, args...)}
}

func rootCommand() *command {
	return &command{
		name:    "medusa",
		summary: "Run Claude Code and Codex agents side by side, each in its own git worktree.",
		usage:   "medusa [command]",
		long: "With no command, medusa starts the TUI. The commands below work without\n" +
			"the TUI and are safe to call from scripts; a running TUI picks up their\n" +
			"changes on its own.",
		examples: []string{
			"medusa                                   # start the TUI",
			"medusa workspace new fix-login --prompt \"Fix the login redirect\"",
			"medusa ws list --json | jq -r '.[].name'",
		},
		children: []*command{workspaceCommand(), skillsCommand()},
	}
}

func workspaceCommand() *command {
	return &command{
		name:    "workspace",
		aliases: []string{"ws"},
		summary: "Create, list and delete workspaces",
		usage:   "medusa workspace <command> [flags]",
		long: "A workspace is a git worktree (or a repo's own checkout) with its own\n" +
			"agent tabs. These commands do the same work as the TUI's dialogs.",
		children: []*command{workspaceNewCommand(), workspaceListCommand(), workspaceDeleteCommand()},
	}
}

func (c *command) find(name string) *command {
	for _, child := range c.children {
		if child.name == name {
			return child
		}
		for _, alias := range child.aliases {
			if alias == name {
				return child
			}
		}
	}
	return nil
}

func isHelpArg(arg string) bool {
	switch arg {
	case "help", "?", "-h", "-help", "--help":
		return true
	}
	return false
}

// runCLI dispatches a command line. handled is false when there is nothing to
// dispatch and the TUI should start.
func runCLI(args []string, c *cliContext) (handled bool, code int) {
	if len(args) == 0 {
		return false, 0
	}
	return true, dispatch(rootCommand(), args, c)
}

func dispatch(cur *command, args []string, c *cliContext) int {
	path := []*command{cur}
	for {
		if cur.bind != nil {
			return runLeaf(path, args, c)
		}
		if len(args) == 0 {
			printHelp(c.stdout, path)
			return 0
		}
		arg := args[0]
		if isHelpArg(arg) {
			if arg == "help" && len(args) > 1 {
				return helpFor(path, args[1:], c)
			}
			printHelp(c.stdout, path)
			return 0
		}
		next := cur.find(arg)
		if next == nil {
			_, _ = fmt.Fprintf(c.stderr, "%s: unknown command %q\n\n", commandPath(path), arg)
			printHelp(c.stderr, path)
			return 2
		}
		cur = next
		path = append(path, cur)
		args = args[1:]
	}
}

// helpFor answers `help <command...>` by walking names from path.
func helpFor(path []*command, names []string, c *cliContext) int {
	for _, name := range names {
		if isHelpArg(name) {
			continue
		}
		next := path[len(path)-1].find(name)
		if next == nil {
			_, _ = fmt.Fprintf(c.stderr, "%s: unknown command %q\n\n", commandPath(path), name)
			printHelp(c.stderr, path)
			return 2
		}
		path = append(path, next)
	}
	printHelp(c.stdout, path)
	return 0
}

func runLeaf(path []*command, args []string, c *cliContext) int {
	leaf := path[len(path)-1]
	// "help" and "?" mean help only in first position; anywhere else they may
	// be a value ("--prompt help"). -h/--help are found by the flag parser,
	// which already knows which arguments are flag values.
	if len(args) > 0 && (args[0] == "help" || args[0] == "?") {
		printHelp(c.stdout, path)
		return 0
	}
	fs := flag.NewFlagSet(commandPath(path), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	run := leaf.bind(fs)
	positional, err := parseInterspersed(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		printHelp(c.stdout, path)
		return 0
	}
	if err == nil {
		err = run(c, positional)
	}
	if err == nil {
		return 0
	}
	if isFlagError(err) {
		err = usageError{msg: flagErrorMessage(err)}
	}
	var uErr usageError
	if errors.As(err, &uErr) {
		_, _ = fmt.Fprintf(c.stderr, "%s: %v\nRun '%s --help' for usage.\n", commandPath(path), err, commandPath(path))
		return 2
	}
	_, _ = fmt.Fprintf(c.stderr, "%s: %v\n", commandPath(path), err)
	return 1
}

func commandPath(path []*command) string {
	names := make([]string, len(path))
	for i, c := range path {
		names[i] = c.name
	}
	return strings.Join(names, " ")
}

func printHelp(w io.Writer, path []*command) {
	cur := path[len(path)-1]
	var b strings.Builder
	b.WriteString(cur.summary + "\n")
	if cur.long != "" {
		b.WriteString("\n" + cur.long + "\n")
	}
	b.WriteString("\nUsage:\n  " + cur.usage + "\n")
	if len(cur.aliases) > 0 {
		b.WriteString("\nAliases:\n  " + strings.Join(append([]string{cur.name}, cur.aliases...), ", ") + "\n")
	}
	if len(cur.children) > 0 {
		b.WriteString("\nCommands:\n")
		for _, child := range cur.children {
			fmt.Fprintf(&b, "  %-12s %s\n", child.name, child.summary)
		}
		if len(path) == 1 {
			fmt.Fprintf(&b, "  %-12s %s\n", "help", "Show help for any command")
		}
	}
	if cur.bind != nil {
		fs := flag.NewFlagSet(cur.name, flag.ContinueOnError)
		cur.bind(fs)
		b.WriteString(formatFlagGroups(fs, cur.flagGroups))
	}
	if len(path) == 1 {
		b.WriteString("\nFlags:\n")
		fmt.Fprintf(&b, "  %-28s %s\n", "-h, --help", "Show help")
		fmt.Fprintf(&b, "  %-28s %s\n", "-v, --version", "Print the version and exit")
	}
	if len(cur.examples) > 0 {
		b.WriteString("\nExamples:\n")
		for _, ex := range cur.examples {
			b.WriteString("  " + ex + "\n")
		}
	}
	if len(cur.children) > 0 {
		fmt.Fprintf(&b, "\nRun '%s <command> --help' for more about a command.\n", commandPath(path))
	}
	_, _ = io.WriteString(w, b.String())
}

// formatFlagGroups lists a leaf's flags under its group headings, then any
// left over under "Flags:".
func formatFlagGroups(fs *flag.FlagSet, groups []flagGroup) string {
	var b strings.Builder
	listed := map[string]bool{}
	for _, g := range groups {
		b.WriteString("\n" + g.title + ":\n")
		for _, name := range g.names {
			if f := fs.Lookup(name); f != nil {
				b.WriteString(formatFlag(f))
				listed[name] = true
			}
		}
	}
	var rest strings.Builder
	fs.VisitAll(func(f *flag.Flag) {
		if !listed[f.Name] {
			rest.WriteString(formatFlag(f))
		}
	})
	if rest.Len() > 0 {
		b.WriteString("\nFlags:\n" + rest.String())
	}
	return b.String()
}

// formatFlag prints one flag GNU-style ("--profile <name>"), which is how the
// help and README spell them; the flag package accepts one dash or two.
func formatFlag(f *flag.Flag) string {
	var b strings.Builder
	{
		name, usage := flag.UnquoteUsage(f)
		label := "--" + f.Name
		if isBoolFlag(f) {
			name = ""
		}
		if name != "" {
			label += " <" + name + ">"
		}
		if f.DefValue != "" && f.DefValue != "false" {
			usage += " (default: " + f.DefValue + ")"
		}
		fmt.Fprintf(&b, "  %-28s %s\n", label, usage)
	}
	return b.String()
}
