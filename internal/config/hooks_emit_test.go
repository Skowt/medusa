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

// TestInjectHooksEmitBinaryCommands verifies the preferred injection mode: each
// hook rule invokes the medusa-hook-emit binary (guarded so both a non-Medusa
// session and a missing binary are silent no-ops) instead of the grep/nc shell
// pipeline.
func TestInjectHooksEmitBinaryCommands(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	_ = os.MkdirAll(profileDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")
	emitBin := "/opt/medusa/medusa-hook-emit"

	if err := InjectHooks(profileDir, hooksDir, emitBin); err != nil {
		t.Fatal(err)
	}

	cmds := collectMedusaCommands(t, profileDir)
	events := []string{"Stop", "StopFailure", "SubagentStart", "SubagentStop", "SessionStart", "PreToolUse", "PostToolUse", "PermissionRequest", "UserPromptSubmit", "Notification"}
	for _, event := range events {
		if len(cmds[event]) == 0 {
			t.Errorf("no medusa hook command for event %s", event)
		}
	}
	for event, list := range cmds {
		for _, cmd := range list {
			if !strings.Contains(cmd, emitBin) {
				t.Errorf("%s: command does not invoke the emit binary: %s", event, cmd)
			}
			if !strings.Contains(cmd, `[ -x "`+emitBin+`" ]`) {
				t.Errorf("%s: command lacks the binary-exists guard: %s", event, cmd)
			}
			if !strings.Contains(cmd, `-socket "`+hooksDir) {
				t.Errorf("%s: command does not pass the hooks socket: %s", event, cmd)
			}
			if strings.Contains(cmd, "nc -U") || strings.Contains(cmd, "printf") {
				t.Errorf("%s: binary mode must not keep the shell pipeline: %s", event, cmd)
			}
		}
	}

	// Notification rules keep per-matcher event names so the app can tell an
	// idle prompt from a permission prompt.
	joined := strings.Join(cmds["Notification"], "\n")
	for _, name := range []string{"NotificationIdle", "NotificationPermission", "NotificationElicitation"} {
		if !strings.Contains(joined, "-event "+name) {
			t.Errorf("Notification rules missing event %s:\n%s", name, joined)
		}
	}
}

// TestInjectHooksReplacesAcrossModes verifies switching between binary and
// fallback injection (medusa upgrade/downgrade, binary appearing later)
// replaces rules instead of accumulating both formats.
func TestInjectHooksReplacesAcrossModes(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	_ = os.MkdirAll(profileDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")

	if err := InjectHooks(profileDir, hooksDir, ""); err != nil {
		t.Fatal(err)
	}
	if err := InjectHooks(profileDir, hooksDir, "/opt/medusa/medusa-hook-emit"); err != nil {
		t.Fatal(err)
	}
	if err := InjectHooks(profileDir, hooksDir, "/opt/medusa/medusa-hook-emit"); err != nil {
		t.Fatal(err)
	}

	cmds := collectMedusaCommands(t, profileDir)
	if n := len(cmds["Stop"]); n != 1 {
		t.Errorf("expected exactly 1 medusa Stop rule after mode switches, got %d", n)
	}
	if n := len(cmds["Notification"]); n != 3 {
		t.Errorf("expected exactly 3 medusa Notification rules after mode switches, got %d", n)
	}
	for event, list := range cmds {
		for _, cmd := range list {
			if strings.Contains(cmd, "nc -U") {
				t.Errorf("%s: fallback rule survived binary re-injection: %s", event, cmd)
			}
		}
	}
}

// buildEmitBinary compiles cmd/medusa-hook-emit into a temp dir once per test.
func buildEmitBinary(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "medusa-emitbin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	bin := filepath.Join(dir, "medusa-hook-emit")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/Skowt/medusa/cmd/medusa-hook-emit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building medusa-hook-emit: %v\n%s", err, out)
	}
	return bin
}

// TestInjectedEmitBinaryCommandSendsToSocket executes a binary-mode hook
// command the way Claude Code would and verifies the event arrives with the
// outstanding background-task count computed from the payload.
func TestInjectedEmitBinaryCommandSendsToSocket(t *testing.T) {
	emitBin := buildEmitBinary(t)
	dir, err := os.MkdirTemp("/tmp", "medusa-hookemit-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	profileDir := filepath.Join(dir, "profile")
	_ = os.MkdirAll(profileDir, 0755)
	hooksDir := filepath.Join(dir, "hooks")
	_ = os.MkdirAll(hooksDir, 0755)

	if err := InjectHooks(profileDir, hooksDir, emitBin); err != nil {
		t.Fatal(err)
	}
	cmds := collectMedusaCommands(t, profileDir)

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

	runHookCommand(t, cmds["Stop"][0], `{"hook_event_name":"Stop","background_tasks":[{"id":"a1","type":"subagent","status":"running"}],"session_crons":[]}`)
	evt := recv()
	if evt["event"] != "Stop" || evt["session"] != "medusa-ws1-tab1" {
		t.Errorf("unexpected payload: %v", evt)
	}
	if evt["outstanding"] != float64(1) {
		t.Errorf("Stop with a running background task must carry outstanding=1: %v", evt)
	}

	// Without the binary present, the command is a silent no-op (exit 0).
	if err := os.Remove(emitBin); err != nil {
		t.Fatal(err)
	}
	runHookCommand(t, cmds["Stop"][0], `{}`)
	select {
	case line := <-lines:
		t.Errorf("command without binary must emit nothing, got %s", line)
	case <-time.After(300 * time.Millisecond):
	}
}
