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
// Bash permission prefixes.
func LoadPermissions(gitRoot string) *Permissions {
	var files []string

	// Global settings: either CLAUDE_CONFIG_DIR or ~/.claude
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			configDir = filepath.Join(home, ".claude")
		}
	}
	if configDir != "" {
		files = append(files,
			filepath.Join(configDir, "settings.json"),
			filepath.Join(configDir, "settings.local.json"),
		)
	}

	// Project settings
	if gitRoot != "" {
		files = append(files,
			filepath.Join(gitRoot, ".claude", "settings.json"),
			filepath.Join(gitRoot, ".claude", "settings.local.json"),
		)
	}

	perms := &Permissions{}
	seen := make(map[string]bool)
	for _, f := range files {
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
func FindGitRoot() string {
	toplevel, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	gitDir, err := exec.Command("git", "rev-parse", "--git-dir").Output()
	if err != nil {
		return strings.TrimSpace(string(toplevel))
	}
	commonDir, err := exec.Command("git", "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return strings.TrimSpace(string(toplevel))
	}
	gd := strings.TrimSpace(string(gitDir))
	cd := strings.TrimSpace(string(commonDir))
	if gd != cd {
		return filepath.Dir(cd)
	}
	return strings.TrimSpace(string(toplevel))
}
