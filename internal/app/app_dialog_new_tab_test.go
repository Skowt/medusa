package app

import (
	"testing"

	"github.com/Skowt/medusa/internal/config"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/ui/common"
)

// testConfig returns a config rooted in a temp MEDUSA_HOME, so saving the
// sticky New Tab settings cannot touch the user's real config.json.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("MEDUSA_HOME", t.TempDir())
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// The New Tab dialog result carries the assistant in select slot 0 and that
// assistant's mode in slot 1, so a Codex submit must never be read with
// Claude's fields — the values share no vocabulary.
func TestNewTabLaunchFromDialogCodex(t *testing.T) {
	a := &App{config: testConfig(t)}

	launch := a.newTabLaunchFromDialog(nil, common.DialogResult{
		ID:             DialogCustomizeTab,
		Confirmed:      true,
		SelectValue:    assistantCodex,
		Select2Value:   appPty.CodexSandboxReadOnly,
		CheckboxValue:  true, // Ask for approval
		Checkbox2Value: true, // Enable web search
		Checkbox3Value: true, // Claude's fullscreen box — absent from this dialog
	})

	if launch.Assistant != assistantCodex {
		t.Errorf("assistant = %q, want codex", launch.Assistant)
	}
	if launch.CodexSandbox != appPty.CodexSandboxReadOnly {
		t.Errorf("sandbox = %q, want read-only", launch.CodexSandbox)
	}
	if launch.CodexApproval != appPty.CodexApprovalOnRequest {
		t.Errorf("approval = %q, want on-request", launch.CodexApproval)
	}
	if !launch.CodexSearch {
		t.Error("web search checkbox did not reach the launch")
	}
	// Claude's own fields must stay zero: a Codex tab launched with
	// PermissionMode or Fullscreen set would pass flags codex rejects.
	if launch.PermissionMode != "" || launch.Fullscreen || launch.Isolated {
		t.Errorf("Claude settings leaked into a Codex launch: %+v", launch)
	}
}

// Unchecking "Ask for approval" is what turns approvals off, and it has to
// reach codex as an explicit policy rather than an omission.
func TestNewTabLaunchFromDialogCodexWithoutApproval(t *testing.T) {
	a := &App{config: testConfig(t)}

	launch := a.newTabLaunchFromDialog(nil, common.DialogResult{
		ID:           DialogCustomizeTab,
		SelectValue:  assistantCodex,
		Select2Value: appPty.CodexSandboxFullAccess,
	})
	if launch.CodexApproval != appPty.CodexApprovalNever {
		t.Errorf("approval = %q, want never", launch.CodexApproval)
	}
}

func TestNewTabLaunchFromDialogClaude(t *testing.T) {
	a := &App{config: testConfig(t)}

	launch := a.newTabLaunchFromDialog(nil, common.DialogResult{
		ID:             DialogCustomizeTab,
		SelectValue:    assistantClaude,
		Select2Value:   "plan",
		CheckboxValue:  true,
		Checkbox2Value: true,
		Checkbox3Value: true,
	})

	if launch.Assistant != assistantClaude {
		t.Errorf("assistant = %q, want claude", launch.Assistant)
	}
	if launch.PermissionMode != "plan" {
		t.Errorf("permission mode = %q, want plan", launch.PermissionMode)
	}
	if !launch.Isolated || !launch.AllowUnsandboxedCommands || !launch.Fullscreen {
		t.Errorf("Claude checkboxes did not reach the launch: %+v", launch)
	}
	if launch.CodexSandbox != "" || launch.CodexApproval != "" || launch.CodexSearch {
		t.Errorf("Codex settings leaked into a Claude launch: %+v", launch)
	}
}

// The choice is sticky: the next tab opens on the assistant and options last
// used, which is also what the no-dialog paths launch with.
func TestNewTabLaunchPersistsAndReplays(t *testing.T) {
	cfg := testConfig(t)
	a := &App{config: cfg}

	a.newTabLaunchFromDialog(nil, common.DialogResult{
		ID:           DialogCustomizeTab,
		SelectValue:  assistantCodex,
		Select2Value: appPty.CodexSandboxReadOnly,
	})

	if cfg.UI.LastAssistant != assistantCodex {
		t.Errorf("LastAssistant = %q, want codex", cfg.UI.LastAssistant)
	}
	replay := a.lastUsedLaunch(nil, cfg.UI.LastAssistant)
	if replay.Assistant != assistantCodex || replay.CodexSandbox != appPty.CodexSandboxReadOnly {
		t.Errorf("replayed launch = %+v, want the Codex settings just saved", replay)
	}
	if replay.CodexApproval != appPty.CodexApprovalNever {
		t.Errorf("replayed approval = %q, want the saved never", replay.CodexApproval)
	}
}

// An unknown assistant (a downgrade reading a newer config, a hand-edited
// file) must fall back to Claude rather than launching a command that is not
// there.
func TestDefaultAssistantFallsBackToClaude(t *testing.T) {
	for _, in := range []string{"", "gemini", "CODEX"} {
		if got := defaultAssistant(in); got != assistantClaude {
			t.Errorf("defaultAssistant(%q) = %q, want claude", in, got)
		}
	}
	if got := defaultAssistant(assistantCodex); got != assistantCodex {
		t.Errorf("defaultAssistant(codex) = %q", got)
	}
}

// Likewise an unknown sandbox policy: codex exits on one it does not know.
func TestDefaultCodexSandboxFallsBack(t *testing.T) {
	if got := defaultCodexSandbox("bypassPermissions"); got != appPty.CodexSandboxWorkspace {
		t.Errorf("defaultCodexSandbox = %q, want workspace-write", got)
	}
	if got := defaultCodexSandbox(appPty.CodexSandboxFullAccess); got != appPty.CodexSandboxFullAccess {
		t.Errorf("defaultCodexSandbox = %q, want the stored policy", got)
	}
}
