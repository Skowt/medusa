package gitreview

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Skowt/medusa/internal/git"
)

// changeSet is everything one snapshot needs from git, gathered in as few
// invocations as possible.
//
// The naive shape -- ask git for the file list, then run `git diff -- <path>`
// per file -- costs one subprocess per changed file on every refresh. Measured
// on a 33-file change that was ~800ms, and the review page refreshes while it is
// open, so it was the dominant cost of the whole feature. Two calls do the same
// work: one name-status for the authoritative list, one bulk diff for every
// file's patch.
type changeSet struct {
	entries []fileEntry
	// diffs holds per-path patch text split out of the bulk diff. A path that
	// is missing simply falls back to its own git call, so attribution never
	// has to be trusted blindly.
	diffs  map[string]string
	notice string
}

// bulkDiff runs one diff for the whole change and splits it per file.
//
// Attribution is by order, not by parsing paths out of the headers: git emits
// --name-status and a plain diff in the same order, whereas paths in headers may
// be C-quoted when they contain unusual bytes, and misreading one would attach
// the wrong patch to the wrong file. Order is then *verified* against each
// header, and any mismatch abandons the whole fast path rather than risking a
// review that shows one file's changes under another's name.
func bulkDiff(root, rev string, entries []fileEntry) map[string]string {
	tracked := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		if !e.untracked {
			tracked = append(tracked, e)
		}
	}
	if len(tracked) == 0 {
		return map[string]string{}
	}

	out, err := git.RunGit(root, "--no-optional-locks", "diff",
		"--no-color", "--no-ext-diff", "-U3", "--find-renames", rev)
	if err != nil || out == "" {
		return nil
	}

	sections := splitDiffSections(out)
	if len(sections) != len(tracked) {
		return nil
	}

	diffs := make(map[string]string, len(tracked))
	for i, entry := range tracked {
		if !headerNamesPath(sections[i], entry) {
			return nil
		}
		diffs[entry.path] = sections[i]
	}
	return diffs
}

// splitDiffSections cuts a multi-file diff at its `diff --git` boundaries.
//
// A bare "diff --git " at the start of a line is unambiguous: every line of
// patch content carries a ' ', '+', '-' or '\' prefix, so a content line that
// happens to be a diff header cannot be mistaken for one.
func splitDiffSections(out string) []string {
	const marker = "diff --git "
	var sections []string
	var current strings.Builder
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, marker) {
			if current.Len() > 0 {
				sections = append(sections, current.String())
				current.Reset()
			}
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	if current.Len() > 0 {
		sections = append(sections, current.String())
	}
	return sections
}

// headerNamesPath verifies a section belongs to the entry it was matched with.
//
// It only accepts the plain, unquoted form. A path git chose to quote is not
// decoded here: rather than reimplementing git's quoting, that file falls back
// to its own diff call, which is correct at the cost of one subprocess for a
// case that is rare.
func headerNamesPath(section string, entry fileEntry) bool {
	header := section
	if idx := strings.IndexByte(section, '\n'); idx >= 0 {
		header = section[:idx]
	}
	return strings.HasSuffix(header, " b/"+entry.path)
}

// untrackedDiff renders a brand-new file as an all-added patch, without asking
// git for it.
//
// `git diff --no-index` would do the same thing at the cost of a subprocess per
// new file, and an agent's work is mostly new files -- so on the poll path that
// was most of the cost. Reading the file is also the only way to be honest about
// what is there: git reports "Binary files differ" and nothing else, whereas
// this can say how large the file was.
func untrackedDiff(root, path string) *git.DiffResult {
	full := filepath.Join(root, path)
	info, err := os.Lstat(full)
	if err != nil {
		return &git.DiffResult{Path: path, Error: err.Error()}
	}
	// Only regular files have reviewable content. A symlink's target is not
	// this workspace's business, and a socket or device would block on read.
	if !info.Mode().IsRegular() {
		return &git.DiffResult{Path: path, Error: "not a regular file"}
	}
	if info.Size() > git.LargeFileSizeThreshold {
		return &git.DiffResult{Path: path, Large: true}
	}

	content, err := os.ReadFile(full)
	if err != nil {
		return &git.DiffResult{Path: path, Error: err.Error()}
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return &git.DiffResult{Path: path, Binary: true}
	}
	if len(content) == 0 {
		return &git.DiffResult{Path: path, Empty: true}
	}

	lines := strings.Split(string(content), "\n")
	// A trailing newline leaves an empty final element that is not a line of
	// the file; numbering it would invent a line past the end.
	trailingNewline := strings.HasSuffix(string(content), "\n")
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}

	var b strings.Builder
	b.WriteString("@@ -0,0 +1," + strconv.Itoa(len(lines)) + " @@\n")
	for _, line := range lines {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}
	if !trailingNewline {
		b.WriteString("\\ No newline at end of file\n")
	}
	return git.ParseDiff(path, b.String())
}
