package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Skowt/medusa/internal/tmux"
)

// cliHome points medusa at a fresh MEDUSA_HOME and returns it.
func cliHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDUSA_HOME", home)
	return home
}

func cliRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "init")
	return dir
}

type cliResult struct {
	code           int
	stdout, stderr string
}

func cli(t *testing.T, stdin string, args ...string) cliResult {
	t.Helper()
	var out, errOut bytes.Buffer
	_, code := runCLI(args, &cliContext{stdout: &out, stderr: &errOut, stdin: strings.NewReader(stdin)})
	return cliResult{code, out.String(), errOut.String()}
}

func decode[T any](t *testing.T, r cliResult) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(r.stdout), &v); err != nil {
		t.Fatalf("stdout is not JSON (%v):\n%s\nstderr:\n%s", err, r.stdout, r.stderr)
	}
	return v
}

func TestNewCreatesAWorktreeWorkspace(t *testing.T) {
	cliHome(t)
	repo := cliRepo(t)

	r := cli(t, "", "workspace", "new", "feat", "--repo", repo, "--from-current", "--no-agent", "--json")
	if r.code != 0 {
		t.Fatalf("code %d, stderr:\n%s", r.code, r.stderr)
	}
	res := decode[newResult](t, r)
	if res.Name != "feat" || !res.Worktree || res.Branch != "feat" || res.Base != "main" || res.Profile != "Default" {
		t.Fatalf("result = %+v", res)
	}
	if res.Agent != nil || res.TUINotified {
		t.Fatalf("result = %+v, want no agent and no TUI", res)
	}
	if info, err := os.Stat(filepath.Join(res.Root, ".git")); err != nil || info.IsDir() {
		t.Fatalf("%s is not a worktree: %v", res.Root, err)
	}
}

func TestNewRefusesAnUnknownProfile(t *testing.T) {
	home := cliHome(t)
	if err := os.MkdirAll(filepath.Join(home, "profiles", "Default"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := cli(t, "", "ws", "new", "x", "--repo", cliRepo(t), "--profile", "Wrok", "--no-agent")
	if r.code != 1 || !strings.Contains(r.stderr, `"Wrok"`) || !strings.Contains(r.stderr, "Default") {
		t.Fatalf("code %d stderr %q", r.code, r.stderr)
	}
	if r := cli(t, "", "ws", "list", "--json"); len(decode[[]listEntry](t, r)) != 0 {
		t.Fatal("a refused create registered a workspace")
	}
}

func TestNewUsageConflictsExitTwo(t *testing.T) {
	cliHome(t)
	repo := cliRepo(t)
	big := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), maxPromptBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{},
		{"a", "b"},
		{"x", "--base", "main", "--from-current"},
		{"x", "--no-worktree", "--base", "main"},
		{"x", "--prompt", "a", "--prompt-file", big},
		{"x", "--no-agent", "--prompt", "a"},
		{"x", "--agent", "codex", "--permission-mode", "plan"},
		{"x", "--agent", "claude", "--sandbox", "read-only"},
		{"x", "--agent", "gpt"},
		{"x", "--permission-mode", "yolo"},
		{"x", "--fullscreen", "--no-fullscreen"},
		{"x", "--prompt-file", big},
		{"bad name!"},
	} {
		r := cli(t, "", append([]string{"ws", "new", "--repo", repo}, args...)...)
		if r.code != 2 {
			t.Errorf("%v: code %d, want 2 (stderr %q)", args, r.code, r.stderr)
		}
	}
	if r := cli(t, "", "ws", "list", "--json"); len(decode[[]listEntry](t, r)) != 0 {
		t.Fatal("a usage error created a workspace")
	}
}

func TestNewRefusesATakenNameOrBranch(t *testing.T) {
	cliHome(t)
	repo := cliRepo(t)
	if r := cli(t, "", "ws", "new", "dup", "--repo", repo, "--no-agent"); r.code != 0 {
		t.Fatalf("first create: %s", r.stderr)
	}
	if r := cli(t, "", "ws", "new", "dup", "--repo", repo, "--no-agent"); r.code != 1 || !strings.Contains(r.stderr, "already exists") {
		t.Fatalf("duplicate: code %d stderr %q", r.code, r.stderr)
	}
	cmd := exec.Command("git", "branch", "leftover")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if r := cli(t, "", "ws", "new", "leftover", "--repo", repo, "--no-agent"); r.code != 1 || !strings.Contains(r.stderr, "branch 'leftover'") {
		t.Fatalf("leftover branch: code %d stderr %q", r.code, r.stderr)
	}
}

