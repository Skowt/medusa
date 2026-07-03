package config

import (
	"path/filepath"
	"strings"

	hookspkg "github.com/Skowt/medusa/internal/hooks"
)

// medusaHookCommandPrefix identifies commands injected by Medusa across all
// versions: every Medusa hook command starts with this session-name guard.
const medusaHookCommandPrefix = `if [ -n "$MEDUSA_SESSION_NAME"`

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

// InjectHooks merges Claude Code hook definitions into a profile's settings.json.
// Each hook sends one JSON event line to the Medusa hooks socket via nc. The
// socket-exists guard makes hooks a silent no-op while Medusa is stopped, so
// detached tmux sessions never accumulate event litter, and the socket is the
// only transport — there is no file fallback. The session name travels in the
// payload, so concurrent hooks (parallel tool calls, subagents) can never
// overwrite each other. The shell guard ensures non-Medusa sessions are no-ops.
// All previously injected Medusa rules (including old-format ones) are replaced;
// foreign hook entries (e.g. compound approve) are preserved.
//
// The timestamp is emitted as `date +%s%N` and trimmed to digits: nanoseconds
// where date supports %N, seconds where it does not (the literal N is stripped).
// The receiver normalizes either magnitude, so the resolution degrades
// gracefully without breaking ordering.
//
// Known limitation: nc variants without -U support (GNU netcat; rare as a
// system default) drop events silently, as does any system without nc.
func InjectHooks(profileDir, hooksDir string) error {
	// Resolved before the closure: the local `hooks` map below shadows the
	// hooks package name.
	sock := hookspkg.SocketPath(hooksDir)
	return readModifyWriteJSON(filepath.Join(profileDir, "settings.json"), func(settings map[string]any) {
		hooks := getOrCreateMap(settings, "hooks")
		stripMedusaHookRules(hooks)
		// deliver emits the printf(1) payload to the socket; the socket-exists
		// guard makes it a silent no-op while Medusa is stopped.
		deliver := func(format, args string) string {
			payload := `printf '` + format + `\n' ` + args
			return `if [ -S "` + sock + `" ]; then ` + payload + ` | nc -U -w 2 "` + sock + `" >/dev/null 2>&1 || true; fi`
		}

		// stamp computes a sub-second timestamp where date supports %N and
		// trims to digits so an unsupported %N degrades to plain seconds.
		const stamp = `TS=$(date +%s%N); TS=${TS%%[!0-9]*}; `

		makeCommand := func(eventName string) string {
			return `if [ -n "$MEDUSA_SESSION_NAME" ]; then ` + stamp +
				deliver(`{"event":"`+eventName+`","ts":%s,"session":"%s"}`, `"$TS" "$MEDUSA_SESSION_NAME"`) + `; fi`
		}

		// makeNotificationCommand reads stdin (JSON from Claude Code) to extract
		// the "message" field and include it in the event payload.
		makeNotificationCommand := func(eventName string) string {
			return `if [ -n "$MEDUSA_SESSION_NAME" ]; then INPUT=$(cat); MSG=$(echo "$INPUT" | grep -o '"message":"[^"]*"' | head -1 | sed 's/"message":"//;s/"$//'); ` + stamp +
				deliver(`{"event":"`+eventName+`","ts":%s,"session":"%s","message":"%s"}`, `"$TS" "$MEDUSA_SESSION_NAME" "$MSG"`) + `; fi`
		}

		// makeSubagentStopCommand reads stdin to extract pending_subagent_count
		// (how many other subagents are still running) so the app can tell a
		// mid-run SubagentStop from the last one. Emits -1 when the field is
		// absent (older Claude Code versions).
		makeSubagentStopCommand := func() string {
			return `if [ -n "$MEDUSA_SESSION_NAME" ]; then INPUT=$(cat); PENDING=$(echo "$INPUT" | grep -o '"pending_subagent_count":[0-9]*' | head -1 | grep -o '[0-9]*$'); ` + stamp +
				deliver(`{"event":"SubagentStop","ts":%s,"session":"%s","pending":%s}`, `"$TS" "$MEDUSA_SESSION_NAME" "${PENDING:--1}"`) + `; fi`
		}

		// makeSessionStartCommand reads stdin to extract Claude Code's live
		// session_id (and agent_type, which is set only for `claude --agent`
		// sessions). The app refreshes the tab's persisted id from this so a
		// later restart resumes the current conversation after a /clear or
		// in-session /resume mints a new id, rather than the original one.
		makeSessionStartCommand := func() string {
			return `if [ -n "$MEDUSA_SESSION_NAME" ]; then INPUT=$(cat); SID=$(echo "$INPUT" | grep -o '"session_id":"[^"]*"' | head -1 | sed 's/"session_id":"//;s/"$//'); AT=$(echo "$INPUT" | grep -o '"agent_type":"[^"]*"' | head -1 | sed 's/"agent_type":"//;s/"$//'); ` + stamp +
				deliver(`{"event":"SessionStart","ts":%s,"session":"%s","claude_session_id":"%s","agent_type":"%s"}`, `"$TS" "$MEDUSA_SESSION_NAME" "$SID" "$AT"`) + `; fi`
		}

		type hookDef struct {
			event   string
			matcher string
		}
		defs := []hookDef{
			{event: "Stop"},
			{event: "StopFailure"},
			{event: "SubagentStart"},
			{event: "SubagentStop"},
			{event: "SessionStart"},
			{event: "PreToolUse"},
			{event: "PostToolUse"},
			{event: "PermissionRequest"},
			{event: "UserPromptSubmit"},
		}

		for _, def := range defs {
			cmd := makeCommand(def.event)
			switch def.event {
			case "SubagentStop":
				cmd = makeSubagentStopCommand()
			case "SessionStart":
				cmd = makeSessionStartCommand()
			}
			rule := map[string]any{
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": cmd,
						"timeout": 5000,
					},
				},
			}
			if def.matcher != "" {
				rule["matcher"] = def.matcher
			}
			existing, _ := hooks[def.event].([]any)
			hooks[def.event] = append(existing, rule)
		}

		// Split Notification into sub-matchers so written JSON
		// distinguishes idle_prompt from permission_prompt.
		notificationDefs := []hookDef{
			{event: "NotificationIdle", matcher: "idle_prompt"},
			{event: "NotificationPermission", matcher: "permission_prompt"},
			{event: "NotificationElicitation", matcher: "elicitation_dialog"},
		}
		// Non-medusa notification entries survived stripMedusaHookRules.
		notificationRules, _ := hooks["Notification"].([]any)
		for _, def := range notificationDefs {
			cmd := makeNotificationCommand(def.event)
			rule := map[string]any{
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": cmd,
						"timeout": 5000,
					},
				},
				"matcher": def.matcher,
			}
			notificationRules = append(notificationRules, rule)
		}
		hooks["Notification"] = notificationRules

		settings["hooks"] = hooks
	})
}

// InjectHooksIntoAllProfiles iterates all profile directories and merges
// hook definitions into each one's settings.json.
func InjectHooksIntoAllProfiles(profilesRoot, hooksDir string) error {
	return forEachProfile(profilesRoot, func(profileDir string) error {
		return InjectHooks(profileDir, hooksDir)
	})
}
