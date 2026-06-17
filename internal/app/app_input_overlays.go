package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/ui/common"
)

// routeOverlayInput sends msg to any visible overlay and returns handled=true
// if the overlay consumed the message (blocking further processing).
// Accumulated commands are appended to *cmds.
func (a *App) routeOverlayInput(msg tea.Msg, cmds *[]tea.Cmd) bool {
	// Handle toast updates
	if _, ok := msg.(common.ToastDismissed); ok {
		newToast, cmd := a.toast.Update(msg)
		a.toast = newToast
		*cmds = append(*cmds, cmd)
	}

	// Block input while creation overlay is visible
	if a.creationOverlay != nil && a.creationOverlay.Visible() {
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return true
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return true
		}
	}

	// Handle dialog input if visible
	if a.dialog != nil && a.dialog.Visible() {
		newDialog, cmd := a.dialog.Update(msg)
		a.dialog = newDialog
		if cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return true
		}
		if _, ok := msg.(tea.PasteMsg); ok {
			return true
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return true
		}
	}

	// Handle file picker if visible
	if a.filePicker != nil && a.filePicker.Visible() {
		newPicker, cmd := a.filePicker.Update(msg)
		a.filePicker = newPicker
		if cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return true
		}
		if _, ok := msg.(tea.PasteMsg); ok {
			return true
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return true
		}
	}

	// Handle settings dialog if visible
	if a.settingsDialog != nil && a.settingsDialog.Visible() {
		newSettings, cmd := a.settingsDialog.Update(msg)
		a.settingsDialog = newSettings
		if cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return true
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return true
		}
	}

	// Handle theme dialog if visible
	if a.themeDialog != nil && a.themeDialog.Visible() {
		newTheme, cmd := a.themeDialog.Update(msg)
		a.themeDialog = newTheme
		if cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return true
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return true
		}
	}

	// Handle sound picker if visible
	if a.soundPicker != nil && a.soundPicker.Visible() {
		newPicker, cmd := a.soundPicker.Update(msg)
		a.soundPicker = newPicker
		if cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return true
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return true
		}
	}

	// Handle IDE picker if visible
	if a.idePicker != nil && a.idePicker.Visible() {
		newPicker, cmd := a.idePicker.Update(msg)
		a.idePicker = newPicker
		if cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return true
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return true
		}
	}

	// Handle permissions dialog if visible
	if a.permissionsDialog != nil && a.permissionsDialog.Visible() {
		newDialog, cmd := a.permissionsDialog.Update(msg)
		a.permissionsDialog = newDialog
		if cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return true
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return true
		}
	}

	// Handle permissions editor if visible
	if a.permissionsEditor != nil && a.permissionsEditor.Visible() {
		newEditor, cmd := a.permissionsEditor.Update(msg)
		a.permissionsEditor = newEditor
		if cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return true
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return true
		}
	}

	// Handle profile manager if visible
	if a.profileManager != nil && a.profileManager.Visible() {
		newManager, cmd := a.profileManager.Update(msg)
		a.profileManager = newManager
		if cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return true
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return true
		}
	}

	return false
}
