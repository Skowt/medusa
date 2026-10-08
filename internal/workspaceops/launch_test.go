package workspaceops

import (
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/pty"
)

func boolPtr(b bool) *bool { return &b }

func savedUI() config.UISettings {
	return config.UISettings{
		LastAssistant:                AssistantClaude,
		LastAssistantByProfile:       map[string]string{"Work": AssistantCodex},
		LastPermissionMode:           "plan",
		LastFullscreen:               true,
		LastIsolated:                 true,
		LastAllowUnsandboxedCommands: true,
		LastCodexSandbox:             pty.CodexSandboxReadOnly,
		LastCodexStartingMode:        "default",
	}
}

func TestResolveLaunchUsesTheProfilesSavedAssistant(t *testing.T) {
	got := ResolveLaunch(savedUI(), "Work", LaunchOverrides{})
	want := Launch{Assistant: AssistantCodex, CodexSandbox: pty.CodexSandboxReadOnly, CodexAuto: false}
	if got != want {
		t.Fatalf("ResolveLaunch(Work) = %+v, want %+v", got, want)
	}
}

func TestResolveLaunchFallsBackToTheGlobalAssistant(t *testing.T) {
	got := ResolveLaunch(savedUI(), "Personal", LaunchOverrides{})
	want := Launch{
		Assistant:                AssistantClaude,
		PermissionMode:           "plan",
		Fullscreen:               true,
		Isolated:                 true,
		AllowUnsandboxedCommands: true,
	}
	if got != want {
		t.Fatalf("ResolveLaunch(Personal) = %+v, want %+v", got, want)
	}
}

func TestResolveLaunchDefaultsWhenNothingIsSaved(t *testing.T) {
	got := ResolveLaunch(config.UISettings{}, "", LaunchOverrides{})
	if got.Assistant != AssistantClaude || got.PermissionMode != "auto" {
		t.Fatalf("empty settings resolved to %+v, want claude in auto mode", got)
	}
	codex := ResolveLaunch(config.UISettings{}, "", LaunchOverrides{Assistant: AssistantCodex})
	if codex.CodexSandbox != pty.CodexSandboxWorkspace || !codex.CodexAuto {
		t.Fatalf("empty settings resolved Codex to %+v, want workspace-write with auto review", codex)
	}
}

func TestResolveLaunchOverridesWin(t *testing.T) {
	got := ResolveLaunch(savedUI(), "Personal", LaunchOverrides{
		PermissionMode: "acceptEdits",
		Fullscreen:     boolPtr(false),
	})
	if got.PermissionMode != "acceptEdits" || got.Fullscreen {
		t.Fatalf("overrides lost: %+v", got)
	}
	codex := ResolveLaunch(savedUI(), "Work", LaunchOverrides{
		CodexSandbox: pty.CodexSandboxFullAccess,
		CodexAuto:    boolPtr(true),
	})
	if codex.CodexSandbox != pty.CodexSandboxFullAccess || !codex.CodexAuto {
		t.Fatalf("Codex overrides lost: %+v", codex)
	}
	claude := ResolveLaunch(savedUI(), "Work", LaunchOverrides{Assistant: AssistantClaude})
	if claude.Assistant != AssistantClaude || claude.CodexSandbox != "" || claude.PermissionMode != "plan" {
		t.Fatalf("assistant override on a Codex profile = %+v, want Claude with Claude's saved settings only", claude)
	}
}
