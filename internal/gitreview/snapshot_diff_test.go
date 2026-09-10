package gitreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Skowt/medusa/internal/git"
)

// variedRepo exercises every status the bulk diff has to attribute: a plain
// edit, a new tracked file, a delete, a rename, and an untracked file.
func variedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	write(t, root, "edited.txt", "one\ntwo\nthree\n")
	write(t, root, "deleted.txt", "gone soon\n")
	write(t, root, "renamed-from.txt", "stable content\nline two\nline three\n")
	write(t, root, "dir/nested.txt", "nested\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	runGit(t, root, "checkout", "-b", "feature")
	write(t, root, "edited.txt", "one\nTWO CHANGED\nthree\n")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "mv", "renamed-from.txt", "renamed-to.txt")
	write(t, root, "added-tracked.txt", "brand new tracked\n")
	runGit(t, root, "add", "added-tracked.txt")
	write(t, root, "dir/nested.txt", "nested\nplus one\n")
	write(t, root, "untracked-new.txt", "never added\nsecond line\n")
	return root
}

// TestBulkDiffMatchesPerFileDiff is the safety net for the fast path. One diff
// split by order has to produce exactly what one call per file produced, or a
// review shows one file's changes under another file's name.
func TestBulkDiffMatchesPerFileDiff(t *testing.T) {
	skipIfNoGit(t)
	root := variedRepo(t)
	rev := resolveBase(root, ScopeBranch).rev

	changes, err := changedFiles(root, rev)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.diffs) == 0 {
		t.Fatal("the bulk diff was not attributed at all, so the fast path is dead")
	}

	for _, entry := range changes.entries {
		if entry.untracked {
			continue
		}
		fast := buildFile(root, rev, entry, changes.diffs)
		slow := buildFile(root, rev, entry, nil) // forces the per-file git call

		if fast.Added != slow.Added || fast.Removed != slow.Removed {
			t.Errorf("%s: bulk +%d-%d, per-file +%d-%d",
				entry.path, fast.Added, fast.Removed, slow.Added, slow.Removed)
		}
		if len(fast.Hunks) != len(slow.Hunks) {
			t.Errorf("%s: bulk has %d hunks, per-file has %d", entry.path, len(fast.Hunks), len(slow.Hunks))
			continue
		}
		for i := range fast.Hunks {
			if fast.Hunks[i].Header != slow.Hunks[i].Header {
				t.Errorf("%s hunk %d: header %q vs %q", entry.path, i, fast.Hunks[i].Header, slow.Hunks[i].Header)
			}
			if len(fast.Hunks[i].Lines) != len(slow.Hunks[i].Lines) {
				t.Errorf("%s hunk %d: %d lines vs %d", entry.path, i, len(fast.Hunks[i].Lines), len(slow.Hunks[i].Lines))
				continue
			}
			for j := range fast.Hunks[i].Lines {
				a, b := fast.Hunks[i].Lines[j], slow.Hunks[i].Lines[j]
				if a != b {
					t.Errorf("%s hunk %d line %d: %+v vs %+v", entry.path, i, j, a, b)
				}
			}
		}
	}
}

// TestBulkDiffAbandonsOnMismatch keeps the fast path honest: if attribution
// cannot be verified, everything must fall back rather than guess.
func TestBulkDiffAbandonsOnMismatch(t *testing.T) {
	skipIfNoGit(t)
	root := variedRepo(t)
	rev := resolveBase(root, ScopeBranch).rev

	// A entry list that does not match what git will emit.
	bogus := []fileEntry{{path: "not-a-real-file.txt", status: "modified"}}
	if got := bulkDiff(root, rev, bogus); got != nil {
		t.Errorf("attribution succeeded against a bogus entry list: %+v", got)
	}
}

func TestSplitDiffSectionsCountsFiles(t *testing.T) {
	out := strings.Join([]string{
		"diff --git a/one.txt b/one.txt",
		"index 111..222 100644",
		"--- a/one.txt",
		"+++ b/one.txt",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"diff --git a/two.txt b/two.txt",
		"index 333..444 100644",
		"--- a/two.txt",
		"+++ b/two.txt",
		"@@ -1 +1 @@",
		"-a",
		"+b",
	}, "\n")

	sections := splitDiffSections(out)
	if len(sections) != 2 {
		t.Fatalf("split into %d sections, want 2", len(sections))
	}
	if !strings.Contains(sections[0], "one.txt") || strings.Contains(sections[0], "two.txt") {
		t.Errorf("first section is not just one.txt:\n%s", sections[0])
	}
}

