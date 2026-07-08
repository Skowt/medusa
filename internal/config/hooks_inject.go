package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	hookspkg "github.com/Skowt/medusa/internal/hooks"
)

// medusaHookCommandPrefix identifies commands injected by Medusa across all
// versions: every Medusa hook command starts with this session-name guard.
const medusaHookCommandPrefix = `if [ -n "$MEDUSA_SESSION_NAME"`

// HookEmitBinaryName is the helper binary that forwards Claude Code hook
// payloads to the Medusa socket, installed alongside the medusa binary.
const HookEmitBinaryName = "medusa-hook-emit"

// ResolveHookEmitBinary locates the medusa-hook-emit helper: next to the
// running medusa binary first, then on PATH. Empty string means unavailable,
// in which case InjectHooks falls back to legacy shell commands (which cannot
// compute the outstanding background-task count and emit second-resolution
// timestamps).
func ResolveHookEmitBinary() string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), HookEmitBinaryName)
		if info, err := os.Stat(candidate); err == nil && info.Mode()&0111 != 0 {
			return candidate
		}
	}
	if path, err := exec.LookPath(HookEmitBinaryName); err == nil {
		return path
	}
	return ""
}

// stripMedusaHookRules removes every Medusa-injected rule from all hook event
// arrays so re-injection replaces rules instead of accumulating duplicates —
// including rules written by older versions with a different command format.
// Foreign rules (e.g. compound approve, user-defined hooks) are preserved.
func stripMedusaHookRules(hooks map[string]any) {
	for event, v := range hooks {
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, entry := range arr {
			if m, ok := entry.(map[string]any); ok && hookRuleHasCommandPrefix(m, medusaHookCommandPrefix) {
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

// hookRuleHasCommand returns true if a hook rule entry contains the given command.
func hookRuleHasCommand(rule map[string]any, cmd string) bool {
	innerHooks, _ := rule["hooks"].([]any)
	for _, h := range innerHooks {
		if hm, ok := h.(map[string]any); ok {
			if c, _ := hm["command"].(string); c == cmd {
				return true
			}
		}
	}
	return false
}

// hookRuleHasCommandPrefix returns true if any command in the rule starts with prefix.
func hookRuleHasCommandPrefix(rule map[string]any, prefix string) bool {
	innerHooks, _ := rule["hooks"].([]any)
	for _, h := range innerHooks {
		if hm, ok := h.(map[string]any); ok {
			if c, _ := hm["command"].(string); strings.HasPrefix(c, prefix) {
				return true
			}
		}
	}
	return false
}

// hookRuleHasCommandSuffix returns true if any command in the rule ends with suffix.
func hookRuleHasCommandSuffix(rule map[string]any, suffix string) bool {
	innerHooks, _ := rule["hooks"].([]any)
	for _, h := range innerHooks {
		if hm, ok := h.(map[string]any); ok {
			if c, _ := hm["command"].(string); strings.HasSuffix(c, suffix) {
				return true
			}
		}
	}
	return false
}

// hookCommandBuilder builds the shell command for one hook event, in either
// binary mode (medusa-hook-emit) or legacy shell fallback mode.
type hookCommandBuilder struct {
	sock    string
	emitBin string
}

// command returns the hook command for an event. Binary mode delegates all
// payload parsing to medusa-hook-emit: the binary extracts messages, session
// ids, and the outstanding background-task count from stdin with a real JSON
// parser and emits nanosecond timestamps. The shell guard keeps two
// properties: non-Medusa sessions are silent no-ops (the env check), and a
// deleted/moved binary degrades to a no-op instead of a failing hook surfacing
// errors inside Claude sessions (the -x check). The guard prefix is also what
// stripMedusaHookRules keys on across all Medusa versions.
func (b hookCommandBuilder) command(eventName string) string {
	if b.emitBin != "" {
		return medusaHookCommandPrefix + ` ] && [ -x "` + b.emitBin + `" ]; then "` + b.emitBin +
			`" -event ` + eventName + ` -socket "` + b.sock + `"; fi`
	}
	switch eventName {
	case "SessionStart":
		return b.legacySessionStartCommand()
	case "NotificationIdle", "NotificationPermission", "NotificationElicitation":
		return b.legacyNotificationCommand(eventName)
	default:
		return b.legacyCommand(eventName)
	}
}

// deliver emits the printf(1) payload to the socket; the socket-exists guard
// makes it a silent no-op while Medusa is stopped. Legacy fallback only.
//
// Known limitation: nc variants without -U support (GNU netcat; rare as a
// system default) drop events silently, as does any system without nc.
func (b hookCommandBuilder) deliver(format, args string) string {
	payload := `printf '` + format + `\n' ` + args
	return `if [ -S "` + b.sock + `" ]; then ` + payload + ` | nc -U -w 2 "` + b.sock + `" >/dev/null 2>&1 || true; fi`
}

// legacyStamp computes a sub-second timestamp where date supports %N and trims
// to digits so an unsupported %N (macOS) degrades to plain seconds.
const legacyStamp = `TS=$(date +%s%N); TS=${TS%%[!0-9]*}; `

func (b hookCommandBuilder) legacyCommand(eventName string) string {
	return medusaHookCommandPrefix + ` ]; then ` + legacyStamp +
		b.deliver(`{"event":"`+eventName+`","ts":%s,"session":"%s"}`, `"$TS" "$MEDUSA_SESSION_NAME"`) + `; fi`
}

// legacyNotificationCommand reads stdin (JSON from Claude Code) to extract the
// "message" field and include it in the event payload.
func (b hookCommandBuilder) legacyNotificationCommand(eventName string) string {
	return medusaHookCommandPrefix + ` ]; then INPUT=$(cat); MSG=$(echo "$INPUT" | grep -o '"message":"[^"]*"' | head -1 | sed 's/"message":"//;s/"$//'); ` + legacyStamp +
		b.deliver(`{"event":"`+eventName+`","ts":%s,"session":"%s","message":"%s"}`, `"$TS" "$MEDUSA_SESSION_NAME" "$MSG"`) + `; fi`
}

// legacySessionStartCommand reads stdin to extract Claude Code's live
// session_id (and agent_type, which is set only for `claude --agent`
// sessions). The app refreshes the tab's persisted id from this so a later
// restart resumes the current conversation after a /clear or in-session
// /resume mints a new id, rather than the original one.
func (b hookCommandBuilder) legacySessionStartCommand() string {
	return medusaHookCommandPrefix + ` ]; then INPUT=$(cat); SID=$(echo "$INPUT" | grep -o '"session_id":"[^"]*"' | head -1 | sed 's/"session_id":"//;s/"$//'); AT=$(echo "$INPUT" | grep -o '"agent_type":"[^"]*"' | head -1 | sed 's/"agent_type":"//;s/"$//'); ` + legacyStamp +
		b.deliver(`{"event":"SessionStart","ts":%s,"session":"%s","claude_session_id":"%s","agent_type":"%s"}`, `"$TS" "$MEDUSA_SESSION_NAME" "$SID" "$AT"`) + `; fi`
}

// InjectHooks merges Claude Code hook definitions into a profile's
// settings.json. Each hook forwards one JSON event line to the Medusa hooks
// socket — via the medusa-hook-emit binary when emitBin is non-empty, or via a
// legacy printf|nc shell pipeline otherwise. The session name travels in the
// payload, so concurrent hooks (parallel tool calls, subagents) can never
// overwrite each other, and the shell guard ensures non-Medusa sessions are
// no-ops. All previously injected Medusa rules (any format, any version) are
// replaced; foreign hook entries (e.g. compound approve) are preserved.
func InjectHooks(profileDir, hooksDir, emitBin string) error {
	builder := hookCommandBuilder{sock: hookspkg.SocketPath(hooksDir), emitBin: emitBin}
	return readModifyWriteJSON(filepath.Join(profileDir, "settings.json"), func(settings map[string]any) {
		hooks := getOrCreateMap(settings, "hooks")
		stripMedusaHookRules(hooks)

		makeRule := func(eventName, matcher string) map[string]any {
			rule := map[string]any{
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": builder.command(eventName),
						"timeout": 5000,
					},
				},
			}
			if matcher != "" {
				rule["matcher"] = matcher
			}
			return rule
		}

		for _, event := range []string{
			"Stop", "StopFailure", "SubagentStart", "SubagentStop",
			"SessionStart", "PreToolUse", "PostToolUse", "PermissionRequest",
			"UserPromptSubmit",
		} {
			existing, _ := hooks[event].([]any)
			hooks[event] = append(existing, makeRule(event, ""))
		}

		// Split Notification into sub-matchers so written JSON distinguishes
		// idle_prompt from permission_prompt. Non-medusa notification entries
		// survived stripMedusaHookRules.
		notificationRules, _ := hooks["Notification"].([]any)
		for _, def := range []struct{ event, matcher string }{
			{"NotificationIdle", "idle_prompt"},
			{"NotificationPermission", "permission_prompt"},
			{"NotificationElicitation", "elicitation_dialog"},
		} {
			notificationRules = append(notificationRules, makeRule(def.event, def.matcher))
		}
		hooks["Notification"] = notificationRules

		settings["hooks"] = hooks
	})
}

// InjectHooksIntoAllProfiles iterates all profile directories and merges
// hook definitions into each one's settings.json.
func InjectHooksIntoAllProfiles(profilesRoot, hooksDir, emitBin string) error {
	return forEachProfile(profilesRoot, func(profileDir string) error {
		return InjectHooks(profileDir, hooksDir, emitBin)
	})
}
