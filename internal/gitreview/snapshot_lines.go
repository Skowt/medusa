package gitreview

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Skowt/medusa/internal/git"
)

// maxExpandLines caps one expansion request.
//
// The page asks for a screenful at a time, so this is generous; it is here so a
// malformed client cannot ask the TUI's process to hold a whole large file in a
// JSON response.
const maxExpandLines = 400

// FileLines is a run of a file's current content, used to fill in the unchanged
// gaps a diff leaves between its hunks.
type FileLines struct {
	Path  string   `json:"path"`
	From  int      `json:"from"`
	Lines []string `json:"lines"`
	// Total is the file's line count, which is what tells the page there is
	// nothing left below the last hunk. It cannot come from the snapshot: that
	// would mean reading every changed file on every two-second poll.
	Total int `json:"total"`
}

// readFileLines returns lines [from, to] of a file, 1-based and inclusive.
//
// It reads the working tree, which is the diff's post-image in both scopes:
// neither scope passes --cached, so the "+" side of every hunk is the file as it
// is on disk right now. That also means an expansion is always consistent with
// what the reader is looking at, since a stale read would renumber the gap.
//
// A file with no post-image -- deleted, or not a regular file -- is an error
// rather than an empty result. The page offers no expander in that case, so
// reaching here means something moved underneath it, and saying so is better
// than rendering a gap that silently stays blank.
func readFileLines(root, path string, from, to int) (FileLines, error) {
	out := FileLines{Path: path, From: from}
	if !staysInWorkspace(path) {
		return out, fmt.Errorf("path escapes the workspace")
	}
	if from < 1 || to < from {
		return out, fmt.Errorf("bad line range %d-%d", from, to)
	}
	if to-from+1 > maxExpandLines {
		to = from + maxExpandLines - 1
	}

	full := filepath.Join(root, path)
	info, err := os.Lstat(full)
	if err != nil {
		return out, err
	}
	if !info.Mode().IsRegular() {
		return out, fmt.Errorf("not a regular file")
	}
	if info.Size() > git.LargeFileSizeThreshold {
		return out, fmt.Errorf("file is too large to expand")
	}

	content, err := os.ReadFile(full)
	if err != nil {
		return out, err
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return out, fmt.Errorf("binary file")
	}

	lines := strings.Split(string(content), "\n")
	// A trailing newline leaves an empty final element that is not a line of the
	// file. Counting it would offer the reader a blank line past the end.
	if strings.HasSuffix(string(content), "\n") {
		lines = lines[:len(lines)-1]
	}
	out.Total = len(lines)

	// A range past the end is clamped rather than refused: the page learns the
	// real total from the same response, so the next click asks for less.
	if from > len(lines) {
		out.Lines = []string{}
		return out, nil
	}
	if to > len(lines) {
		to = len(lines)
	}
	out.Lines = lines[from-1 : to]
	return out, nil
}
