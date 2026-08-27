package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// resultOf runs a command and returns the DialogResult it produced, if any.
func resultOf(cmd tea.Cmd) (DialogResult, bool) {
	if cmd == nil {
		return DialogResult{}, false
	}
	res, ok := cmd().(DialogResult)
	return res, ok
}

// TestSingleSelectRunsValidation is the property that makes the repo picker
// safe to shrink to one selection: without it, single-select hands back any
// directory and the caller only finds out once creation fails.
func TestSingleSelectRunsValidation(t *testing.T) {
	tmp := t.TempDir()
	fp := NewFilePicker("id", tmp, true)
	fp.SetMultiSelect(false)
	fp.SetValidatePath(func(string, []string) string { return "Not a git repository" })
	fp.Show()

	_, cmd := fp.confirmCurrentDirectory()
	if _, ok := resultOf(cmd); ok {
		t.Fatal("a rejected path was returned as a result")
	}
	if !fp.Visible() {
		t.Error("picker closed on a rejected path")
	}
	if fp.statusMessage != "Not a git repository" {
		t.Errorf("statusMessage = %q, want the validator's message", fp.statusMessage)
	}
	// The message has to be on screen — a refusal that says nothing reads as a
	// dropped keystroke.
	if !strings.Contains(strings.Join(fp.renderLines(), "\n"), "Not a git repository") {
		t.Error("the rejection message is not rendered")
	}
}

// TestAutoSelectPicksInsteadOfDescending covers the one-keystroke path: enter on
// a repo row selects it rather than opening it.
func TestAutoSelectPicksInsteadOfDescending(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "myrepo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fp := NewFilePicker("id", tmp, true)
	fp.SetMultiSelect(false)
	fp.SetAutoSelect(func(path string) bool { return filepath.Base(path) == "myrepo" })
	fp.Show()

	// Filter down to the repo row so it is the highlighted entry.
	fp.input.SetValue(fp.inputBasePath() + "myrepo")
	fp.applyFilter()

	_, cmd := fp.handleEnter()
	res, ok := resultOf(cmd)
	if !ok {
		t.Fatal("enter on a repo row did not produce a result")
	}
	if res.Value != repo {
		t.Errorf("Value = %q, want %q", res.Value, repo)
	}
	if fp.currentPath != tmp {
		t.Errorf("enter navigated into the repo instead of selecting it (currentPath = %q)", fp.currentPath)
	}
}

// TestAutoSelectStillDescendsIntoAPlainDirectory keeps the predicate from
// turning every directory into a dead end.
func TestAutoSelectStillDescendsIntoAPlainDirectory(t *testing.T) {
	tmp := t.TempDir()
	plain := filepath.Join(tmp, "src")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fp := NewFilePicker("id", tmp, true)
	fp.SetMultiSelect(false)
	fp.SetAutoSelect(func(string) bool { return false })
	fp.Show()

	fp.input.SetValue(fp.inputBasePath() + "src")
	fp.applyFilter()

	_, cmd := fp.handleEnter()
	if _, ok := resultOf(cmd); ok {
		t.Fatal("enter on a plain directory returned a result instead of navigating")
	}
	if fp.currentPath != plain {
		t.Errorf("currentPath = %q, want %q", fp.currentPath, plain)
	}
}
