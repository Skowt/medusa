package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
)

// deleteOrphanWorkspace removes an orphaned workspace.
// Metadata orphans: remove store + registry entries.
// Directory orphans: remove the directory from disk.
func (a *App) deleteOrphanWorkspace(ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.OrphanWorkspaceDeleted{Workspace: ws}
		}
	}
	return func() tea.Msg {
		var err error
		switch ws.Orphan {
		case data.OrphanMetadata:
			if storeErr := a.workspaces.Delete(ws.ID()); storeErr != nil {
				err = fmt.Errorf("remove workspace store: %w", storeErr)
			}
			if regErr := a.registry.RemoveWorkspace(string(ws.ID())); regErr != nil && err == nil {
				err = fmt.Errorf("remove registry entry: %w", regErr)
			}
			// Removing an ID the registry does not hold succeeds, and deleting a
			// store directory that is not there succeeds too, so a workspace
			// answering to the wrong ID was reported as cleaned up and came
			// straight back on the next load. Check rather than assume: an
			// orphan the user cannot get rid of is worse when it claims to be
			// gone every time. The check is by name as well as ID, since a
			// mismatched ID is the very thing that would slip past it.
			if err == nil && a.registryStillLists(ws) {
				err = fmt.Errorf("registry still lists %s", ws.Name)
			}
		case data.OrphanDirectory:
			if ws.OrphanPath != "" {
				if rmErr := forceRemoveAll(ws.OrphanPath); rmErr != nil {
					err = fmt.Errorf("remove %s: %w", ws.OrphanPath, rmErr)
				}
			}
		}
		return messages.OrphanWorkspaceDeleted{Workspace: ws, Err: err}
	}
}

// registryStillLists reports whether the registry has an entry that is still
// this workspace. Name counts alongside ID because two live workspaces may not
// share one, and it is the only key that survives an ID the delete missed.
func (a *App) registryStillLists(ws *data.Workspace) bool {
	entries, err := a.registry.ListWorkspaces()
	if err != nil {
		// Nothing was proved either way, and claiming the cleanup failed on a
		// read error would be its own false report.
		logging.Warn("Failed to re-read the registry after an orphan cleanup: %v", err)
		return false
	}
	for _, entry := range entries {
		if entry.ID == string(ws.ID()) || entry.Name == ws.Name {
			return true
		}
	}
	return false
}

// forceRemoveAll removes path, first chmod'ing any directories under it to
// 0o700 so os.RemoveAll can unlink their contents. Needed for Go module
// cache trees (e.g. .gotools/pkg/mod/) which are created mode 0o555.
func forceRemoveAll(path string) error {
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(path)
}
