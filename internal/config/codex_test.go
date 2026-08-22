package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readCodexHooks(t *testing.T, codexHome string) map[string][]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(codexHome, "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Hooks map[string][]any `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v", err)
	}
	return file.Hooks
}

// Codex reads the same rule shape Claude Code does, and every event Medusa's
// state machine consumes has to be present or that tab never leaves "ready".
func TestInjectCodexHooksWritesEveryEvent(t *testing.T) {
	home := t.TempDir()
	if err := InjectCodexHooks(home, t.TempDir(), "/usr/local/bin/medusa-hook-emit"); err != nil {
		t.Fatal(err)
	}

	hooks := readCodexHooks(t, home)
	for _, event := range codexHookEvents {
		rules, ok := hooks[event]
		if !ok || len(rules) != 1 {
			t.Errorf("event %s: got %d rules, want 1", event, len(rules))
			continue
		}
		rule, _ := rules[0].(map[string]any)
		inner, _ := rule["hooks"].([]any)
		if len(inner) != 1 {
			t.Errorf("event %s: got %d handlers, want 1", event, len(inner))
			continue
		}
		handler, _ := inner[0].(map[string]any)
		if handler["type"] != "command" {
			t.Errorf("event %s: handler type %v, want command", event, handler["type"])
		}
		cmd, _ := handler["command"].(string)
		if !strings.Contains(cmd, "-event "+event) {
			t.Errorf("event %s: command does not emit that event: %s", event, cmd)
		}
		// Codex runs hook commands through $SHELL -lc, so the guard that makes
		// non-Medusa sessions silent no-ops works here as it does for Claude.
		if !strings.HasPrefix(cmd, medusaHookCommandPrefix) {
			t.Errorf("event %s: command lacks the session-name guard: %s", event, cmd)
		}
	}
}

// Codex reads `timeout` in seconds where Claude Code's settings.json reads
// milliseconds. Sharing the 5000 both use there would be an 83-minute timeout.
func TestInjectCodexHooksTimeoutIsSeconds(t *testing.T) {
	home := t.TempDir()
	if err := InjectCodexHooks(home, t.TempDir(), "/usr/local/bin/medusa-hook-emit"); err != nil {
		t.Fatal(err)
	}

	hooks := readCodexHooks(t, home)
	rule, _ := hooks["Stop"][0].(map[string]any)
	inner, _ := rule["hooks"].([]any)
	handler, _ := inner[0].(map[string]any)
	timeout, ok := handler["timeout"].(float64)
	if !ok || timeout > 60 {
		t.Errorf("timeout = %v, want a small number of seconds", handler["timeout"])
	}
}

// Re-injection replaces Medusa's own rules and keeps everyone else's: Codex
// re-prompts for trust on any changed hook, and a duplicate would fire twice.
func TestInjectCodexHooksReplacesOwnRulesAndKeepsForeign(t *testing.T) {
	home := t.TempDir()
	foreign := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{"hooks": []any{
					map[string]any{"type": "command", "command": "notify-send done"},
				}},
			},
		},
	}
	raw, _ := json.Marshal(foreign)
	if err := os.WriteFile(filepath.Join(home, "hooks.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if err := InjectCodexHooks(home, t.TempDir(), "/usr/local/bin/medusa-hook-emit"); err != nil {
			t.Fatal(err)
		}
	}

	stop := readCodexHooks(t, home)["Stop"]
	if len(stop) != 2 {
		t.Fatalf("Stop has %d rules after 3 injections, want 2 (one foreign, one Medusa)", len(stop))
	}
	var sawForeign bool
	for _, rule := range stop {
		m, _ := rule.(map[string]any)
		if hookRuleHasCommand(m, "notify-send done") {
			sawForeign = true
		}
	}
	if !sawForeign {
		t.Error("injection dropped the user's own Stop hook")
	}
}

// Codex refuses to start in an untrusted directory, which is every fresh
// worktree, so the trust entry has to be there before the first launch.
func TestInjectCodexTrustedDirectory(t *testing.T) {
	home := t.TempDir()
	root := "/Users/me/.medusa/workspaces/ws"

	if err := InjectCodexTrustedDirectory(home, root); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, `[projects."`+root+`"]`) || !strings.Contains(got, `trust_level = "trusted"`) {
		t.Errorf("config.toml does not trust the worktree:\n%s", got)
	}
}