func TestListShowsWorkspaces(t *testing.T) {
	cliHome(t)
	repo := cliRepo(t)
	for _, name := range []string{"one", "two"} {
		if r := cli(t, "", "ws", "new", name, "--repo", repo, "--no-agent", "--group", "g"); r.code != 0 {
			t.Fatalf("create %s: %s", name, r.stderr)
		}
	}
	entries := decode[[]listEntry](t, cli(t, "", "ws", "list", "--json"))
	if len(entries) != 2 || entries[0].Name != "one" || entries[1].Group != "g" || entries[0].Live != 0 {
		t.Fatalf("entries = %+v", entries)
	}
	if got := decode[[]listEntry](t, cli(t, "", "ws", "list", "--json", "--group", "other")); len(got) != 0 {
		t.Fatalf("group filter kept %+v", got)
	}
	table := cli(t, "", "ws", "list")
	if table.code != 0 || !strings.HasPrefix(table.stdout, "NAME") || !strings.Contains(table.stdout, "two") {
		t.Fatalf("table:\n%s", table.stdout)
	}
}

func TestDeleteNeedsYesWithoutATerminal(t *testing.T) {
	cliHome(t)
	repo := cliRepo(t)
	if r := cli(t, "", "ws", "new", "keep", "--repo", repo, "--no-agent"); r.code != 0 {
		t.Fatal(r.stderr)
	}
	if r := cli(t, "", "ws", "delete", "keep"); r.code != 2 || !strings.Contains(r.stderr, "--yes") {
		t.Fatalf("code %d stderr %q", r.code, r.stderr)
	}
	if got := decode[[]listEntry](t, cli(t, "", "ws", "list", "--json")); len(got) != 1 {
		t.Fatal("delete without --yes deleted the workspace")
	}
}

