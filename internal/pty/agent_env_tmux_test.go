package pty

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/tmux"
)

const (
	sessionEnvWaitTimeout = 5 * time.Second
	// extraSessionVar stands in for any other per-workspace var. It and the
	// WORKSPACE_PORT values below are sentinels outside the default 6200 port
	// base: a test run from a medusa pane carries real values, which reach the
	// client via os.Environ.
	extraSessionVar = "POSTGRES_PORT=49999"
)

// Regression: the first agent launch started the medusa tmux server from a
// client carrying workspace A's WORKSPACE_PORT, tmux copied that into its
// global env, and every later pane on the server — other workspaces, fallback
// shells, sidebar terminals — inherited A's port.
func TestAgentClientCommand_ScopesWorkspaceEnvToItsSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	socket := fmt.Sprintf("medusa-sessenv-%d", os.Getpid())
	t.Setenv("MEDUSA_TMUX_SERVER", socket)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	portA := launchAndReadPort(t, "alpha", "WORKSPACE_PORT=49200")
	assertGlobalEnvLacks(t, socket, "WORKSPACE_PORT=49200", extraSessionVar)
	portB := launchAndReadPort(t, "beta", "WORKSPACE_PORT=49210")

	if portA != "49200" || portB != "49210" {
		t.Errorf("pane WORKSPACE_PORT = %q / %q, want 49200 / 49210", portA, portB)
	}
	assertGlobalEnvLacks(t, socket, "WORKSPACE_PORT=49200", "WORKSPACE_PORT=49210", extraSessionVar)
}

// launchAndReadPort launches a session through the agent launch path whose
// pane writes the WORKSPACE_PORT it sees, without the agent command prefix,
// so the value can only have come from the session env.
func launchAndReadPort(t *testing.T, name, portVar string) string {
	t.Helper()
	root := t.TempDir()
	out := filepath.Join(root, "port")
	ws := data.NewWorkspace(name, name, "main", root, root)
	fullCommand := fmt.Sprintf(`printf '%%s' "$WORKSPACE_PORT" > %s; sleep 60`, out)

	p := PreparedAgent{
		SessionName: name,
		Command:     fullCommand,
		Env:         agentClientEnv(),
		SessionEnv:  append(workspaceIdentityEnv(ws), portVar, extraSessionVar),
	}
	term, err := NewWithSize(agentClientCommand(p, root, tmux.SessionTags{}), root, p.Env, 24, 80)
	if err != nil {
		t.Fatalf("start client: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })

	deadline := time.Now().Add(sessionEnvWaitTimeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
			return string(b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session %s never reported its WORKSPACE_PORT", name)
	return ""
}

func assertGlobalEnvLacks(t *testing.T, socket string, pairs ...string) {
	t.Helper()
	out, err := exec.Command("tmux", "-L", socket, "show-environment", "-g").Output()
	if err != nil {
		t.Fatalf("show-environment -g: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		for _, pair := range pairs {
			if line == pair {
				t.Errorf("server global env leaked %q", line)
			}
		}
	}
}
