package approve

import (
	"testing"
)

func TestParseBashPrefix(t *testing.T) {
	tests := []struct {
		pattern string
		want    string
		ok      bool
	}{
		{"Bash(ls *)", "ls", true},
		{"Bash(git *)", "git", true},
		{"Bash(npm run *)", "npm run", true},
		{"Bash(git:*)", "git", true},
		{"Bash(ls)", "ls", true},
		{"Bash(*)", "", false},
		{"Edit(**)", "", false},
		{"ls", "", false},
		{"Bash()", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseBashPrefix(tt.pattern)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseBashPrefix(%q) = (%q, %v), want (%q, %v)",
				tt.pattern, got, ok, tt.want, tt.ok)
		}
	}
}

func TestStripEnvAssignments(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"ls -la", "ls -la"},
		{"FOO=bar ls -la", "ls -la"},
		{"FOO=bar BAZ=1 npm test", "npm test"},
		{"NODE_ENV=production npm run build", "npm run build"},
		{"=invalid ls", "=invalid ls"},
	}
	for _, tt := range tests {
		if got := StripEnvAssignments(tt.cmd); got != tt.want {
			t.Errorf("StripEnvAssignments(%q) = %q, want %q", tt.cmd, got, tt.want)
		}
	}
}

func TestMatchCommand(t *testing.T) {
	perms := &Permissions{
		Allow: []string{"ls", "grep", "git", "npm run"},
		Deny:  []string{"git push --force", "rm -rf /"},
	}

	tests := []struct {
		cmd  string
		want string
	}{
		{"ls -la", "allow"},
		{"ls", "allow"},
		{"grep foo", "allow"},
		{"git status", "allow"},
		{"git push --force origin main", "deny"},
		{"rm -rf /", "deny"},
		{"npm run build", "allow"},
		{"npm install", ""},
		{"curl http://example.com", ""},
		{"FOO=bar ls", "allow"},
		{"NODE_ENV=prod npm run test", "allow"},
	}

	for _, tt := range tests {
		if got := MatchCommand(tt.cmd, perms); got != tt.want {
			t.Errorf("MatchCommand(%q) = %q, want %q", tt.cmd, got, tt.want)
		}
	}
}

func TestMatchCommand_DenyPrecedence(t *testing.T) {
	perms := &Permissions{
		Allow: []string{"rm"},
		Deny:  []string{"rm -rf /"},
	}
	// "rm -rf /" matches both, deny should win
	if got := MatchCommand("rm -rf /", perms); got != "deny" {
		t.Errorf("MatchCommand deny precedence: got %q, want %q", got, "deny")
	}
	// "rm file.txt" should still be allowed
	if got := MatchCommand("rm file.txt", perms); got != "allow" {
		t.Errorf("MatchCommand allow: got %q, want %q", got, "allow")
	}
}
