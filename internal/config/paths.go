package config

import (
	"os"
	"path/filepath"
)

// Paths holds all the file system paths used by the application
type Paths struct {
	Home                  string // ~/.medusa
	WorkspacesRoot        string // ~/.medusa/workspaces
	RegistryPath          string // ~/.medusa/workspaces.json
	MetadataRoot          string // ~/.medusa/workspaces-metadata
	RecentsPath           string // ~/.medusa/recents.json
	ConfigPath            string // ~/.medusa/config.json
	ProfilesRoot          string // ~/.medusa/profiles
	SharedProfileRoot     string // ~/.medusa/profiles/shared
	GlobalPermissionsPath string // ~/.medusa/global_permissions.json
	HooksDir              string // ~/.medusa/hooks
}

// MedusaHome returns the base medusa directory. It respects the MEDUSA_HOME
// environment variable, falling back to ~/.medusa.
func MedusaHome() (string, error) {
	if env := os.Getenv("MEDUSA_HOME"); env != "" {
		return filepath.Abs(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".medusa"), nil
}

// DefaultPaths returns the default paths configuration
func DefaultPaths() (*Paths, error) {
	medusaHome, err := MedusaHome()
	if err != nil {
		return nil, err
	}

	profilesRoot := filepath.Join(medusaHome, "profiles")
	workspacesRoot := filepath.Join(medusaHome, "workspaces")
	return &Paths{
		Home:                  medusaHome,
		WorkspacesRoot:        workspacesRoot,
		RegistryPath:          filepath.Join(medusaHome, "workspaces.json"),
		MetadataRoot:          filepath.Join(medusaHome, "workspaces-metadata"),
		RecentsPath:           filepath.Join(medusaHome, "recents.json"),
		ConfigPath:            filepath.Join(medusaHome, "config.json"),
		ProfilesRoot:          profilesRoot,
		SharedProfileRoot:     filepath.Join(profilesRoot, "shared"),
		GlobalPermissionsPath: filepath.Join(medusaHome, "global_permissions.json"),
		HooksDir:              filepath.Join(medusaHome, "hooks"),
	}, nil
}

// EnsureDirectories creates all required directories if they don't exist
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.Home,
		p.WorkspacesRoot,
		p.MetadataRoot,
		p.ProfilesRoot,
		filepath.Join(p.SharedProfileRoot, "skills"),
		filepath.Join(p.SharedProfileRoot, "plugins"),
		p.HooksDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}
