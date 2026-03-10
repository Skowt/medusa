// medusa-approve-compound is a Claude Code PreToolUse hook that auto-approves
// compound Bash commands when every sub-command matches the allow list and
// none match the deny list.
package main

import (
	"io"
	"os"

	"github.com/andyrewlee/medusa/internal/approve"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(0)
	}
	input, err := approve.ParseHookInput(data)
	if err != nil || input.ToolInput.Command == "" {
		os.Exit(0)
	}

	gitRoot := approve.FindGitRoot()
	perms := approve.LoadPermissions(gitRoot)
	if len(perms.Allow) == 0 {
		os.Exit(0)
	}

	command := input.ToolInput.Command

	// Simple command: check directly without parsing
	if !approve.IsCompound(command) {
		if approve.MatchCommand(command, perms) == "allow" {
			_ = approve.WriteAllow(os.Stdout)
		}
		os.Exit(0)
	}

	// Compound command: parse and check each sub-command
	commands, err := approve.ExtractCommands(command)
	if err != nil || len(commands) == 0 {
		os.Exit(0) // Parse failed, fall through to Claude's prompt
	}

	allAllowed := true
	anyDenied := false
	for _, cmd := range commands {
		switch approve.MatchCommand(cmd, perms) {
		case "deny":
			anyDenied = true
			allAllowed = false
		case "allow":
			// continue
		default:
			allAllowed = false
		}
	}

	if allAllowed {
		_ = approve.WriteAllow(os.Stdout)
		os.Exit(0)
	}
	if anyDenied {
		_ = approve.WriteDeny(os.Stderr, "Compound command contains a denied sub-command")
		os.Exit(2)
	}
	// Unknown commands present: fall through
	os.Exit(0)
}
