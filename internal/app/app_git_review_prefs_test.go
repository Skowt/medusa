package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/ui/common"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/gitreview"
	"github.com/Skowt/medusa/internal/messages"
)

func prefsApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := &config.Config{Paths: &config.Paths{ConfigPath: path}}
	return &App{config: cfg}, path
}

func readUIField(t *testing.T, path, key string) any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var payload struct {
		UI map[string]any `json:"ui"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return payload.UI[key]
}

// TestReviewPrefsArePersisted is the point of the whole path: ticking the toggle
// once has to survive the next press of Review Changes, and every one after it.
func TestReviewPrefsArePersisted(t *testing.T) {
	a, path := prefsApp(t)

	a.handleGitReviewPrefsChanged(messages.GitReviewPrefsChanged{
		Scope: string(gitreview.ScopeBranch), AutoSend: true,
	})

	if got := a.config.UI.LastReviewScope; got != "branch" {
		t.Errorf("in-memory scope = %q, want branch", got)
	}
	if !a.config.UI.LastReviewAutoSend {
		t.Error("in-memory auto-send was not set")
	}
	if got := readUIField(t, path, "last_review_scope"); got != "branch" {
		t.Errorf("saved scope = %v, want branch", got)
	}
	if got := readUIField(t, path, "last_review_autosend"); got != true {
		t.Errorf("saved auto-send = %v, want true", got)
	}
}

// TestUnchangedReviewPrefsAreNotRewritten: the page reports its scope whenever it
// changes, and a report that matches is a config write for nothing.
func TestUnchangedReviewPrefsAreNotRewritten(t *testing.T) {
	a, path := prefsApp(t)
	a.config.UI.LastReviewScope = "working"
	a.config.UI.LastReviewAutoSend = false

	a.handleGitReviewPrefsChanged(messages.GitReviewPrefsChanged{Scope: "working", AutoSend: false})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("an unchanged preference wrote the config file (err = %v)", err)
	}
}

// TestReviewPrefsWithNoConfigDoNotPanic covers an App without one, which is what
// several tests build.
func TestReviewPrefsWithNoConfigDoNotPanic(t *testing.T) {
	a := &App{}
	a.handleGitReviewPrefsChanged(messages.GitReviewPrefsChanged{Scope: "branch", AutoSend: true})
}

// TestWiringRegistersBothReviewHooks is the test the seam needed. The
// preference hook was once registered inside the send callback, where it
// compiled fine and simply never ran until a review was submitted -- so a
// preference silently never saved.
func TestWiringRegistersBothReviewHooks(t *testing.T) {
	a, path := prefsApp(t)
	a.externalMsgs = make(chan tea.Msg, 8)
	a.externalCritical = make(chan tea.Msg, 8)
	a.wireGitReview(t.TempDir())
	t.Cleanup(func() { _ = a.gitReview.Close() })

	// A preference change with nothing submitted, which is the case that broke.
	a.gitReview.FirePreferences(gitreview.Preferences{
		Scope: gitreview.ScopeBranch, AutoSend: true, Split: true,
	})

	select {
	case msg := <-a.externalMsgs:
		prefs, ok := msg.(messages.GitReviewPrefsChanged)
		if !ok {
			t.Fatalf("got %T on the pump, want GitReviewPrefsChanged", msg)
		}
		a.handleGitReviewPrefsChanged(prefs)
	default:
		t.Fatal("no preference message reached the pump, so the hook was never registered")
	}

	if got := readUIField(t, path, "last_review_split"); got != true {
		t.Errorf("saved split = %v, want true", got)
	}
	if got := readUIField(t, path, "last_review_scope"); got != "branch" {
		t.Errorf("saved scope = %v, want branch", got)
	}
}

// TestOpenSeedsTheReviewFromTheRememberedPrefs is the other end of the chain.
// Saving a preference is pointless if the next press of Review Changes does not
// open with it, and that link -- config to OpenRequest -- is the one nothing
// else covers.
func TestOpenSeedsTheReviewFromTheRememberedPrefs(t *testing.T) {
	skipWithoutGit(t)
	root := gitRepoWithAChange(t)

	cfg := &config.Config{}
	cfg.UI.LastReviewScope = "branch"
	cfg.UI.LastReviewAutoSend = true
	cfg.UI.LastReviewSplit = true

	a := &App{
		toast:  common.NewToastModel(),
		config: cfg,
		activeWorkspace: &data.Workspace{
			Name:      "demo",
			Repos:     []data.RepoRef{{Path: root, Name: "demo"}},
			Worktrees: []data.WorktreeRef{{Root: root, Branch: "main"}},
		},
	}
	a.externalMsgs = make(chan tea.Msg, 8)
	a.externalCritical = make(chan tea.Msg, 8)
	a.wireGitReview(t.TempDir())
	t.Cleanup(func() { _ = a.gitReview.Close() })

	// handleOpenGitReview does the git read in a command, so run it here.
	if cmd := a.handleOpenGitReview(messages.OpenGitReview{Assistant: "claude"}); cmd != nil {
		cmd()
	}

	sessions := a.gitReview.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("got %d review sessions, want 1", len(sessions))
	}
	if got := sessions[0].Scope(); got != gitreview.ScopeBranch {
		t.Errorf("opened on scope %q, want the remembered branch", got)
	}
	if !sessions[0].AutoSend() {
		t.Error("opened with auto-send off, having remembered it on")
	}
	if !sessions[0].Split() {
		t.Error("opened unified, having remembered the split view")
	}
}

func skipWithoutGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// gitRepoWithAChange gives handleOpenGitReview something real to diff.
func gitRepoWithAChange(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
