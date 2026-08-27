package app

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/validation"
)

// handleDialogResult handles dialog completion
func (a *App) handleDialogResult(result common.DialogResult) tea.Cmd {
	workspace := a.dialogWorkspace
	defaultName := a.dialogDefaultName
	workspaceRoot := a.dialogWorkspaceRoot
	profile := a.dialogProfile
	a.dialog = nil
	a.dialogWorkspace = nil
	a.dialogDefaultName = ""
	a.dialogWorkspaceRoot = ""
	a.dialogProfile = ""
	logging.Debug("Dialog result: id=%s confirmed=%v value=%s", result.ID, result.Confirmed, result.Value)

	if !result.Confirmed {
		a.pendingProfileLaunch = ""
		a.pendingProfileLaunchRoot = ""
		logging.Debug("Dialog cancelled")
		// Return to profile manager if we were creating/renaming/deleting a profile
		if result.ID == DialogCreateProfile || result.ID == DialogRenameProfile || result.ID == DialogDeleteProfile {
			return func() tea.Msg { return common.ShowProfileManager{} }
		}
		return nil
	}

	// Delegate to group dialog handler (handles confirmed group dialogs)
	if cmd, handled := a.handleGroupDialogResult(result.ID, result.Confirmed, result.Value, workspace, defaultName); handled {
		return cmd
	}

	// Delegate to the New Workspace flow (repo → name → profile → group → base).
	if cmd, handled := a.handleCreateDialogResult(result, workspace, defaultName); handled {
		return cmd
	}

	switch result.ID {
	case DialogAddReposToWorkspace:
		// File picker returns repos to add to existing workspace
		if workspace != nil && len(result.Values) > 0 {
			repos := make([]data.RepoRef, len(result.Values))
			for i, p := range result.Values {
				repos[i] = data.RepoRef{Path: p, Name: filepath.Base(p)}
			}
			ws := workspace
			return func() tea.Msg {
				return messages.AddReposToWorkspace{Workspace: ws, Repos: repos}
			}
		}

	case DialogArchiveWorkspace:
		if workspace != nil {
			ws := workspace
			return func() tea.Msg {
				return messages.ArchiveWorkspace{Workspace: ws}
			}
		}

	case DialogArchivedWorkspace:
		if workspace != nil {
			ws := workspace
			switch result.Index {
			case 0: // Unarchive
				return func() tea.Msg {
					return messages.UnarchiveWorkspace{Workspace: ws}
				}
			case 1: // Delete
				if ws.IsOrphaned() {
					return func() tea.Msg {
						return messages.DeleteOrphanWorkspace{Workspace: ws}
					}
				}
				return func() tea.Msg {
					return messages.DeleteWorkspace{Workspace: ws}
				}
			default: // Cancel
				return nil
			}
		}

	case DialogDeleteWorkspace:
		if workspace != nil {
			ws := workspace
			if ws.IsOrphaned() {
				return func() tea.Msg {
					return messages.DeleteOrphanWorkspace{Workspace: ws}
				}
			}
			return func() tea.Msg {
				return messages.DeleteWorkspace{
					Workspace: ws,
				}
			}
		}

	case DialogCustomizeTab:
		if a.activeWorkspace != nil {
			launch := a.newTabLaunchFromDialog(a.activeWorkspace, result)
			return func() tea.Msg { return launch }
		}

	case DialogSetNote:
		if workspace != nil {
			note := validation.SanitizeInput(result.Value)
			ws := workspace
			return func() tea.Msg {
				return messages.SetWorkspaceNote{Workspace: ws, Note: note}
			}
		}

	case DialogRenameWorkspace:
		if workspace != nil {
			name := validation.SanitizeInput(result.Value)
			if name == "" || name == workspace.Name {
				return nil // No change
			}
			if err := validation.ValidateWorkspaceName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating workspace name"}
				}
			}
			ws := workspace
			return func() tea.Msg {
				return messages.RenameWorkspace{Workspace: ws, NewName: name}
			}
		}

	case DialogSetProfile:
		if workspace != nil {
			if result.Value == common.NewProfileOption {
				// User chose "New profile..." — show the input dialog
				a.dialogWorkspace = workspace
				a.dialogDefaultName = "Default"
				a.dialog = common.NewInputDialog(DialogSetProfile, "Set Profile", "Default")
				a.dialog.SetMessage("Profile isolates Claude settings (permissions, memory) for this workspace.")
				a.dialog.SetSize(a.width, a.height)
				a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
				a.dialog.Show()
				return nil
			}
			selectedProfile := result.Value
			if selectedProfile == "" {
				selectedProfile = defaultName
			}
			ws := workspace
			return func() tea.Msg {
				return messages.SetWorkspaceProfile{
					Workspace: ws,
					Profile:   selectedProfile,
				}
			}
		}

	case DialogRenameProfile:
		if profile != "" {
			newName := validation.SanitizeInput(result.Value)
			if newName == "" || newName == profile {
				return nil // No change
			}
			if err := validation.ValidateProfileName(newName); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating profile name"}
				}
			}
			oldName := profile
			return func() tea.Msg {
				return messages.RenameProfile{OldName: oldName, NewName: newName}
			}
		}

	case DialogCreateProfile:
		name := validation.SanitizeInput(result.Value)
		if name == "" {
			return func() tea.Msg { return common.ShowProfileManager{} }
		}
		if err := validation.ValidateProfileName(name); err != nil {
			return func() tea.Msg {
				return messages.Error{Err: err, Context: "validating profile name"}
			}
		}
		return func() tea.Msg {
			return messages.CreateProfile{Name: name}
		}

	case DialogCloseTab:
		idx := a.dialogCloseTabIdx
		switch result.Index {
		case 0: // Close
			return func() tea.Msg {
				return messages.ConfirmCloseTab{Index: idx}
			}
		case 1: // Restart
			return func() tea.Msg {
				return messages.ConfirmRestartTab{Index: idx}
			}
		default: // Cancel
			return nil
		}

	case DialogDeleteProfile:
		if profile != "" {
			p := profile
			return func() tea.Msg {
				return messages.DeleteProfile{Profile: p}
			}
		}

	case DialogQuit:
		// Persist workspace tabs synchronously before shutdown.
		a.persistAllWorkspacesNow()
		a.Shutdown()
		a.quitting = true
		return tea.Quit

	case DialogCleanupTmux:
		return func() tea.Msg { return messages.CleanupTmuxSessions{} }

	case DialogCommit:
		if workspaceRoot != "" && result.Value != "" {
			message := validation.SanitizeInput(result.Value)
			root := workspaceRoot
			return func() tea.Msg {
				hash, err := git.CreateCommit(root, message)
				return messages.ActionBarCommitResult{
					Success:    err == nil,
					CommitHash: hash,
					Err:        err,
				}
			}
		}
	}

	return nil
}

func (a *App) showQuitDialog() {
	if a.dialog != nil && a.dialog.Visible() {
		return
	}
	a.dialog = common.NewConfirmDialog(
		DialogQuit,
		"Quit MEDUSA",
		"Are you sure you want to quit?",
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}