// TestSplitDiffSectionsIgnoresAHeaderInsideContent covers a diff of a file that
// itself contains diff text — a patch file, or this project's own tests. Content
// lines always carry a +/-/space prefix, which is what keeps them distinct.
func TestSplitDiffSectionsIgnoresAHeaderInsideContent(t *testing.T) {
	out := strings.Join([]string{
		"diff --git a/patch.txt b/patch.txt",
		"@@ -1,2 +1,3 @@",
		" diff --git a/inner.txt b/inner.txt",
		"+diff --git a/added.txt b/added.txt",
		"-diff --git a/removed.txt b/removed.txt",
	}, "\n")

	if sections := splitDiffSections(out); len(sections) != 1 {
		t.Errorf("split into %d sections, want 1 — a content line was read as a file header", len(sections))
	}
}

func TestHeaderNamesPathRejectsTheWrongFile(t *testing.T) {
	section := "diff --git a/real.txt b/real.txt\n@@ -1 +1 @@\n"
	if !headerNamesPath(section, fileEntry{path: "real.txt"}) {
		t.Error("the matching path was rejected")
	}
	if headerNamesPath(section, fileEntry{path: "other.txt"}) {
		t.Error("a different path was accepted")
	}
	// A path git would quote is not decoded here; it must fall back instead.
	quoted := "diff --git \"a/od\\303\\251.txt\" \"b/od\\303\\251.txt\"\n"
	if headerNamesPath(quoted, fileEntry{path: "odé.txt"}) {
		t.Error("a quoted header was accepted instead of falling back")
	}
}

// TestUntrackedDiffMatchesGit keeps the hand-built patch for a new file
// equivalent to what git would have produced, since that is what it replaced.
func TestUntrackedDiffMatchesGit(t *testing.T) {
	skipIfNoGit(t)
	root := variedRepo(t)

	for _, name := range []string{"untracked-new.txt", "no-trailing-newline.txt"} {
		if name == "no-trailing-newline.txt" {
			write(t, root, name, "first\nsecond without newline")
		}
		mine := untrackedDiff(root, name)
		theirs, err := git.GetUntrackedFileContent(root, name)
		if err != nil {
			t.Fatal(err)
		}

		if mine.AddedLines() != theirs.AddedLines() {
			t.Errorf("%s: added %d, git says %d", name, mine.AddedLines(), theirs.AddedLines())
		}
		mineHunks, theirsHunks := hunksFrom(mine.Lines), hunksFrom(theirs.Lines)
		if len(mineHunks) != len(theirsHunks) {
			t.Fatalf("%s: %d hunks, git says %d", name, len(mineHunks), len(theirsHunks))
		}
		for i := range mineHunks {
			if len(mineHunks[i].Lines) != len(theirsHunks[i].Lines) {
				t.Errorf("%s hunk %d: %d lines, git says %d",
					name, i, len(mineHunks[i].Lines), len(theirsHunks[i].Lines))
				continue
			}
			for j := range mineHunks[i].Lines {
				if mineHunks[i].Lines[j] != theirsHunks[i].Lines[j] {
					t.Errorf("%s hunk %d line %d: %+v, git says %+v",
						name, i, j, mineHunks[i].Lines[j], theirsHunks[i].Lines[j])
				}
			}
		}
	}
}

// TestUntrackedDiffHandlesAwkwardFiles: reading files directly means this code
// owns the cases git used to absorb.
func TestUntrackedDiffHandlesAwkwardFiles(t *testing.T) {
	root := t.TempDir()

	write(t, root, "binary.bin", "before\x00after")
	if got := untrackedDiff(root, "binary.bin"); !got.Binary {
		t.Error("a file with a NUL byte was not treated as binary")
	}

	write(t, root, "empty.txt", "")
	if got := untrackedDiff(root, "empty.txt"); !got.Empty || len(got.Lines) != 0 {
		t.Errorf("empty file produced %+v", got)
	}

	if got := untrackedDiff(root, "missing.txt"); got.Error == "" {
		t.Error("a vanished file should report an error, not an empty diff")
	}

	if err := os.Symlink("/etc/passwd", filepath.Join(root, "link")); err == nil {
		got := untrackedDiff(root, "link")
		if got.Error == "" {
			t.Error("a symlink should be refused rather than followed")
		}
		if len(got.Lines) != 0 {
			t.Error("a symlink's target content leaked into the review")
		}
	}
}
