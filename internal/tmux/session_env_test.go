package tmux

import (
	"strings"
	"testing"
)

func TestClientCommandWithEnv_SetsSessionEnvOnCreate(t *testing.T) {
	tests := []struct {
		name           string
		env            []string
		expectedOutput string
	}{
		{
			name:           "quotes each pair",
			env:            []string{"WORKSPACE_PORT=6200", "DSN=a b'c"},
			expectedOutput: `-c '/tmp/work' -e 'WORKSPACE_PORT=6200' -e 'DSN=a b'\''c' sh -lc`,
		},
		{
			name:           "drops unnamed entries",
			env:            []string{"=x", "no-equals", "OK=1"},
			expectedOutput: `-c '/tmp/work' -e 'OK=1' sh -lc`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := ClientCommandWithEnv("s", "/tmp/work", "echo hi", DefaultOptions(), SessionTags{}, tt.env)
			create, _, _ := strings.Cut(cmd, " && ")
			if !strings.Contains(create, tt.expectedOutput) {
				t.Errorf("new-session = %q, want it to contain %q", create, tt.expectedOutput)
			}
		})
	}
}
