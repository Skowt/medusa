package app

import (
	tea "charm.land/bubbletea/v2"
)

// handleToggleCaffeinate starts or stops `caffeinate -d` from the toolbar's
// [C] button. It asks the keeper rather than the button's colour, so a
// caffeinate killed from outside is simply started again on the next press.
func (a *App) handleToggleCaffeinate() tea.Cmd {
	if a.caffeinate == nil {
		return nil
	}
	if a.caffeinate.Active() {
		a.caffeinate.Stop()
		a.dashboard.SetCaffeinate(true, false)
		return a.toast.ShowInfo("Caffeination disabled (Mac can sleep again)")
	}
	if err := a.caffeinate.Start(); err != nil {
		a.dashboard.SetCaffeinate(true, false)
		return a.toast.ShowError("Could not start caffeinate: " + err.Error())
	}
	a.dashboard.SetCaffeinate(true, true)
	return a.toast.ShowInfo("Caffeination enabled (Mac will stay awake)")
}
