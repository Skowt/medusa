package app

import (
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
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
		Select2Value:   "auto",
		Select3Value:   appPty.CodexSandboxReadOnly,
		Checkbox3Value: true, // Claude's fullscreen box — absent from this dialog
	})

	if launch.Assistant != assistantCodex {
		t.Errorf("assistant = %q, want codex", launch.Assistant)
	}
	if launch.CodexSandbox != appPty.CodexSandboxReadOnly {
		t.Errorf("sandbox = %q, want read-only", launch.CodexSandbox)
	}
	if !launch.CodexAuto {
		t.Error("auto starting mode did not reach the launch")
	}
	// Claude's own fields must stay zero: a Codex tab launched with
	// PermissionMode or Fullscreen set would pass flags codex rejects.
	if launch.PermissionMode != "" || launch.Fullscreen || launch.Isolated {
		t.Errorf("Claude settings leaked into a Codex launch: %+v", launch)
	}
}

func TestNewTabLaunchFromDialogCodexDefaultMode(t *testing.T) {
	a := &App{config: testConfig(t)}

	launch := a.newTabLaunchFromDialog(nil, common.DialogResult{
		ID:           DialogCustomizeTab,
		SelectValue:  assistantCodex,
		Select2Value: "default",
		Select3Value: appPty.CodexSandboxFullAccess,
	})
	if launch.CodexAuto {
		t.Error("default starting mode unexpectedly enabled automatic approval")
	}
}

func TestCodexStartingModeRebuildPreservesSandbox(t *testing.T) {
	a := &App{config: testConfig(t), activeWorkspace: &data.Workspace{}}
	a.showNewTabDialogWithCodexValues(assistantCodex, "default", appPty.CodexSandboxFullAccess)
	a.handleDialogSelectChanged(common.DialogSelectChanged{ID: DialogCustomizeTab, Slot: 1, Value: "auto"})

	if got := a.dialog.Select2Value(); got != "auto" {
		t.Errorf("starting mode = %q, want auto", got)
	}
	if got := a.dialog.Select3Value(); got != appPty.CodexSandboxFullAccess {
		t.Errorf("sandbox changed during mode rebuild: %q", got)
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
	if launch.CodexSandbox != "" || launch.CodexAuto {
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
		Select2Value: "auto",
		Select3Value: appPty.CodexSandboxReadOnly,
	})

	if cfg.UI.LastAssistant != assistantCodex {
		t.Errorf("LastAssistant = %q, want codex", cfg.UI.LastAssistant)
	}
	replay := a.lastUsedLaunch(nil, cfg.UI.LastAssistant)
	if replay.Assistant != assistantCodex || replay.CodexSandbox != appPty.CodexSandboxReadOnly {
		t.Errorf("replayed launch = %+v, want the Codex settings just saved", replay)
	}
	if !replay.CodexAuto {
		t.Error("replayed launch lost auto starting mode")
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
