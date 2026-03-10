package config

import "strings"

// upsertHookRule appends rule to the existing hook event array, replacing any
// entry whose command matches cmd to avoid duplicates.
func upsertHookRule(existing any, rule map[string]any, cmd string) []any {
	arr, _ := existing.([]any)
	var result []any
	for _, entry := range arr {
		m, ok := entry.(map[string]any)
		if !ok {
			result = append(result, entry)
			continue
		}
		if hookRuleHasCommand(m, cmd) {
			continue // Will be replaced by the new rule
		}
		result = append(result, entry)
	}
	return append(result, rule)
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
