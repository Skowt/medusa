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

// AddWorkspace adds a workspace to the registry
func (r *Registry) AddWorkspace(name, id, profile string) error {
	workspaces, err := r.ListWorkspaces()
	if err != nil {
		return err
	}

	// Check if already exists
	for _, ws := range workspaces {
		if ws.ID == id {
			return nil // Already registered
		}
	}

	workspaces = append(workspaces, registryWorkspace{
		Name:    name,
		ID:      id,
		Profile: profile,
	})
	return r.save(workspaces)
}

// RemoveWorkspace removes a workspace from the registry by ID
func (r *Registry) RemoveWorkspace(id string) error {
	workspaces, err := r.ListWorkspaces()
	if err != nil {
		return err
	}

	var filtered []registryWorkspace
	for _, ws := range workspaces {
		if ws.ID != id {
			filtered = append(filtered, ws)
		}
	}

	return r.save(filtered)
}

// UpdateWorkspace updates the name and ID of an existing workspace entry.
func (r *Registry) UpdateWorkspace(oldID, newName, newID string) error {
	workspaces, err := r.ListWorkspaces()
	if err != nil {
		return err
	}

	for i := range workspaces {
		if workspaces[i].ID == oldID {
			workspaces[i].Name = newName
			workspaces[i].ID = newID
			return r.save(workspaces)
		}
	}

	return fmt.Errorf("workspace not found: %s", oldID)
}

// SetProfile sets the profile for a workspace identified by its ID
func (r *Registry) SetProfile(id, profile string) error {
	workspaces, err := r.ListWorkspaces()
	if err != nil {
		return err
	}

	for i := range workspaces {
		if workspaces[i].ID == id {
			workspaces[i].Profile = profile
			return r.save(workspaces)
		}
	}

	return fmt.Errorf("workspace not found: %s", id)
}

// RenameProfile updates all workspaces using oldProfile to use newProfile
func (r *Registry) RenameProfile(oldProfile, newProfile string) error {
	workspaces, err := r.ListWorkspaces()
	if err != nil {
		return err
	}

	changed := false
	for i := range workspaces {
		if workspaces[i].Profile == oldProfile {
			workspaces[i].Profile = newProfile
			changed = true
		}
	}

	if changed {
		return r.save(workspaces)
	}
	return nil
}

// ClearProfile clears the profile from all workspaces using the specified profile
func (r *Registry) ClearProfile(profile string) error {
	workspaces, err := r.ListWorkspaces()
	if err != nil {
		return err
	}

	changed := false
	for i := range workspaces {
		if workspaces[i].Profile == profile {
			workspaces[i].Profile = ""
			changed = true
		}
	}

	if changed {
		return r.save(workspaces)
	}
	return nil
}

// save writes the workspace entries to the registry file
func (r *Registry) save(workspaces []registryWorkspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()

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
