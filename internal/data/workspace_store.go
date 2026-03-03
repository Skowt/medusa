package data

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/andyrewlee/medusa/internal/logging"
)

const workspaceFilename = "workspace.json"

// WorkspaceStore manages workspace persistence
type WorkspaceStore struct {
	root string // ~/.medusa/workspaces
}

// NewWorkspaceStore creates a new workspace store
func NewWorkspaceStore(root string) *WorkspaceStore {
	return &WorkspaceStore{
		root: root,
	}
}

// workspacePath returns the path to the workspace file for a workspace ID
func (s *WorkspaceStore) workspacePath(id WorkspaceID) string {
	return filepath.Join(s.root, string(id), workspaceFilename)
}

// List returns all workspace IDs stored in the store
func (s *WorkspaceStore) List() ([]WorkspaceID, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var ids []WorkspaceID
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		wsPath := filepath.Join(s.root, entry.Name(), workspaceFilename)
		if _, err := os.Stat(wsPath); err == nil {
			ids = append(ids, WorkspaceID(entry.Name()))
		}
	}
	return ids, nil
}

// Load loads a workspace by its ID
func (s *WorkspaceStore) Load(id WorkspaceID) (*Workspace, error) {
	path := s.workspacePath(id)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var ws Workspace
	if err := json.Unmarshal(raw, &ws); err != nil {
		return nil, err
	}

	ws.storeID = id
	applyWorkspaceDefaults(&ws)

	return &ws, nil
}

// Save saves a workspace to the store using atomic write
func (s *WorkspaceStore) Save(ws *Workspace) error {
	id := ws.ID()
	path := s.workspacePath(id)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}

	// Write to temp file first, then rename for atomic operation
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return err
	}
	if ws.storeID != "" && ws.storeID != id {
		if err := s.Delete(ws.storeID); err != nil {
			logging.Warn("Failed to remove old workspace metadata %s: %v", ws.storeID, err)
		}
	}
	ws.storeID = id
	return nil
}

// Delete removes a workspace from the store
func (s *WorkspaceStore) Delete(id WorkspaceID) error {
	dir := filepath.Join(s.root, string(id))
	return os.RemoveAll(dir)
}

// ListAll loads all workspaces in the store
func (s *WorkspaceStore) ListAll() ([]*Workspace, error) {
	ids, err := s.List()
	if err != nil {
		return nil, err
	}

	var workspaces []*Workspace
	for _, id := range ids {
		ws, err := s.Load(id)
		if err != nil {
			logging.Warn("Failed to load workspace %s: %v", id, err)
			continue
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, nil
}

func applyWorkspaceDefaults(ws *Workspace) {
	if ws.Assistant == "" {
		ws.Assistant = "claude"
	}
	if ws.ScriptMode == "" {
		ws.ScriptMode = "nonconcurrent"
	}
	if ws.Env == nil {
		ws.Env = make(map[string]string)
	}
	if ws.Runtime == "" {
		ws.Runtime = RuntimeLocalWorktree
	}
}

// snapshotWorkspaceForSave creates a copy of the workspace suitable for saving.
// This exists so callers can safely pass a snapshot to a goroutine.
func SnapshotWorkspaceForSave(ws *Workspace) *Workspace {
	if ws == nil {
		return nil
	}
	snap := *ws
	// Deep copy slices that matter
	if ws.OpenTabs != nil {
		snap.OpenTabs = make([]TabInfo, len(ws.OpenTabs))
		copy(snap.OpenTabs, ws.OpenTabs)
	}
	if ws.Repos != nil {
		snap.Repos = make([]RepoRef, len(ws.Repos))
		copy(snap.Repos, ws.Repos)
	}
	if ws.Worktrees != nil {
		snap.Worktrees = make([]WorktreeRef, len(ws.Worktrees))
		copy(snap.Worktrees, ws.Worktrees)
	}
	if ws.Env != nil {
		snap.Env = make(map[string]string, len(ws.Env))
		for k, v := range ws.Env {
			snap.Env[k] = v
		}
	}
	return &snap
}
