package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/data"
)

func newProfiledRunner(t *testing.T, profileConfig string) *ScriptRunner {
	t.Helper()
	root := t.TempDir()
	writeProfileConfig(t, root, "work", profileConfig)
	runner := NewScriptRunner(6200, 10)
	runner.SetProfilesRoot(root)
	return runner
}

func readTrimmed(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.TrimSpace(string(got))
}

func TestScriptRunnerRunSetupRunsProfileSetupAfterRepoSetup(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["echo repo >> order.txt"]}`)
	runner := newProfiledRunner(t, `{
  "setup": ["echo profile-$POSTGRES_PORT >> order.txt"],
  "env": {"POSTGRES_PORT": "$WORKSPACE_PORT"}
}`)
	ws := data.NewWorkspace("feat", "feat", "main", repo, wsRoot)
	ws.Profile = "work"

	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	if got, want := readTrimmed(t, filepath.Join(wsRoot, "order.txt")), "repo\nprofile-6200"; got != want {
		t.Errorf("order.txt = %q, want %q", got, want)
	}
}

func TestScriptRunnerRunTeardownRunsArchiveThenProfileTeardown(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"archive": "echo archive >> order.txt"}`)
	runner := newProfiledRunner(t, `{"teardown": ["echo teardown-$WORKSPACE_PORT >> order.txt"]}`)
	ws := data.NewWorkspace("feat", "feat", "main", repo, wsRoot)
	ws.Profile = "work"

	if errs := runner.RunTeardown(ws); len(errs) != 0 {
		t.Fatalf("RunTeardown() errors = %v", errs)
	}
	if got, want := readTrimmed(t, filepath.Join(wsRoot, "order.txt")), "archive\nteardown-6200"; got != want {
		t.Errorf("order.txt = %q, want %q", got, want)
	}
}

func TestScriptRunnerRunTeardownContinuesPastFailures(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"archive": "echo archive-broke >&2; exit 1"}`)
	runner := newProfiledRunner(t, `{"teardown": ["exit 2", "touch `+marker+`"]}`)
	ws := data.NewWorkspace("feat", "feat", "main", repo, t.TempDir())
	ws.Profile = "work"

	errs := runner.RunTeardown(ws)
	if len(errs) != 2 {
		t.Fatalf("RunTeardown() errors = %v, want 2", errs)
	}
	if !strings.Contains(errs[0].Error(), "archive-broke") {
		t.Errorf("archive error %q should carry stderr", errs[0])
	}
	if !strings.Contains(errs[1].Error(), "exit 2") {
		t.Errorf("teardown error %q should name the command", errs[1])
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("later teardown command did not run: %v", err)
	}
}

func TestScriptRunnerRunTeardownTimesOut(t *testing.T) {
	runner := newProfiledRunner(t, `{"teardown": ["sleep 30"]}`)
	runner.teardownTimeout = 100 * time.Millisecond
	ws := data.NewWorkspace("feat", "", "", t.TempDir(), t.TempDir())
	ws.Profile = "work"

	start := time.Now()
	errs := runner.RunTeardown(ws)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("RunTeardown() took %s, want bounded by timeout", elapsed)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "timed out") {
		t.Fatalf("RunTeardown() errors = %v, want one timeout", errs)
	}
}

func TestScriptRunnerRunTeardownMissingWorktreeStillRuns(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	runner := newProfiledRunner(t, `{"teardown": ["touch `+marker+`"]}`)
	ws := data.NewWorkspace("feat", "", "", t.TempDir(), filepath.Join(t.TempDir(), "gone"))
	ws.Profile = "work"

	if errs := runner.RunTeardown(ws); len(errs) != 0 {
		t.Fatalf("RunTeardown() errors = %v", errs)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("teardown did not run for a missing worktree: %v", err)
	}
}

func TestScriptRunnerRunSetupShouldRunProfileSetupWhenRepoSetupFails(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "profile-ran")
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["echo no-client-dir >&2; exit 1", "touch skipped"]}`)
	runner := newProfiledRunner(t, `{"setup": ["touch `+marker+`"]}`)
	wsRoot := t.TempDir()
	ws := data.NewWorkspace("feat", "feat", "main", repo, wsRoot)
	ws.Profile = "work"

	err := runner.RunSetup(ws)
	if err == nil || !strings.Contains(err.Error(), "no-client-dir") {
		t.Fatalf("RunSetup() error = %v, want the repo failure", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("profile setup did not run after a repo failure: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(wsRoot, "skipped")); statErr == nil {
		t.Error("repo commands after the failing one should not run")
	}
}

func TestScriptRunnerRunSetupShouldReportEveryFailure(t *testing.T) {
	tests := []struct {
		name           string
		profileConfig  string
		expectedOutput []string
	}{
		{name: "repo and profile commands fail", profileConfig: `{"setup": ["echo profile-broke >&2; exit 2"]}`, expectedOutput: []string{"repo-broke", "profile work", "profile-broke"}},
		{name: "profile config is malformed", profileConfig: `{bad`, expectedOutput: []string{"repo-broke", "profile work"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			writeWorkspaceConfig(t, repo, `{"setup-workspace": ["echo repo-broke >&2; exit 1"]}`)
			runner := newProfiledRunner(t, tt.profileConfig)
			ws := data.NewWorkspace("feat", "feat", "main", repo, t.TempDir())
			ws.Profile = "work"

			err := runner.RunSetup(ws)
			if err == nil {
				t.Fatal("RunSetup() error = nil, want failures")
			}
			for _, want := range tt.expectedOutput {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("RunSetup() error = %q, want containing %q", err, want)
				}
			}
		})
	}
}

func TestScriptRunnerRunSetupShouldRunOtherReposWhenOneFails(t *testing.T) {
	ws, repoPaths, worktreePaths := newMultiRepoTestWorkspace(t, "feat", "offer", "backend")
	writeWorkspaceConfig(t, repoPaths[0], `{"setup-workspace": ["cd client && npm i"]}`)
	writeWorkspaceConfig(t, repoPaths[1], `{"setup-workspace": ["printf backend > setup.txt"]}`)
	runner := NewScriptRunner(6200, 10)

	err := runner.RunSetup(ws)
	if err == nil || !strings.Contains(err.Error(), "offer") {
		t.Fatalf("RunSetup() error = %v, want the offer failure", err)
	}
	if got := readTrimmed(t, filepath.Join(worktreePaths[1], "setup.txt")); got != "backend" {
		t.Errorf("backend setup.txt = %q, want %q", got, "backend")
	}
}
