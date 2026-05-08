package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/data"
)

func writeWorkspaceConfig(t *testing.T, repoPath, content string) {
	configDir := filepath.Join(repoPath, ".medusa")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir .medusa: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "workspaces.json"), []byte(content), 0644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}
}

func TestScriptRunnerLoadConfigMissing(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6200, 10)

	cfg, err := runner.LoadConfig(repo)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.RunScript != "" || cfg.ArchiveScript != "" || len(cfg.SetupWorkspace) != 0 {
		t.Fatalf("expected empty config when file missing, got %+v", cfg)
	}
}

func TestScriptRunnerLoadConfigMalformedJSON(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{invalid json}`)

	runner := NewScriptRunner(6200, 10)
	_, err := runner.LoadConfig(repo)
	if err == nil {
		t.Fatalf("LoadConfig() should fail for malformed JSON")
	}
}

func TestScriptRunnerLoadConfigValidJSON(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{
  "setup-workspace": ["echo setup1", "echo setup2"],
  "run": "npm start",
  "archive": "tar -czf archive.tar.gz ."
}`)

	runner := NewScriptRunner(6200, 10)
	cfg, err := runner.LoadConfig(repo)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.SetupWorkspace) != 2 {
		t.Fatalf("expected 2 setup commands, got %d", len(cfg.SetupWorkspace))
	}
	if cfg.RunScript != "npm start" {
		t.Fatalf("expected run script 'npm start', got %s", cfg.RunScript)
	}
	if cfg.ArchiveScript != "tar -czf archive.tar.gz ." {
		t.Fatalf("expected archive script, got %s", cfg.ArchiveScript)
	}
}

func TestScriptRunnerLoadConfigPermissionError(t *testing.T) {
	repo := t.TempDir()
	configDir := filepath.Join(repo, ".medusa")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir .medusa: %v", err)
	}
	configPath := filepath.Join(configDir, "workspaces.json")
	if err := os.WriteFile(configPath, []byte(`{"run":"test"}`), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// Make file unreadable
	if err := os.Chmod(configPath, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(configPath, 0644)
	})

	runner := NewScriptRunner(6200, 10)
	_, err := runner.LoadConfig(repo)
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
	if os.IsNotExist(err) {
		t.Fatalf("expected permission error, got IsNotExist: %v", err)
	}
}

func TestScriptRunnerRunSetupAndEnv(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{
  "setup-workspace": ["printf \"$MEDUSA_WORKSPACE_NAME-$CUSTOM_VAR\" > setup.txt"]
}`)

	runner := NewScriptRunner(6200, 10)
	wt := data.NewWorkspace("feature-1", "feature-1", "main", repo, wsRoot)
	wt.Env = map[string]string{"CUSTOM_VAR": "hello"}

	if err := runner.RunSetup(wt); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(wsRoot, "setup.txt"))
	if err != nil {
		t.Fatalf("expected setup.txt to exist: %v", err)
	}
	if strings.TrimSpace(string(contents)) != "feature-1-hello" {
		t.Fatalf("unexpected setup.txt contents: %s", contents)
	}
}

func TestScriptRunnerRunSetupFailure(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{
  "setup-workspace": ["exit 1"]
}`)

	runner := NewScriptRunner(6200, 10)
	wt := data.NewWorkspace("test", "", "", repo, wsRoot)

	if err := runner.RunSetup(wt); err == nil {
		t.Fatalf("expected RunSetup() to fail for failing command")
	}
}

func TestScriptRunnerRunScriptConfigAndWorkspaceScripts(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{
  "run": "printf run-config > run.txt"
}`)

	runner := NewScriptRunner(6200, 10)
	wt := data.NewWorkspace("test", "", "", repo, wsRoot)

	_, err := runner.RunScript(wt, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	if err := waitForFile(filepath.Join(wsRoot, "run.txt"), 2*time.Second); err != nil {
		t.Fatalf("expected run.txt to be created: %v", err)
	}

	// Now test workspace scripts fallback when config missing.
	writeWorkspaceConfig(t, repo, `{}`)
	wt.Scripts = data.ScriptsConfig{Run: "printf run-workspace > run-workspace.txt"}
	_, err = runner.RunScript(wt, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript() workspace scripts error = %v", err)
	}
	if err := waitForFile(filepath.Join(wsRoot, "run-workspace.txt"), 2*time.Second); err != nil {
		t.Fatalf("expected run-workspace.txt to be created: %v", err)
	}
}

func TestScriptRunnerRunScriptMissing(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{}`)

	runner := NewScriptRunner(6200, 10)
	wt := data.NewWorkspace("test", "", "", repo, wsRoot)

	if _, err := runner.RunScript(wt, ScriptRun); err == nil {
		t.Fatalf("expected RunScript() to fail when no script configured")
	}
}

