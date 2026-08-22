package center

import (
	"testing"

	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
)

func allSetOptions() agentTabOptions {
	return agentTabOptions{Isolated: true, AllowUnsandboxedCommands: true, PermissionMode: "plan", Fullscreen: true, CodexSandbox: appPty.CodexSandboxReadOnly, CodexAuto: true}
}

func TestOptionsForCodexDropClaudeSettings(t *testing.T) {
	got := allSetOptions().forAssistant("codex")
	if got.PermissionMode != "" || got.Isolated || got.AllowUnsandboxedCommands || got.Fullscreen {
		t.Errorf("Claude settings survived: %+v", got)
	}
	if got.CodexSandbox != appPty.CodexSandboxReadOnly || !got.CodexAuto {
		t.Errorf("Codex settings were lost: %+v", got)
	}
}

func TestCodexInlineModeUsesMedusaScrollback(t *testing.T) {
	if agentPaintsFrames("codex", false) {
		t.Fatal("inline Codex must not be treated as a frame renderer")
	}
	if agentPaintsFrames("claude", false) {
		t.Fatal("classic Claude must retain transcript scrollback")
	}
	if !agentPaintsFrames("claude", true) {
		t.Fatal("fullscreen Claude must be a frame renderer")
	}
}

func TestOptionsForClaudeDropCodexSettings(t *testing.T) {
	got := allSetOptions().forAssistant("claude")
	if got.CodexSandbox != "" || got.CodexAuto {
		t.Errorf("Codex settings survived: %+v", got)
	}
	if !got.Fullscreen || got.PermissionMode != "plan" || !got.Isolated {
		t.Errorf("Claude settings were lost: %+v", got)
	}
}

func TestOptionsRoundTripThroughTab(t *testing.T) {
	got := agentTabOptionsFromTab(&Tab{Assistant: "codex", CodexSandbox: appPty.CodexSandboxFullAccess, CodexAuto: true})
	if got.CodexSandbox != appPty.CodexSandboxFullAccess || !got.CodexAuto {
		t.Errorf("restart would relaunch with %+v", got)
	}
}

func TestOptionsRoundTripThroughTabInfo(t *testing.T) {
	got := agentTabOptionsFromTabInfo(data.TabInfo{Assistant: "codex", CodexSandbox: appPty.CodexSandboxReadOnly, CodexAuto: true})
	if got.CodexSandbox != appPty.CodexSandboxReadOnly || !got.CodexAuto {
		t.Errorf("restore would relaunch with %+v", got)
	}
}

func TestAgentOptionsCarriesCodexPolicies(t *testing.T) {
	got := allSetOptions().forAssistant("codex").agentOptions()
	if got.CodexSandbox != appPty.CodexSandboxReadOnly || !got.CodexAuto {
		t.Errorf("agentOptions dropped Codex policies: %+v", got)
	}
}