func TestDeleteRemovesTheWorktree(t *testing.T) {
	cliHome(t)
	repo := cliRepo(t)
	created := decode[newResult](t, cli(t, "", "ws", "new", "gone", "--repo", repo, "--no-agent", "--json"))

	r := cli(t, "", "ws", "delete", "gone", "--yes", "--json")
	if r.code != 0 {
		t.Fatalf("delete: code %d stderr %q", r.code, r.stderr)
	}
	if res := decode[deleteResult](t, r); res.ID != created.ID || !res.Deleted {
		t.Fatalf("delete result = %+v", res)
	}
	if _, err := os.Stat(created.Root); !os.IsNotExist(err) {
		t.Fatalf("worktree still there: %v", err)
	}
	if got := decode[[]listEntry](t, cli(t, "", "ws", "list", "--json")); len(got) != 0 {
		t.Fatalf("still listed: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "README")); err != nil {
		t.Fatalf("source repo damaged: %v", err)
	}
	if r := cli(t, "", "ws", "delete", "gone", "--yes"); r.code != 1 {
		t.Fatalf("deleting a missing workspace: code %d", r.code)
	}
}

func TestDeleteCheckoutWorkspaceKeepsTheRepo(t *testing.T) {
	cliHome(t)
	repo := cliRepo(t)
	created := decode[newResult](t, cli(t, "", "ws", "new", "co", "--repo", repo, "--no-worktree", "--no-agent", "--json"))
	if created.Worktree || created.Root != repo {
		t.Fatalf("checkout workspace = %+v", created)
	}
	if r := cli(t, "", "ws", "delete", created.ID, "--yes"); r.code != 0 {
		t.Fatalf("delete by id: %s", r.stderr)
	}
	if info, err := os.Stat(filepath.Join(repo, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("repo's .git is gone: %v", err)
	}
}

// withFakeAgent puts a stand-in `claude` on PATH and a private tmux server
// in the environment, so starting an agent runs neither the real CLI nor
// touches the user's tmux sessions.
func withFakeAgent(t *testing.T) (tmux.Options, string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	bin := t.TempDir()
	marker := filepath.Join(bin, "ran")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\nexec sleep 60\n", marker)
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SHELL", "/bin/sh")
	server := fmt.Sprintf("medusa-cli-test-%d", os.Getpid())
	t.Setenv("MEDUSA_TMUX_SERVER", server)
	t.Setenv("MEDUSA_TMUX_CONFIG", "/dev/null")
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", server, "kill-server").Run() })
	return tmux.DefaultOptions(), marker
}

func TestNewStartsAnAgentWithTheFirstPrompt(t *testing.T) {
	cliHome(t)
	opts, marker := withFakeAgent(t)
	repo := cliRepo(t)

	r := cli(t, "Fix the -bug\nfor real", "ws", "new", "agentic", "--repo", repo, "--prompt-file", "-", "--json")
	if r.code != 0 {
		t.Fatalf("code %d stderr:\n%s", r.code, r.stderr)
	}
	res := decode[newResult](t, r)
	if res.Agent == nil || res.Agent.Assistant != "claude" || res.Agent.SessionID == "" {
		t.Fatalf("agent = %+v", res.Agent)
	}
	if got, _ := tmux.SessionTagValue(res.Agent.TmuxSession, "@medusa_workspace", opts); got != res.ID {
		t.Fatalf("@medusa_workspace = %q, want %q", got, res.ID)
	}

	var args string
	for i := 0; i < 50 && !strings.Contains(args, "for real"); i++ {
		raw, _ := os.ReadFile(marker)
		args = string(raw)
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(args, "--\nFix the -bug\nfor real\n") {
		t.Fatalf("the agent did not get the prompt as its last argument; its arguments were:\n%s", args)
	}

	if r := cli(t, "", "ws", "delete", "agentic", "--yes"); r.code != 0 {
		t.Fatalf("delete: %s", r.stderr)
	}
	if state, _ := tmux.SessionStateFor(res.Agent.TmuxSession, opts); state.Exists {
		t.Fatal("delete left the agent's tmux session running")
	}
}

// tmux refuses a command over ~16 KB, so a prompt cannot ride on the
// new-session command line. One near the CLI's own limit must still arrive.
func TestNewDeliversALongPrompt(t *testing.T) {
	cliHome(t)
	_, marker := withFakeAgent(t)
	repo := cliRepo(t)
	prompt := strings.Repeat("it's a \"long\" -task\n", (maxPromptBytes-100)/20) + "END"

	r := cli(t, prompt, "ws", "new", "long", "--repo", repo, "--prompt-file", "-", "--json")
	if r.code != 0 {
		t.Fatalf("code %d stderr:\n%s", r.code, r.stderr)
	}
	var args string
	for i := 0; i < 50 && !strings.Contains(args, "END"); i++ {
		raw, _ := os.ReadFile(marker)
		args = string(raw)
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.HasSuffix(args, "--\n"+prompt+"\n") {
		t.Fatalf("the agent did not receive the %d-byte prompt intact (got %d bytes of arguments)", len(prompt), len(args))
	}
}

// An agent runs inside a worktree, and a workspace it creates must branch from
// the real repo, not from the worktree it happens to be standing in.
func TestNewFromInsideAWorktreeUsesTheMainRepo(t *testing.T) {
	cliHome(t)
	repo := cliRepo(t)
	parent := decode[newResult](t, cli(t, "", "ws", "new", "parent", "--repo", repo, "--no-agent", "--json"))

	t.Chdir(filepath.Join(parent.Root))
	r := cli(t, "", "ws", "new", "child", "--no-agent", "--json")
	if r.code != 0 {
		t.Fatalf("code %d stderr %s", r.code, r.stderr)
	}
	child := decode[newResult](t, r)

	entries := decode[[]listEntry](t, cli(t, "", "ws", "list", "--json"))
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	recents, err := os.ReadFile(filepath.Join(os.Getenv("MEDUSA_HOME"), "recents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(recents), parent.Root) {
		t.Fatalf("the parent worktree was recorded as a source repo:\n%s", recents)
	}
	cmd := exec.Command("git", "worktree", "list")
	cmd.Dir = repo
	out, _ := cmd.Output()
	if !strings.Contains(string(out), child.Root) {
		t.Fatalf("child worktree is not registered with the main repo:\n%s", out)
	}
}
