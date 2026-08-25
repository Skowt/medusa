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
		// Every rule goes through the shim, which is what keeps the command
		// string — and so Codex's trust hash — stable across Medusa builds.
		if !strings.Contains(cmd, codexHookShimName) {
			t.Errorf("event %s: command does not go through the shim: %s", event, cmd)
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

// Each profile is its own authentication boundary. A global Codex login must
// not silently authenticate a newly created Medusa profile.
func TestEnsureCodexHomeDoesNotCopyGlobalCredentials(t *testing.T) {
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
	if _, err := os.Stat(filepath.Join(codexHome, "auth.json")); !os.IsNotExist(err) {
		t.Errorf("profile auth.json exists after setup; want no copied credentials (err = %v)", err)
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

func TestEnsureCodexHomeCreatesDirectory(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	codexHome := filepath.Join(fakeHome, "profiles", "Work", CodexHomeSubdir)
	if err := EnsureCodexHome(codexHome); err != nil {
		t.Fatalf("EnsureCodexHome = %v", err)
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

// Codex hashes a hook's command string and re-asks for trust whenever it
// changes. Naming the emit binary directly meant every Medusa that lived
// somewhere new — a make run build, an air rebuild, an upgrade — brought the
// prompt back, so the command must not mention the binary's path at all.
func TestInjectCodexHooksCommandIsStableAcrossBinaryPaths(t *testing.T) {
	home := t.TempDir()
	hooksDir := t.TempDir()

	if err := InjectCodexHooks(home, hooksDir, "/opt/build-one/medusa-hook-emit"); err != nil {
		t.Fatal(err)
	}
	first := readCodexHooks(t, home)

	if err := InjectCodexHooks(home, hooksDir, "/tmp/air-build-42/medusa-hook-emit"); err != nil {
		t.Fatal(err)
	}
	second := readCodexHooks(t, home)

	for _, event := range codexHookEvents {
		before, after := ruleCommand(t, first, event), ruleCommand(t, second, event)
		if before != after {
			t.Errorf("event %s: command changed with the binary path, which re-asks for trust:\n  %s\n  %s",
				event, before, after)
		}
		if strings.Contains(after, "build-one") || strings.Contains(after, "air-build-42") {
			t.Errorf("event %s: command carries the binary path: %s", event, after)
		}
	}
}

// The shim is where the varying parts live, so it must actually point at the
// current binary and socket after a re-injection.
func TestCodexHookShimTracksTheCurrentBinary(t *testing.T) {
	home := t.TempDir()
	hooksDir := t.TempDir()

	if err := InjectCodexHooks(home, hooksDir, "/opt/build-one/medusa-hook-emit"); err != nil {
		t.Fatal(err)
	}
	if err := InjectCodexHooks(home, hooksDir, "/opt/build-two/medusa-hook-emit"); err != nil {
		t.Fatal(err)
	}

	shim, err := os.ReadFile(filepath.Join(home, codexHookShimName))
	if err != nil {
		t.Fatal(err)
	}
	got := string(shim)
	if !strings.Contains(got, "/opt/build-two/medusa-hook-emit") {
		t.Errorf("shim does not point at the current binary:\n%s", got)
	}
	if strings.Contains(got, "build-one") {
		t.Errorf("shim still points at the old binary:\n%s", got)
	}
	// The guards the command string used to carry moved in here.
	if !strings.Contains(got, "MEDUSA_SESSION_NAME") || !strings.Contains(got, `[ -x "$BIN" ]`) {
		t.Errorf("shim is missing the session/binary guards:\n%s", got)
	}
	info, err := os.Stat(filepath.Join(home, codexHookShimName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("shim mode = %v, want 0700", info.Mode().Perm())
	}
}

// An upgrade from the version that named the binary directly must replace those
// rules, not sit alongside them — two rules per event would fire every hook
// twice.
func TestInjectCodexHooksReplacesPreShimRules(t *testing.T) {
	home := t.TempDir()
	hooksDir := t.TempDir()
	legacy := hookCommandBuilder{sock: "/tmp/medusa.sock", emitBin: "/opt/old/medusa-hook-emit"}
	preShim := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{"hooks": []any{
					map[string]any{"type": "command", "command": legacy.command("Stop")},
				}},
			},
		},
	}
	raw, _ := json.Marshal(preShim)
	if err := os.WriteFile(filepath.Join(home, "hooks.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}

	if err := InjectCodexHooks(home, hooksDir, "/opt/new/medusa-hook-emit"); err != nil {
		t.Fatal(err)
	}

	stop := readCodexHooks(t, home)["Stop"]
	if len(stop) != 1 {
		t.Fatalf("Stop has %d rules after the upgrade, want 1: %v", len(stop), stop)
	}
	if cmd := ruleCommand(t, readCodexHooks(t, home), "Stop"); strings.Contains(cmd, "/opt/old/") {
		t.Errorf("the pre-shim rule survived: %s", cmd)
	}
}

// TestCodexHooksNeverDecidePermissions guards the property that makes it safe
// for Medusa to observe PermissionRequest at all: Codex lets a hook on that
// event answer the approval, and Medusa must only ever watch it.
//
// Exit 2 plus a stderr message is a *denial*, so the shim has to swallow both
// — a Go runtime panic in medusa-hook-emit exits 2 and writes a stack trace to
// stderr, which Codex would hand the agent as the reason its command was
// blocked. exec would replace the shell and let that status through.
func TestCodexHooksNeverDecidePermissions(t *testing.T) {
	home := t.TempDir()
	if err := InjectCodexHooks(home, t.TempDir(), "/usr/local/bin/medusa-hook-emit"); err != nil {
		t.Fatal(err)
	}
	hooks := readCodexHooks(t, home)

	for _, event := range codexHookEvents {
		if cmd := ruleCommand(t, hooks, event); strings.Contains(cmd, "-decide") {
			t.Errorf("event %s must not decide: %s", event, cmd)
		}
	}
	if _, ok := hooks["PermissionRequest"]; !ok {
		t.Fatal("Medusa must observe PermissionRequest — it is Codex's only approval signal")
	}

	shim, err := os.ReadFile(filepath.Join(home, codexHookShimName))
	if err != nil {
		t.Fatal(err)
	}
	script := string(shim)
	if !strings.Contains(script, "2>/dev/null") {
		t.Errorf("shim must discard stderr, or a panic reads as a denial reason:\n%s", script)
	}
	if !strings.HasSuffix(script, "\nexit 0\n") {
		t.Errorf("shim must end by exiting 0:\n%s", script)
	}
	if strings.Contains(script, "exec \"$BIN\"") {
		t.Errorf("shim must not exec — that returns the binary's own exit status:\n%s", script)
	}
}

// ruleCommand returns the command of an event's single Medusa rule.
func ruleCommand(t *testing.T, hooks map[string][]any, event string) string {
	t.Helper()
	rules, ok := hooks[event]
	if !ok || len(rules) == 0 {
		t.Fatalf("event %s has no rule", event)
	}
	rule, _ := rules[len(rules)-1].(map[string]any)
	inner, _ := rule["hooks"].([]any)
	if len(inner) == 0 {
		t.Fatalf("event %s has no handler", event)
	}
	handler, _ := inner[0].(map[string]any)
	cmd, _ := handler["command"].(string)
	return cmd
}
