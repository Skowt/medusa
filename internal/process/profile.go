package process

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ProfileConfigFilename is the optional per-profile script config, read from
// ~/.medusa/profiles/<name>/.
const ProfileConfigFilename = "medusa-profile.json"

// ProfileConfig holds the scripts and env a profile contributes to every
// workspace launched with it — e.g. a per-workspace database container started
// on WORKSPACE_PORT at creation and removed when the workspace is deleted.
type ProfileConfig struct {
	Setup    []string          `json:"setup"`
	Teardown []string          `json:"teardown"`
	Env      map[string]string `json:"env"`
}

// LoadProfileConfig reads the named profile's medusa-profile.json. A missing
// file, an empty profile name, or an unset profiles root is an empty config.
func LoadProfileConfig(profilesRoot, profile string) (*ProfileConfig, error) {
	if profilesRoot == "" || profile == "" {
		return &ProfileConfig{}, nil
	}
	path := filepath.Join(profilesRoot, profile, ProfileConfigFilename)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &ProfileConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg ProfileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, nil
}

// ExpandEnv resolves each env value with os.Expand against base (the
// workspace env), falling back to the process environment so values like
// $HOME still resolve. Entries are returned as KEY=VALUE sorted by key, so the
// result is deterministic.
func (c *ProfileConfig) ExpandEnv(base map[string]string) []string {
	if c == nil || len(c.Env) == 0 {
		return nil
	}
	lookup := func(name string) string {
		if v, ok := base[name]; ok {
			return v
		}
		return os.Getenv(name)
	}
	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+os.Expand(c.Env[k], lookup))
	}
	return out
}
