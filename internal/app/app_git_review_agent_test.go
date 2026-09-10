package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
)

func reviewCfg(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{Paths: &config.Paths{ProfilesRoot: t.TempDir()}}
}

func reviewWorkspace(profile string) *data.Workspace {
	ws := data.NewWorkspace("demo", "feature", "main", "/repo", "/repo/demo")
	ws.Profile = profile
	return ws
}

// TestCodexOnDefaultSandboxCannotReply is the case that prompted all of this.
// Medusa launches Codex tabs with workspace-write, which denies network, so the
// reply endpoint is unreachable — and the page has to say so rather than
// promising replies that can never arrive.
func TestCodexOnDefaultSandboxCannotReply(t *testing.T) {
	cfg := reviewCfg(t)
	got := reviewAgentProfile(cfg, reviewWorkspace("Work"), assistantCodex, appPty.CodexSandboxWorkspace)

	if got.Label != codexLabel {
		t.Errorf("label = %q, want %q", got.Label, codexLabel)
	}
	if got.CanReply {
		t.Fatal("a workspace-write Codex tab was reported as able to reply")
	}
	if got.Blocked == "" || got.Fix == "" {
		t.Fatal("a blocked agent must explain itself and say how to fix it")
	}
	// The fix has to name the config Codex actually reads. Medusa points
	// CODEX_HOME at the profile dir, so ~/.codex/config.toml is the wrong file
	// and following that advice would change nothing.
	if !strings.Contains(got.Fix, filepath.Join(cfg.Paths.ProfilesRoot, "Work", "codex")) {
		t.Errorf("fix does not name the profile's CODEX_HOME:\n%s", got.Fix)
	}
	if !strings.Contains(got.Fix, "network_access = true") {
		t.Errorf("fix does not give the setting:\n%s", got.Fix)
	}
	// Granting network access is broader than this page; saying so is honest.
	if !strings.Contains(got.Fix, "not just to this page") {
		t.Errorf("fix does not note the breadth of the change:\n%s", got.Fix)
	}
}

// TestCodexWithNetworkAccessEnabledCanReply keeps the check from being a blanket
// assumption about the sandbox name: a user who has already turned network
// access on must not be told replies are off.
func TestCodexWithNetworkAccessEnabledCanReply(t *testing.T) {
	cfg := reviewCfg(t)
	home := config.CodexHomeDir(cfg.Paths.ProfilesRoot, "Work")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[projects.\"/repo\"]\ntrust_level = \"trusted\"\n\n[sandbox_workspace_write]\nnetwork_access = true\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := reviewAgentProfile(cfg, reviewWorkspace("Work"), assistantCodex, appPty.CodexSandboxWorkspace)
	if !got.CanReply {
		t.Errorf("network_access = true was not honoured: %+v", got)
	}
}

func TestCodexSandboxModesMapToCapability(t *testing.T) {
	cfg := reviewCfg(t)
	cases := map[string]bool{
		appPty.CodexSandboxFullAccess: true,
		appPty.CodexSandboxReadOnly:   false,
		appPty.CodexSandboxWorkspace:  false,
		"":                            false, // unset: assume the default policy
		"something-new":               false, // unknown: assume the restrictive answer
	}
	for sandbox, wantReply := range cases {
		got := reviewAgentProfile(cfg, reviewWorkspace("Work"), assistantCodex, sandbox)
		if got.CanReply != wantReply {
			t.Errorf("sandbox %q: CanReply = %v, want %v", sandbox, got.CanReply, wantReply)
		}
		if !got.CanReply && got.Blocked == "" {
			t.Errorf("sandbox %q: blocked with no explanation", sandbox)
		}
	}
}

func TestClaudeIsReplyCapable(t *testing.T) {
	got := reviewAgentProfile(reviewCfg(t), reviewWorkspace("Work"), assistantClaude, "")
	if got.Label != claudeLabel || !got.CanReply {
		t.Errorf("Claude profile = %+v", got)
	}
	if got.Blocked != "" {
		t.Errorf("Claude should carry no warning: %q", got.Blocked)
	}
}

// TestProfileSurvivesAMissingWorkspace keeps the button working when the
// registry has moved under the click.
func TestProfileSurvivesAMissingWorkspace(t *testing.T) {
	got := reviewAgentProfile(reviewCfg(t), nil, assistantCodex, appPty.CodexSandboxWorkspace)
	if got.CanReply {
		t.Error("a workspace-less Codex tab was reported as able to reply")
	}
	if got.Fix == "" {
		t.Error("the fix text should still be offered")
	}
}
