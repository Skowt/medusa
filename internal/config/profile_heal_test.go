package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// linkProfileToShared recreates what the old sync did: a relative symlink from
// the profile's skills/plugins to the shared tree.
func linkProfileToShared(t *testing.T, root, profile string) {
	t.Helper()
	mustMkdirAll(t, filepath.Join(root, profile))
	for _, name := range sharedLinkedDirs {
		mustMkdirAll(t, filepath.Join(root, "shared", name))
		link := filepath.Join(root, profile, name)
		if err := os.Symlink(filepath.Join("..", "shared", name), link); err != nil {
			t.Fatalf("Symlink %s: %v", link, err)
		}
	}
}

func TestHealReplacesSymlinksWithCopies(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "shared", "skills", "reviewer"))
	mustWriteFile(t, filepath.Join(root, "shared", "skills", "reviewer", "SKILL.md"), []byte("skill"))
	mustMkdirAll(t, filepath.Join(root, "shared", "plugins"))
	mustWriteFile(t, filepath.Join(root, "shared", "plugins", "marker.txt"), []byte("plugin"))

	linkProfileToShared(t, root, "work")

	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("HealSharedProfileLinks: %v", err)
	}

	for _, name := range sharedLinkedDirs {
		target := filepath.Join(root, "work", name)
		fi, err := os.Lstat(target)
		if err != nil {
			t.Fatalf("Lstat %s: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is still a symlink after healing", name)
		}
		if !fi.IsDir() {
			t.Errorf("%s should be a real directory after healing", name)
		}
	}

	data, err := os.ReadFile(filepath.Join(root, "work", "skills", "reviewer", "SKILL.md"))
	if err != nil {
		t.Fatalf("nested skill file should have been copied: %v", err)
	}
	if string(data) != "skill" {
		t.Errorf("copied skill = %q, want %q", data, "skill")
	}
	if _, err := os.Stat(filepath.Join(root, "work", "plugins", "marker.txt")); err != nil {
		t.Errorf("plugin file should have been copied: %v", err)
	}
}

// The copy has to be a copy: writing into one profile must not reach another.
func TestHealedProfilesAreIndependent(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "shared", "skills"))
	mustWriteFile(t, filepath.Join(root, "shared", "skills", "a.md"), []byte("original"))
	linkProfileToShared(t, root, "work")
	linkProfileToShared(t, root, "personal")

	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("HealSharedProfileLinks: %v", err)
	}

	mustWriteFile(t, filepath.Join(root, "work", "skills", "a.md"), []byte("edited"))

	for _, path := range []string{
		filepath.Join(root, "personal", "skills", "a.md"),
		filepath.Join(root, "shared", "skills", "a.md"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", path, err)
		}
		if string(data) != "original" {
			t.Errorf("%s = %q, want %q — the copy is still shared", path, data, "original")
		}
	}
}

func TestHealLeavesOwnDirectoriesAlone(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "shared", "skills"))
	mustWriteFile(t, filepath.Join(root, "shared", "skills", "shared.md"), []byte("shared"))

	own := filepath.Join(root, "solo", "skills")
	mustMkdirAll(t, own)
	mustWriteFile(t, filepath.Join(own, "mine.md"), []byte("mine"))

	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("HealSharedProfileLinks: %v", err)
	}

	entries, err := os.ReadDir(own)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "mine.md" {
		t.Errorf("an unlinked profile's skills were modified: %v", entries)
	}
}

// A symlink pointing somewhere else is the user's own arrangement.
func TestHealIgnoresUnrelatedSymlinks(t *testing.T) {
	root := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "skills")
	mustMkdirAll(t, elsewhere)
	mustMkdirAll(t, filepath.Join(root, "shared", "skills"))
	mustMkdirAll(t, filepath.Join(root, "work"))
	if err := os.Symlink(elsewhere, filepath.Join(root, "work", "skills")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("HealSharedProfileLinks: %v", err)
	}

	fi, err := os.Lstat(filepath.Join(root, "work", "skills"))
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("a symlink outside the shared tree should have been left alone")
	}
}

// A link left over after the shared tree was deleted resolves to nothing, so
// the only repair available is to drop it.
func TestHealDropsDanglingSharedLinks(t *testing.T) {
	root := t.TempDir()
	linkProfileToShared(t, root, "work")
	if err := os.RemoveAll(filepath.Join(root, "shared")); err != nil {
		t.Fatalf("RemoveAll shared: %v", err)
	}

	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("HealSharedProfileLinks: %v", err)
	}

	for _, name := range sharedLinkedDirs {
		if _, err := os.Lstat(filepath.Join(root, "work", name)); !os.IsNotExist(err) {
			t.Errorf("%s: dangling link should be gone, got err %v", name, err)
		}
	}
}

func TestHealSkipsTheSharedDirectoryItself(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "shared", "skills"))
	mustMkdirAll(t, filepath.Join(root, "shared", "plugins"))

	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("HealSharedProfileLinks: %v", err)
	}

	for _, name := range sharedLinkedDirs {
		fi, err := os.Lstat(filepath.Join(root, "shared", name))
		if err != nil {
			t.Fatalf("Lstat shared/%s: %v", name, err)
		}
		if !fi.IsDir() {
			t.Errorf("shared/%s should still be a plain directory", name)
		}
	}
}

