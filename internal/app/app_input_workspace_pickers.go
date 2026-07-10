package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/validation"
)

// handleShowQuickDuplicateDialog shows a name input dialog for quick duplicate with pre-filled repos and profile.
func (a *App) handleShowQuickDuplicateDialog(msg messages.ShowQuickDuplicateDialog) {
	a.dialogWorkspace = &data.Workspace{Repos: msg.Repos, Profile: msg.Profile, CopyIgnored: msg.CopyIgnored, Group: msg.Group}
	a.dialogDefaultName = generateWorkspaceName(msg.Repos)
	a.dialog = common.NewInputDialog(DialogQuickDuplicate, "Quick Duplicate", a.dialogDefaultName)
	a.dialog.SetMessage("Enter a name for the new workspace.")
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateWorkspaceName(s); err != nil {
			return err.Error()
		}
		if a.workspaceNameExists(s) {
			return "workspace with this name already exists"
		}
		for _, repo := range a.dialogWorkspace.Repos {
			if git.BranchExists(repo.Path, s) {
				return fmt.Sprintf("branch already exists in %s", repo.Name)
			}
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowCreateWorkspaceDialog shows a recents picker or falls back to file picker.
func (a *App) handleShowCreateWorkspaceDialog() {
	// Check for recent repo combinations
	if a.recents != nil {
		recents, _ := a.recents.List()
		if len(recents) > 0 {
			logging.Info("Showing recent repos dialog with %d entries", len(recents))
			// Store snapshot so the dialog result handler uses the same list
			a.dialogRecents = recents
			options := make([]string, 0, len(recents)+1)
			for _, entry := range recents {
				options = append(options, formatRecentLabel(entry.Repos))
			}
			options = append(options, "Select repos…")
			a.dialog = common.NewSelectDialog(
				DialogSelectRecentRepos,
				"New Workspace",
				"Choose a recent repo combination or select new repos.",
				options,
			)
			a.dialog.SetVerticalLayout(true)
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
			return
		}
	}
	// No recents — go straight to file picker
	a.showRepoFilePicker()
}

// showRepoFilePicker opens the file picker for selecting repos.
func (a *App) showRepoFilePicker() {
	logging.Info("Showing Add Repos file picker")
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	a.filePicker = common.NewFilePicker(DialogAddRepos, home, true)
	a.filePicker.SetTitle("Select Repos")
	a.filePicker.SetPrimaryActionLabel("Add repo")
	a.filePicker.SetMultiSelect(true)
	a.filePicker.SetValidatePath(func(path string, existing []string) string {
		if !git.IsGitRepository(path) {
			return "Not a git repository"
		}
		if git.IsWorktree(path) {
			return "Worktrees cannot be used as workspace sources"
		}
		for _, p := range existing {
			if p == path {
				return "Already added"
			}
			if strings.HasPrefix(path, p+"/") {
				return "Nested inside " + filepath.Base(p)
			}
			if strings.HasPrefix(p, path+"/") {
				return "Contains already-added " + filepath.Base(p)
			}
		}
		return ""
	})
	a.filePicker.SetSize(a.width, a.height)
	a.filePicker.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.filePicker.Show()
}

// formatRecentLabel formats a recent entry's repos as a display label.
func formatRecentLabel(repos []data.RepoRef) string {
	if len(repos) == 1 {
		return repos[0].Name
	}
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}

// showAddReposToWorkspaceFilePicker opens a file picker for adding repos to an existing workspace.
func (a *App) showAddReposToWorkspaceFilePicker(ws *data.Workspace) {
	if ws == nil {
		return
	}
	if !ws.IsMultiRepo() {
		return
	}
	a.dialogWorkspace = ws

	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}

	// Track existing repos so we can exclude them
	existingPaths := make(map[string]bool, len(ws.Repos))
	for _, r := range ws.Repos {
		existingPaths[r.Path] = true
	}

	a.filePicker = common.NewFilePicker(DialogAddReposToWorkspace, home, true)
	a.filePicker.SetTitle("Add Repos to " + ws.Name)
	a.filePicker.SetPrimaryActionLabel("Add repo")
	a.filePicker.SetMultiSelect(true)
	a.filePicker.SetValidatePath(func(path string, existing []string) string {
		if !git.IsGitRepository(path) {
			return "Not a git repository"
		}
		if git.IsWorktree(path) {
			return "Worktrees cannot be used as workspace sources"
		}
		if existingPaths[path] {
			return "Already in this workspace"
		}
		for _, p := range existing {
			if p == path {
				return "Already added"
			}
			if strings.HasPrefix(path, p+"/") {
				return "Nested inside " + filepath.Base(p)
			}
			if strings.HasPrefix(p, path+"/") {
				return "Contains already-added " + filepath.Base(p)
			}
		}
		return ""
	})
	a.filePicker.SetSize(a.width, a.height)
	a.filePicker.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.filePicker.Show()
}
