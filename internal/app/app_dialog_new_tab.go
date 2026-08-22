package app

import (
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// handleShowCustomizeTabDialog opens the New Tab dialog on the assistant the
// user last launched.
func (a *App) handleShowCustomizeTabDialog() {
	a.showNewTabDialog(defaultAssistant(a.config.UI.LastAssistant))
}

// showNewTabDialog builds the New Tab dialog for one assistant: an "Assistant"
// cycler on top, then that assistant's own fields. Everything below the
// assistant belongs to it alone — Claude's permission modes and Codex's sandbox
// policies share no values — so cycling the assistant rebuilds the dialog
// rather than reinterpreting the fields already on screen.
func (a *App) showNewTabDialog(assistant string) {
	if a.activeWorkspace == nil {
		return
	}
	title := "New Claude Tab"
	if assistant == assistantCodex {
		title = "New Codex Tab"
	}
	a.dialog = common.NewInputDialog(DialogCustomizeTab, title, "")
	a.dialog.SetInputHidden(true)
	a.dialog.SetMessage("Configure settings for this tab.")
	a.dialog.SetSelect("Assistant:", assistantOptions(), assistant)
	a.dialog.SetSelectNotifiesChange(0)

	if assistant == assistantCodex {
		a.configureCodexTabFields()
	} else {
		a.configureClaudeTabFields()
	}

	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	// The assistant cycler is the dialog's primary control, and a rebuild has
	// to land the user back on it rather than at the top of the ring.
	a.dialog.FocusSelect(0)
}

// configureClaudeTabFields adds the Claude-only fields: a "Starting Mode"
// select wired to claude --permission-mode, the sandbox pair, and the
// fullscreen renderer toggle.
func (a *App) configureClaudeTabFields() {
	a.dialog.SetSelect2("Starting Mode:", permissionModeOptions(), defaultPermissionMode(a.config.UI.LastPermissionMode))
	a.dialog.SetCheckbox("Sandboxed", a.config.UI.LastIsolated)
	a.dialog.SetCheckboxDescription(1, "Sandboxes subprocess calls including Bash commands. Tool use does not use sandbox (e.g. Write, Edit).")
	a.dialog.SetCheckbox2("Allow unsandboxed commands", a.config.UI.LastAllowUnsandboxedCommands)
	a.dialog.SetCheckboxDescription(2, "Allows Claude to try run blocked commands outside of the sandbox, using the user's allowed permissions. Do not use in 'Bypass Permissions' mode.")
	a.dialog.SetCheckbox2RequiresFirst(true)
	a.dialog.SetCheckbox3("Fullscreen TUI", a.config.UI.LastFullscreen)
	a.dialog.SetCheckboxDescription(3, "Runs Claude in its fullscreen renderer and forwards the mouse to Claude. Requires Claude Code v2.1.89+.")
}

// configureCodexTabFields adds the Codex-only fields. Codex sandboxes natively
// rather than through a settings file, so its sandbox is a policy select rather
// than Claude's checkbox pair, and it has no fullscreen renderer to choose.
func (a *App) configureCodexTabFields() {
	a.dialog.SetSelect2("Sandbox:", codexSandboxOptions(), defaultCodexSandbox(a.config.UI.LastCodexSandbox))
	a.dialog.SetCheckbox("Ask for approval", codexApprovalAsks(a.config.UI.LastCodexApproval))
	a.dialog.SetCheckboxDescription(1, "Codex asks before running a command the sandbox would block. Unchecked, it never asks and a blocked command just fails.")
	a.dialog.SetCheckbox2("Enable web search", a.config.UI.LastCodexSearch)
	a.dialog.SetCheckboxDescription(2, "Gives Codex its live web-search tool, which it may use without asking.")
}

// handleDialogSelectChanged rebuilds the New Tab dialog when its assistant
// cycler moves. Nothing else uses the notification.
func (a *App) handleDialogSelectChanged(msg common.DialogSelectChanged) {
	if msg.ID != DialogCustomizeTab || msg.Slot != 0 {
		return
	}
	a.showNewTabDialog(defaultAssistant(msg.Value))
}

// newTabLaunchFromDialog turns a New Tab dialog result into a launch request,
// persisting the choices so the next tab opens on them.
func (a *App) newTabLaunchFromDialog(ws *data.Workspace, result common.DialogResult) messages.LaunchAgent {
	assistant := defaultAssistant(result.SelectValue)
	a.config.UI.LastAssistant = assistant

	launch := messages.LaunchAgent{Assistant: assistant, Workspace: ws}
	if assistant == assistantCodex {
		launch.CodexSandbox = defaultCodexSandbox(result.Select2Value)
		launch.CodexApproval = codexApprovalValue(result.CheckboxValue)
		launch.CodexSearch = result.Checkbox2Value
		a.config.UI.LastCodexSandbox = launch.CodexSandbox
		a.config.UI.LastCodexApproval = launch.CodexApproval
		a.config.UI.LastCodexSearch = launch.CodexSearch
	} else {
		launch.Isolated = result.CheckboxValue
		launch.AllowUnsandboxedCommands = result.Checkbox2Value
		launch.PermissionMode = defaultPermissionMode(result.Select2Value)
		launch.Fullscreen = result.Checkbox3Value
		a.config.UI.LastIsolated = launch.Isolated
		a.config.UI.LastAllowUnsandboxedCommands = launch.AllowUnsandboxedCommands
		a.config.UI.LastPermissionMode = launch.PermissionMode
		a.config.UI.LastFullscreen = launch.Fullscreen
	}
	_ = a.config.SaveUISettings()
	return launch
}

// lastUsedLaunch builds a launch request from the sticky New Tab settings, for
// the paths that open a tab without asking: the new-agent-tab key, a profile
// launch, and a freshly created workspace.
func (a *App) lastUsedLaunch(ws *data.Workspace, assistant string) messages.LaunchAgent {
	launch := messages.LaunchAgent{Assistant: defaultAssistant(assistant), Workspace: ws}
	if launch.Assistant == assistantCodex {
		launch.CodexSandbox = defaultCodexSandbox(a.config.UI.LastCodexSandbox)
		launch.CodexApproval = codexApprovalValue(codexApprovalAsks(a.config.UI.LastCodexApproval))
		launch.CodexSearch = a.config.UI.LastCodexSearch
		return launch
	}
	launch.Isolated = a.config.UI.LastIsolated
	launch.AllowUnsandboxedCommands = a.config.UI.LastAllowUnsandboxedCommands
	launch.PermissionMode = defaultPermissionMode(a.config.UI.LastPermissionMode)
	launch.Fullscreen = a.config.UI.LastFullscreen
	return launch
}
