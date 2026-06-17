package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/ide"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// handleIDEInstallsDetected builds and shows the IDE picker over the detected
// installs, pre-selecting the remembered choice.
func (a *App) handleIDEInstallsDetected(msg common.IDEInstallsDetected) {
	a.idePicker = common.NewIDEPicker(msg.Installs, a.config.UI.IDE, msg.Root)
	a.idePicker.SetSize(a.width, a.height)
	a.idePicker.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.idePicker.Show()
}

// handleIDEPickerResult persists the chosen install and opens the workspace in
// it. A cancel (Esc) clears the picker and does nothing else.
func (a *App) handleIDEPickerResult(msg common.IDEPickerResult) tea.Cmd {
	a.idePicker = nil
	if !msg.Confirmed {
		return nil
	}
	a.config.UI.IDE = msg.Install.LaunchPath
	_ = a.config.SaveUISettings() // best-effort; a failed save just re-prompts next time

	install := msg.Install
	root := msg.Root
	return func() tea.Msg {
		if err := ide.Open(install, root); err != nil {
			return messages.Toast{Message: "Failed to open IDE: " + err.Error(), Level: messages.ToastError}
		}
		return messages.Toast{Message: "Opened in " + install.Name, Level: messages.ToastSuccess}
	}
}
