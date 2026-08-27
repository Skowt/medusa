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

// Base Branch dialog options, in the order handleCreateDialogResult switches on
// them:
// index 0 = remote default, 1 = checked out, 2 = named branch.
var branchModeOptions = []string{"Latest remote default", "Checked out branch", "Pick a branch"}

var branchModeHints = []string{
	"Fetches origin, then branches from this repo's default branch (main, master, or develop).",
	"Branches from whatever this repo currently has checked out. Does not fetch.",
	"Type a branch name. Looked up locally, then on origin.",
}

// branchPickMessage describes how a picked branch is resolved. Multi-repo
// workspaces get the extra sentence because the partial-match rule only applies
// to them — a single repo without the branch is an error.
func branchPickMessage(repoCount int) string {
	msg := "Looked up locally first, then on origin."
	if repoCount > 1 {
		msg += " Repos without this branch use their default branch instead."
	}
	return msg
}

// handleCreateDialogResult handles the dialogs of the New Workspace flow, which
// is a chain rather than one dialog: repo, name, profile, group, and — only when
// a worktree is being made — the base branch. Returns (cmd, true) when the ID
// belongs to that chain, so the caller falls through to its main switch for
// everything else, matching handleGroupDialogResult.
func (a *App) handleCreateDialogResult(result common.DialogResult, workspace *data.Workspace, defaultName string) (tea.Cmd, bool) {
	switch result.ID {
	case DialogSelectRecentRepos:
		// Use the snapshot stored when the dialog was opened to avoid race conditions
		recents := a.dialogRecents
		a.dialogRecents = nil
		if result.Index >= len(recents) {
			// User chose "Select a repo…" — open file picker
			a.showRepoFilePicker()
			return nil, true
		}
		// User chose a recent repo — use it directly
		entry := recents[result.Index]
		repos := make([]data.RepoRef, len(entry.Repos))
		copy(repos, entry.Repos)
		a.showNameWorkspaceDialog(repos)
		return nil, true

	case DialogAddRepos:
		// The repo picker is single-select: one path, straight to the name step.
		if result.Value != "" {
			repo := data.RepoRef{Path: result.Value, Name: filepath.Base(result.Value)}
			a.showNameWorkspaceDialog([]data.RepoRef{repo})
			return nil, true
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
				}, true
			}
			// Profile is already set on workspace from the dialog message — skip profile picker
			a.dialogWorkspace = workspace
			a.dialogDefaultName = name
			a.dialog = common.NewSelectDialog(
				DialogSelectBranchMode,
				"Base Branch",
				"Which branch should this worktree be based on?",
				branchModeOptions,
			)
			a.dialog.SetOptionHints(branchModeHints)
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
			return nil, true
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
				}, true
			}
			// Capture checkboxes and re-set dialog state for chaining. Runtime is
			// how the worktree choice rides the rest of the dialog chain, and it
			// is what gets persisted on the workspace.
			workspace.Runtime = data.RuntimeLocalWorktree
			if !result.CheckboxValue {
				workspace.Runtime = data.RuntimeLocalCheckout
			}
			workspace.CopyIgnored = result.CheckboxValue && result.Checkbox2Value
			a.rememberCreateWorktree(result.CheckboxValue)
			a.dialogWorkspace = workspace
			a.dialogDefaultName = name
			a.showProfilePickerForCreate()
			return nil, true
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
				return nil, true
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
			a.showGroupPickerForCreate()
			return nil, true
		}

	case DialogSelectBranchMode:
		if workspace != nil && len(workspace.Repos) > 0 {
			name := defaultName
			repos := workspace.Repos
			wsProfile := workspace.Profile
			wsGroup := workspace.Group
			copyIgnored := workspace.CopyIgnored
			switch result.Index {
			case 0: // Latest remote default
				steps := []string{"Fetching latest changes", "Creating worktree"}
				if copyIgnored {
					steps = append(steps, "Copying gitignored files")
				}
				a.creationOverlay = common.NewProgressOverlay("Creating Workspace", steps)
				a.creationOverlay.SetStepDetail(repos[0].Name)
				a.creationOverlay.SetSize(a.width, a.height)
				return a.fetchRemoteBase(repos, name, wsProfile, wsGroup, copyIgnored), true
			case 1: // Checked out branch
				steps := []string{"Resolving checked out branch", "Creating worktree"}
				if copyIgnored {
					steps = append(steps, "Copying gitignored files")
				}
				a.creationOverlay = common.NewProgressOverlay("Creating Workspace", steps)
				a.creationOverlay.SetStepDetail(repos[0].Name)
				a.creationOverlay.SetSize(a.width, a.height)
				return a.fetchCheckedOutBase(repos, name, wsProfile, wsGroup, copyIgnored), true
			case 2: // Pick a branch
				a.dialogWorkspace = workspace
				a.dialogDefaultName = name
				a.dialog = common.NewInputDialog(DialogCustomBranch, "Pick a Branch", "")
				a.dialog.SetMessage(branchPickMessage(len(repos)))
				a.dialog.SetSize(a.width, a.height)
				a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
				a.dialog.Show()
				return nil, true
			}
		}

	case DialogCustomBranch:
		customBranch := validation.SanitizeInput(result.Value)
		if customBranch == "" {
			return nil, true
		}
		if workspace != nil && len(workspace.Repos) > 0 {
			name := defaultName
			repos := workspace.Repos
			wsProfile := workspace.Profile
			wsGroup := workspace.Group
			copyIgnored := workspace.CopyIgnored
			steps := []string{"Resolving branch", "Creating worktree"}
			if copyIgnored {
				steps = append(steps, "Copying gitignored files")
			}
			a.creationOverlay = common.NewProgressOverlay("Creating Workspace", steps)
			a.creationOverlay.SetStepDetail(repos[0].Name)
			a.creationOverlay.SetSize(a.width, a.height)
			return a.fetchCustomBase(repos, name, wsProfile, wsGroup, customBranch, copyIgnored), true
		}

	default:
		return nil, false
	}

	return nil, true
}

// showNameWorkspaceDialog sets up the workspace name input dialog for a repo.
//
// The worktree checkbox is the step's real decision, not a detail: with it on
// the name also becomes a branch name and a directory under the workspaces
// root, and with it off the workspace is the repo itself and the name is only a
// label. That is why the branch-name clash is checked against the checkbox
// rather than always — with no worktree there is no branch to clash with.
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
		if a.dialog == nil || !a.dialog.CheckboxValue() {
			return ""
		}
		for _, repo := range a.dialogWorkspace.Repos {
			if git.BranchExists(repo.Path, s) {
				return fmt.Sprintf("branch already exists in %s", repo.Name)
			}
		}
		return ""
	})
	a.dialog.SetCheckbox("Create a git worktree", a.config.UI.LastCreateWorktree)
	a.dialog.SetCheckboxDescription(1,
		"On: The agent gets its own directory and branch.\n"+
			"Off: The agent works in the selected repo, on the branch you have "+
			"checked out.")
	a.dialog.SetCheckbox2("Copy gitignored files", true)
	a.dialog.SetCheckbox2RequiresFirst(true)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// rememberCreateWorktree persists the worktree checkbox so the next New
// Workspace opens on the same choice.
func (a *App) rememberCreateWorktree(worktree bool) {
	if a.config == nil || a.config.UI.LastCreateWorktree == worktree {
		return
	}
	a.config.UI.LastCreateWorktree = worktree
	if err := a.config.SaveUISettings(); err != nil {
		logging.Warn("Failed to save worktree preference: %v", err)
	}
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
