package pty

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/process"
)

func TestEnvAssignments(t *testing.T) {
	tests := []struct {
		name           string
		env            []string
		expectedOutput string
	}{
		{name: "quotes values", env: []string{"WORKSPACE_PORT=6200", "DSN=a b'c"}, expectedOutput: `WORKSPACE_PORT='6200' DSN='a b'\''c' `},
		{name: "drops invalid names", env: []string{"1BAD=x", "no-equals", "OK=1"}, expectedOutput: `OK='1' `},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := envAssignments(tt.env); got != tt.expectedOutput {
				t.Errorf("envAssignments() = %q, want %q", got, tt.expectedOutput)
			}
		})
	}
}

// The agent must see the profile env expanded against the same port the
// scripts get, so a DB container started by setup and the agent agree.
func TestAgentSessionEnv_IncludesPortAndExpandedProfileEnv(t *testing.T) {
	profilesRoot := t.TempDir()
	dir := filepath.Join(profilesRoot, "work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"env":{"POSTGRES_PORT":"$WORKSPACE_PORT"}}`
	if err := os.WriteFile(filepath.Join(dir, process.ProfileConfigFilename), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ports := process.NewPortAllocator(6200, 10)
	ports.AllocatePort("other-workspace")
	ws := data.NewWorkspace("alpha", "alpha", "main", "/repo", "/wt/alpha")
	ws.Profile = "work"

	got := agentSessionEnv(ports, profilesRoot, ws)
	expectedOutput := []string{"WORKSPACE_PORT=6210", "WORKSPACE_PORT_RANGE=6210-6219", "POSTGRES_PORT=6210"}
	if !reflect.DeepEqual(got, expectedOutput) {
		t.Errorf("agentSessionEnv() = %v, want %v", got, expectedOutput)
	}
}
