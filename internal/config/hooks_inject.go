package config

import "strings"

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
