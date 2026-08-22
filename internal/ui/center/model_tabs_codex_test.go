package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
)

func allSetOptions() agentTabOptions {
	return agentTabOptions{
		Isolated:                 true,
		AllowUnsandboxedCommands: true,
		PermissionMode:           "plan",
		Fullscreen:               true,
		CodexSandbox:             appPty.CodexSandboxReadOnly,
		CodexApproval:            appPty.CodexApprovalNever,
		CodexSearch:              true,
	}
}

// A Codex tab must never carry Claude's launch settings: codex rejects unknown
// flags, and the tab would drop straight to a bare shell.
func TestOptionsForCodexDropClaudeSettings(t *testing.T) {
	got := allSetOptions().forAssistant("codex")

	if got.PermissionMode != "" || got.Isolated || got.AllowUnsandboxedCommands {
		t.Errorf("Claude settings survived: %+v", got)
	}
	if got.CodexSandbox != appPty.CodexSandboxReadOnly || got.CodexApproval != appPty.CodexApprovalNever || !got.CodexSearch {
		t.Errorf("Codex settings were lost: %+v", got)
	}
}

// Fullscreen is Claude's renderer. Codex reports no mouse and does not take the
// pane's alternate screen, so a fullscreen Codex tab would forward the mouse
// into an app that ignores it and disable medusa's own scrollback instead.
func TestOptionsForCodexClearFullscreen(t *testing.T) {
	if got := allSetOptions().forAssistant("codex"); got.Fullscreen {
		t.Error("a Codex tab must never be marked fullscreen")
	}
}

func TestOptionsForClaudeDropCodexSettings(t *testing.T) {
	got := allSetOptions().forAssistant("claude")

	if got.CodexSandbox != "" || got.CodexApproval != "" || got.CodexSearch {
		t.Errorf("Codex settings survived: %+v", got)
	}
	if !got.Fullscreen || got.PermissionMode != "plan" || !got.Isolated {
		t.Errorf("Claude settings were lost: %+v", got)
	}
}

// Codex options have to survive a restart, which reads them back off the tab.
func TestOptionsRoundTripThroughTab(t *testing.T) {
	tab := &Tab{
		Assistant:     "codex",
		CodexSandbox:  appPty.CodexSandboxFullAccess,
		CodexApproval: appPty.CodexApprovalOnRequest,
		CodexSearch:   true,
	}
	got := agentTabOptionsFromTab(tab)
	if got.CodexSandbox != appPty.CodexSandboxFullAccess || got.CodexApproval != appPty.CodexApprovalOnRequest || !got.CodexSearch {
		t.Errorf("restart would relaunch with %+v", got)
	}
}

// And a medusa restart, which reads them back off the persisted workspace JSON.
func TestOptionsRoundTripThroughTabInfo(t *testing.T) {
	info := data.TabInfo{
		Assistant:     "codex",
		CodexSandbox:  appPty.CodexSandboxReadOnly,
		CodexApproval: appPty.CodexApprovalNever,
		CodexSearch:   true,
	}
	got := agentTabOptionsFromTabInfo(info)
	if got.CodexSandbox != appPty.CodexSandboxReadOnly || got.CodexApproval != appPty.CodexApprovalNever || !got.CodexSearch {
		t.Errorf("restore would relaunch with %+v", got)
	}
}

// The launch options handed to the pty layer carry both assistants' fields;
// buildAgentCommand is what picks between them.
func TestAgentOptionsCarriesCodexPolicies(t *testing.T) {
	got := allSetOptions().forAssistant("codex").agentOptions()
	if got.CodexSandbox != appPty.CodexSandboxReadOnly || got.CodexApproval != appPty.CodexApprovalNever || !got.CodexSearch {
		t.Errorf("agentOptions dropped the Codex policies: %+v", got)
	}
}
