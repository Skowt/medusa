package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// StripAllowEdits removes any Edit(**) entry from a workspace's
// .claude/settings.local.json. Cleanup for the legacy "Immediately allow
// edits" feature: a stale entry from a prior agent run must not silently
// pre-grant Edit on subsequent launches that didn't ask for it.
//
// No-op if the file is absent or the entry is not present. Unlike the
// Inject* helpers, this does NOT create the settings file.
func StripAllowEdits(workspaceRoot string) error {
	path := filepath.Join(workspaceRoot, ".claude", "settings.local.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var settings map[string]any
	if jsonErr := json.Unmarshal(raw, &settings); jsonErr != nil {
		return fmt.Errorf("corrupt JSON in %s: %w", path, jsonErr)
	}
	perms, _ := settings["permissions"].(map[string]any)
	if perms == nil {
		return nil
	}
	allow := toStringSlice(perms["allow"])
	filtered := make([]string, 0, len(allow))
	changed := false
	for _, e := range allow {
		if e == "Edit(**)" {
			changed = true
			continue
		}
		filtered = append(filtered, e)
	}
	if !changed {
		return nil
	}
	perms["allow"] = filtered
	settings["permissions"] = perms
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0644)
}
