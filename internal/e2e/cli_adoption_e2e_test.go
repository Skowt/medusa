package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/tmux"
)

// TestCLIWorkspaceIsAdoptedByTheTUI drives the whole headless path: with no
// TUI running, `medusa workspace new --prompt` creates a workspace and starts
// its agent in a tagged tmux session; a TUI started afterwards must attach to
// that session as an ordinary agent tab, showing the agent that got the
// prompt. It breaks if the tags, the OpenTabs record, or the restore path
// stop agreeing.
func TestCLIWorkspaceIsAdoptedByTheTUI(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeConfig(t, home, true)
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\necho \"STUB GOT: $*\"\nexec sleep 1000\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	server := fmt.Sprintf("medusa-e2e-cli-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	defer killTmuxServer(t, server)
	env := append(sessionEnv(binDir, server), "HOME="+home, "MEDUSA_HOME="+filepath.Join(home, ".medusa"))

	bin, cleanupBin, err := buildMedusaBinary()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	defer cleanupBin()
	cmd := exec.Command(bin, "workspace", "new", "adopted", "--repo", repo, "--prompt", "hello from e2e", "--json")
	cmd.Env = append(stripGitEnv(os.Environ()), env...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("medusa workspace new: %v\n%s", err, out)
	}
	var created struct {
		ID    string `json:"id"`
		Agent struct {
			TmuxSession string `json:"tmux_session"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got, _ := tmux.SessionTagValue(created.Agent.TmuxSession, "@medusa_workspace", opts); got != created.ID {
		t.Fatalf("session %q tagged %q, want %q", created.Agent.TmuxSession, got, created.ID)
	}

	session, cleanup, err := StartPTYSession(PTYOptions{Home: home, Env: env, Width: 160, Height: 40})
	if err != nil {
		t.Fatalf("start TUI: %v", err)
	}
	defer cleanup()

	waitForUIContains(t, session, "adopted", persistenceTimeout)
	activatePrimaryWorkspace(t, session)
	waitForUIContains(t, session, "STUB GOT", persistenceTimeout)
	waitForUIContains(t, session, "hello from e2e", persistenceTimeout)

	if sessions, _ := tmux.ListSessions(opts); len(sessions) != 1 || sessions[0] != created.Agent.TmuxSession {
		t.Fatalf("sessions = %v, want only the CLI's %s: the TUI started a second agent instead of adopting", sessions, created.Agent.TmuxSession)
	}

	quitApp(t, session)
	if err := session.WaitForExit(persistenceTimeout); err != nil {
		t.Fatalf("waiting for exit: %v", err)
	}
}
