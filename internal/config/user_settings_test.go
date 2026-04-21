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
