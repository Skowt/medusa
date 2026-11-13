package pty

import (
	"regexp"
	"strings"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/process"
	"github.com/Skowt/medusa/internal/shellutil"
	"github.com/Skowt/medusa/internal/tmux"
)

// agentClientEnv returns what the agent's tmux client process gets on top of
// medusa's own env: terminal settings only, never anything per workspace. The
// client that starts the tmux server has its env copied into the server's
// global env, so a workspace var here would leak into every later session.
func agentClientEnv() []string {
	return []string{
		"LINES=",   // Unset to force ioctl usage
		"COLUMNS=", // Unset to force ioctl usage
		"COLORTERM=truecolor",
	}
}

// agentClientCommand returns the tmux client command for a prepared agent.
// The workspace vars ride on the session (new-session -e), so the shell the
// pane drops to after the agent exits keeps them.
func agentClientCommand(p PreparedAgent, workDir string, tags tmux.SessionTags) string {
	return tmux.ClientCommandWithEnv(p.SessionName, workDir, p.Command, tmux.DefaultOptions(), tags, p.SessionEnv)
}

// workspaceIdentityEnv names the workspace a session belongs to.
func workspaceIdentityEnv(ws *data.Workspace) []string {
	return []string{
		"WORKSPACE_ROOT=" + ws.Root(),
		"WORKSPACE_NAME=" + ws.Name,
	}
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// agentSessionEnv returns the vars the agent itself must see: the workspace
// port and the workspace profile's medusa-profile.json env, expanded against
// the same workspace env the setup scripts get.
func agentSessionEnv(ports *process.PortAllocator, profilesRoot string, ws *data.Workspace) []string {
	env := workspacePortEnv(ports, ws)
	builder := process.NewEnvBuilder(ports)
	builder.SetProfilesRoot(profilesRoot)
	return append(env, builder.ProfileEnv(ws)...)
}

// envAssignments renders env as a `K='v' ` prefix for the agent command.
//
// The same vars also reach the pane via new-session -e; the prefix is kept so
// the agent wins over a login-shell rc (`sh -lc`) that reassigns them, and so
// the agent command string alone shows what it runs with. Names that are not
// valid shell identifiers are dropped, since the shell would run them as the
// command.
func envAssignments(env []string) string {
	var b strings.Builder
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || !envNamePattern.MatchString(name) {
			logging.Warn("Skipping invalid agent env entry %q", kv)
			continue
		}
		b.WriteString(name + "=" + shellutil.Quote(value) + " ")
	}
	return b.String()
}
