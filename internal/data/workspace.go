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
)

// OrphanType identifies why a workspace is orphaned.
type OrphanType int

const (
	OrphanNone      OrphanType = iota // Not orphaned
	OrphanMetadata                    // Registry/metadata exists but worktree directory is missing
	OrphanDirectory                   // Directory exists under workspaces root but no metadata references it
)

// WorkspaceStatus represents the lifecycle status of a workspace
type WorkspaceStatus string

const (
	StatusNone     WorkspaceStatus = "" // default/no status set
	StatusStarted  WorkspaceStatus = "started"
	StatusBlocked  WorkspaceStatus = "blocked"
	StatusReview   WorkspaceStatus = "review"
	StatusMerged   WorkspaceStatus = "merged"
	StatusArchived WorkspaceStatus = "archived"
)

// NextStatus returns the status the manual status toggle moves to from the
// given one. Review comes before Blocked: a workspace normally reaches review
// on its way to merged, and blocked is the exception, so the common path is
// the shorter one. Both the dashboard and the center info tab cycle through
// here so the two can never disagree on the order.
func NextStatus(current WorkspaceStatus) WorkspaceStatus {
	switch current {
	case StatusNone, StatusStarted:
		return StatusReview
	case StatusReview:
		return StatusBlocked
	case StatusBlocked:
		return StatusMerged
	case StatusMerged, StatusArchived:
		return StatusNone
	default:
		return StatusStarted
	}
}

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
	Assistant                string `json:"assistant"`
	Name                     string `json:"name"`
	SessionName              string `json:"session_name,omitempty"`
	Status                   string `json:"status,omitempty"`
	ActivityState            string `json:"activity_state,omitempty"`
	ClaudeSessionID          string `json:"claude_session_id,omitempty"`
	Isolated                 bool   `json:"isolated,omitempty"`
	AllowUnsandboxedCommands bool   `json:"allow_unsandboxed_commands,omitempty"`
	PermissionMode           string `json:"permission_mode,omitempty"`
	Fullscreen               bool   `json:"fullscreen,omitempty"` // Claude launched in fullscreen TUI mode.
	CodexSandbox             string `json:"codex_sandbox,omitempty"`
	CodexAuto                bool   `json:"codex_auto,omitempty"`
	// ScriptFullCmd is the env-prefixed shell command for script tabs,
	// persisted so Restart can rerun the same command after a medusa restart.
	ScriptFullCmd string `json:"script_full_cmd,omitempty"`
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
	Repos     []RepoRef     `json:"repos"`
	Worktrees []WorktreeRef `json:"worktrees"`

	// Execution
	Runtime string `json:"runtime"` // local-worktree, local-checkout, cloud-sandbox

	// Agent config
	Assistant string `json:"assistant"` // claude, codex, gemini
	Profile   string `json:"profile"`   // Named profile (stored directly)

	// Scripts
	Scripts    ScriptsConfig `json:"scripts"`
	ScriptMode string        `json:"script_mode"`

	// Environment
	Env map[string]string `json:"env"`

	// UI state
	OpenTabs       []TabInfo `json:"open_tabs,omitempty"`
	ActiveTabIndex int       `json:"active_tab_index"`

	// Note
	Note  string `json:"note,omitempty"`
	Group string `json:"group,omitempty"` // User-assigned group label; empty = ungrouped

	// Lifecycle
	Status        WorkspaceStatus `json:"status"`
	StatusChanged time.Time       `json:"status_changed,omitempty"`
	ArchivedAt    time.Time       `json:"archived_at,omitempty"`

	// Isolation
	Isolated                 bool   `json:"isolated,omitempty"`                   // Enable Claude sandbox via --settings
	AllowUnsandboxedCommands bool   `json:"allow_unsandboxed_commands,omitempty"` // sandbox.allowUnsandboxedCommands when Isolated
	PermissionMode           string `json:"permission_mode,omitempty"`            // claude --permission-mode value

	// Worktree setup
	CopyIgnored bool `json:"copy_ignored,omitempty"` // Copy gitignored files from source repo

	// Activity state (persisted so indicators like '!' survive restarts)
	ActivityState string `json:"activity_state,omitempty"`

	// Orphan detection (runtime only, not persisted)
	Orphan     OrphanType `json:"-"` // Whether this workspace is orphaned and why
	OrphanPath string     `json:"-"` // For directory orphans: the path on disk
}

// IsOrphaned returns true if the workspace is orphaned (metadata or directory).
func (w Workspace) IsOrphaned() bool {
	return w.Orphan != OrphanNone
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

// Root returns the workspace root directory.
// For single-repo workspaces the worktree IS the root (flat layout).
// For multi-repo workspaces it returns the parent of all worktrees.
func (w Workspace) Root() string {
	if len(w.Worktrees) == 0 {
		return ""
	}
	if len(w.Repos) <= 1 {
		return w.Worktrees[0].Root
	}
	return filepath.Dir(w.Worktrees[0].Root)
}

// PrimaryWorktreeRoot returns the first worktree's root directory.
// Use this for git operations (status, diff) and terminal CWD, since Root() is not a git repo.
func (w Workspace) PrimaryWorktreeRoot() string {
	if len(w.Worktrees) == 0 {
		return ""
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
