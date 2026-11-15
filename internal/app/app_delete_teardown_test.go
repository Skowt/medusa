package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/process"
)

// Teardown must run while the worktree still exists, see the workspace's
// port, and never block the delete when a command fails. A workspace over the
// repo's own checkout runs none: its setup never ran either.
func TestDeleteWorkspace_RunsProfileTeardown(t *testing.T) {
	tests := []struct {
		name           string
		worktree       bool
		expectedOutput string // what the teardown marker holds; "" = never ran
	}{
		{name: "worktree workspace", worktree: true, expectedOutput: "6200\n"},
		{name: "checkout workspace", worktree: false, expectedOutput: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skipIfNoGit(t)
			repo := t.TempDir()
			runGit(t, repo, "init", "-b", "main")
			runGit(t, repo, "commit", "--no-verify", "--allow-empty", "-m", "init")
			ws := data.NewCheckoutWorkspace("co", "main", repo)
			if tt.worktree {
				worktree := filepath.Join(normalizePath(t.TempDir()), "feat")
				runGit(t, repo, "worktree", "add", "-b", "feat", worktree, "main")
				ws = data.NewWorkspace("feat", "feat", "main", repo, worktree)
			}
			ws.Profile = "work"
			marker := filepath.Join(t.TempDir(), "teardown")
			profilesRoot := t.TempDir()
			if err := os.MkdirAll(filepath.Join(profilesRoot, "work"), 0o755); err != nil {
				t.Fatal(err)
			}
			profile := `{"teardown": ["exit 1", "test -d \"$MEDUSA_WORKSPACE_ROOT\" && echo $WORKSPACE_PORT > ` + marker + `"]}`
			if err := os.WriteFile(filepath.Join(profilesRoot, "work", process.ProfileConfigFilename), []byte(profile), 0o644); err != nil {
				t.Fatal(err)
			}

			tmp := t.TempDir()
			a := &App{
				registry:   data.NewRegistry(filepath.Join(tmp, "workspaces.json")),
				workspaces: data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata")),
				scripts:    process.NewScriptRunner(6200, 10),
			}
			a.scripts.SetProfilesRoot(profilesRoot)

			if _, ok := a.deleteWorkspace(ws)().(messages.WorkspaceDeleted); !ok {
				t.Fatal("deleteWorkspace() did not delete despite a failing teardown")
			}
			got, _ := os.ReadFile(marker)
			if string(got) != tt.expectedOutput {
				t.Errorf("teardown marker = %q, want %q", got, tt.expectedOutput)
			}
		})
	}
}
