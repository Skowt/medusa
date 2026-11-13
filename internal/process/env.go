package process

import (
	"fmt"
	"os"
	"strings"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/logging"
)

// EnvBuilder builds environment variables for script execution
type EnvBuilder struct {
	portAllocator *PortAllocator
	// profilesRoot locates each workspace profile's medusa-profile.json; empty
	// means profiles contribute no env.
	profilesRoot string
}

// NewEnvBuilder creates a new environment builder
func NewEnvBuilder(ports *PortAllocator) *EnvBuilder {
	return &EnvBuilder{
		portAllocator: ports,
	}
}

// SetProfilesRoot points the builder at ~/.medusa/profiles so a workspace's
// profile env is layered into what it builds.
func (b *EnvBuilder) SetProfilesRoot(root string) {
	b.profilesRoot = root
}

// BuildEnv creates environment variables for a workspace. Later entries win,
// so the order is: process env, workspace vars, profile env, ws.Env — the
// workspace's own overrides are the most specific and take precedence.
func (b *EnvBuilder) BuildEnv(ws *data.Workspace) []string {
	env := os.Environ()
	env = append(env, b.workspaceVars(ws)...)
	env = append(env, b.ProfileEnv(ws)...)
	for k, v := range ws.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

// BuildEnvMap creates a map of environment variables, with the same
// precedence as BuildEnv.
func (b *EnvBuilder) BuildEnvMap(ws *data.Workspace) map[string]string {
	envMap := make(map[string]string)
	for _, kv := range b.workspaceVars(ws) {
		setKV(envMap, kv)
	}
	for _, kv := range b.ProfileEnv(ws) {
		setKV(envMap, kv)
	}
	for k, v := range ws.Env {
		envMap[k] = v
	}
	return envMap
}

// ProfileEnv returns the workspace profile's env, expanded against the
// workspace vars and ws.Env. A malformed profile config is logged and
// contributes nothing: env building has no error path, and setup surfaces the
// same parse error when it loads the file.
func (b *EnvBuilder) ProfileEnv(ws *data.Workspace) []string {
	cfg, err := LoadProfileConfig(b.profilesRoot, ws.Profile)
	if err != nil {
		logging.Warn("Ignoring profile env for workspace %s (profile %q): %v", ws.Name, ws.Profile, err)
		return nil
	}
	if len(cfg.Env) == 0 {
		return nil
	}
	base := make(map[string]string)
	for _, kv := range b.workspaceVars(ws) {
		setKV(base, kv)
	}
	for k, v := range ws.Env {
		base[k] = v
	}
	return cfg.ExpandEnv(base)
}

// workspaceVars returns the medusa-defined workspace variables, including the
// port allocation when an allocator is wired.
func (b *EnvBuilder) workspaceVars(ws *data.Workspace) []string {
	vars := []string{
		fmt.Sprintf("MEDUSA_WORKSPACE_NAME=%s", ws.Name),
		fmt.Sprintf("MEDUSA_WORKSPACE_ROOT=%s", ws.Root()),
		fmt.Sprintf("MEDUSA_WORKSPACE_BRANCH=%s", ws.Branch()),
		fmt.Sprintf("ROOT_WORKSPACE_PATH=%s", ws.PrimaryRepo().Path),
	}
	if b.portAllocator != nil {
		port, rangeEnd := b.portAllocator.PortRange(string(ws.ID()))
		vars = append(vars,
			fmt.Sprintf("WORKSPACE_PORT=%d", port),
			fmt.Sprintf("WORKSPACE_PORT_RANGE=%d-%d", port, rangeEnd),
		)
	}
	return vars
}

func setKV(m map[string]string, kv string) {
	if k, v, ok := strings.Cut(kv, "="); ok {
		m[k] = v
	}
}
