package app

import (
	"fmt"
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

	switch result.ID {
	case DialogSelectRecentRepos:
		// Use the snapshot stored when the dialog was opened to avoid race conditions
		recents := a.dialogRecents
		a.dialogRecents = nil
		if result.Index >= len(recents) {
			// User chose "Select repos…" — open file picker
			a.showRepoFilePicker()
			return nil
		}
		// User chose a recent combo — use those repos directly
		entry := recents[result.Index]
		repos := make([]data.RepoRef, len(entry.Repos))
		copy(repos, entry.Repos)
		a.showNameWorkspaceDialog(repos)
		return nil

	case DialogAddRepos:
		// File picker returns selected repos
		if len(result.Values) > 0 {
			repos := make([]data.RepoRef, len(result.Values))
			for i, p := range result.Values {
				repos[i] = data.RepoRef{Path: p, Name: filepath.Base(p)}
			}
			a.showNameWorkspaceDialog(repos)
			return nil
		}

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

	case DialogQuickDuplicate:
		if workspace != nil && len(workspace.Repos) > 0 {
			name := validation.SanitizeInput(result.Value)
			if name == "" {
				name = defaultName
			}
			if err := validation.ValidateWorkspaceName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating workspace name"}
				}
			}
			// Profile is already set on workspace from the dialog message — skip profile picker
			a.dialogWorkspace = workspace
			a.dialogDefaultName = name
			a.dialog = common.NewSelectDialog(
				DialogSelectBranchMode,
				"Base Branch",
				"Which branch should this worktree be based on?",
				[]string{"Latest remote main", "Checked out branch", "Custom branch"},
			)
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
			return nil
		}

	case DialogCreateWorkspace:
		if workspace != nil && len(workspace.Repos) > 0 {
			name := validation.SanitizeInput(result.Value)
			if name == "" {
				name = defaultName
			}
			if err := validation.ValidateWorkspaceName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating workspace name"}
				}
			}
			// Capture checkbox and re-set dialog state for chaining
			workspace.CopyIgnored = result.CheckboxValue
			a.dialogWorkspace = workspace
			a.dialogDefaultName = name
			a.showProfilePickerForCreate()
			return nil
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
			// Read checkbox values and save as last-used settings
			allowEdits := result.CheckboxValue
			isolated := result.Checkbox2Value
			skipPerms := result.Checkbox3Value
			a.config.UI.LastAllowEdits = allowEdits
			a.config.UI.LastIsolated = isolated
			a.config.UI.LastSkipPermissions = skipPerms
			_ = a.config.SaveUISettings()
			ws := a.activeWorkspace
			return func() tea.Msg {
				return messages.LaunchAgent{
					Assistant:       "claude",
					Workspace:       ws,
					AllowEdits:      allowEdits,
					Isolated:        isolated,
					SkipPermissions: skipPerms,
				}
			}
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

	case DialogSetProfileForCreate:
		if workspace != nil && len(workspace.Repos) > 0 {
			if result.Value == common.NewProfileOption {
				// User chose "New profile..." — show the input dialog, chain back
				a.dialogWorkspace = workspace
				a.dialogDefaultName = defaultName
				a.dialog = common.NewInputDialog(DialogSetProfileForCreate, "Set Profile", "Default")
				a.dialog.SetMessage("Profile isolates Claude settings (permissions, memory) for this workspace.")
				a.dialog.SetSize(a.width, a.height)
				a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
				a.dialog.Show()
				return nil
			}
			selectedProfile := result.Value
			if selectedProfile == "" {
				selectedProfile = "Default"
			}
			workspace.Profile = selectedProfile
			// Track as most recently chosen profile
			a.config.UI.LastProfile = selectedProfile
			_ = a.config.SaveUISettings()
			a.dialogWorkspace = workspace
			a.dialogDefaultName = defaultName
			a.dialog = common.NewSelectDialog(
				DialogSelectBranchMode,
				"Base Branch",
				"Which branch should this worktree be based on?",
				[]string{"Latest remote main", "Checked out branch", "Custom branch"},
			)
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
			return nil
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

	case DialogSelectBranchMode:
		if workspace != nil && len(workspace.Repos) > 0 {
			name := defaultName
			repos := workspace.Repos
			wsProfile := workspace.Profile
			wsGroup := workspace.Group
			copyIgnored := workspace.CopyIgnored
			switch result.Index {
			case 0: // Latest remote main
				steps := []string{"Fetching latest changes", "Creating worktree"}
				if copyIgnored {
					steps = append(steps, "Copying gitignored files")
				}
				a.creationOverlay = common.NewProgressOverlay("Creating Workspace", steps)
				a.creationOverlay.SetStepDetail(repos[0].Name)
				a.creationOverlay.SetSize(a.width, a.height)
				return a.fetchRemoteBase(repos, name, wsProfile, wsGroup, copyIgnored)
			case 1: // Checked out branch
				steps := []string{"Resolving checked out branch", "Creating worktree"}
				if copyIgnored {
					steps = append(steps, "Copying gitignored files")
				}
				a.creationOverlay = common.NewProgressOverlay("Creating Workspace", steps)
				a.creationOverlay.SetStepDetail(repos[0].Name)
				a.creationOverlay.SetSize(a.width, a.height)
				return a.fetchCheckedOutBase(repos, name, wsProfile, wsGroup, copyIgnored)
			case 2: // Custom branch
				a.dialogWorkspace = workspace
				a.dialogDefaultName = name
				a.dialog = common.NewInputDialog(DialogCustomBranch, "Custom Branch", "")
				a.dialog.SetMessage("Branch will be looked up locally first, then on remote.")
				a.dialog.SetSize(a.width, a.height)
				a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
				a.dialog.Show()
				return nil
			}
		}

	case DialogCustomBranch:
		customBranch := validation.SanitizeInput(result.Value)
		if customBranch == "" {
			return nil
		}
		if workspace != nil && len(workspace.Repos) > 0 {
			name := defaultName
			repos := workspace.Repos
			wsProfile := workspace.Profile
			wsGroup := workspace.Group
			copyIgnored := workspace.CopyIgnored
			steps := []string{"Resolving custom branch", "Creating worktree"}
			if copyIgnored {
				steps = append(steps, "Copying gitignored files")
			}
			a.creationOverlay = common.NewProgressOverlay("Creating Workspace", steps)
			a.creationOverlay.SetStepDetail(repos[0].Name)
			a.creationOverlay.SetSize(a.width, a.height)
			return a.fetchCustomBase(repos, name, wsProfile, wsGroup, customBranch, copyIgnored)
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

// showNameWorkspaceDialog sets up the workspace name input dialog for a given set of repos.
func (a *App) showNameWorkspaceDialog(repos []data.RepoRef) {
	a.dialogWorkspace = &data.Workspace{Repos: repos}
	a.dialogDefaultName = generateWorkspaceName(repos)
	a.dialog = common.NewInputDialog(DialogCreateWorkspace, "Name Your Workspace", a.dialogDefaultName)
	a.dialog.SetMessage("Enter a name for the workspace.")
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
	a.dialog.SetCheckbox("Copy gitignored files", true)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// showProfilePickerForCreate shows the profile picker during workspace creation flow.
func (a *App) showProfilePickerForCreate() {
	profiles := a.listProfiles()
	if len(profiles) > 0 {
		a.dialog = common.NewProfilePicker(DialogSetProfileForCreate, profiles, "")
	} else {
		a.dialog = common.NewInputDialog(DialogSetProfileForCreate, "Set Profile", "Default")
		a.dialog.SetMessage("Profile isolates Claude settings (permissions, memory) for this workspace.")
	}
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
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
