package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/ui/common"
)

// routeDialogMsg handles the two messages the Dialog widget emits, reporting
// whether it consumed the message. Both are routed ahead of the overlay chain
// in App.update: a DialogResult arrives after its dialog hid itself, and a
// DialogSelectChanged while the dialog is still open, so neither may be
// shadowed by an overlay that happens to be visible.
func (a *App) routeDialogMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case common.DialogResult:
		logging.Info("Received DialogResult: id=%s confirmed=%v", msg.ID, msg.Confirmed)
		switch msg.ID {
		case DialogAddRepos, DialogAddReposToWorkspace, DialogCreateWorkspace, DialogDeleteWorkspace, DialogCustomizeTab, DialogQuit, DialogCleanupTmux, DialogSetProfile, DialogRenameWorkspace, DialogRenameProfile, DialogCreateProfile, DialogDeleteProfile, DialogCommit,
			DialogSelectBranchMode, DialogCustomBranch, DialogSelectRecentRepos, DialogCloseTab, DialogSetProfileForCreate, DialogQuickDuplicate,
			DialogArchiveWorkspace, DialogArchivedWorkspace, DialogSetNote,
			DialogSetWorkspaceGroup, DialogSetGroupForCreate, DialogRenameGroup, DialogDeleteGroup:
			return a, a.safeCmd(a.handleDialogResult(msg)), true
		}
		// If not an App-level dialog, let it fall through to components.
		// Currently only Center uses custom dialogs.
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return a, a.safeCmd(cmd), true

	case common.DialogSelectChanged:
		// A select field asked its owner to rebuild the dialog around a new
		// value — the New Tab dialog swapping in the chosen assistant's fields.
		a.handleDialogSelectChanged(msg)
		return a, nil, true
	}
	return a, nil, false
}
