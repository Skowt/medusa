package config

import (
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
