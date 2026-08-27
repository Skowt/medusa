package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultUISettingsTmuxPersistence(t *testing.T) {
	settings := defaultUISettings()
	if !settings.TmuxPersistence {
		t.Fatal("TmuxPersistence should default to true")
	}
}

func TestLoadUISettingsDefaultsTmuxPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	settings := loadUISettings(path)
	if !settings.TmuxPersistence {
		t.Fatal("TmuxPersistence should default to true when missing from config")
	}
}

func TestSaveLoadUISettingsTmuxPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	settings := defaultUISettings()
	settings.TmuxPersistence = false

	if err := saveUISettings(path, settings); err != nil {
		t.Fatalf("saveUISettings failed: %v", err)
	}

	loaded := loadUISettings(path)
	if loaded.TmuxPersistence {
		t.Fatal("TmuxPersistence should persist false value")
	}
}

func TestSaveLoadUISettingsCollapsedGroups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	settings := defaultUISettings()
	settings.CollapsedGroups = map[string]bool{
		"shipping-q2": true,
		"":            true, // Ungrouped
	}
	if err := saveUISettings(path, settings); err != nil {
		t.Fatalf("saveUISettings: %v", err)
	}

	loaded := loadUISettings(path)
	if !loaded.CollapsedGroups["shipping-q2"] {
		t.Errorf("expected shipping-q2 collapsed after reload")
	}
	if !loaded.CollapsedGroups[""] {
		t.Errorf("expected ungrouped (empty key) collapsed after reload")
	}
}

func TestUISettingsFullscreenDefaultsOn(t *testing.T) {
	if !defaultUISettings().LastFullscreen {
		t.Error("LastFullscreen should default to on")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if !loadUISettings(path).LastFullscreen {
		t.Error("LastFullscreen should default to on when missing from config")
	}
}

// Unticking the box must stick: a stored false is a user decision, not the
// absence of one.
func TestUISettingsFullscreenRespectsStoredFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	in := defaultUISettings()
	in.LastFullscreen = false // user unticks the box; the dialog saves immediately
	if err := saveUISettings(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}

	if loadUISettings(path).LastFullscreen {
		t.Error("a stored false must survive a reload")
	}
}

func TestUISettingsFullscreenRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	in := defaultUISettings()
	in.LastFullscreen = true
	if err := saveUISettings(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}

	if !loadUISettings(path).LastFullscreen {
		t.Error("LastFullscreen should persist true value")
	}
}

func TestUISettingsIDERoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	in := defaultUISettings()
	in.IDE = "/Applications/Cursor.app"
	if err := saveUISettings(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := loadUISettings(path)
	if got.IDE != "/Applications/Cursor.app" {
		t.Errorf("IDE = %q, want %q", got.IDE, "/Applications/Cursor.app")
	}
}

// TestUISettingsIDEAlwaysOpenRoundTrip guards the half of the IDE preference
// that decides whether the picker opens at all. Losing it on reload turns
// "don't ask again" into "don't ask again until you restart medusa".
func TestUISettingsIDEAlwaysOpenRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	in := defaultUISettings()
	in.IDE = "/Applications/Cursor.app"
	in.IDEAlwaysOpen = true
	if err := saveUISettings(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !loadUISettings(path).IDEAlwaysOpen {
		t.Error("IDEAlwaysOpen did not survive a save/load round trip")
	}

	in.IDEAlwaysOpen = false
	if err := saveUISettings(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	if loadUISettings(path).IDEAlwaysOpen {
		t.Error("turning the preference back off did not persist")
	}
}

// TestUISettingsIDEAlwaysOpenDefaultsOff keeps the picker as the default for
// every config written before this setting existed.
func TestUISettingsIDEAlwaysOpenDefaultsOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"ui":{"ide":"/Applications/Cursor.app"}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if loadUISettings(path).IDEAlwaysOpen {
		t.Error("a config with no ide_always_open key must still show the picker")
	}
}

func TestUISettingsLastWorkspaceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	const wsID = "a1b2c3d4e5f6"
	in := defaultUISettings()
	in.LastWorkspace = wsID
	if err := saveUISettings(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := loadUISettings(path)
	if got.LastWorkspace != wsID {
		t.Errorf("LastWorkspace = %q, want %q", got.LastWorkspace, wsID)
	}
}

func TestSaveLoadUISettingsGroupOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	settings := defaultUISettings()
	settings.GroupOrder = []string{"shipping-q2", "", "infra"}
	if err := saveUISettings(path, settings); err != nil {
		t.Fatalf("saveUISettings: %v", err)
	}

	loaded := loadUISettings(path)
	want := []string{"shipping-q2", "", "infra"}
	if len(loaded.GroupOrder) != len(want) {
		t.Fatalf("GroupOrder = %v, want %v", loaded.GroupOrder, want)
	}
	for i := range want {
		if loaded.GroupOrder[i] != want[i] {
			t.Fatalf("GroupOrder = %v, want %v (the empty Ungrouped key must survive)", loaded.GroupOrder, want)
		}
	}
}

func TestUISettingsGroupOrderDefaultsEmpty(t *testing.T) {
	if got := defaultUISettings().GroupOrder; got != nil {
		t.Errorf("GroupOrder default = %v, want nil so the dashboard keeps its alphabetical fallback", got)
	}
}

func TestUISettingsCreateWorktreeDefaultsOn(t *testing.T) {
	if !defaultUISettings().LastCreateWorktree {
		t.Error("LastCreateWorktree should default to on")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if !loadUISettings(path).LastCreateWorktree {
		t.Error("LastCreateWorktree should default to on when missing from config")
	}
}

// Unticking the worktree box must stick, for the same reason the fullscreen one
// does: a stored false is a decision, not the absence of one.
func TestUISettingsCreateWorktreeRespectsStoredFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	in := defaultUISettings()
	in.LastCreateWorktree = false
	if err := saveUISettings(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}

	if loadUISettings(path).LastCreateWorktree {
		t.Error("a stored false must survive a reload")
	}
}
