package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// readModifyWriteJSON reads a JSON file into a map, applies a modifier function,
// and writes it back. Creates the file and parent directories if they don't exist.
func readModifyWriteJSON(path string, modifier func(map[string]any)) error {
	var settings map[string]any
	if existing, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(existing, &settings)
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
	return os.WriteFile(path, data, 0644)
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
func InjectTrustedDirectory(workspaceRoot string, configDir string) error {
	var claudeConfigPath string
	if configDir != "" {
		claudeConfigPath = filepath.Join(configDir, ".claude.json")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		claudeConfigPath = filepath.Join(home, ".claude.json")
	}

	var config map[string]any
	if existing, err := os.ReadFile(claudeConfigPath); err == nil {
		_ = json.Unmarshal(existing, &config)
	}
	if config == nil {
		config = make(map[string]any)
	}

	// Get or create the projects map
	projects, _ := config["projects"].(map[string]any)
	if projects == nil {
		projects = make(map[string]any)
	}

	// Get or create the project entry for this workspace
	projectEntry, _ := projects[workspaceRoot].(map[string]any)
	if projectEntry == nil {
		projectEntry = map[string]any{
			"allowedTools":            []any{},
			"mcpContextUris":          []any{},
			"mcpServers":              map[string]any{},
			"enabledMcpjsonServers":   []any{},
			"disabledMcpjsonServers":  []any{},
			"hasTrustDialogAccepted":  true,
		}
	} else {
		// Update existing entry to mark as trusted
		projectEntry["hasTrustDialogAccepted"] = true
	}

	projects[workspaceRoot] = projectEntry
	config["projects"] = projects

	// Ensure config directory exists
	if configDir != "" {
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(claudeConfigPath, data, 0600)
}

// InjectIntoAllProfiles iterates all profile directories and merges global
// permissions into each one's settings.json.
func InjectIntoAllProfiles(profilesRoot string, global *GlobalPermissions) error {
	if global == nil || (len(global.Allow) == 0 && len(global.Deny) == 0) {
		return nil
	}
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
		profileDir := filepath.Join(profilesRoot, entry.Name())
		if err := InjectGlobalPermissions(profileDir, global); err != nil {
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
