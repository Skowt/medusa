package config

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
			if !strings.Contains(cmd, "nc -U") || !strings.Contains(cmd, "medusa.sock") {
				t.Errorf("%s: command does not send to the hooks socket: %s", event, cmd)
			}
			if !strings.Contains(cmd, `[ -S `) {
				t.Errorf("%s: command lacks the socket-exists guard (must be a no-op while Medusa is stopped): %s", event, cmd)
			}
			// File fallback for nc-less systems must stay atomic and unique.
			if !strings.Contains(cmd, "evt-$$") {
				t.Errorf("%s: fallback does not write a unique per-event file: %s", event, cmd)
			}
			if !strings.Contains(cmd, "mv ") {
				t.Errorf("%s: fallback does not rename atomically: %s", event, cmd)
			}
			if !strings.Contains(cmd, `"session":"%s"`) {
				t.Errorf("%s: payload does not carry the session name: %s", event, cmd)
			}
		}
	}
}

// hookTestEnv injects hooks into a temp profile and returns the collected
// commands plus the hooks dir. The hooks dir is created under /tmp because
// macOS caps Unix socket paths at 104 bytes and t.TempDir() can exceed that.
func hookTestEnv(t *testing.T) (map[string][]string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "medusa-hookfmt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	profileDir := filepath.Join(dir, "profile")
	_ = os.MkdirAll(profileDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")
	_ = os.MkdirAll(hooksDir, 0755)
	if err := InjectHooks(profileDir, hooksDir); err != nil {
		t.Fatal(err)
	}
	return collectMedusaCommands(t, profileDir), hooksDir
}

func runHookCommand(t *testing.T, cmd, stdin string, env ...string) {
	t.Helper()
	c := exec.Command("sh", "-c", cmd)
	c.Env = append(os.Environ(), append([]string{"MEDUSA_SESSION_NAME=medusa-ws1-tab1"}, env...)...)
	c.Stdin = strings.NewReader(stdin)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("hook command failed: %v\n%s\ncmd: %s", err, out, cmd)
	}
}

func hookFilesIn(t *testing.T, hooksDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".tmp") {
			names = append(names, e.Name())
		}
	}
	return names
}

// TestInjectedHookCommandSendsToSocket executes the injected commands the way
// Claude Code would, with a live listener on the hooks socket, and verifies
// the event arrives over the socket with no files written.
func TestInjectedHookCommandSendsToSocket(t *testing.T) {
	if _, err := exec.LookPath("nc"); err != nil {
		t.Skip("nc not available")
	}
	cmds, hooksDir := hookTestEnv(t)

	ln, err := net.Listen("unix", filepath.Join(hooksDir, "medusa.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	lines := make(chan string, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadString('\n')
			_ = conn.Close()
			lines <- line
		}
	}()

	recv := func() map[string]any {
		t.Helper()
		select {
		case line := <-lines:
			var evt map[string]any
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				t.Fatalf("socket payload is not valid JSON: %v\n%s", err, line)
			}
			return evt
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for socket payload")
			return nil
		}
	}

	// Plain lifecycle event (Stop).
	runHookCommand(t, cmds["Stop"][0], "")
	evt := recv()
	if evt["event"] != "Stop" || evt["session"] != "medusa-ws1-tab1" {
		t.Errorf("unexpected payload: %v", evt)
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
	runHookCommand(t, permCmd, `{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash","notification_type":"permission_prompt"}`)
	evt = recv()
	if evt["event"] != "NotificationPermission" || evt["message"] != "Claude needs your permission to use Bash" {
		t.Errorf("unexpected payload: %v", evt)
	}

	if files := hookFilesIn(t, hooksDir); len(files) != 0 {
		t.Errorf("socket path must not write files, found %v", files)
	}
}

// TestInjectedHookCommandDropsWhenSocketAbsent verifies the core property of
// the socket design: with Medusa stopped (no socket), hooks are a silent
// no-op — no files accumulate and the command still exits 0.
func TestInjectedHookCommandDropsWhenSocketAbsent(t *testing.T) {
	if _, err := exec.LookPath("nc"); err != nil {
		t.Skip("nc not available")
	}
	cmds, hooksDir := hookTestEnv(t)

	runHookCommand(t, cmds["Stop"][0], "")

	if files := hookFilesIn(t, hooksDir); len(files) != 0 {
		t.Errorf("expected no files while Medusa is stopped, found %v", files)
	}
}

// TestInjectedHookCommandFallsBackToFileWithoutNC verifies systems without nc
// still deliver events via atomic per-event files (consumed by the watcher).
func TestInjectedHookCommandFallsBackToFileWithoutNC(t *testing.T) {
	cmds, hooksDir := hookTestEnv(t)

	// PATH with the tools the command needs, but no nc.
	fakebin := filepath.Join(t.TempDir(), "bin")
	_ = os.MkdirAll(fakebin, 0755)
	for _, tool := range []string{"date", "mv", "cat", "grep", "sed", "head", "echo"} {
		src, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("tool %s not found on host: %v", tool, err)
		}
		if err := os.Symlink(src, filepath.Join(fakebin, tool)); err != nil {
			t.Fatal(err)
		}
	}

	runHookCommand(t, cmds["Stop"][0], "", "PATH="+fakebin)

	files := hookFilesIn(t, hooksDir)
	if len(files) != 1 || !strings.HasPrefix(files[0], "evt-") || !strings.HasSuffix(files[0], ".json") {
		t.Fatalf("expected exactly one evt-*.json fallback file, found %v", files)
	}
	raw, err := os.ReadFile(filepath.Join(hooksDir, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	var evt map[string]any
	if err := json.Unmarshal(raw, &evt); err != nil {
		t.Fatalf("fallback file is not valid JSON: %v\n%s", err, raw)
	}
	if evt["event"] != "Stop" || evt["session"] != "medusa-ws1-tab1" {
		t.Errorf("unexpected fallback payload: %v", evt)
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
