package process

import (
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/data"
)

func TestEnvBuilder_BuildEnv(t *testing.T) {
	ports := NewPortAllocator(6200, 10)
	builder := NewEnvBuilder(ports)

	wt := data.NewWorkspace("feature-1", "feature-1", "main", "/home/user/repo", "/home/user/.medusa/workspaces/feature-1")
	wt.Env = map[string]string{
		"CUSTOM_VAR": "custom_value",
	}

	env := builder.BuildEnv(wt)

	// Check required variables are present
	checks := map[string]string{
		"MEDUSA_WORKSPACE_NAME":   "feature-1",
		"MEDUSA_WORKSPACE_ROOT":   "/home/user/.medusa/workspaces/feature-1",
		"MEDUSA_WORKSPACE_BRANCH": "feature-1",
		"ROOT_WORKSPACE_PATH":     "/home/user/repo",
		"CUSTOM_VAR":              "custom_value",
	}

	for key, wantValue := range checks {
		found := false
		for _, e := range env {
			if strings.HasPrefix(e, key+"=") {
				found = true
				gotValue := strings.TrimPrefix(e, key+"=")
				if gotValue != wantValue {
					t.Errorf("%s = %v, want %v", key, gotValue, wantValue)
				}
				break
			}
		}
		if !found {
			t.Errorf("Missing env var: %s", key)
		}
	}

	// Check port variables
	portFound := false
	for _, e := range env {
		if strings.HasPrefix(e, "WORKSPACE_PORT=") {
			portFound = true
			break
		}
	}
	if !portFound {
		t.Error("Missing WORKSPACE_PORT env var")
	}
}

func TestEnvBuilder_BuildEnvMap(t *testing.T) {
	ports := NewPortAllocator(6200, 10)
	builder := NewEnvBuilder(ports)

	wt := data.NewWorkspace("feature-1", "feature-1", "main", "/home/user/repo", "/home/user/.medusa/workspaces/feature-1")

	envMap := builder.BuildEnvMap(wt)

	if envMap["MEDUSA_WORKSPACE_NAME"] != "feature-1" {
		t.Errorf("MEDUSA_WORKSPACE_NAME = %v, want feature-1", envMap["MEDUSA_WORKSPACE_NAME"])
	}
	if envMap["WORKSPACE_PORT"] != "6200" {
		t.Errorf("WORKSPACE_PORT = %v, want 6200", envMap["WORKSPACE_PORT"])
	}
}

func TestEnvBuilder_NilPortAllocator(t *testing.T) {
	builder := NewEnvBuilder(nil)

	wt := data.NewWorkspace("feature-1", "", "", "", "/path/to/wt")

	env := builder.BuildEnv(wt)

	// Should not crash with nil port allocator
	// And should not have port vars
	for _, e := range env {
		if strings.HasPrefix(e, "WORKSPACE_PORT=") {
			t.Error("Should not have WORKSPACE_PORT with nil allocator")
		}
	}
}

// Two workspaces over one repo with no worktree of their own share a root, so
// keying the port allocation off the root handed both run scripts the same
// port and they fought over it.
func TestEnvBuilder_CheckoutWorkspacesGetSeparatePorts(t *testing.T) {
	builder := NewEnvBuilder(NewPortAllocator(6200, 10))

	first := data.NewCheckoutWorkspace("first", "main", "/home/user/repo")
	second := data.NewCheckoutWorkspace("second", "main", "/home/user/repo")
	if first.Root() != second.Root() {
		t.Fatalf("fixture is wrong: roots %q and %q differ", first.Root(), second.Root())
	}

	firstPort := builder.BuildEnvMap(first)["WORKSPACE_PORT"]
	secondPort := builder.BuildEnvMap(second)["WORKSPACE_PORT"]
	if firstPort == secondPort {
		t.Fatalf("both workspaces were given port %s", firstPort)
	}

	// The same workspace must keep its port across calls — a run script that
	// restarts has to come back on the port it advertised.
	if again := builder.BuildEnvMap(first)["WORKSPACE_PORT"]; again != firstPort {
		t.Errorf("port moved from %s to %s on the second call", firstPort, again)
	}
}
