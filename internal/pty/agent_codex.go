package pty

import (
	"path/filepath"
	"strings"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/shellutil"
)

// Codex sandbox policies, the values codex --sandbox accepts.
const (
	CodexSandboxReadOnly   = "read-only"
	CodexSandboxWorkspace  = "workspace-write"
	CodexSandboxFullAccess = "danger-full-access"
)

// codexSandboxModes gates what reaches the command line.
// Both values ride in from persisted UI settings, and Codex exits on an
// unrecognised one instead of ignoring it — which would drop the tab to a bare
// shell — so an unknown value is dropped rather than forwarded.
var codexSandboxModes = map[string]bool{
	CodexSandboxReadOnly:   true,
	CodexSandboxWorkspace:  true,
	CodexSandboxFullAccess: true,
}

// codexSessionExists reports whether CODEX_HOME holds a rollout transcript for
// sessionID. Codex files them as
// sessions/<yyyy>/<mm>/<dd>/rollout-<timestamp>-<session-id>.jsonl, so the id
// is a filename suffix rather than the whole name.
func codexSessionExists(codexHome, sessionID string) bool {
	if codexHome == "" || sessionID == "" {
		return false
	}
	pattern := filepath.Join(codexHome, "sessions", "*", "*", "*", "rollout-*-"+sessionID+".jsonl")
	matches, err := filepath.Glob(pattern)
	return err == nil && len(matches) > 0
}

// codexSessionArgs returns the leading arguments for the codex command line:
// either the `resume <id>` subcommand or nothing at all for a fresh session.
//
// The existence check is load-bearing. `codex resume <id>` on an id Codex has
// no rollout for does not degrade to a new session — it exits 1 with "No saved
// session found with ID …", which drops the tab to a bare shell. Codex mints
// its own session ids (there is no --session-id to pre-assign one), so a tab
// only learns its id from the SessionStart hook: before the first turn, or when
// the hooks are not trusted yet, the recorded id is empty or stale and a fresh
// session is the only correct answer.
func codexSessionArgs(codexHome string, opts AgentOptions) string {
	if !opts.Resume || opts.ClaudeSessionID == "" {
		return ""
	}
	if !codexSessionExists(codexHome, opts.ClaudeSessionID) {
		logging.Info("No Codex rollout for session %s in %s; starting a fresh session", opts.ClaudeSessionID, codexHome)
		return ""
	}
	return " resume " + shellutil.Quote(opts.ClaudeSessionID)
}

// codexPolicyArgs returns the optional --sandbox flag for a Codex launch.
//
// Note what is absent: --dangerously-bypass-approvals-and-sandbox. The two
// orthogonal controls reach the same place — danger-full-access with approvals
// set to never is a session with no sandbox and no prompts — so Medusa never
// has to emit the blanket flag to offer that.
func codexPolicyArgs(opts AgentOptions) string {
	// --approve-for-me is itself a complete policy bundle: Codex couples its
	// automatic reviewer to workspace-write and rejects a simultaneous
	// --sandbox flag. Keep the user's sandbox choice for Default mode only.
	if opts.CodexAuto {
		return ""
	}
	var b strings.Builder
	if codexSandboxModes[opts.CodexSandbox] {
		b.WriteString(" --sandbox " + opts.CodexSandbox)
	} else if opts.CodexSandbox != "" {
		logging.Warn("Ignoring unknown Codex sandbox mode %q", opts.CodexSandbox)
	}
	return b.String()
}

// buildCodexCommand assembles the env-prefixed shell command for a Codex tab.
// CODEX_HOME is what makes a Codex tab profile-scoped: auth, config, hooks and
// session rollouts all live under it, so it must be set for every launch —
// including a resume, whose id only resolves against the same home that
// recorded it.
func buildCodexCommand(command, sessionName, codexHome string, opts AgentOptions) string {
	cmd := "MEDUSA_SESSION_NAME=" + shellutil.Quote(sessionName)
	if codexHome != "" {
		cmd = "CODEX_HOME=" + shellutil.Quote(codexHome) + " " + cmd
	}
	// Medusa owns scrolling for embedded Codex tabs. --no-alt-screen is Codex's
	// supported inline mode and keeps its transcript in terminal history instead
	// of splitting scroll ownership between Codex, tmux, and Medusa.
	cmd += " " + command + " --no-alt-screen --search"
	if opts.CodexAuto {
		cmd += " --approve-for-me"
	}
	cmd += codexSessionArgs(codexHome, opts) + codexPolicyArgs(opts)
	return cmd
}

// prepareCodexHome creates a profile's CODEX_HOME, trusts the worktree, and
// injects the activity hooks. Every step is best-effort: a
// Codex tab that starts without hooks loses activity detection, which is a
// lesser failure than not starting at all.
func prepareCodexHome(codexHome, workspaceRoot, hooksDir, emitBin string) {
	if codexHome == "" {
		return
	}
	if err := config.EnsureCodexHome(codexHome); err != nil {
		logging.Warn("Could not prepare CODEX_HOME %s: %v", codexHome, err)
		return
	}
	if err := config.InjectCodexTrustedDirectory(codexHome, workspaceRoot); err != nil {
		logging.Warn("Could not trust %s for Codex: %v", workspaceRoot, err)
	}
	if err := config.InjectCodexHooks(codexHome, hooksDir, emitBin); err != nil {
		logging.Warn("Could not inject Codex hooks into %s: %v", codexHome, err)
	}
}

// isAgentAssistant reports whether an agent type is a profile-scoped assistant
// rather than a viewer or script session. Both assistants keep their state
// under a per-profile directory, so both require a workspace profile.
func isAgentAssistant(agentType AgentType) bool {
	return agentType == AgentClaude || agentType == AgentCodex
}
