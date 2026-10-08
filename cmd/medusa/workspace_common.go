package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/workspaceops"
)

// defaultProfile is the profile a fresh install uses before any exists on disk.
const defaultProfile = "Default"

// workspaceEnv is what every workspace command needs: the config (with the
// TUI's saved settings) and the stores, opened exactly as the TUI opens them.
type workspaceEnv struct {
	cfg  *config.Config
	deps workspaceops.Deps
}

func loadWorkspaceEnv() (*workspaceEnv, error) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		return nil, err
	}
	applyTmuxEnv(cfg)
	if err := cfg.Paths.EnsureDirectories(); err != nil {
		return nil, err
	}
	return &workspaceEnv{
		cfg: cfg,
		deps: workspaceops.Deps{
			Config:   cfg,
			Registry: data.NewRegistry(cfg.Paths.RegistryPath),
			Store:    data.NewWorkspaceStore(cfg.Paths.MetadataRoot),
			Recents:  data.NewRecentsStore(cfg.Paths.RecentsPath),
		},
	}, nil
}

// applyTmuxEnv points tmux.DefaultOptions at the server the TUI uses when the
// config names one, as the TUI does at startup, so sessions the CLI starts are
// on the server the TUI looks at. An environment value already set wins only
// when the config is silent.
func applyTmuxEnv(cfg *config.Config) {
	for key, value := range map[string]string{
		"MEDUSA_TMUX_SERVER": cfg.UI.TmuxServer,
		"MEDUSA_TMUX_CONFIG": cfg.UI.TmuxConfigPath,
	} {
		if value = strings.TrimSpace(value); value != "" {
			_ = os.Setenv(key, value)
		}
	}
}

// resolveRepo returns the repo containing path (the current directory when
// empty), resolved to its main working tree. From inside a linked worktree —
// where an agent started by medusa is always standing — that is the repo the
// worktree belongs to, not the worktree itself: show-toplevel alone would
// record another workspace's worktree as a source repo.
func resolveRepo(path string) (data.RepoRef, error) {
	if path == "" {
		wd, err := os.Getwd()
		if err != nil {
			return data.RepoRef{}, err
		}
		path = wd
	}
	top, err := gitOutput(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return data.RepoRef{}, fmt.Errorf("%s is not inside a git repository (use --repo)", path)
	}
	// The common dir is the main repo's .git for every worktree of it. Its
	// parent is the main working tree, unless the repo is bare.
	if common, err := gitOutput(top, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil &&
		filepath.Base(common) == ".git" {
		if main, err := gitOutput(filepath.Dir(common), "rev-parse", "--show-toplevel"); err == nil {
			top = main
		}
	}
	if resolved, err := filepath.EvalSymlinks(top); err == nil {
		top = resolved
	}
	return data.RepoRef{Name: filepath.Base(top), Path: top}, nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// listProfiles returns the profile directories, as the TUI's profile picker
// lists them.
func listProfiles(cfg *config.Config) []string {
	entries, err := os.ReadDir(cfg.Paths.ProfilesRoot)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "shared" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

// checkProfile refuses a profile that does not exist, so a typo fails instead
// of quietly creating an empty profile with no login or settings.
func checkProfile(cfg *config.Config, name string) error {
	if name == defaultProfile {
		return nil
	}
	profiles := listProfiles(cfg)
	for _, p := range profiles {
		if p == name {
			return nil
		}
	}
	if len(profiles) == 0 {
		profiles = []string{defaultProfile}
	}
	return fmt.Errorf("no profile named %q (profiles: %s); create it in the TUI first", name, strings.Join(profiles, ", "))
}

// findWorkspace looks a workspace up by exact ID, then by name.
func findWorkspace(env *workspaceEnv, nameOrID string) (*data.Workspace, error) {
	entries, err := env.deps.Registry.ListWorkspaces()
	if err != nil {
		return nil, fmt.Errorf("reading the registry: %w", err)
	}
	match := -1
	for i, e := range entries {
		if e.ID == nameOrID {
			match = i
			break
		}
	}
	if match < 0 {
		for i, e := range entries {
			if strings.EqualFold(e.Name, nameOrID) {
				match = i
				break
			}
		}
	}
	if match < 0 {
		return nil, fmt.Errorf("no workspace named %q", nameOrID)
	}
	entry := entries[match]
	ws, err := env.deps.Store.Load(data.WorkspaceID(entry.ID))
	if err != nil {
		return nil, fmt.Errorf("workspace %q has no readable metadata (%v); remove it from the TUI", entry.Name, err)
	}
	ws.Profile = entry.Profile
	return ws, nil
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