func TestScriptRunnerStop(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{
  "run": "sleep 5"
}`)

	runner := NewScriptRunner(6200, 10)
	wt := data.NewWorkspace("test", "", "", repo, wsRoot)

	if _, err := runner.RunScript(wt, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}

	if !runner.IsRunning(wt) {
		t.Fatalf("expected script to be running")
	}

	if err := runner.Stop(wt); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for runner.IsRunning(wt) {
		select {
		case <-deadline:
			t.Fatalf("script did not stop in time")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestNormalizeRunCommandNames(t *testing.T) {
	tests := []struct {
		name      string
		input     []RunCommand
		wantNames []string
		wantWarns int
	}{
		{
			name:      "empty name derived from short command",
			input:     []RunCommand{{Command: "npm start"}},
			wantNames: []string{"npm start"},
		},
		{
			name:      "empty name truncated for long command",
			input:     []RunCommand{{Command: "python manage.py runserver 0.0.0.0:8000"}},
			wantNames: []string{"python manage.py runs…"},
		},
		{
			name:      "explicit name preserved",
			input:     []RunCommand{{Name: "api", Command: "anything"}},
			wantNames: []string{"api"},
		},
		{
			name:      "empty name and empty command falls back to dev server",
			input:     []RunCommand{{}},
			wantNames: []string{"dev server"},
		},
		{
			name: "duplicate explicit names get numeric suffixes",
			input: []RunCommand{
				{Name: "backend", Command: "a"},
				{Name: "backend", Command: "b"},
				{Name: "backend", Command: "c"},
			},
			wantNames: []string{"backend", "backend (2)", "backend (3)"},
			wantWarns: 2,
		},
		{
			name: "pre-existing suffix is respected when de-duping",
			input: []RunCommand{
				{Name: "backend", Command: "a"},
				{Name: "backend (2)", Command: "b"},
				{Name: "backend", Command: "c"},
			},
			wantNames: []string{"backend", "backend (2)", "backend (3)"},
			wantWarns: 1,
		},
		{
			name: "duplicate derived names also de-duped",
			input: []RunCommand{
				{Command: "npm start"},
				{Command: "npm start"},
			},
			wantNames: []string{"npm start", "npm start (2)"},
			wantWarns: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warns := normalizeRunCommandNames(tt.input)
			if len(got) != len(tt.wantNames) {
				t.Fatalf("got %d commands, want %d", len(got), len(tt.wantNames))
			}
			for i, want := range tt.wantNames {
				if got[i].Name != want {
					t.Errorf("name[%d] = %q, want %q", i, got[i].Name, want)
				}
			}
			if len(warns) != tt.wantWarns {
				t.Errorf("warnings = %d, want %d (%v)", len(warns), tt.wantWarns, warns)
			}
		})
	}
}

// newMultiRepoTestWorkspace builds a multi-repo Workspace where each repo has
// its own source dir and its own worktree dir under a shared parent.
func newMultiRepoTestWorkspace(t *testing.T, name string, repoNames ...string) (*data.Workspace, []string, []string) {
	t.Helper()
	parent := t.TempDir()
	repos := make([]data.RepoRef, len(repoNames))
	worktrees := make([]data.WorktreeRef, len(repoNames))
	repoPaths := make([]string, len(repoNames))
	worktreePaths := make([]string, len(repoNames))
	for i, n := range repoNames {
		repoPath := filepath.Join(t.TempDir(), n)
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("mkdir repo %s: %v", n, err)
		}
		wtPath := filepath.Join(parent, n)
		if err := os.MkdirAll(wtPath, 0755); err != nil {
			t.Fatalf("mkdir worktree %s: %v", n, err)
		}
		repos[i] = data.RepoRef{Path: repoPath, Name: n}
		worktrees[i] = data.WorktreeRef{Branch: name, Base: "main", Root: wtPath}
		repoPaths[i] = repoPath
		worktreePaths[i] = wtPath
	}
	ws := data.NewMultiRepoWorkspace(name, repos, worktrees)
	return ws, repoPaths, worktreePaths
}

func TestScriptRunnerRunSetupMultiRepo(t *testing.T) {
	ws, repoPaths, worktreePaths := newMultiRepoTestWorkspace(t, "feat", "frontend", "backend")

	writeWorkspaceConfig(t, repoPaths[0], `{
  "setup-workspace": ["printf frontend > setup.txt"]
}`)
	writeWorkspaceConfig(t, repoPaths[1], `{
  "setup-workspace": ["printf backend > setup.txt"]
}`)

	runner := NewScriptRunner(6200, 10)
	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}

	for i, want := range []string{"frontend", "backend"} {
		got, err := os.ReadFile(filepath.Join(worktreePaths[i], "setup.txt"))
		if err != nil {
			t.Fatalf("read setup.txt for %s: %v", ws.Repos[i].Name, err)
		}
		if string(got) != want {
			t.Errorf("setup.txt[%s] = %q, want %q", ws.Repos[i].Name, got, want)
		}
	}
}

func TestScriptRunnerRunSetupMultiRepoOneRepoEmpty(t *testing.T) {
	ws, repoPaths, worktreePaths := newMultiRepoTestWorkspace(t, "feat", "frontend", "backend")

	writeWorkspaceConfig(t, repoPaths[0], `{
  "setup-workspace": ["printf only-frontend > setup.txt"]
}`)
	// backend has no .medusa/workspaces.json — should be skipped silently.

	runner := NewScriptRunner(6200, 10)
	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(worktreePaths[0], "setup.txt")); err != nil {
		t.Fatalf("expected frontend/setup.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worktreePaths[1], "setup.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected backend/setup.txt to NOT exist, got err=%v", err)
	}
}

func TestScriptRunnerGetRunCommandsMultiRepo(t *testing.T) {
	ws, repoPaths, worktreePaths := newMultiRepoTestWorkspace(t, "feat", "frontend", "backend")

	writeWorkspaceConfig(t, repoPaths[0], `{
  "run": [{"name": "dev", "command": "npm start"}]
}`)
	writeWorkspaceConfig(t, repoPaths[1], `{
  "run": [{"name": "api", "command": "go run ."}]
}`)

	runner := NewScriptRunner(6200, 10)
	cmds, _, warnings, err := runner.GetRunCommands(ws)
	if err != nil {
		t.Fatalf("GetRunCommands() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d (%+v)", len(cmds), cmds)
	}
	wantNames := []string{"frontend: dev", "backend: api"}
	for i, want := range wantNames {
		if cmds[i].Name != want {
			t.Errorf("name[%d] = %q, want %q", i, cmds[i].Name, want)
		}
	}
	// Each command must cd into its worktree first (multi-repo: ws.Root() is
	// the parent, so wrapping is required to start the dev server in the
	// correct worktree).
	for i, wt := range worktreePaths {
		if !strings.Contains(cmds[i].Command, "cd '"+wt+"'") {
			t.Errorf("command[%d] = %q, expected to cd into %q", i, cmds[i].Command, wt)
		}
	}
}

func TestScriptRunnerGetRunCommandsSingleRepoNoCdWrap(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{
  "run": "npm start"
}`)

	runner := NewScriptRunner(6200, 10)
	ws := data.NewWorkspace("solo", "feat", "main", repo, wsRoot)

	cmds, _, _, err := runner.GetRunCommands(ws)
	if err != nil {
		t.Fatalf("GetRunCommands() error = %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	// Single-repo: ws.Root() == worktree, so no cd wrapping.
	if cmds[0].Command != "npm start" {
		t.Errorf("command = %q, want %q (single-repo should not wrap with cd)", cmds[0].Command, "npm start")
	}
}

func TestScriptRunnerGetRunCommandsMultiRepoFallsBackToWorkspaceScripts(t *testing.T) {
	ws, _, _ := newMultiRepoTestWorkspace(t, "feat", "frontend", "backend")
	// No per-repo configs, but workspace has a Scripts.Run.
	ws.Scripts = data.ScriptsConfig{Run: "echo workspace"}

	runner := NewScriptRunner(6200, 10)
	cmds, _, _, err := runner.GetRunCommands(ws)
	if err != nil {
		t.Fatalf("GetRunCommands() error = %v", err)
	}
	if len(cmds) != 1 || cmds[0].Command != "echo workspace" {
		t.Fatalf("expected workspace fallback, got %+v", cmds)
	}
}

func TestScriptRunnerGetRunCommandsMultiRepoNoScripts(t *testing.T) {
	ws, _, _ := newMultiRepoTestWorkspace(t, "feat", "frontend", "backend")

	runner := NewScriptRunner(6200, 10)
	if _, _, _, err := runner.GetRunCommands(ws); err == nil {
		t.Fatal("expected GetRunCommands() to fail when no repo has run scripts and ws.Scripts.Run is empty")
	}
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-deadline:
			return os.ErrNotExist
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}
