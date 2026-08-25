// Package ide provides utilities for detecting and opening folders in IDEs.
package ide

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Install is a discovered IDE installation.
type Install struct {
	Name       string // display name, e.g. "Cursor"
	Location   string // "System" or "User" (macOS); the $PATH directory (Linux)
	LaunchPath string // .app bundle path (macOS) or binary path (Linux)
}

// macBundle maps a macOS .app bundle file name to its display name. Distinct
// editions (e.g. PyCharm CE) get distinct display names so that "same IDE"
// only ever differs by install location, never by edition.
type macBundle struct {
	file string
	name string
}

// macBundles is the macOS detection registry, in list-priority order.
var macBundles = []macBundle{
	{"Cursor.app", "Cursor"},
	{"Visual Studio Code.app", "Visual Studio Code"},
	{"Zed.app", "Zed"},
	{"PyCharm.app", "PyCharm"},
	{"PyCharm CE.app", "PyCharm CE"},
	{"IntelliJ IDEA.app", "IntelliJ IDEA"},
	{"IntelliJ IDEA CE.app", "IntelliJ IDEA CE"},
	{"WebStorm.app", "WebStorm"},
	{"GoLand.app", "GoLand"},
	{"Sublime Text.app", "Sublime Text"},
}

// pathCommands is the Linux detection registry (CLI command names), in
// list-priority order. Only GUI editors are listed: launching is detached
// (exec without a controlling terminal), so terminal editors like vim/nvim
// are intentionally excluded.
var pathCommands = []string{
	"cursor", "code", "zed", "pycharm", "idea", "webstorm", "goland", "subl",
}

type scanDir struct {
	path     string
	location string
}

// DetectInstalls returns every IDE install found on the system, ordered by
// registry priority (and System before User within one IDE on macOS).
func DetectInstalls() []Install {
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return detectMac([]scanDir{
			{"/Applications", "System"},
			{filepath.Join(home, "Applications"), "User"},
		})
	}
	return detectPath(filepath.SplitList(os.Getenv("PATH")))
}

// detectMac scans dirs (in the given order) for known .app bundles, iterating
// the registry outer and dirs inner so installs group by IDE.
func detectMac(dirs []scanDir) []Install {
	var installs []Install
	for _, b := range macBundles {
		for _, d := range dirs {
			p := filepath.Join(d.path, b.file)
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				installs = append(installs, Install{Name: b.name, Location: d.location, LaunchPath: p})
			}
		}
	}
	return installs
}

// detectPath scans $PATH dirs for known CLI commands, registry outer and dirs
// inner so installs group by command.
func detectPath(pathDirs []string) []Install {
	var installs []Install
	for _, cmd := range pathCommands {
		for _, dir := range pathDirs {
			if dir == "" {
				continue
			}
			p := filepath.Join(dir, cmd)
			if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				installs = append(installs, Install{Name: cmd, Location: dir, LaunchPath: p})
			}
		}
	}
	return installs
}

// DisplayLabel returns the list label for installs[i], appending a bracketed
// location only when another install shares the same Name.
func DisplayLabel(installs []Install, i int) string {
	name := installs[i].Name
	for j, other := range installs {
		if j != i && other.Name == name {
			return name + " (" + installs[i].Location + ")"
		}
	}
	return name
}

// Open opens folder in the given install. macOS launches the bundle via `open
// -a`; other platforms exec the binary directly. The child is reaped in the
// background to avoid leaking resources.
func Open(install Install, folder string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "-a", install.LaunchPath, folder)
	} else {
		cmd = exec.Command(install.LaunchPath, folder)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// Find returns the install whose LaunchPath matches launchPath. It reports
// false when nothing matches, which is how a remembered choice that has since
// been uninstalled is detected.
func Find(installs []Install, launchPath string) (Install, bool) {
	if launchPath == "" {
		return Install{}, false
	}
	for _, ins := range installs {
		if ins.LaunchPath == launchPath {
			return ins, true
		}
	}
	return Install{}, false
}

// NameForPath derives a display name from a remembered LaunchPath, so a
// remembered choice can be named without re-scanning the disk. macOS bundle
// paths lose their ".app" suffix; elsewhere the binary name is the name.
func NameForPath(launchPath string) string {
	if launchPath == "" {
		return ""
	}
	base := filepath.Base(launchPath)
	return strings.TrimSuffix(base, ".app")
}
