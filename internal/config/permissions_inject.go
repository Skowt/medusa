package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// atomicWriteFile writes data to a temporary file then renames it to path,
// ensuring the target is never partially written.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		if !success {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	success = true
	return nil
}

// acquireDirLock acquires a directory-based lock (compatible with Claude Code's
// config dir lock). Returns a cleanup function to release the lock.
func acquireDirLock(lockDir string) (func(), error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.Mkdir(lockDir, 0700)
		if err == nil {
			return func() { os.Remove(lockDir) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("failed to acquire lock %s: %w", lockDir, err)
		}
		if time.Now().After(deadline) {
			// Stale lock — force remove and retry once
			os.Remove(lockDir)
			if err := os.Mkdir(lockDir, 0700); err != nil {
				return nil, fmt.Errorf("failed to acquire lock %s: %w", lockDir, err)
			}
			return func() { os.Remove(lockDir) }, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// readModifyWriteJSON reads a JSON file into a map, applies a modifier function,
// and writes it back atomically. Creates the file and parent directories if they
// don't exist.
func readModifyWriteJSON(path string, modifier func(map[string]any)) error {
	var settings map[string]any
	if existing, err := os.ReadFile(path); err == nil {
		if jsonErr := json.Unmarshal(existing, &settings); jsonErr != nil {
			return fmt.Errorf("corrupt JSON in %s: %w", path, jsonErr)
		}
	}
	if settings == nil {
		settings = make(map[string]any)
	}

	modifier(settings)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0644)
}

// getOrCreatePerms extracts or initializes the "permissions" sub-map from settings.
func getOrCreatePerms(settings map[string]any) map[string]any {
	perms, _ := settings["permissions"].(map[string]any)
	if perms == nil {
		perms = make(map[string]any)
	}
	return perms
}

// InjectGlobalPermissions merges global permissions into a profile's settings.json.
// Creates the file if it does not exist.
func InjectGlobalPermissions(profileDir string, global *GlobalPermissions) error {
	if global == nil || (len(global.Allow) == 0 && len(global.Deny) == 0) {
		return nil
	}
	return readModifyWriteJSON(filepath.Join(profileDir, "settings.json"), func(settings map[string]any) {
		perms := getOrCreatePerms(settings)
		perms["allow"] = mergeUnique(toStringSlice(perms["allow"]), global.Allow)
		perms["deny"] = mergeUnique(toStringSlice(perms["deny"]), global.Deny)
		settings["permissions"] = perms
	})
}

// InjectAdditionalDirectories writes additionalDirectories into
// {primaryRoot}/.claude/settings.local.json → permissions.additionalDirectories.
func InjectAdditionalDirectories(primaryRoot string, additionalRoots []string) error {
	if len(additionalRoots) == 0 {
		return nil
	}
	return readModifyWriteJSON(filepath.Join(primaryRoot, ".claude", "settings.local.json"), func(settings map[string]any) {
		perms := getOrCreatePerms(settings)
		dirs := make([]any, len(additionalRoots))
		for i, root := range additionalRoots {
			dirs[i] = root
		}
		perms["additionalDirectories"] = dirs
		settings["permissions"] = perms
	})
}

// InjectAllowEdits adds Edit(**) to a workspace's .claude/settings.local.json.
// This pre-grants the Edit permission for this specific workspace only.
func InjectAllowEdits(workspaceRoot string) error {
	return readModifyWriteJSON(filepath.Join(workspaceRoot, ".claude", "settings.local.json"), func(settings map[string]any) {
		perms := getOrCreatePerms(settings)
		perms["allow"] = mergeUnique(toStringSlice(perms["allow"]), []string{"Edit(**)"})
		settings["permissions"] = perms
	})
}

// InjectSkipPermissionPrompt sets skipDangerousModePermissionPrompt=true
// in the profile's settings.json so Claude Code doesn't show the bypass
// permissions confirmation dialog when --dangerously-skip-permissions is used.
func InjectSkipPermissionPrompt(profileDir string) error {
	return readModifyWriteJSON(filepath.Join(profileDir, "settings.json"), func(settings map[string]any) {
		settings["skipDangerousModePermissionPrompt"] = true
	})
}

// InjectTrustedDirectory adds a directory to Claude's trusted projects.
// If configDir is empty, uses ~/.claude.json. Otherwise uses configDir/.claude.json.
// This prevents the "do you want to trust this directory" prompt when Claude starts.
//
// Uses a directory-based lock (configDir + ".lock") compatible with Claude Code's
// own config lock to prevent concurrent write corruption.
func InjectTrustedDirectory(workspaceRoot string, configDir string) error {
	var claudeConfigPath string
	var lockDir string
	if configDir != "" {
		claudeConfigPath = filepath.Join(configDir, ".claude.json")
		lockDir = configDir + ".lock"
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		claudeConfigPath = filepath.Join(home, ".claude.json")
		lockDir = filepath.Join(home, ".claude.lock")
	}

	// Ensure config directory exists before acquiring lock
	if configDir != "" {
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return err
		}
	}

	// Acquire directory lock shared with Claude Code
	unlock, err := acquireDirLock(lockDir)
	if err != nil {
		return err
	}
	defer unlock()

	var cfg map[string]any
	if existing, err := os.ReadFile(claudeConfigPath); err == nil {
		if jsonErr := json.Unmarshal(existing, &cfg); jsonErr != nil {
			return fmt.Errorf("corrupt JSON in %s: %w", claudeConfigPath, jsonErr)
		}
	}
	if cfg == nil {
		cfg = make(map[string]any)
	}

	// Get or create the projects map
	projects, _ := cfg["projects"].(map[string]any)
	if projects == nil {
		projects = make(map[string]any)
	}

	// Get or create the project entry for this workspace
	projectEntry, _ := projects[workspaceRoot].(map[string]any)
	if projectEntry == nil {
		projectEntry = map[string]any{
			"allowedTools":           []any{},
			"mcpContextUris":         []any{},
			"mcpServers":             map[string]any{},
			"enabledMcpjsonServers":  []any{},
			"disabledMcpjsonServers": []any{},
			"hasTrustDialogAccepted": true,
		}
	} else {
		// Update existing entry to mark as trusted
		projectEntry["hasTrustDialogAccepted"] = true
	}

	projects[workspaceRoot] = projectEntry
	cfg["projects"] = projects

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(claudeConfigPath, data, 0600)
}

// InjectIntoAllProfiles iterates all profile directories and merges global
// permissions into each one's settings.json.
func InjectIntoAllProfiles(profilesRoot string, global *GlobalPermissions) error {
	if global == nil || (len(global.Allow) == 0 && len(global.Deny) == 0) {
		return nil
	}
	return forEachProfile(profilesRoot, func(profileDir string) error {
		return InjectGlobalPermissions(profileDir, global)
	})
}

// getOrCreateMap extracts or initializes a sub-map from settings.
func getOrCreateMap(settings map[string]any, key string) map[string]any {
	m, _ := settings[key].(map[string]any)
	if m == nil {
		m = make(map[string]any)
	}
	return m
}

// InjectHooks merges Claude Code hook definitions into a profile's settings.json.
// Each hook writes a JSON event file to hooksDir so the Medusa watcher can detect
// agent lifecycle transitions. The shell guard ensures non-Medusa sessions are no-ops.
// Existing hook entries (e.g. compound approve) are preserved via append-and-dedup.
func InjectHooks(profileDir, hooksDir string) error {
	return readModifyWriteJSON(filepath.Join(profileDir, "settings.json"), func(settings map[string]any) {
		hooks := getOrCreateMap(settings, "hooks")

		makeCommand := func(eventName string) string {
			return `if [ -n "$MEDUSA_SESSION_NAME" ]; then printf '{"event":"` + eventName + `","ts":%s}\n' "$(date +%s)" > ` + hooksDir + `/"$MEDUSA_SESSION_NAME".json; fi`
		}

		// makeNotificationCommand reads stdin (JSON from Claude Code) to extract
		// the "message" field and include it in the event file.
		makeNotificationCommand := func(eventName string) string {
			return `if [ -n "$MEDUSA_SESSION_NAME" ]; then INPUT=$(cat); MSG=$(echo "$INPUT" | grep -o '"message":"[^"]*"' | head -1 | sed 's/"message":"//;s/"$//'); printf '{"event":"` + eventName + `","ts":%s,"message":"%s"}\n' "$(date +%s)" "$MSG" > ` + hooksDir + `/"$MEDUSA_SESSION_NAME".json; fi`
		}

		type hookDef struct {
			event   string
			matcher string
		}
		defs := []hookDef{
			{event: "Stop"},
			{event: "StopFailure"},
			{event: "SubagentStop"},
			{event: "PreToolUse"},
			{event: "PostToolUse"},
			{event: "PermissionRequest"},
			{event: "UserPromptSubmit"},
		}

		for _, def := range defs {
			cmd := makeCommand(def.event)
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
			hooks[def.event] = upsertHookRule(hooks[def.event], rule, cmd)
		}

		// Split Notification into sub-matchers so written JSON
		// distinguishes idle_prompt from permission_prompt.
		notificationDefs := []hookDef{
			{event: "NotificationIdle", matcher: "idle_prompt"},
			{event: "NotificationPermission", matcher: "permission_prompt"},
			{event: "NotificationElicitation", matcher: "elicitation_dialog"},
		}
		var notificationRules []any
		existing, _ := hooks["Notification"].([]any)
		// Preserve non-medusa notification entries
		for _, e := range existing {
			if m, ok := e.(map[string]any); ok {
				if !hookRuleHasCommandPrefix(m, "if [ -n \"$MEDUSA_SESSION_NAME\"") {
					notificationRules = append(notificationRules, e)
				}
			}
		}
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

// InjectCompoundApproveHook adds the medusa-approve-compound PreToolUse hook
// to a profile's settings.json. The hook auto-approves compound Bash commands
// when every sub-command is individually allowed.
func InjectCompoundApproveHook(profileDir string, hookBinaryPath string) error {
	return readModifyWriteJSON(filepath.Join(profileDir, "settings.json"), func(settings map[string]any) {
		hooks := getOrCreateMap(settings, "hooks")

		hookEntry := map[string]any{
			"matcher": "Bash",
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": hookBinaryPath,
					"timeout": 3000,
				},
			},
		}

		// Replace any existing medusa-approve-compound entry (path may have
		// changed between builds/branches) and dedup by binary name.
		existing, _ := hooks["PreToolUse"].([]any)
		var kept []any
		for _, entry := range existing {
			if m, ok := entry.(map[string]any); ok && hookRuleHasCommandSuffix(m, "medusa-approve-compound") {
				continue // Remove stale entry; will be replaced below
			}
			kept = append(kept, entry)
		}
		hooks["PreToolUse"] = append(kept, hookEntry)
		settings["hooks"] = hooks
	})
}

// RemoveCompoundApproveHook removes the medusa-approve-compound PreToolUse hook
// from a profile's settings.json.
func RemoveCompoundApproveHook(profileDir string, hookBinaryPath string) error {
	settingsPath := filepath.Join(profileDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		return nil
	}
	return readModifyWriteJSON(settingsPath, func(settings map[string]any) {
		hooks, _ := settings["hooks"].(map[string]any)
		if hooks == nil {
			return
		}
		existing, _ := hooks["PreToolUse"].([]any)
		var kept []any
		for _, entry := range existing {
			m, ok := entry.(map[string]any)
			if !ok || !hookRuleHasCommand(m, hookBinaryPath) {
				kept = append(kept, entry)
			}
		}
		if len(kept) > 0 {
			hooks["PreToolUse"] = kept
		} else {
			delete(hooks, "PreToolUse")
		}
		if len(hooks) == 0 {
			delete(settings, "hooks")
		} else {
			settings["hooks"] = hooks
		}
	})
}

// InjectCompoundApproveHookAllProfiles adds the hook to all profiles.
func InjectCompoundApproveHookAllProfiles(profilesRoot, hookBinaryPath string) error {
	return forEachProfile(profilesRoot, func(profileDir string) error {
		return InjectCompoundApproveHook(profileDir, hookBinaryPath)
	})
}

// RemoveCompoundApproveHookAllProfiles removes the hook from all profiles.
func RemoveCompoundApproveHookAllProfiles(profilesRoot, hookBinaryPath string) error {
	return forEachProfile(profilesRoot, func(profileDir string) error {
		return RemoveCompoundApproveHook(profileDir, hookBinaryPath)
	})
}

func forEachProfile(profilesRoot string, fn func(string) error) error {
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "shared" {
			continue
		}
		if err := fn(filepath.Join(profilesRoot, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// mergeUnique merges two string slices, returning a deduplicated result.
// Preserves order: existing entries first, then new entries not in existing.
func mergeUnique(existing, additions []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(existing)+len(additions))

	// Add existing entries (deduplicated)
	for _, s := range existing {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			result = append(result, trimmed)
		}
	}

	// Add new entries not already present
	for _, s := range additions {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			result = append(result, trimmed)
		}
	}

	return result
}
