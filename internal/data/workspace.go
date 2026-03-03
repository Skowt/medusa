package data

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"time"
)

// Runtime constants for workspace execution backends
const (
	RuntimeLocalWorktree = "local-worktree"
	RuntimeLocalCheckout = "local-checkout"
	RuntimeLocalDocker   = "local-docker"
	RuntimeCloudSandbox  = "cloud-sandbox"
)

// NormalizeRuntime returns a normalized runtime string
func NormalizeRuntime(runtime string) string {
	switch runtime {
	case RuntimeLocalWorktree, RuntimeLocalCheckout, RuntimeLocalDocker, RuntimeCloudSandbox:
		return runtime
	case "sandbox":
		return RuntimeCloudSandbox
	case "local", "":
		return RuntimeLocalWorktree
	default:
		return RuntimeLocalWorktree
	}
}

// WorkspaceStatus represents the lifecycle status of a workspace
type WorkspaceStatus string

const (
	StatusNone     WorkspaceStatus = ""         // default/no status set
	StatusStarted  WorkspaceStatus = "started"
	StatusBlocked  WorkspaceStatus = "blocked"
	StatusMerged   WorkspaceStatus = "merged"
	StatusArchived WorkspaceStatus = "archived"
)

// RepoRef identifies a source git repository
type RepoRef struct {
	Path string `json:"path"` // Absolute path to the source repo
	Name string `json:"name"` // Basename
}

// WorktreeRef identifies a worktree created from a repo
type WorktreeRef struct {
	Branch string `json:"branch"`
	Base   string `json:"base"`
	Root   string `json:"root"` // Worktree directory on disk
}

// TabInfo stores information about an open tab
type TabInfo struct {
	Assistant       string `json:"assistant"`
	Name            string `json:"name"`
	SessionName     string `json:"session_name,omitempty"`
	Status          string `json:"status,omitempty"`
	ClaudeSessionID string `json:"claude_session_id,omitempty"`
	AllowEdits      bool   `json:"allow_edits,omitempty"`
	Isolated        bool   `json:"isolated,omitempty"`
	SkipPermissions bool   `json:"skip_permissions,omitempty"`
}

// ScriptsConfig holds the setup/run/archive script commands
type ScriptsConfig struct {
	Setup   string `json:"setup"`
	Run     string `json:"run"`
	Archive string `json:"archive"`
}

// Workspace represents a unified workspace with one or more repos
type Workspace struct {
	// Identity
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	storeID WorkspaceID

	// Repos and worktrees (parallel slices, same length)
	Repos     []RepoRef    `json:"repos"`
	Worktrees []WorktreeRef `json:"worktrees"`

	// Execution
	Runtime string `json:"runtime"` // local-worktree, local-checkout, cloud-sandbox

	// Agent config
	Assistant string          `json:"assistant"` // claude, codex, gemini
	Profile   string          `json:"profile"`   // Named profile (stored directly)

	// Scripts
	Scripts    ScriptsConfig `json:"scripts"`
	ScriptMode string        `json:"script_mode"`

	// Environment
	Env map[string]string `json:"env"`

	// UI state
	OpenTabs       []TabInfo `json:"open_tabs,omitempty"`
	ActiveTabIndex int       `json:"active_tab_index"`

	// Lifecycle
	Status        WorkspaceStatus `json:"status"`
	StatusChanged time.Time       `json:"status_changed,omitempty"`
	ArchivedAt    time.Time       `json:"archived_at,omitempty"`

	// Permissions
	AllowEdits bool `json:"allow_edits,omitempty"` // Pre-grant Edit permission when true

	// Isolation
	Isolated        bool `json:"isolated,omitempty"`         // Run in sandbox-exec
	SkipPermissions bool `json:"skip_permissions,omitempty"` // Run with --dangerously-skip-permissions
}

// WorkspaceID is a unique identifier based on repo+root hash
type WorkspaceID string

