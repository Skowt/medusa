package approve

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Permissions holds the parsed allow and deny command prefixes.
type Permissions struct {
	Allow []string
	Deny  []string
}

// LoadPermissions reads Claude Code settings from all layers and extracts
// Bash permission prefixes. Reads global settings first; only spawns git
// to find the project root if needed.
func LoadPermissions(gitRoot string) *Permissions {
	perms := &Permissions{}
	seen := make(map[string]bool)

	addPerms := func(f string) {
		allow, deny := readSettingsPermissions(f)
		for _, p := range allow {
			if !seen["a:"+p] {
				seen["a:"+p] = true
				perms.Allow = append(perms.Allow, p)
			}
		}
		for _, p := range deny {
			if !seen["d:"+p] {
				seen["d:"+p] = true
				perms.Deny = append(perms.Deny, p)
			}
		}
	}

	// Global settings: either CLAUDE_CONFIG_DIR or ~/.claude
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			configDir = filepath.Join(home, ".claude")
		}
	}
	if configDir != "" {
		addPerms(filepath.Join(configDir, "settings.json"))
		addPerms(filepath.Join(configDir, "settings.local.json"))
	}

	// Project settings
	if gitRoot != "" {
		addPerms(filepath.Join(gitRoot, ".claude", "settings.json"))
		addPerms(filepath.Join(gitRoot, ".claude", "settings.local.json"))
	}

	return perms
}

// readSettingsPermissions reads a single settings.json and extracts Bash
// command prefixes from the permissions.allow and permissions.deny arrays.
func readSettingsPermissions(path string) (allow, deny []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, nil
	}
	for _, p := range settings.Permissions.Allow {
		if prefix, ok := ParseBashPrefix(p); ok {
			allow = append(allow, prefix)
		}
	}
	for _, p := range settings.Permissions.Deny {
		if prefix, ok := ParseBashPrefix(p); ok {
			deny = append(deny, prefix)
		}
	}
	return allow, deny
}

// FindGitRoot returns the top-level directory of the git repository, handling
// worktrees by resolving --git-common-dir. Returns "" if not in a git repo.
// Uses a single git process for all three queries.
func FindGitRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel", "--git-dir", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		if len(lines) > 0 {
			return strings.TrimSpace(lines[0])
		}
		return ""
	}
	toplevel := strings.TrimSpace(lines[0])
	gitDir := strings.TrimSpace(lines[1])
	commonDir := strings.TrimSpace(lines[2])
	if gitDir != commonDir {
		return filepath.Dir(commonDir)
	}
	return toplevel
}
