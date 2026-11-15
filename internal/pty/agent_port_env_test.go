package pty

import (
	"reflect"
	"testing"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/process"
)

// The agent must see the port the scripts see: both read the shared allocator
// by workspace ID, so a workspace the scripts touched first keeps its number.
func TestWorkspacePortEnv(t *testing.T) {
	ws := data.NewWorkspace("beta", "beta", "main", "/repo", "/wt/beta")
	tests := []struct {
		name           string
		ports          func() *process.PortAllocator
		expectedOutput []string
	}{
		{name: "no allocator wired", ports: func() *process.PortAllocator { return nil }, expectedOutput: nil},
		{
			name: "matches the port the scripts were given",
			ports: func() *process.PortAllocator {
				p := process.NewPortAllocator(6200, 10)
				p.AllocatePort("other-workspace")
				p.PortRange(string(ws.ID()))
				return p
			},
			expectedOutput: []string{"WORKSPACE_PORT=6210", "WORKSPACE_PORT_RANGE=6210-6219"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workspacePortEnv(tt.ports(), ws); !reflect.DeepEqual(got, tt.expectedOutput) {
				t.Errorf("workspacePortEnv() = %v, want %v", got, tt.expectedOutput)
			}
		})
	}
}
