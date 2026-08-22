package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
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
			return func() { _ = os.Remove(lockDir) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("failed to acquire lock %s: %w", lockDir, err)
		}
		if time.Now().After(deadline) {
			// Stale lock — force remove and retry once
			_ = os.Remove(lockDir)
			if err := os.Mkdir(lockDir, 0700); err != nil {
				return nil, fmt.Errorf("failed to acquire lock %s: %w", lockDir, err)
			}
			return func() { _ = os.Remove(lockDir) }, nil
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

// getOrCreateMap extracts or initializes a sub-map from settings.
func getOrCreateMap(settings map[string]any, key string) map[string]any {
	m, _ := settings[key].(map[string]any)
	if m == nil {
		m = make(map[string]any)
	}
	return m
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