// ID returns a unique identifier for the workspace based on its primary repo and root paths
func (w Workspace) ID() WorkspaceID {
	if len(w.Repos) == 0 || len(w.Worktrees) == 0 {
		return workspaceIDFromIdentity(w.Name)
	}
	return workspaceIDFromIdentity(workspaceIdentity(w.Repos[0].Path, w.Root()))
}

// Root returns the primary worktree root (or group workspace root for multi-repo)
func (w Workspace) Root() string {
	if len(w.Worktrees) == 0 {
		return ""
	}
	if w.IsMultiRepo() {
		// For multi-repo, root is the parent directory of all worktrees
		return filepath.Dir(w.Worktrees[0].Root)
	}
	return w.Worktrees[0].Root
}

// PrimaryRepo returns the first repo reference
func (w Workspace) PrimaryRepo() RepoRef {
	if len(w.Repos) == 0 {
		return RepoRef{}
	}
	return w.Repos[0]
}

// IsMultiRepo returns true if this workspace spans multiple repos
func (w Workspace) IsMultiRepo() bool {
	return len(w.Repos) > 1
}

// SecondaryRoots returns non-primary worktree root paths (for sandbox git-dir whitelisting)
func (w Workspace) SecondaryRoots() []string {
	if len(w.Worktrees) <= 1 {
		return nil
	}
	roots := make([]string, len(w.Worktrees)-1)
	for i := 1; i < len(w.Worktrees); i++ {
		roots[i-1] = w.Worktrees[i].Root
	}
	return roots
}

// AllRoots returns all worktree root paths
func (w Workspace) AllRoots() []string {
	roots := make([]string, len(w.Worktrees))
	for i, wt := range w.Worktrees {
		roots[i] = wt.Root
	}
	return roots
}

// Branch returns the primary worktree branch
func (w Workspace) Branch() string {
	if len(w.Worktrees) == 0 {
		return ""
	}
	return w.Worktrees[0].Branch
}

// Base returns the primary worktree base ref
func (w Workspace) Base() string {
	if len(w.Worktrees) == 0 {
		return ""
	}
	return w.Worktrees[0].Base
}

// Archived returns true if the workspace has archived status
func (w Workspace) Archived() bool {
	return w.Status == StatusArchived
}

// IsPrimaryCheckout returns true if this is the primary checkout (root == repo path)
func (w Workspace) IsPrimaryCheckout() bool {
	if len(w.Repos) == 0 || len(w.Worktrees) == 0 {
		return false
	}
	return w.Worktrees[0].Root == w.Repos[0].Path
}

// IsMainBranch returns true if this workspace is on main or master branch
func (w Workspace) IsMainBranch() bool {
	branch := w.Branch()
	return branch == "main" || branch == "master"
}

// NewWorkspace creates a new single-repo Workspace with the current timestamp and defaults
func NewWorkspace(name, branch, base, repo, root string) *Workspace {
	return &Workspace{
		Name: name,
		Repos: []RepoRef{
			{Path: repo, Name: filepath.Base(repo)},
		},
		Worktrees: []WorktreeRef{
			{Branch: branch, Base: base, Root: root},
		},
		Created:    time.Now(),
		Runtime:    RuntimeLocalWorktree,
		Assistant:  "claude",
		ScriptMode: "nonconcurrent",
		Env:        make(map[string]string),
	}
}

// NewMultiRepoWorkspace creates a multi-repo Workspace
func NewMultiRepoWorkspace(name string, repos []RepoRef, worktrees []WorktreeRef) *Workspace {
	return &Workspace{
		Name:       name,
		Repos:      repos,
		Worktrees:  worktrees,
		Created:    time.Now(),
		Runtime:    RuntimeLocalWorktree,
		Assistant:  "claude",
		ScriptMode: "nonconcurrent",
		Env:        make(map[string]string),
	}
}

func workspaceIDFromIdentity(identity string) WorkspaceID {
	hash := sha1.Sum([]byte(identity))
	return WorkspaceID(hex.EncodeToString(hash[:8]))
}
