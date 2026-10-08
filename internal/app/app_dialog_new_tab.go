package app

import (
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/workspaceops"
)

// handleShowCustomizeTabDialog opens the New Tab dialog on the assistant the
// user last launched under the active workspace's profile.
func (a *App) handleShowCustomizeTabDialog() {
	a.showNewTabDialog(a.stickyAssistant(a.activeWorkspace))
}

// stickyAssistant is the assistant a new tab in ws opens on. It is remembered
// per profile, since profiles tend to be tied to one assistant (a work profile
// on Claude, a personal one on Codex), and falls back to the global last-used
// assistant for a profile that has never launched one.
func (a *App) stickyAssistant(ws *data.Workspace) string {
	profile := ""
	if ws != nil {
		profile = ws.Profile
	}
	return workspaceops.DefaultAssistant(a.config.UI.AssistantFor(profile))
}

// showNewTabDialog builds the New Tab dialog for one assistant: an "Assistant"
// cycler on top, then that assistant's own fields. Everything below the
// assistant belongs to it alone — Claude's permission modes and Codex's sandbox
// policies share no values — so cycling the assistant rebuilds the dialog
// rather than reinterpreting the fields already on screen.
func (a *App) showNewTabDialog(assistant string) {
	a.showNewTabDialogWithCodexValues(assistant, a.config.UI.LastCodexStartingMode, a.config.UI.LastCodexSandbox)
}

func (a *App) showNewTabDialogWithCodexValues(assistant, codexMode, codexSandbox string) {
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
		a.configureCodexTabFields(codexMode, codexSandbox)
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
	a.dialog.SetSelect2("Starting Mode:", permissionModeOptions(), workspaceops.DefaultPermissionMode(a.config.UI.LastPermissionMode))
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
func (a *App) configureCodexTabFields(mode, sandbox string) {
	mode = workspaceops.DefaultCodexStartingMode(mode)
	a.dialog.SetSelect2("Starting Mode:", codexStartingModeOptions(), mode)
	a.dialog.SetSelectNotifiesChange(1)
	a.dialog.SetSelect3("Sandbox:", codexSandboxOptions(), workspaceops.DefaultCodexSandbox(sandbox))
	a.dialog.SetSelectDisabled(2, mode == "auto")
}

// handleDialogSelectChanged rebuilds the New Tab dialog when its assistant
// cycler moves. Nothing else uses the notification.
func (a *App) handleDialogSelectChanged(msg common.DialogSelectChanged) {
	if msg.ID != DialogCustomizeTab {
		return
	}
	if msg.Slot == 0 {
		a.showNewTabDialog(workspaceops.DefaultAssistant(msg.Value))
		return
	}
	if msg.Slot == 1 && a.dialog.SelectValue() == assistantCodex {
		sandbox := a.dialog.Select3Value()
		a.showNewTabDialogWithCodexValues(assistantCodex, msg.Value, sandbox)
		a.dialog.FocusSelect(1)
	}
}

// newTabLaunchFromDialog turns a New Tab dialog result into a launch request,
// persisting the choices so the next tab opens on them.
func (a *App) newTabLaunchFromDialog(ws *data.Workspace, result common.DialogResult) messages.LaunchAgent {
	assistant := workspaceops.DefaultAssistant(result.SelectValue)
	profile := ""
	if ws != nil {
		profile = ws.Profile
	}
	a.config.UI.RememberAssistant(profile, assistant)

	launch := messages.LaunchAgent{Assistant: assistant, Workspace: ws}
	if assistant == assistantCodex {
		mode := workspaceops.DefaultCodexStartingMode(result.Select2Value)
		launch.CodexSandbox = workspaceops.DefaultCodexSandbox(result.Select3Value)
		launch.CodexAuto = mode == "auto"
		a.config.UI.LastCodexSandbox = launch.CodexSandbox
		a.config.UI.LastCodexStartingMode = mode
	} else {
		launch.Isolated = result.CheckboxValue
		launch.AllowUnsandboxedCommands = result.Checkbox2Value
		launch.PermissionMode = workspaceops.DefaultPermissionMode(result.Select2Value)
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
// launch, and a freshly created workspace. The headless CLI resolves through
// the same workspaceops.ResolveLaunch.
func (a *App) lastUsedLaunch(ws *data.Workspace, assistant string) messages.LaunchAgent {
	profile := ""
	if ws != nil {
		profile = ws.Profile
	}
	l := workspaceops.ResolveLaunch(a.config.UI, profile, workspaceops.LaunchOverrides{Assistant: assistant})
	return messages.LaunchAgent{
		Assistant:                l.Assistant,
		Workspace:                ws,
		Isolated:                 l.Isolated,
		AllowUnsandboxedCommands: l.AllowUnsandboxedCommands,
		PermissionMode:           l.PermissionMode,
		Fullscreen:               l.Fullscreen,
		CodexSandbox:             l.CodexSandbox,
		CodexAuto:                l.CodexAuto,
	}
}
