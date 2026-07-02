package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// collectMedusaCommands returns every hook command guarded by the Medusa
// session check, keyed by event name.
func collectMedusaCommands(t *testing.T, profileDir string) map[string][]string {
	t.Helper()
	settings := readSettings(t, profileDir)
	hooks, _ := settings["hooks"].(map[string]any)
	out := make(map[string][]string)
	for event, v := range hooks {
		arr, _ := v.([]any)
		for _, entry := range arr {
			rule, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			innerHooks, _ := rule["hooks"].([]any)
			for _, h := range innerHooks {
				hm, ok := h.(map[string]any)
				if !ok {
					continue
				}
				cmd, _ := hm["command"].(string)
				if strings.HasPrefix(cmd, `if [ -n "$MEDUSA_SESSION_NAME"`) {
					out[event] = append(out[event], cmd)
				}
			}
		}
	}
	return out
}

// TestInjectHooksPerEventAtomicCommands verifies the injected commands write
// one unique file per event via an atomic tmp+rename, carrying the session
// name in the payload. The old design (shared per-session file, truncating
// overwrite) lost events that arrived within the debounce window and could
// interleave concurrent writes into corrupt JSON.
func TestInjectHooksPerEventAtomicCommands(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	_ = os.MkdirAll(profileDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")

	if err := InjectHooks(profileDir, hooksDir); err != nil {
		t.Fatal(err)
	}

	cmds := collectMedusaCommands(t, profileDir)
	for _, event := range []string{"Stop", "StopFailure", "SubagentStop", "PreToolUse", "PostToolUse", "PermissionRequest", "UserPromptSubmit", "Notification"} {
		if len(cmds[event]) == 0 {
			t.Errorf("no medusa hook command for event %s", event)
		}
	}
	for event, list := range cmds {
		for _, cmd := range list {
			if strings.Contains(cmd, `"$MEDUSA_SESSION_NAME".json`) {
				t.Errorf("%s: command still writes shared per-session file: %s", event, cmd)
			}
			if !strings.Contains(cmd, "evt-$$") {
				t.Errorf("%s: command does not write a unique per-event file: %s", event, cmd)
			}
			if !strings.Contains(cmd, "mv ") {
				t.Errorf("%s: command does not rename atomically: %s", event, cmd)
			}
			if !strings.Contains(cmd, `"session":"%s"`) {
				t.Errorf("%s: payload does not carry the session name: %s", event, cmd)
			}
		}
	}
}

// TestInjectedHookCommandProducesValidEventFile executes the injected shell
// commands the way Claude Code would and verifies the event file they produce:
// unique per-event name, valid JSON, session carried in the payload, no .tmp
// leftovers.
func TestInjectedHookCommandProducesValidEventFile(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	_ = os.MkdirAll(profileDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")
	_ = os.MkdirAll(hooksDir, 0755)

	if err := InjectHooks(profileDir, hooksDir); err != nil {
		t.Fatal(err)
	}
	cmds := collectMedusaCommands(t, profileDir)

	runHook := func(cmd, stdin string) {
		t.Helper()
		c := exec.Command("sh", "-c", cmd)
		c.Env = append(os.Environ(), "MEDUSA_SESSION_NAME=medusa-ws1-tab1")
		c.Stdin = strings.NewReader(stdin)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("hook command failed: %v\n%s\ncmd: %s", err, out, cmd)
		}
	}

	readSingleEvent := func() map[string]any {
		t.Helper()
		entries, err := os.ReadDir(hooksDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected exactly 1 event file, found %d", len(entries))
		}
		name := entries[0].Name()
		if !strings.HasPrefix(name, "evt-") || !strings.HasSuffix(name, ".json") {
			t.Fatalf("unexpected event file name %q", name)
		}
		raw, err := os.ReadFile(filepath.Join(hooksDir, name))
		if err != nil {
			t.Fatal(err)
		}
		var evt map[string]any
		if err := json.Unmarshal(raw, &evt); err != nil {
			t.Fatalf("event file is not valid JSON: %v\n%s", err, raw)
		}
		_ = os.Remove(filepath.Join(hooksDir, name))
		return evt
	}

	// Plain lifecycle event (Stop).
	runHook(cmds["Stop"][0], "")
	evt := readSingleEvent()
	if evt["event"] != "Stop" {
		t.Errorf("event = %v, want Stop", evt["event"])
	}
	if evt["session"] != "medusa-ws1-tab1" {
		t.Errorf("session = %v, want medusa-ws1-tab1", evt["session"])
	}

	// Notification event with message extraction from stdin.
	var permCmd string
	for _, cmd := range cmds["Notification"] {
		if strings.Contains(cmd, `"event":"NotificationPermission"`) {
			permCmd = cmd
		}
	}
	if permCmd == "" {
		t.Fatal("no NotificationPermission command injected")
	}
	runHook(permCmd, `{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash","notification_type":"permission_prompt"}`)
	evt = readSingleEvent()
	if evt["event"] != "NotificationPermission" {
		t.Errorf("event = %v, want NotificationPermission", evt["event"])
	}
	if evt["message"] != "Claude needs your permission to use Bash" {
		t.Errorf("message = %v", evt["message"])
	}
	if evt["session"] != "medusa-ws1-tab1" {
		t.Errorf("session = %v, want medusa-ws1-tab1", evt["session"])
	}
}

// TestInjectHooksRemovesOldFormatRules verifies upgrading: rules injected by
// older Medusa versions (shared-file commands) are replaced, not duplicated,
// while foreign rules are preserved.
func TestInjectHooksRemovesOldFormatRules(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	_ = os.MkdirAll(profileDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")

	oldCmd := `if [ -n "$MEDUSA_SESSION_NAME" ]; then printf '{"event":"Stop","ts":%s}\n' "$(date +%s)" > ` + hooksDir + `/"$MEDUSA_SESSION_NAME".json; fi`
	seed := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": oldCmd}}},
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "/usr/local/bin/foreign-hook"}}},
			},
			"Notification": []any{
				map[string]any{"matcher": "idle_prompt", "hooks": []any{map[string]any{"type": "command", "command": oldCmd}}},
			},
		},
	}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(filepath.Join(profileDir, "settings.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := InjectHooks(profileDir, hooksDir); err != nil {
		t.Fatal(err)
	}

	cmds := collectMedusaCommands(t, profileDir)
	if n := len(cmds["Stop"]); n != 1 {
		t.Errorf("expected exactly 1 medusa Stop rule after upgrade, got %d", n)
	}
	for event, list := range cmds {
		for _, cmd := range list {
			if strings.Contains(cmd, `"$MEDUSA_SESSION_NAME".json`) {
				t.Errorf("%s: old-format rule survived upgrade: %s", event, cmd)
			}
		}
	}

	// Foreign rule must survive.
	settings := readSettings(t, profileDir)
	hooks, _ := settings["hooks"].(map[string]any)
	stopRules, _ := hooks["Stop"].([]any)
	foreign := false
	for _, entry := range stopRules {
		if m, ok := entry.(map[string]any); ok && hookRuleHasCommand(m, "/usr/local/bin/foreign-hook") {
			foreign = true
		}
	}
	if !foreign {
		t.Error("foreign Stop rule was removed by InjectHooks")
	}
}
