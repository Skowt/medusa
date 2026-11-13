package pty

import "testing"

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
