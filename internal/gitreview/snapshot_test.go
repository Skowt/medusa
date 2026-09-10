package gitreview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_DIR=") &&
			!strings.HasPrefix(e, "GIT_WORK_TREE=") &&
			!strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			env = append(env, e)
		}
	}
	cmd.Env = append(env,
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// branchRepo builds a repo whose branch has both a commit and uncommitted work,
// which is the shape a review has to cope with.
func branchRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	write(t, root, "keep.txt", "one\ntwo\nthree\n")
	write(t, root, "committed.txt", "alpha\nbeta\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	runGit(t, root, "checkout", "-b", "feature")
	write(t, root, "committed.txt", "alpha\nBETA\n")
	runGit(t, root, "commit", "-am", "change committed.txt")

	write(t, root, "keep.txt", "one\nTWO\nthree\n")
	write(t, root, "brand-new.txt", "fresh\n")
	return root
}

// TestBuildBranchScopeSeesCommittedAndUncommitted is the property the branch
// scope exists for: an agent that commits as it works must not have those
// commits disappear from the review.
func TestBuildBranchScopeSeesCommittedAndUncommitted(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	snap := Build(root, "ws", ScopeBranch)
	if snap.Error != "" {
		t.Fatalf("build: %s", snap.Error)
	}

	byPath := map[string]File{}
	for _, f := range snap.Files {
		byPath[f.Path] = f
	}
	for _, want := range []string{"committed.txt", "keep.txt", "brand-new.txt"} {
		if _, ok := byPath[want]; !ok {
			t.Errorf("branch scope missed %s; got %v", want, keys(byPath))
		}
	}
	if got := byPath["brand-new.txt"]; !got.Untracked {
		t.Errorf("brand-new.txt should be flagged untracked")
	}
	if snap.Branch != "feature" {
		t.Errorf("branch = %q, want feature", snap.Branch)
	}
}

// TestParseScopeDefaultsToUncommitted pins the default a page gets when it asks
// for no scope at all.
//
// Uncommitted is what the user opened the review to look at: the work the agent
// has just done. The branch view also carries commits made earlier in the task,
// which buries those fresh changes.
func TestParseScopeDefaultsToUncommitted(t *testing.T) {
	for _, raw := range []string{"", "nonsense", "working"} {
		if got := ParseScope(raw); got != ScopeWorking {
			t.Errorf("ParseScope(%q) = %q, want %q", raw, got, ScopeWorking)
		}
	}
	if got := ParseScope("branch"); got != ScopeBranch {
		t.Errorf("ParseScope(\"branch\") = %q, want %q", got, ScopeBranch)
	}
}

// TestBuildWorkingScopeExcludesCommits keeps the two scopes actually distinct.
func TestBuildWorkingScopeExcludesCommits(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	snap := Build(root, "ws", ScopeWorking)
	for _, f := range snap.Files {
		if f.Path == "committed.txt" {
			t.Errorf("working scope should not include the committed change")
		}
	}
	if len(snap.Files) == 0 {
		t.Fatal("working scope found no uncommitted changes")
	}
}

// TestBuildAnchorsLinesToTheFile guards the numbering a comment hangs off. A
// comment reported at the wrong line sends the agent to the wrong code.
func TestBuildAnchorsLinesToTheFile(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	snap := Build(root, "ws", ScopeBranch)
	var target File
	for _, f := range snap.Files {
		if f.Path == "keep.txt" {
			target = f
		}
	}
	if len(target.Hunks) == 0 {
		t.Fatalf("keep.txt has no hunks: %+v", target)
	}

	var added Line
	for _, hunk := range target.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind == LineAdd {
				added = line
			}
		}
	}
	if added.Content != "+TWO" {
		t.Fatalf("added line = %q, want +TWO", added.Content)
	}
	if added.New != 2 {
		t.Errorf("added line anchored at new line %d, want 2", added.New)
	}
}

// TestDigestTracksContentNotJustFileNames is why live refresh works: an agent
// rewriting a line in place leaves the file list and the line counts identical.
func TestDigestTracksContentNotJustFileNames(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	before := Build(root, "ws", ScopeBranch).Digest
	write(t, root, "keep.txt", "one\nSOMETHING ELSE\nthree\n")
	after := Build(root, "ws", ScopeBranch).Digest

	if before == after {
		t.Error("digest did not change when a changed line was rewritten in place")
	}
}

