package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Skowt/medusa/internal/shellutil"
)

// detachedStartTimeout bounds starting a session, including the tmux server
// itself when none is running yet.
const detachedStartTimeout = 10 * time.Second

// StartDetachedSession starts command in a new detached tmux session with the
// same options and tags the TUI's client command sets, so the TUI can later
// attach to it as an ordinary tab. It fails if a session with that name
// already exists, rather than attaching to it as the client command would.
func StartDetachedSession(sessionName, workDir, command string, env []string, opts Options, tags SessionTags) error {
	if err := EnsureAvailable(); err != nil {
		return err
	}
	base := tmuxBase(opts)
	session := shellutil.Quote(sessionName)
	create := fmt.Sprintf("%s new-session -ds %s -c %s sh -lc %s",
		base, session, shellutil.Quote(workDir), shellutil.Quote(command))
	// The settings are "; "-separated, so "create && settings" would gate only
	// the first of them and report the last one's status. Stop on a failed
	// create instead — a duplicate name must be an error, not a silent no-op.
	script := create + " || exit $?; " + sessionSettings(base, session, opts, tags) + "true"

	ctx, cancel := context.WithTimeout(context.Background(), detachedStartTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("starting tmux session %s: %w: %s", sessionName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SessionClientCount returns how many tmux clients are attached to a session.
// A session that does not exist has none.
func SessionClientCount(sessionName string, opts Options) (int, error) {
	if err := EnsureAvailable(); err != nil {
		return 0, err
	}
	exists, err := hasSession(sessionName, opts)
	if err != nil || !exists {
		return 0, err
	}
	cmd, cancel := tmuxCommand(opts, "list-clients", "-t", sessionName, "-F", "#{client_name}")
	defer cancel()
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}