func TestHealIsIdempotent(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "shared", "skills"))
	mustWriteFile(t, filepath.Join(root, "shared", "skills", "a.md"), []byte("v1"))
	linkProfileToShared(t, root, "work")

	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("first heal: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "work", "skills", "a.md"), []byte("v2"))
	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("second heal: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "work", "skills", "a.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "v2" {
		t.Errorf("a second heal overwrote the profile's own copy: %q", data)
	}
	if _, err := os.Stat(filepath.Join(root, "work", "skills"+healTempSuffix)); !os.IsNotExist(err) {
		t.Error("the staging directory should not survive a heal")
	}
}

// Plugins only load while settings.json lists them, so a healed profile has to
// come out with its copied plugins still enabled.
func TestHealKeepsCopiedPluginsEnabled(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "shared", "plugins"))
	registry := map[string]any{
		"version": 2,
		"plugins": map[string]any{
			"context7@official": []any{map[string]any{"scope": "user"}},
			"github@official":   []any{map[string]any{"scope": "user"}},
		},
	}
	data, _ := json.Marshal(registry)
	mustWriteFile(t, filepath.Join(root, "shared", "plugins", "installed_plugins.json"), data)

	linkProfileToShared(t, root, "work")
	existing := map[string]any{
		"enabledPlugins": map[string]any{"github@official": false},
		"otherSetting":   "preserved",
	}
	settingsData, _ := json.Marshal(existing)
	mustWriteFile(t, filepath.Join(root, "work", "settings.json"), settingsData)

	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("HealSharedProfileLinks: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(root, "work", "settings.json"))
	if err != nil {
		t.Fatalf("ReadFile settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(out, &settings); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	enabled, ok := settings["enabledPlugins"].(map[string]any)
	if !ok {
		t.Fatalf("enabledPlugins missing or wrong type")
	}
	if enabled["context7@official"] != true {
		t.Errorf("context7 should be enabled, got %v", enabled["context7@official"])
	}
	if enabled["github@official"] != false {
		t.Errorf("an explicitly disabled plugin should stay disabled, got %v", enabled["github@official"])
	}
	if settings["otherSetting"] != "preserved" {
		t.Errorf("unrelated settings should survive, got %v", settings["otherSetting"])
	}
}

// A shared store records absolute paths naming whichever profile wrote them,
// so a copy that keeps them sends the profile to another profile's directory.
func TestHealRepointsPluginPathsAtTheProfile(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "shared", "plugins", "marketplaces", "official"))

	marketplaces := map[string]any{
		"official": map[string]any{
			// Written while another profile held the shared store.
			"installLocation": filepath.Join(root, "Default", "plugins", "marketplaces", "official"),
		},
		"local": map[string]any{
			// Written under the shared path itself.
			"installLocation": filepath.Join(root, "shared", "plugins", "marketplaces", "local"),
		},
	}
	data, _ := json.Marshal(marketplaces)
	mustWriteFile(t, filepath.Join(root, "shared", "plugins", "known_marketplaces.json"), data)

	installed := map[string]any{
		"version": 2,
		"plugins": map[string]any{
			"context7@official": []any{map[string]any{
				"scope":       "user",
				"installPath": filepath.Join(root, "Default", "plugins", "cache", "official", "context7"),
			}},
		},
	}
	data, _ = json.Marshal(installed)
	mustWriteFile(t, filepath.Join(root, "shared", "plugins", "installed_plugins.json"), data)

	linkProfileToShared(t, root, "Work")

	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("HealSharedProfileLinks: %v", err)
	}

	own := filepath.Join(root, "Work", "plugins")
	for _, name := range []string{"known_marketplaces.json", "installed_plugins.json"} {
		out, err := os.ReadFile(filepath.Join(own, name))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", name, err)
		}
		text := string(out)
		if strings.Contains(text, filepath.Join(root, "Default", "plugins")) {
			t.Errorf("%s still points at another profile's plugins:\n%s", name, text)
		}
		if strings.Contains(text, filepath.Join(root, "shared", "plugins")) {
			t.Errorf("%s still points at the shared plugins:\n%s", name, text)
		}
		if !strings.Contains(text, own) {
			t.Errorf("%s does not point at the profile's own plugins:\n%s", name, text)
		}
	}

	// Only the profile segment moves — the rest of each path is untouched.
	out, err := os.ReadFile(filepath.Join(own, "known_marketplaces.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got map[string]map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if want := filepath.Join(own, "marketplaces", "official"); got["official"]["installLocation"] != want {
		t.Errorf("installLocation = %v, want %v", got["official"]["installLocation"], want)
	}
}

// Paths outside the profiles tree are the user's own and must survive.
func TestHealLeavesForeignPluginPathsAlone(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "checkouts", "plugins", "mine")
	mustMkdirAll(t, filepath.Join(root, "shared", "plugins"))
	data, _ := json.Marshal(map[string]any{
		"mine": map[string]any{"installLocation": outside},
	})
	mustWriteFile(t, filepath.Join(root, "shared", "plugins", "known_marketplaces.json"), data)

	linkProfileToShared(t, root, "Work")
	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("HealSharedProfileLinks: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(root, "Work", "plugins", "known_marketplaces.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(out), outside) {
		t.Errorf("a path outside the profiles tree was rewritten:\n%s", out)
	}
}

// The marketplace checkouts below the store are repository content: a file
// there that happens to name a profile path is not the store's bookkeeping.
func TestHealDoesNotRewriteMarketplaceContent(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "shared", "plugins", "marketplaces", "official")
	mustMkdirAll(t, nested)
	body := []byte(filepath.Join(root, "Default", "plugins", "whatever"))
	mustWriteFile(t, filepath.Join(nested, "plugin.json"), body)

	linkProfileToShared(t, root, "Work")
	if err := HealSharedProfileLinks(root); err != nil {
		t.Fatalf("HealSharedProfileLinks: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(root, "Work", "plugins", "marketplaces", "official", "plugin.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(out) != string(body) {
		t.Errorf("marketplace content was rewritten: %q", out)
	}
}