// TestBuildHunksDropTheDiffPreamble keeps `diff --git`/`index`/`---`/`+++` out
// of the reviewable rows; they name the file the navigator already shows.
func TestBuildHunksDropTheDiffPreamble(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	snap := Build(root, "ws", ScopeBranch)
	for _, f := range snap.Files {
		for _, hunk := range f.Hunks {
			for _, line := range hunk.Lines {
				for _, bad := range []string{"diff --git", "index ", "--- ", "+++ "} {
					if strings.HasPrefix(line.Content, bad) {
						t.Errorf("%s: preamble line %q leaked into a hunk", f.Path, line.Content)
					}
				}
			}
		}
	}
}

func TestStaysInWorkspaceRejectsEscapes(t *testing.T) {
	for _, bad := range []string{"", "../secret", "../../etc/passwd", "/etc/passwd", ".."} {
		if staysInWorkspace(bad) {
			t.Errorf("staysInWorkspace accepted %q", bad)
		}
	}
	for _, ok := range []string{"a.txt", "dir/a.txt", "./dir/a.txt"} {
		if !staysInWorkspace(ok) {
			t.Errorf("staysInWorkspace rejected %q", ok)
		}
	}
}

func keys(m map[string]File) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestBuildListsFilesInsideANewDirectory guards the bug that made a whole new
// package unreviewable.
//
// Medusa reads git status with --untracked-files=normal, which stops at
// untracked-directory boundaries: a new package arrives as the single entry
// "pkg/" rather than the files in it. Reusing that here cost a review twice —
// the directory row has no diff to show, and because its content never changes,
// editing a file inside it did not move the digest, so an open page never
// repainted.
func TestBuildListsFilesInsideANewDirectory(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	write(t, root, "pkg/newthing/one.go", "package newthing\n\nfunc One() {}\n")
	write(t, root, "pkg/newthing/two.go", "package newthing\n\nfunc Two() {}\n")

	snap := Build(root, "ws", ScopeBranch)
	paths := map[string]File{}
	for _, f := range snap.Files {
		paths[f.Path] = f
	}

	for _, want := range []string{"pkg/newthing/one.go", "pkg/newthing/two.go"} {
		f, ok := paths[want]
		if !ok {
			t.Errorf("%s is missing; got %v", want, keys(paths))
			continue
		}
		if len(f.Hunks) == 0 {
			t.Errorf("%s has no reviewable rows", want)
		}
	}
	for path := range paths {
		if strings.HasSuffix(path, "/") {
			t.Errorf("a directory (%q) was listed as a reviewable file", path)
		}
	}
}

// TestDigestMovesWhenAFileInANewDirectoryChanges is the live-refresh half of the
// same bug: the page only repaints when the digest moves.
func TestDigestMovesWhenAFileInANewDirectoryChanges(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)
	write(t, root, "pkg/newthing/one.go", "package newthing\n")

	before := Build(root, "ws", ScopeBranch).Digest
	write(t, root, "pkg/newthing/one.go", "package newthing\n\nfunc AddedLater() {}\n")
	after := Build(root, "ws", ScopeBranch).Digest

	if before == after {
		t.Error("editing a file inside a new directory did not move the digest, so an open page would never repaint")
	}
}

// TestBuildRespectsGitignoreForNewFiles keeps enumeration from walking build
// output and dependency trees, which is what makes listing new files affordable
// on every poll.
func TestBuildRespectsGitignoreForNewFiles(t *testing.T) {
	skipIfNoGit(t)
	root := branchRepo(t)

	write(t, root, ".gitignore", "ignored/\n")
	write(t, root, "ignored/huge.bin", "noise\n")
	write(t, root, "ignored/nested/also.bin", "noise\n")

	snap := Build(root, "ws", ScopeBranch)
	for _, f := range snap.Files {
		if strings.HasPrefix(f.Path, "ignored/") {
			t.Errorf("ignored path %q was enumerated", f.Path)
		}
	}
}
