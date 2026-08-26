package config

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// sharedLinkedDirs are the profile subdirectories that older builds replaced
// with a symlink into profiles/shared.
var sharedLinkedDirs = []string{"skills", "plugins"}

// healTempSuffix names the staging directory a copy is built in, so a heal
// interrupted halfway never leaves a profile with neither the symlink nor a
// directory in its place.
const healTempSuffix = ".medusa-heal"

// HealSharedProfileLinks gives every profile its own copy of the skills and
// plugins that used to be shared between them.
//
// Medusa used to point profiles/<name>/skills and profiles/<name>/plugins at
// profiles/shared with a symlink. For each profile still linked that way, the
// shared tree is copied into the profile and the symlink is removed, so the
// profile owns its plugins and skills from then on. A profile that already has
// real directories is left untouched, and profiles/shared itself is left on
// disk — it is the user's data, and nothing reads it any more.
func HealSharedProfileLinks(profilesRoot string) error {
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "shared" {
			continue
		}
		if err := healProfileSharedLinks(profilesRoot, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

// healProfileSharedLinks heals a single profile. It reports no error when the
// profile was never linked — that is the common case after the first run.
func healProfileSharedLinks(profilesRoot, profileName string) error {
	sharedRoot := filepath.Join(profilesRoot, "shared")
	profileDir := filepath.Join(profilesRoot, profileName)

	healedPlugins := false
	for _, name := range sharedLinkedDirs {
		target := filepath.Join(profileDir, name)
		sharedDir := filepath.Join(sharedRoot, name)

		if !linksToShared(target, sharedDir, profileDir) {
			continue
		}

		if err := replaceLinkWithCopy(target, sharedDir); err != nil {
			return err
		}
		if name == "plugins" {
			healedPlugins = true
		}
	}

	if healedPlugins {
		// The copy carries paths written while the tree was shared, so they
		// name whichever profile happened to write them — repoint them before
		// anything reads the store.
		repointPluginPaths(profilesRoot, profileDir)

		// The copied plugins are only active while settings.json lists them,
		// and that file is written per profile — so a profile whose settings
		// were reset would come out of the heal with plugins it never loads.
		enableInstalledPlugins(profileDir)
	}
	return nil
}

// profileSegment matches one profile directory name, which validation limits to
// letters, numbers, dots, dashes and underscores.
const profileSegment = `[A-Za-z0-9._-]+`

// repointPluginPaths rewrites the absolute paths in a healed profile's plugin
// store so they name that profile's own directory.
//
// Claude records where each marketplace and plugin lives as an absolute path
// (installLocation, installPath). While the store was shared, whichever profile
// wrote an entry put its own path in — so the copy Work ends up with can say
// .../profiles/Default/plugins/... or .../profiles/shared/plugins/..., and
// Claude then reads a marketplace out of a directory this profile does not own.
// Every such path has the same tail, so only the profile segment moves.
//
// Only the JSON files at the top of the plugins directory are rewritten. The
// marketplace and cache checkouts below it are repository content, and the
// paths that matter all live in the store's own files.
func repointPluginPaths(profilesRoot, profileDir string) {
	pluginsDir := filepath.Join(profileDir, "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return
	}

	pattern, err := regexp.Compile(regexp.QuoteMeta(profilesRoot) + "/" + profileSegment + "/plugins")
	if err != nil {
		return
	}
	// ReplaceAllLiteral, since a path is not a replacement template — a "$" in
	// the user's home directory would otherwise expand.
	replacement := []byte(pluginsDir)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(pluginsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fixed := pattern.ReplaceAllLiteral(data, replacement)
		if bytes.Equal(fixed, data) {
			continue
		}
		// The file exists, so its own mode is kept.
		_ = os.WriteFile(path, fixed, 0644)
	}
}

// linksToShared reports whether target is a symlink pointing at sharedDir.
// A link is matched by identity where it resolves, since the stored path may be
// relative and either side may sit under a symlinked parent (/var on macOS).
// A dangling link is matched by path instead, because it resolves to nothing.
func linksToShared(target, sharedDir, profileDir string) bool {
	fi, err := os.Lstat(target)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}

	if linked, err := os.Stat(target); err == nil {
		if shared, err := os.Stat(sharedDir); err == nil {
			return os.SameFile(linked, shared)
		}
		return false
	}

	dest, err := os.Readlink(target)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(profileDir, dest)
	}
	return filepath.Clean(dest) == filepath.Clean(sharedDir)
}

// replaceLinkWithCopy stages a copy of sharedDir alongside the symlink, then
// swaps the two. The copy is built first so an interrupted heal leaves the
// symlink in place rather than an empty slot.
func replaceLinkWithCopy(target, sharedDir string) error {
	if _, err := os.Stat(sharedDir); err != nil {
		// The shared tree is gone, so there is nothing to copy and the link
		// only points at a hole. Drop it and let the agent recreate the dir.
		return os.Remove(target)
	}

	staged := target + healTempSuffix
	if err := os.RemoveAll(staged); err != nil {
		return err
	}
	if err := copyTree(sharedDir, staged); err != nil {
		_ = os.RemoveAll(staged)
		return err
	}
	if err := os.Remove(target); err != nil {
		_ = os.RemoveAll(staged)
		return err
	}
	return os.Rename(staged, target)
}

// copyTree copies src to dst recursively. Symlinks inside the tree are copied
// as symlinks: a plugin's checkout can carry them, and following one would
// duplicate whatever it points at.
func copyTree(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}

	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		dest, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(dest, dst)

	case fi.IsDir():
		if err := os.MkdirAll(dst, fi.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil

	case fi.Mode().IsRegular():
		return copyFile(src, dst, fi.Mode().Perm())

	default:
		// Sockets, devices and the like have no meaning in a copied profile.
		return nil
	}
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// enableInstalledPlugins lists every plugin in the profile's own
// installed_plugins.json under enabledPlugins in its settings.json, which is
// where Claude tracks enablement. Entries already present are left as they are
// — a plugin explicitly disabled stays disabled — and none are removed.
func enableInstalledPlugins(profileDir string) {
	installedPath := filepath.Join(profileDir, "plugins", "installed_plugins.json")
	data, err := os.ReadFile(installedPath)
	if err != nil {
		return
	}

	var registry struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(data, &registry); err != nil || len(registry.Plugins) == 0 {
		return
	}

	settingsPath := filepath.Join(profileDir, "settings.json")
	var settings map[string]any
	if existing, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(existing, &settings)
	}
	if settings == nil {
		settings = make(map[string]any)
	}

	enabled, _ := settings["enabledPlugins"].(map[string]any)
	if enabled == nil {
		enabled = make(map[string]any)
	}
	for key := range registry.Plugins {
		if _, exists := enabled[key]; !exists {
			enabled[key] = true
		}
	}
	settings["enabledPlugins"] = enabled

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(settingsPath, out, 0644)
}
