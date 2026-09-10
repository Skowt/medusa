package gitreview

import (
	"path/filepath"
	"strings"
	"testing"
)

func linesRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%3))
		b.WriteString("\n")
	}
	write(t, root, "big.txt", b.String())
	return root
}

// TestReadFileLinesIsOneBasedAndInclusive pins the numbering the page counts on.
// Off by one here puts every expanded line against the wrong number, which is
// the number a comment would then be anchored to.
func TestReadFileLinesIsOneBasedAndInclusive(t *testing.T) {
	root := linesRepo(t)
	got, err := readFileLines(root, "big.txt", 5, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) != 3 {
		t.Fatalf("asked for 5-7, got %d lines: %q", len(got.Lines), got.Lines)
	}
	if got.Total != 40 {
		t.Errorf("total = %d, want 40", got.Total)
	}
	if got.From != 5 {
		t.Errorf("from = %d, want 5", got.From)
	}
}

// TestReadFileLinesMatchesTheDiffsOwnNumbering is the property that keeps an
// expanded gap lining up with the hunks above and below it: line N here must be
// the same line N the diff numbered.
func TestReadFileLinesMatchesTheDiffsOwnNumbering(t *testing.T) {
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
		t.Fatal("no hunks for keep.txt")
	}

	for _, line := range target.Hunks[0].Lines {
		if line.New == 0 || line.Kind == LineDel {
			continue
		}
		got, err := readFileLines(root, "keep.txt", line.New, line.New)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Lines) != 1 {
			t.Fatalf("line %d not readable", line.New)
		}
		// The diff line still carries its +/- prefix; the file does not.
		if want := line.Content[1:]; got.Lines[0] != want {
			t.Errorf("line %d: file has %q, diff has %q", line.New, got.Lines[0], want)
		}
	}
}

// TestReadFileLinesClampsPastTheEnd: the page learns the real total from the
// same response, so a request off the end is answered rather than refused.
func TestReadFileLinesClampsPastTheEnd(t *testing.T) {
	root := linesRepo(t)

	got, err := readFileLines(root, "big.txt", 38, 90)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) != 3 {
		t.Errorf("38-90 of a 40-line file gave %d lines", len(got.Lines))
	}

	past, err := readFileLines(root, "big.txt", 41, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(past.Lines) != 0 || past.Total != 40 {
		t.Errorf("a range wholly past the end gave %+v", past)
	}
}

// TestReadFileLinesRefusesWhatItCannotReview covers the cases where reading
// would be wrong rather than merely empty.
func TestReadFileLinesRefusesWhatItCannotReview(t *testing.T) {
	root := linesRepo(t)
	write(t, root, "binary.bin", "before\x00after\n")

	cases := []struct {
		name, path string
		from, to   int
	}{
		{"escapes the workspace", "../outside.txt", 1, 5},
		{"absolute path", filepath.Join(root, "big.txt"), 1, 5},
		{"reversed range", "big.txt", 9, 2},
		{"zero start", "big.txt", 0, 5},
		{"binary", "binary.bin", 1, 2},
		{"missing", "nope.txt", 1, 2},
	}
	for _, tc := range cases {
		if _, err := readFileLines(root, tc.path, tc.from, tc.to); err == nil {
			t.Errorf("%s: expansion was allowed", tc.name)
		}
	}
}

// TestReadFileLinesCapsOneRequest keeps a malformed client from asking for a
// whole large file in one response.
func TestReadFileLinesCapsOneRequest(t *testing.T) {
	root := t.TempDir()
	write(t, root, "huge.txt", strings.Repeat("x\n", maxExpandLines*3))

	got, err := readFileLines(root, "huge.txt", 1, maxExpandLines*3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) != maxExpandLines {
		t.Errorf("got %d lines, want the cap of %d", len(got.Lines), maxExpandLines)
	}
}
