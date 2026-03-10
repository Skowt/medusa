package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestHookCoexistence verifies that InjectHooks and InjectCompoundApproveHook
// can be called in any order without clobbering each other's entries.
func TestHookCoexistence(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "TestProfile")
	_ = os.MkdirAll(profileDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")
	hookBin := "/usr/local/bin/medusa-approve-compound"

	// Order 1: InjectHooks first, then InjectCompoundApproveHook
	t.Run("monitoring_then_compound", func(t *testing.T) {
		pd := filepath.Join(profileDir, "order1")
		_ = os.MkdirAll(pd, 0755)

		if err := InjectHooks(pd, hooksDir); err != nil {
			t.Fatal(err)
		}
		if err := InjectCompoundApproveHook(pd, hookBin); err != nil {
			t.Fatal(err)
		}
		assertBothHooksPresent(t, pd, hookBin)
	})

	// Order 2: InjectCompoundApproveHook first, then InjectHooks
	t.Run("compound_then_monitoring", func(t *testing.T) {
		pd := filepath.Join(profileDir, "order2")
		_ = os.MkdirAll(pd, 0755)

		if err := InjectCompoundApproveHook(pd, hookBin); err != nil {
			t.Fatal(err)
		}
		if err := InjectHooks(pd, hooksDir); err != nil {
			t.Fatal(err)
		}
		assertBothHooksPresent(t, pd, hookBin)
	})

	// Idempotency: calling both multiple times doesn't create duplicates
	t.Run("idempotent", func(t *testing.T) {
		pd := filepath.Join(profileDir, "idempotent")
		_ = os.MkdirAll(pd, 0755)

		for i := 0; i < 3; i++ {
			if err := InjectHooks(pd, hooksDir); err != nil {
				t.Fatal(err)
			}
			if err := InjectCompoundApproveHook(pd, hookBin); err != nil {
				t.Fatal(err)
			}
		}

		settings := readSettings(t, pd)
		hooks := settings["hooks"].(map[string]any)
		preToolUse := hooks["PreToolUse"].([]any)
		if len(preToolUse) != 2 {
			t.Errorf("expected 2 PreToolUse entries, got %d", len(preToolUse))
		}
	})

	// Removal: RemoveCompoundApproveHook leaves monitoring hooks intact
	t.Run("removal_preserves_monitoring", func(t *testing.T) {
		pd := filepath.Join(profileDir, "removal")
		_ = os.MkdirAll(pd, 0755)

		_ = InjectHooks(pd, hooksDir)
		_ = InjectCompoundApproveHook(pd, hookBin)
		if err := RemoveCompoundApproveHook(pd, hookBin); err != nil {
			t.Fatal(err)
		}

		settings := readSettings(t, pd)
		hooks := settings["hooks"].(map[string]any)
		preToolUse := hooks["PreToolUse"].([]any)
		if len(preToolUse) != 1 {
			t.Errorf("expected 1 PreToolUse entry after removal, got %d", len(preToolUse))
		}
		// Verify it's the monitoring one, not compound
		rule := preToolUse[0].(map[string]any)
		innerHooks := rule["hooks"].([]any)
		hm := innerHooks[0].(map[string]any)
		cmd := hm["command"].(string)
		if cmd == hookBin {
			t.Error("compound approve hook should have been removed")
		}
	})
}

func assertBothHooksPresent(t *testing.T, profileDir, hookBin string) {
	t.Helper()
	settings := readSettings(t, profileDir)
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("no hooks in settings")
	}
	preToolUse, ok := hooks["PreToolUse"].([]any)
	if !ok {
		t.Fatal("no PreToolUse in hooks")
	}

	hasMonitoring := false
	hasCompound := false
	for _, entry := range preToolUse {
		rule, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		innerHooks, _ := rule["hooks"].([]any)
		for _, h := range innerHooks {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
			if cmd == hookBin {
				hasCompound = true
			}
			if len(cmd) > 0 && cmd != hookBin {
				hasMonitoring = true
			}
		}
	}
	if !hasMonitoring {
		t.Error("monitoring hook missing from PreToolUse")
	}
	if !hasCompound {
		t.Error("compound approve hook missing from PreToolUse")
	}
}

func readSettings(t *testing.T, profileDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(profileDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}
