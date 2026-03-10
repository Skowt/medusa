package approve

import (
	"sort"
	"testing"
)

func TestIsCompound(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"ls -la", false},
		{"git status", false},
		{"ls | grep foo", true},
		{"cd /tmp && make", true},
		{"echo hello; echo world", true},
		{"echo $(whoami)", true},
		{"diff <(ls a) <(ls b)", true},
		{"echo `date`", true},
		{"FOO=bar ls", false},
	}
	for _, tt := range tests {
		if got := IsCompound(tt.cmd); got != tt.want {
			t.Errorf("IsCompound(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestExtractCommands(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want []string
	}{
		{
			name: "simple pipe",
			cmd:  "ls | grep foo",
			want: []string{"ls", "grep foo"},
		},
		{
			name: "chain with &&",
			cmd:  "cd /tmp && make build",
			want: []string{"cd /tmp", "make build"},
		},
		{
			name: "chain with ||",
			cmd:  "test -f file || touch file",
			want: []string{"test -f file", "touch file"},
		},
		{
			name: "semicolons",
			cmd:  "echo hello; echo world",
			want: []string{"echo hello", "echo world"},
		},
		{
			name: "subshell",
			cmd:  "(echo hello; echo world)",
			want: []string{"echo hello", "echo world"},
		},
		{
			name: "command substitution",
			cmd:  "echo $(whoami)",
			want: []string{"echo $(whoami)", "whoami"},
		},
		{
			name: "pipe chain",
			cmd:  "cat file | grep error | wc -l",
			want: []string{"cat file", "grep error", "wc -l"},
		},
		{
			name: "complex chain",
			cmd:  "mkdir -p /tmp/test && cd /tmp/test && git init",
			want: []string{"mkdir -p /tmp/test", "cd /tmp/test", "git init"},
		},
		{
			name: "for loop",
			cmd:  "for f in *.txt; do cat $f; done",
			want: []string{"cat $f"},
		},
		{
			name: "if clause",
			cmd:  "if true; then echo yes; else echo no; fi",
			want: []string{"true", "echo yes", "echo no"},
		},
		{
			name: "while loop",
			cmd:  "while read line; do echo $line; done",
			want: []string{"read line", "echo $line"},
		},
		{
			name: "case statement",
			cmd:  `case $x in a) echo a;; b) echo b;; esac`,
			want: []string{"echo a", "echo b"},
		},
		{
			name: "process substitution",
			cmd:  "diff <(ls dir1) <(ls dir2)",
			want: []string{"diff <(ls dir1) <(ls dir2)", "ls dir1", "ls dir2"},
		},
		{
			name: "bash -c recursion",
			cmd:  `bash -c 'echo hello && echo world'`,
			want: []string{`bash -c 'echo hello && echo world'`, "echo hello", "echo world"},
		},
		{
			name: "env prefix in pipe",
			cmd:  "NODE_ENV=test npm run build | tee log.txt",
			want: []string{"NODE_ENV=test npm run build", "tee log.txt"},
		},
		{
			name: "nested command substitution",
			cmd:  "echo $(cat $(find . -name '*.txt'))",
			want: []string{
				"echo $(cat $(find . -name '*.txt'))",
				"cat $(find . -name '*.txt')",
				"find . -name '*.txt'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractCommands(tt.cmd)
			if err != nil {
				t.Fatalf("ExtractCommands(%q) error: %v", tt.cmd, err)
			}
			sort.Strings(got)
			sort.Strings(tt.want)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractCommands(%q) = %v (len %d), want %v (len %d)",
					tt.cmd, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ExtractCommands(%q)[%d] = %q, want %q",
						tt.cmd, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractCommands_ParseError(t *testing.T) {
	// Incomplete command should fail parsing
	_, err := ExtractCommands("if; then")
	if err == nil {
		t.Error("expected parse error for invalid command")
	}
}
