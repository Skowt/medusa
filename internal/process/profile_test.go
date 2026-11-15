package process

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Skowt/medusa/internal/data"
)

func writeProfileConfig(t *testing.T, profilesRoot, profile, content string) {
	t.Helper()
	dir := filepath.Join(profilesRoot, profile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ProfileConfigFilename), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", ProfileConfigFilename, err)
	}
}

func TestLoadProfileConfig(t *testing.T) {
	tests := []struct {
		name           string
		content        string // "" = no file written
		expectedOutput ProfileConfig
		expectedErr    bool
	}{
		{name: "missing file is empty"},
		{
			name:    "valid",
			content: `{"setup":["podman run db"],"teardown":["podman rm -f db"],"env":{"POSTGRES_PORT":"$WORKSPACE_PORT"}}`,
			expectedOutput: ProfileConfig{
				Setup:    []string{"podman run db"},
				Teardown: []string{"podman rm -f db"},
				Env:      map[string]string{"POSTGRES_PORT": "$WORKSPACE_PORT"},
			},
		},
		{name: "malformed", content: `{not json`, expectedErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.content != "" {
				writeProfileConfig(t, root, "work", tt.content)
			}
			got, err := LoadProfileConfig(root, "work")
			if (err != nil) != tt.expectedErr {
				t.Fatalf("LoadProfileConfig() error = %v, want error %v", err, tt.expectedErr)
			}
			if err == nil && !reflect.DeepEqual(*got, tt.expectedOutput) {
				t.Errorf("LoadProfileConfig() = %+v, want %+v", *got, tt.expectedOutput)
			}
		})
	}
}

func TestProfileConfigExpandEnv(t *testing.T) {
	t.Setenv("MEDUSA_TEST_FROM_PROCESS", "proc")
	base := map[string]string{"WORKSPACE_PORT": "6210"}
	tests := []struct {
		name           string
		env            map[string]string
		expectedOutput []string
	}{
		{name: "workspace var", env: map[string]string{"POSTGRES_PORT": "$WORKSPACE_PORT"}, expectedOutput: []string{"POSTGRES_PORT=6210"}},
		{name: "process env fallback", env: map[string]string{"X": "$MEDUSA_TEST_FROM_PROCESS"}, expectedOutput: []string{"X=proc"}},
		{name: "sorted by key", env: map[string]string{"B": "2", "A": "1"}, expectedOutput: []string{"A=1", "B=2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ProfileConfig{Env: tt.env}
			if got := cfg.ExpandEnv(base); !reflect.DeepEqual(got, tt.expectedOutput) {
				t.Errorf("ExpandEnv() = %v, want %v", got, tt.expectedOutput)
			}
		})
	}
}

// Profile env expands against the workspace vars, and the workspace's own env
// still wins over it.
func TestEnvBuilder_BuildEnvMapLayersProfileEnv(t *testing.T) {
	tests := []struct {
		name           string
		profileConfig  string
		expectedOutput map[string]string
	}{
		{
			name:           "expanded, workspace env wins",
			profileConfig:  `{"env":{"POSTGRES_PORT":"$WORKSPACE_PORT","SHARED":"profile","NAME":"$MEDUSA_WORKSPACE_NAME"}}`,
			expectedOutput: map[string]string{"POSTGRES_PORT": "6200", "NAME": "feature-1", "SHARED": "workspace"},
		},
		{
			name:           "malformed config contributes nothing",
			profileConfig:  `{broken`,
			expectedOutput: map[string]string{"POSTGRES_PORT": "", "SHARED": "workspace"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeProfileConfig(t, root, "work", tt.profileConfig)
			builder := NewEnvBuilder(NewPortAllocator(6200, 10))
			builder.SetProfilesRoot(root)
			ws := data.NewWorkspace("feature-1", "feature-1", "main", "/repo", "/ws/feature-1")
			ws.Profile = "work"
			ws.Env = map[string]string{"SHARED": "workspace"}

			envMap := builder.BuildEnvMap(ws)
			for k, v := range tt.expectedOutput {
				if envMap[k] != v {
					t.Errorf("%s = %q, want %q", k, envMap[k], v)
				}
			}
		})
	}
}
