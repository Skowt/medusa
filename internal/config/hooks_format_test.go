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

// TestInjectHooksSocketCommands verifies the injected commands send one event
// per connection to the hooks socket, guarded by the socket-exists check, and
// carry the session name in the payload. The socket is the only transport;
// there is no file fallback.
func TestInjectHooksSocketCommands(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	_ = os.MkdirAll(profileDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")

	if err := InjectHooks(profileDir, hooksDir); err != nil {
		t.Fatal(err)
	}

	cmds := collectMedusaCommands(t, profileDir)
	for _, event := range []string{"Stop", "StopFailure", "SubagentStart", "SubagentStop", "SessionStart", "PreToolUse", "PostToolUse", "PermissionRequest", "UserPromptSubmit", "Notification"} {
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
			// The socket is the only transport: no file fallback must remain.
			if strings.Contains(cmd, "evt-$$") || strings.Contains(cmd, ".tmp") {
				t.Errorf("%s: command still contains a file fallback: %s", event, cmd)
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

	// SubagentStop forwards pending_subagent_count so the app can tell a
	// mid-run subagent stop from the last one.
	runHookCommand(t, cmds["SubagentStop"][0], `{"hook_event_name":"SubagentStop","agent_id":"a1","background":true,"pending_subagent_count":2}`)
	evt = recv()
	if evt["event"] != "SubagentStop" || evt["pending"] != float64(2) {
		t.Errorf("SubagentStop payload must carry pending count: %v", evt)
	}

	// Absent field (older Claude Code) degrades to -1 = unknown.
	runHookCommand(t, cmds["SubagentStop"][0], `{"hook_event_name":"SubagentStop","agent_id":"a1"}`)
	evt = recv()
	if evt["event"] != "SubagentStop" || evt["pending"] != float64(-1) {
		t.Errorf("SubagentStop without pending_subagent_count must emit -1: %v", evt)
	}

	// SessionStart forwards the live session_id (and agent_type when present)
	// so the app can refresh a tab's persisted id after /clear.
	runHookCommand(t, cmds["SessionStart"][0], `{"hook_event_name":"SessionStart","source":"clear","session_id":"new-sid-123"}`)
	evt = recv()
	if evt["event"] != "SessionStart" || evt["claude_session_id"] != "new-sid-123" || evt["agent_type"] != "" {
		t.Errorf("SessionStart must carry claude_session_id and empty agent_type: %v", evt)
	}

	// An agent session (claude --agent) carries agent_type; the app uses it to
	// skip adopting the id.
	runHookCommand(t, cmds["SessionStart"][0], `{"hook_event_name":"SessionStart","source":"startup","session_id":"agent-sid","agent_type":"Explore"}`)
	evt = recv()
	if evt["event"] != "SessionStart" || evt["claude_session_id"] != "agent-sid" || evt["agent_type"] != "Explore" {
		t.Errorf("SessionStart must carry agent_type when present: %v", evt)
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
