package workspaceops

import (
	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/pty"
)

const (
	AssistantClaude = "claude"
	AssistantCodex  = "codex"
)

// Launch is how an agent tab starts. Only the fields belonging to Assistant
// are set: Claude's permission modes and Codex's sandbox policies share no
// values, and each assistant exits on the other's flags.
type Launch struct {
	Assistant                string
	Isolated                 bool
	AllowUnsandboxedCommands bool
	PermissionMode           string
	Fullscreen               bool
	CodexSandbox             string
	CodexAuto                bool
}

// LaunchOverrides are explicit choices that win over the saved settings. A
// zero value (empty string, nil pointer) means "use what is saved".
type LaunchOverrides struct {
	Assistant      string
	PermissionMode string
	Fullscreen     *bool
	CodexSandbox   string
	CodexAuto      *bool
}

// ResolveLaunch builds the launch a tab opens with when nobody is asked: the
// assistant last used under profile (falling back to the global one), and the
// global "last used" values for everything else. The TUI's no-dialog launches
// and the CLI both come through here, so they cannot drift apart.
func ResolveLaunch(ui config.UISettings, profile string, o LaunchOverrides) Launch {
	assistant := o.Assistant
	if assistant == "" {
		assistant = ui.AssistantFor(profile)
	}
	l := Launch{Assistant: DefaultAssistant(assistant)}
	if l.Assistant == AssistantCodex {
		l.CodexSandbox = DefaultCodexSandbox(ui.LastCodexSandbox)
		if o.CodexSandbox != "" {
			l.CodexSandbox = o.CodexSandbox
		}
		l.CodexAuto = DefaultCodexStartingMode(ui.LastCodexStartingMode) == "auto"
		if o.CodexAuto != nil {
			l.CodexAuto = *o.CodexAuto
		}
		return l
	}
	l.Isolated = ui.LastIsolated
	l.AllowUnsandboxedCommands = ui.LastAllowUnsandboxedCommands
	l.PermissionMode = DefaultPermissionMode(ui.LastPermissionMode)
	if o.PermissionMode != "" {
		l.PermissionMode = o.PermissionMode
	}
	l.Fullscreen = ui.LastFullscreen
	if o.Fullscreen != nil {
		l.Fullscreen = *o.Fullscreen
	}
	return l
}

// DefaultAssistant maps a stored assistant to one medusa can launch. Anything
// unrecognised — a downgrade reading a newer config, a hand-edited file —
// falls back to Claude rather than to a command that is not there.
func DefaultAssistant(last string) string {
	if last == AssistantCodex {
		return AssistantCodex
	}
	return AssistantClaude
}

// DefaultCodexSandbox returns a stored sandbox policy if codex accepts it,
// and workspace-write otherwise.
func DefaultCodexSandbox(last string) string {
	switch last {
	case pty.CodexSandboxReadOnly, pty.CodexSandboxWorkspace, pty.CodexSandboxFullAccess:
		return last
	}
	return pty.CodexSandboxWorkspace
}

// DefaultCodexStartingMode returns "default" only when that was stored;
// automatic review is medusa's default Codex mode.
func DefaultCodexStartingMode(last string) string {
	if last == "default" {
		return "default"
	}
	return "auto"
}

// DefaultPermissionMode returns the stored Claude permission mode, falling
// back to "auto" when nothing has been saved yet.
func DefaultPermissionMode(last string) string {
	if last == "" {
		return "auto"
	}
	return last
}
