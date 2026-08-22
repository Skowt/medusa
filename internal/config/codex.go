package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	hookspkg "github.com/Skowt/medusa/internal/hooks"
)

// CodexHomeSubdir is the per-profile directory CODEX_HOME points at. Codex
// keeps everything — auth, config, session rollouts, its state databases — under
// CODEX_HOME, so giving each profile its own makes Codex tabs profile-scoped the
// way CLAUDE_CONFIG_DIR makes Claude tabs profile-scoped.
const CodexHomeSubdir = "codex"

// codexHookTimeoutSec bounds one hook invocation. Codex reads `timeout` in
// seconds (Claude Code's settings.json reads milliseconds), so the two must not
// share a constant: 5000 here would be an eighty-three minute timeout.
const codexHookTimeoutSec = 5

// CodexHomeDir returns the CODEX_HOME for a profile.
func CodexHomeDir(profilesRoot, profile string) string {
	if profile == "" {
		return ""
	}
	return filepath.Join(profilesRoot, profile, CodexHomeSubdir)
}

// EnsureCodexHome creates a profile's CODEX_HOME and seeds it with the user's
// existing ~/.codex/auth.json when it has none of its own. Without the seed
// every new profile would open its first Codex tab at a login prompt, since
// credentials live under CODEX_HOME like everything else. The copy is
// one-directional and never overwrites: once a profile has its own auth.json,
// re-logins there stay local to it.
func EnsureCodexHome(codexHome string) error {
	if codexHome == "" {
		return fmt.Errorf("codex home is required")
	}
	if err := os.MkdirAll(codexHome, 0700); err != nil {
		return err
	}
	target := filepath.Join(codexHome, "auth.json")
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil // no home to seed from; Codex will prompt for login
	}
	auth, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return nil // nothing to seed; not an error
	}
	return atomicWriteFile(target, auth, 0600)
}

// codexHookEvents are the Codex lifecycle events Medusa listens to. Codex's
// payloads use the same field names as Claude Code's (session_id, cwd,
// hook_event_name), so medusa-hook-emit parses them unchanged.
//
// Codex has no Notification event, so a Codex tab has no idle_prompt or
// permission_prompt ping; PermissionRequest is the one needs-input signal it
// offers. Its Stop payload carries no background_tasks list either, so the
// outstanding count stays unknown and a Stop reads as plain ready — the same
// degradation as a Claude Code older than v2.1.145.
var codexHookEvents = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"PermissionRequest", "SubagentStart", "SubagentStop", "Stop",
}

// InjectCodexHooks merges Medusa's lifecycle hooks into a profile's
// <CODEX_HOME>/hooks.json, which Codex discovers alongside config.toml. The
// rule shape is the same one Claude Code's settings.json takes, so both
// assistants share hookCommandBuilder and the same medusa-hook-emit binary.
//
// hooks.json rather than config.toml is deliberate: it keeps injected rules in
// a file Medusa owns outright (and can rewrite with the existing JSON
// machinery) instead of rewriting the TOML a user also hand-edits.
//
// Codex gates hooks behind its own trust prompt — it hashes each hook and skips
// untrusted ones, so the first Codex tab in a profile opens on "Hooks need
// review" and stays without activity detection until the user picks "Trust all
// and continue". The hash covers the command string, which is stable per
// install, so it is a one-time gesture per profile.
func InjectCodexHooks(codexHome, hooksDir, emitBin string) error {
	builder := hookCommandBuilder{sock: hookspkg.SocketPath(hooksDir), emitBin: emitBin}
	return readModifyWriteJSON(filepath.Join(codexHome, "hooks.json"), func(root map[string]any) {
		hooks := getOrCreateMap(root, "hooks")
		stripMedusaHookRules(hooks)

		for _, event := range codexHookEvents {
			existing, _ := hooks[event].([]any)
			hooks[event] = append(existing, map[string]any{
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": builder.command(event),
						"timeout": codexHookTimeoutSec,
					},
				},
			})
		}
		root["hooks"] = hooks
	})
}

// codexTrustHeader is the config.toml table that marks one directory trusted.
func codexTrustHeader(root string) string {
	return fmt.Sprintf("[projects.%q]", root)
}

// InjectCodexTrustedDirectory pre-trusts a worktree in a profile's
// CODEX_HOME/config.toml, the Codex counterpart of InjectTrustedDirectory.
// Codex refuses to start in an untrusted directory ("Not inside a trusted
// directory and --skip-git-repo-check was not specified"), which for Medusa is
// every fresh worktree, so without this the first launch in each workspace
// stalls on a prompt the user cannot see the reason for.
//
// The append is text, not a TOML round-trip: Codex writes its own state into
// this file (hook trust hashes among it), and reserializing would reorder and
// reformat a file it owns. Already-trusted roots are left alone.
func InjectCodexTrustedDirectory(codexHome, root string) error {
	if codexHome == "" || root == "" {
		return fmt.Errorf("codex home and workspace root are required")
	}
	if err := os.MkdirAll(codexHome, 0700); err != nil {
		return err
	}
	path := filepath.Join(codexHome, "config.toml")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	header := codexTrustHeader(root)
	if containsTOMLTable(string(existing), header) {
		return nil
	}
	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	if len(existing) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(header + "\ntrust_level = \"trusted\"\n")
	return atomicWriteFile(path, []byte(b.String()), 0600)
}

// containsTOMLTable reports whether content already declares the given table
// header on a line of its own, ignoring surrounding whitespace.
func containsTOMLTable(content, header string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == header {
			return true
		}
	}
	return false
}