// Codex writes its own state into this file — the hook trust hashes among it —
// so re-trusting must append at most once and never rewrite what is there.
func TestInjectCodexTrustedDirectoryIsIdempotent(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	existing := "model = \"gpt-5.6\"\n\n[hooks.state.\"x:stop:0:0\"]\ntrusted_hash = \"sha256:abc\"\n"
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	root := "/Users/me/.medusa/workspaces/ws"
	for i := 0; i < 3; i++ {
		if err := InjectCodexTrustedDirectory(home, root); err != nil {
			t.Fatal(err)
		}
	}

	raw, _ := os.ReadFile(path)
	got := string(raw)
	if n := strings.Count(got, `[projects."`+root+`"]`); n != 1 {
		t.Errorf("trust entry appears %d times, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, `trusted_hash = "sha256:abc"`) || !strings.Contains(got, `model = "gpt-5.6"`) {
		t.Errorf("append clobbered Codex's own state:\n%s", got)
	}
}

// Two workspaces both get trusted; the second must not displace the first.
func TestInjectCodexTrustedDirectoryKeepsEarlierRoots(t *testing.T) {
	home := t.TempDir()
	roots := []string{"/ws/one", "/ws/two"}
	for _, root := range roots {
		if err := InjectCodexTrustedDirectory(home, root); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	for _, root := range roots {
		if !strings.Contains(string(raw), `[projects."`+root+`"]`) {
			t.Errorf("root %s lost its trust entry:\n%s", root, raw)
		}
	}
}

// A profile's first Codex tab must not land on a login prompt when the user is
// already logged in under ~/.codex.
func TestEnsureCodexHomeSeedsCredentials(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	if err := os.MkdirAll(filepath.Join(fakeHome, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeHome, ".codex", "auth.json"), []byte(`{"token":"real"}`), 0600); err != nil {
		t.Fatal(err)
	}

	codexHome := filepath.Join(fakeHome, "profiles", "Work", CodexHomeSubdir)
	if err := EnsureCodexHome(codexHome); err != nil {
		t.Fatal(err)
	}
	seeded, err := os.ReadFile(filepath.Join(codexHome, "auth.json"))
	if err != nil {
		t.Fatalf("auth.json was not seeded: %v", err)
	}
	if string(seeded) != `{"token":"real"}` {
		t.Errorf("seeded auth.json = %s", seeded)
	}
}

// Once a profile has its own credentials, a re-login there stays local to it.
func TestEnsureCodexHomeNeverOverwritesCredentials(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	if err := os.MkdirAll(filepath.Join(fakeHome, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeHome, ".codex", "auth.json"), []byte(`{"token":"global"}`), 0600); err != nil {
		t.Fatal(err)
	}

	codexHome := filepath.Join(fakeHome, "profiles", "Work", CodexHomeSubdir)
	if err := os.MkdirAll(codexHome, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"token":"profile"}`), 0600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureCodexHome(codexHome); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(codexHome, "auth.json"))
	if string(got) != `{"token":"profile"}` {
		t.Errorf("profile credentials were overwritten: %s", got)
	}
}

// Nothing to seed is not a failure — Codex simply prompts for login.
func TestEnsureCodexHomeWithoutGlobalLogin(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	codexHome := filepath.Join(fakeHome, "profiles", "Work", CodexHomeSubdir)
	if err := EnsureCodexHome(codexHome); err != nil {
		t.Fatalf("EnsureCodexHome = %v, want nil when there is nothing to seed", err)
	}
	if _, err := os.Stat(codexHome); err != nil {
		t.Errorf("CODEX_HOME was not created: %v", err)
	}
}

func TestCodexHomeDir(t *testing.T) {
	if got := CodexHomeDir("/root/profiles", "Work"); got != "/root/profiles/Work/codex" {
		t.Errorf("CodexHomeDir = %q", got)
	}
	if got := CodexHomeDir("/root/profiles", ""); got != "" {
		t.Errorf("CodexHomeDir with no profile = %q, want empty", got)
	}
}
