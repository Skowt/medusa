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
	Groups     []RegistryGroup     `json:"groups,omitempty"`
}

type registryWorkspace struct {
	Name    string `json:"name"`
	ID      string `json:"id"`
	Profile string `json:"profile,omitempty"`
}

// RegistryGroup is a user-defined collapsible group scoped to a repo.
// Name uniqueness is enforced per (Name, RepoKey); the same name can exist under different repos.
type RegistryGroup struct {
	Name     string `json:"name"`
	RepoKey  string `json:"repo_key"`
	Expanded bool   `json:"expanded"`
	Order    int    `json:"order"`
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
	file, err := r.loadFileLocked()
	if err != nil {
		return nil, err
	}
	return file.Workspaces, nil
}

// ListGroups reads all group entries from the registry.
func (r *Registry) ListGroups() ([]RegistryGroup, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	file, err := r.loadFileLocked()
	if err != nil {
		return nil, err
	}
	return file.Groups, nil
}

// loadLocked reads workspace entries without acquiring locks.
// The caller must hold at least r.mu.RLock().
func (r *Registry) loadLocked() ([]registryWorkspace, error) {
	file, err := r.loadFileLocked()
	if err != nil {
		return nil, err
	}
	return file.Workspaces, nil
}

// loadFileLocked reads the full registry file without acquiring locks.
// The caller must hold at least r.mu.RLock().
func (r *Registry) loadFileLocked() (registryFile, error) {
	raw, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return registryFile{}, nil
	}
	if err != nil {
		return registryFile{}, err
	}

	var registry registryFile
	if err := json.Unmarshal(raw, &registry); err != nil {
		return registryFile{}, err
	}
	return registry, nil
}

// modifyWorkspaces loads, modifies, and saves workspace entries atomically
// under a single write lock. Groups are preserved.
func (r *Registry) modifyWorkspaces(fn func([]registryWorkspace) ([]registryWorkspace, error)) error {
	return r.modifyFile(func(file *registryFile) error {
		updated, err := fn(file.Workspaces)
		if err != nil {
			return err
		}
		file.Workspaces = updated
		return nil
	})
}

// modifyGroups loads, modifies, and saves group entries atomically under a single write lock.
func (r *Registry) modifyGroups(fn func([]RegistryGroup) ([]RegistryGroup, error)) error {
	return r.modifyFile(func(file *registryFile) error {
		updated, err := fn(file.Groups)
		if err != nil {
			return err
		}
		file.Groups = updated
		return nil
	})
}

// modifyFile is the shared atomic read/modify/write core used by modifyWorkspaces and modifyGroups.
func (r *Registry) modifyFile(fn func(*registryFile) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	file, err := r.loadFileLocked()
	if err != nil {
		return err
	}

	if err := fn(&file); err != nil {
		return err
	}

	return r.saveFileLocked(file)
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

// saveLocked writes workspace entries while preserving existing groups.
// The caller must hold r.mu.Lock().
func (r *Registry) saveLocked(workspaces []registryWorkspace) error {
	file, err := r.loadFileLocked()
	if err != nil {
		return err
	}
	file.Workspaces = workspaces
	return r.saveFileLocked(file)
}

// saveFileLocked writes the full registry file.
// The caller must hold r.mu.Lock().
func (r *Registry) saveFileLocked(file registryFile) error {
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if file.Workspaces == nil {
		file.Workspaces = []registryWorkspace{}
	}

	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(r.path, raw, 0644)
}

// AddGroup appends a new group if (name, repoKey) is not already present.
// Newly created groups start expanded and are appended at the end of their repo scope's order.
func (r *Registry) AddGroup(name, repoKey string) error {
	return r.modifyGroups(func(groups []RegistryGroup) ([]RegistryGroup, error) {
		maxOrder := -1
		for _, g := range groups {
			if g.RepoKey == repoKey {
				if g.Name == name {
					return groups, nil // already exists; no-op
				}
				if g.Order > maxOrder {
					maxOrder = g.Order
				}
			}
		}
		return append(groups, RegistryGroup{
			Name:     name,
			RepoKey:  repoKey,
			Expanded: true,
			Order:    maxOrder + 1,
		}), nil
	})
}

// RemoveGroup deletes a group entry. Clearing the Group field on member workspaces is the caller's responsibility.
func (r *Registry) RemoveGroup(name, repoKey string) error {
	return r.modifyGroups(func(groups []RegistryGroup) ([]RegistryGroup, error) {
		filtered := groups[:0]
		for _, g := range groups {
			if g.Name == name && g.RepoKey == repoKey {
				continue
			}
			filtered = append(filtered, g)
		}
		return filtered, nil
	})
}

// RenameGroup updates the name of a group. Rewriting the Group field on member workspaces is the caller's responsibility.
func (r *Registry) RenameGroup(oldName, newName, repoKey string) error {
	return r.modifyGroups(func(groups []RegistryGroup) ([]RegistryGroup, error) {
		for _, g := range groups {
			if g.Name == newName && g.RepoKey == repoKey {
				return nil, fmt.Errorf("group %q already exists in repo scope %q", newName, repoKey)
			}
		}
		for i := range groups {
			if groups[i].Name == oldName && groups[i].RepoKey == repoKey {
				groups[i].Name = newName
				return groups, nil
			}
		}
		return nil, fmt.Errorf("group not found: %s/%s", repoKey, oldName)
	})
}

// SetGroupExpanded toggles the expanded state of a group.
func (r *Registry) SetGroupExpanded(name, repoKey string, expanded bool) error {
	return r.modifyGroups(func(groups []RegistryGroup) ([]RegistryGroup, error) {
		for i := range groups {
			if groups[i].Name == name && groups[i].RepoKey == repoKey {
				groups[i].Expanded = expanded
				return groups, nil
			}
		}
		return nil, fmt.Errorf("group not found: %s/%s", repoKey, name)
	})
}
