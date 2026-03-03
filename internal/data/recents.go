package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const maxRecents = 5

// RecentsStore manages recently used repo combinations
type RecentsStore struct {
	path string
}

// RecentEntry stores a recently used repo combination
type RecentEntry struct {
	Repos     []RepoRef `json:"repos"`
	CreatedAt time.Time `json:"created_at"`
}

// NewRecentsStore creates a new recents store at the specified path
func NewRecentsStore(path string) *RecentsStore {
	return &RecentsStore{path: path}
}

// Add prepends a repo combination to the recents list, deduplicating by repo paths
func (s *RecentsStore) Add(repos []RepoRef) error {
	entries, _ := s.List() // ignore error, start fresh if file missing

	// Build key for dedup
	newKey := repoKey(repos)

	// Remove duplicates
	var filtered []RecentEntry
	for _, entry := range entries {
		if repoKey(entry.Repos) != newKey {
			filtered = append(filtered, entry)
		}
	}

	// Prepend new entry
	filtered = append([]RecentEntry{{Repos: repos, CreatedAt: time.Now()}}, filtered...)

	// Cap at max
	if len(filtered) > maxRecents {
		filtered = filtered[:maxRecents]
	}

	return s.save(filtered)
}

// List returns all recent entries
func (s *RecentsStore) List() ([]RecentEntry, error) {
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var entries []RecentEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *RecentsStore) save(entries []RecentEntry) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0644)
}

// repoKey creates a dedup key from repo paths (sorted)
func repoKey(repos []RepoRef) string {
	key := ""
	for _, r := range repos {
		if key != "" {
			key += "\n"
		}
		key += NormalizePath(r.Path)
	}
	return key
}
