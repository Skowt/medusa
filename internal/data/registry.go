package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Registry manages the workspaces.json file for persistent workspace tracking
type Registry struct {
	path string
	mu   sync.RWMutex
}

// registryFile represents the JSON structure of workspaces.json
type registryFile struct {
	Workspaces []registryWorkspace `json:"workspaces"`
}

type registryWorkspace struct {
	Name    string `json:"name"`
	ID      string `json:"id"`
	Profile string `json:"profile,omitempty"`
}

// NewRegistry creates a new registry at the specified path
func NewRegistry(path string) *Registry {
	return &Registry{
		path: path,
	}
}

// ListWorkspaces reads all workspace entries from the registry
func (r *Registry) ListWorkspaces() ([]registryWorkspace, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loadLocked()
}

// loadLocked reads workspace entries without acquiring locks.
// The caller must hold at least r.mu.RLock().
func (r *Registry) loadLocked() ([]registryWorkspace, error) {
	raw, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var registry registryFile
	if err := json.Unmarshal(raw, &registry); err != nil {
		return nil, err
	}
	return registry.Workspaces, nil
}

// modifyWorkspaces loads, modifies, and saves workspace entries atomically
// under a single write lock.
func (r *Registry) modifyWorkspaces(fn func([]registryWorkspace) ([]registryWorkspace, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	workspaces, err := r.loadLocked()
	if err != nil {
		return err
	}

	workspaces, err = fn(workspaces)
	if err != nil {
		return err
	}

	return r.saveLocked(workspaces)
}

// AddWorkspace adds a workspace to the registry
func (r *Registry) AddWorkspace(name, id, profile string) error {
	return r.modifyWorkspaces(func(workspaces []registryWorkspace) ([]registryWorkspace, error) {
		for _, ws := range workspaces {
			if ws.ID == id {
				return workspaces, nil // Already registered
			}
		}
		return append(workspaces, registryWorkspace{
			Name:    name,
			ID:      id,
			Profile: profile,
		}), nil
	})
}

// RemoveWorkspace removes a workspace from the registry by ID
func (r *Registry) RemoveWorkspace(id string) error {
	return r.modifyWorkspaces(func(workspaces []registryWorkspace) ([]registryWorkspace, error) {
		var filtered []registryWorkspace
		for _, ws := range workspaces {
			if ws.ID != id {
				filtered = append(filtered, ws)
			}
		}
		return filtered, nil
	})
}

// UpdateWorkspace updates the name and ID of an existing workspace entry.
func (r *Registry) UpdateWorkspace(oldID, newName, newID string) error {
	return r.modifyWorkspaces(func(workspaces []registryWorkspace) ([]registryWorkspace, error) {
		for i := range workspaces {
			if workspaces[i].ID == oldID {
				workspaces[i].Name = newName
				workspaces[i].ID = newID
				return workspaces, nil
			}
		}
		return nil, fmt.Errorf("workspace not found: %s", oldID)
	})
}

// SetProfile sets the profile for a workspace identified by its ID
func (r *Registry) SetProfile(id, profile string) error {
	return r.modifyWorkspaces(func(workspaces []registryWorkspace) ([]registryWorkspace, error) {
		for i := range workspaces {
			if workspaces[i].ID == id {
				workspaces[i].Profile = profile
				return workspaces, nil
			}
		}
		return nil, fmt.Errorf("workspace not found: %s", id)
	})
}

// RenameProfile updates all workspaces using oldProfile to use newProfile
func (r *Registry) RenameProfile(oldProfile, newProfile string) error {
	return r.modifyWorkspaces(func(workspaces []registryWorkspace) ([]registryWorkspace, error) {
		for i := range workspaces {
			if workspaces[i].Profile == oldProfile {
				workspaces[i].Profile = newProfile
			}
		}
		return workspaces, nil
	})
}

// ClearProfile clears the profile from all workspaces using the specified profile
func (r *Registry) ClearProfile(profile string) error {
	return r.modifyWorkspaces(func(workspaces []registryWorkspace) ([]registryWorkspace, error) {
		for i := range workspaces {
			if workspaces[i].Profile == profile {
				workspaces[i].Profile = ""
			}
		}
		return workspaces, nil
	})
}

// saveLocked writes the workspace entries to the registry file.
// The caller must hold r.mu.Lock().
func (r *Registry) saveLocked(workspaces []registryWorkspace) error {
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if workspaces == nil {
		workspaces = []registryWorkspace{}
	}

	registry := registryFile{Workspaces: workspaces}
	raw, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(r.path, raw, 0644)
}
