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

// codexHookShimName is the launcher every Codex hook rule points at, inside the
// profile's CODEX_HOME.
//
// The indirection exists because Codex hashes each hook's command string and
// re-asks for trust whenever it changes. Naming medusa-hook-emit directly put
// its absolute path in that string, so the prompt came back for every medusa
// that lived somewhere new — a `make run` build, an `air` rebuild, an upgrade,
// or a PATH lookup that missed and fell back to the shell pipeline. Pointing at
// a fixed path inside CODEX_HOME instead makes the string constant, and the
// shim (which Medusa rewrites on every launch) carries what actually varies.
//
// The trust boundary is unchanged by this: Medusa owns CODEX_HOME outright, so
// trusting a rule that names medusa-hook-emit and trusting one that names
// Medusa's own shim are the same act of trusting Medusa.
const codexHookShimName = "medusa-hook.sh"

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

// EnsureCodexHome creates a profile's CODEX_HOME. Credentials are deliberately
// not copied from the user's global ~/.codex directory: each profile must log
// in independently so profiles remain separate authentication boundaries.
func EnsureCodexHome(codexHome string) error {
	if codexHome == "" {
		return fmt.Errorf("codex home is required")
	}
	return os.MkdirAll(codexHome, 0700)
}

// codexHookEvents are the Codex lifecycle events Medusa listens to. Codex's
// payloads use the same field names as Claude Code's (session_id, cwd,
// hook_event_name), so medusa-hook-emit parses them unchanged.
//
// Codex has no Notification event, so a Codex tab has no idle_prompt or
// permission_prompt ping. Its Stop payload carries no background_tasks list
// either, so the outstanding count stays unknown and a Stop reads as plain
// ready — the same degradation as a Claude Code older than v2.1.145.
var codexHookEvents = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"SubagentStart", "SubagentStop", "Stop",
}

// codexHookShimPath returns where the launcher lives for a CODEX_HOME.
func codexHookShimPath(codexHome string) string {
	return filepath.Join(codexHome, codexHookShimName)
}

// writeCodexHookShim writes the launcher every Codex hook rule invokes, and
// returns its path — or "" when there is no emit binary to launch, which leaves
// the caller on the legacy shell commands.
//
// The shim holds everything that can change between Medusa builds: the emit
// binary's path and the socket. It also holds the two guards the command string
// used to carry, so a non-Medusa session and a missing binary are both silent
// no-ops.
func writeCodexHookShim(codexHome, sock, emitBin string) string {
	if codexHome == "" || emitBin == "" {
		return ""
	}
	script := "#!/bin/sh\n" +
		"# Managed by Medusa — rewritten on every launch. Edits will be lost.\n" +
		"# Pointed at by CODEX_HOME/hooks.json so the hook command Codex hashes\n" +
		"# for trust stays the same across Medusa builds and upgrades.\n" +
		"[ -n \"$MEDUSA_SESSION_NAME\" ] || exit 0\n" +
		"BIN=" + shellQuote(emitBin) + "\n" +
		"SOCK=" + shellQuote(sock) + "\n" +
		"[ -x \"$BIN\" ] || exit 0\n" +
		"exec \"$BIN\" -socket \"$SOCK\" \"$@\"\n"
	path := codexHookShimPath(codexHome)
	if err := atomicWriteFile(path, []byte(script), 0700); err != nil {
		return ""
	}
	return path
}

// shellQuote wraps a value in single quotes for the shim.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// codexShimCommand is the stable command string a hook rule carries.
func codexShimCommand(shim, eventName string) string {
	return shellQuote(shim) + " -event " + eventName
}

// stripMedusaCodexHookRules removes the rules Medusa injected, in either form:
// the current shim-based ones and the older ones that named medusa-hook-emit
// directly. Without the second, an upgrade would leave the old rule behind and
// every event would fire twice.
func stripMedusaCodexHookRules(hooks map[string]any) {
	stripMedusaHookRules(hooks) // legacy: commands opening with the env guard
	for event, v := range hooks {
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, entry := range arr {
			if m, ok := entry.(map[string]any); ok && hookRuleHasCommandContaining(m, codexHookShimName) {
				continue
			}
			kept = append(kept, entry)
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
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
	sock := hookspkg.SocketPath(hooksDir)
	builder := hookCommandBuilder{sock: sock, emitBin: emitBin}
	shim := writeCodexHookShim(codexHome, sock, emitBin)
	return readModifyWriteJSON(filepath.Join(codexHome, "hooks.json"), func(root map[string]any) {
		hooks := getOrCreateMap(root, "hooks")
		stripMedusaCodexHookRules(hooks)

		rule := func(command string, timeoutSec int) map[string]any {
			return map[string]any{
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": command,
						"timeout": timeoutSec,
					},
				},
			}
		}

		// Without the shim there is no emit binary, so the rules fall back to
		// the legacy shell pipeline. It carries no binary path either, so it is
		// just as stable.
		eventCommand := func(event string) string {
			if shim == "" {
				return builder.command(event)
			}
			return codexShimCommand(shim, event)
		}

		for _, event := range codexHookEvents {
			existing, _ := hooks[event].([]any)
			hooks[event] = append(existing, rule(eventCommand(event), codexHookTimeoutSec))
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
