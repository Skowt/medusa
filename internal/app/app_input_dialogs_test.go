package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/ui/common"
)

// TestDialogSetProfileForCreate_UpdatesLastProfile verifies that selecting a
// profile during workspace creation updates LastProfile in UI settings, so the
// profile picker shows the most recently chosen profile at the top next time.
func TestDialogSetProfileForCreate_UpdatesLastProfile(t *testing.T) {
	tmp := normalizePath(t.TempDir())
	configPath := filepath.Join(tmp, "config.json")
	profilesRoot := filepath.Join(tmp, "profiles")

	// Create two profile directories so listProfiles returns them.
	for _, name := range []string{"Default", "Work"} {
		if err := os.MkdirAll(filepath.Join(profilesRoot, name), 0o755); err != nil {
			t.Fatalf("mkdir profile %s: %v", name, err)
		}
	}

	cfg := &config.Config{
		Paths: &config.Paths{
			ProfilesRoot: profilesRoot,
			ConfigPath:   configPath,
		},
		UI: config.UISettings{},
	}
	cfg.UI.LastProfile = "Work" // Simulate "Work" was previously chosen

	app := &App{
		config: cfg,
		// Set up dialogWorkspace with a repo so the handler processes it.
		dialogWorkspace: &data.Workspace{
			Repos: []data.RepoRef{{Path: "/tmp/repo", Name: "repo"}},
		},
	}

	// Simulate the user selecting "Default" from the profile picker.
	app.handleDialogResult(common.DialogResult{
		ID:        DialogSetProfileForCreate,
		Confirmed: true,
		Value:     "Default",
	})

	// LastProfile should now be "Default".
	if app.config.UI.LastProfile != "Default" {
		t.Errorf("LastProfile = %q, want %q", app.config.UI.LastProfile, "Default")
	}

	// Verify it was persisted to disk.
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var saved struct {
		UI struct {
			LastProfile string `json:"last_profile"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if saved.UI.LastProfile != "Default" {
		t.Errorf("persisted last_profile = %q, want %q", saved.UI.LastProfile, "Default")
	}
}

// TestDialogSetProfile_SendsSetWorkspaceProfile verifies the existing-workspace
// profile picker produces a SetWorkspaceProfile message with the selected value.
func TestDialogSetProfile_SendsSetWorkspaceProfile(t *testing.T) {
	tmp := normalizePath(t.TempDir())
	profilesRoot := filepath.Join(tmp, "profiles")

	for _, name := range []string{"Default", "Work"} {
		if err := os.MkdirAll(filepath.Join(profilesRoot, name), 0o755); err != nil {
			t.Fatalf("mkdir profile %s: %v", name, err)
		}
	}

	ws := &data.Workspace{
		Repos: []data.RepoRef{{Path: "/tmp/repo", Name: "repo"}},
	}

	app := &App{
		config: &config.Config{
			Paths: &config.Paths{ProfilesRoot: profilesRoot},
			UI:    config.UISettings{},
		},
		dialogWorkspace: ws,
	}

	cmd := app.handleDialogResult(common.DialogResult{
		ID:        DialogSetProfile,
		Confirmed: true,
		Value:     "Default",
	})

	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	msg := cmd()
	setMsg, ok := msg.(messages.SetWorkspaceProfile)
	if !ok {
		t.Fatalf("expected SetWorkspaceProfile, got %T", msg)
	}
	if setMsg.Profile != "Default" {
		t.Errorf("Profile = %q, want %q", setMsg.Profile, "Default")
	}
}

// TestListProfiles_LastProfileFirst verifies that listProfiles puts the
// LastProfile at the top of the returned list.
func TestListProfiles_LastProfileFirst(t *testing.T) {
	tmp := normalizePath(t.TempDir())
	profilesRoot := filepath.Join(tmp, "profiles")

	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		if err := os.MkdirAll(filepath.Join(profilesRoot, name), 0o755); err != nil {
			t.Fatalf("mkdir profile %s: %v", name, err)
		}
	}

	cfg := &config.Config{
		Paths: &config.Paths{
			ProfilesRoot: profilesRoot,
		},
		UI: config.UISettings{},
	}
	cfg.UI.LastProfile = "Gamma"

	app := &App{config: cfg}
	profiles := app.listProfiles()

	if len(profiles) != 3 {
		t.Fatalf("got %d profiles, want 3", len(profiles))
	}
	if profiles[0] != "Gamma" {
		t.Errorf("profiles[0] = %q, want %q", profiles[0], "Gamma")
	}
}
