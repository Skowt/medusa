package git

import (
	"strings"
	"testing"
)

func TestParseDiff(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantEmpty   bool
		wantBinary  bool
		wantHunks   int
		wantAdded   int
		wantDeleted int
	}{
		{
			name:      "empty diff",
			content:   "",
			wantEmpty: true,
		},
		{
			name:       "binary file",
			content:    "Binary files a/image.png and b/image.png differ",
			wantBinary: true,
		},
		{
			name: "single hunk",
			content: `diff --git a/file.txt b/file.txt
index abc123..def456 100644
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 line 1
+added line
 line 2
 line 3`,
			wantHunks:   1,
			wantAdded:   1,
			wantDeleted: 0,
		},
		{
			name: "multiple hunks",
			content: `diff --git a/file.txt b/file.txt
index abc123..def456 100644
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 line 1
+added line
 line 2
@@ -10,3 +11,2 @@
 line 10
-removed line
 line 12`,
			wantHunks:   2,
			wantAdded:   1,
			wantDeleted: 1,
		},
		{
			name: "with deletions only",
			content: `diff --git a/file.txt b/file.txt
index abc123..def456 100644
--- a/file.txt
+++ b/file.txt
@@ -1,5 +1,3 @@
 line 1
-line 2
-line 3
 line 4
 line 5`,
			wantHunks:   1,
			wantAdded:   0,
			wantDeleted: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDiff("test.txt", tt.content)

			if result.Empty != tt.wantEmpty {
				t.Errorf("Empty = %v, want %v", result.Empty, tt.wantEmpty)
			}
			if result.Binary != tt.wantBinary {
				t.Errorf("Binary = %v, want %v", result.Binary, tt.wantBinary)
			}
			if len(result.Hunks) != tt.wantHunks {
				t.Errorf("Hunks count = %d, want %d", len(result.Hunks), tt.wantHunks)
			}
			if result.AddedLines() != tt.wantAdded {
				t.Errorf("AddedLines = %d, want %d", result.AddedLines(), tt.wantAdded)
			}
			if result.DeletedLines() != tt.wantDeleted {
				t.Errorf("DeletedLines = %d, want %d", result.DeletedLines(), tt.wantDeleted)
			}
		})
	}
}

func TestHunkParsing(t *testing.T) {
	content := `diff --git a/file.txt b/file.txt
index abc123..def456 100644
--- a/file.txt
+++ b/file.txt
@@ -10,5 +10,6 @@ function context
 line 10
+added
 line 11`

	result := parseDiff("test.txt", content)

	if len(result.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(result.Hunks))
	}

	hunk := result.Hunks[0]
	if hunk.OldStart != 10 {
		t.Errorf("OldStart = %d, want 10", hunk.OldStart)
	}
	if hunk.OldCount != 5 {
		t.Errorf("OldCount = %d, want 5", hunk.OldCount)
	}
	if hunk.NewStart != 10 {
		t.Errorf("NewStart = %d, want 10", hunk.NewStart)
	}
	if hunk.NewCount != 6 {
		t.Errorf("NewCount = %d, want 6", hunk.NewCount)
	}
	if !strings.Contains(hunk.Header, "function context") {
		t.Errorf("Header should contain context, got %q", hunk.Header)
	}
}

func TestDiffLineKinds(t *testing.T) {
	content := `diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,3 @@
 context
+added
-deleted`

	result := parseDiff("test.txt", content)

	// Check that we have the right line kinds
	hasContext := false
	hasAdd := false
	hasDelete := false
	hasHeader := false

	for _, line := range result.Lines {
		switch line.Kind {
		case DiffLineContext:
			hasContext = true
		case DiffLineAdd:
			hasAdd = true
		case DiffLineDelete:
			hasDelete = true
		case DiffLineHeader:
			hasHeader = true
		}
	}

	if !hasContext {
		t.Error("expected context lines")
	}
	if !hasAdd {
		t.Error("expected add lines")
	}
	if !hasDelete {
		t.Error("expected delete lines")
	}
	if !hasHeader {
		t.Error("expected header lines")
	}
}

func TestDiffResult_HunkCount(t *testing.T) {
	result := &DiffResult{
		Hunks: []Hunk{{}, {}, {}},
	}
	if result.HunkCount() != 3 {
		t.Errorf("HunkCount() = %d, want 3", result.HunkCount())
	}
}

// TestParseDiffLineNumbers covers the mapping from diff rows to real file
// lines, which is what lets a reviewer anchor a comment to file:line.
//
// The second hunk is the point of the test: its numbering restarts from the
// @@ header, so anything that counts from the top of the diff — or from 1 —
// reports the wrong line for every row after the first hunk.
func TestParseDiffLineNumbers(t *testing.T) {
	content := `diff --git a/file.txt b/file.txt
index abc123..def456 100644
--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 line 1
+added line
 line 2
 line 3
@@ -20,4 +21,4 @@
 line 20
-old line 21
+new line 21
 line 22
`
	result := parseDiff("file.txt", content)

	// Each want is keyed by the row's content, which is unique here.
	wants := map[string]struct{ old, new int }{
		" line 1":      {1, 1},
		"+added line":  {0, 2},
		" line 2":      {2, 3},
		" line 3":      {3, 4},
		" line 20":     {20, 21},
		"-old line 21": {21, 0},
		"+new line 21": {0, 22},
		" line 22":     {22, 23},
	}
	seen := map[string]bool{}
	for _, line := range result.Lines {
		want, ok := wants[line.Content]
		if !ok {
			continue
		}
		seen[line.Content] = true
		if line.OldLine != want.old || line.NewLine != want.new {
			t.Errorf("%q: old/new = %d/%d, want %d/%d",
				line.Content, line.OldLine, line.NewLine, want.old, want.new)
		}
	}
	for content := range wants {
		if !seen[content] {
			t.Errorf("row %q never appeared in the parsed diff", content)
		}
	}

	// Headers and the preamble name no line at all.
	for _, line := range result.Lines {
		if line.Kind == DiffLineHeader && (line.OldLine != 0 || line.NewLine != 0) {
			t.Errorf("header %q was numbered %d/%d, want 0/0",
				line.Content, line.OldLine, line.NewLine)
		}
	}

	// The trailing newline leaves an empty row that is not a line of the file;
	// numbering it would put a phantom line past the end.
	last := result.Lines[len(result.Lines)-1]
	if last.Content != "" {
		t.Fatalf("expected a trailing empty row, got %q", last.Content)
	}
	if last.OldLine != 0 || last.NewLine != 0 {
		t.Errorf("trailing empty row numbered %d/%d, want 0/0", last.OldLine, last.NewLine)
	}
}

// TestParseDiffNoNewlineMarker keeps git's "\ No newline at end of file" from
// consuming a line number, which would shift every row after it.
func TestParseDiffNoNewlineMarker(t *testing.T) {
	content := `@@ -1,3 +1,3 @@
 kept
-old tail
\ No newline at end of file
+new tail
 after`
	result := parseDiff("f.txt", content)

	for _, line := range result.Lines {
		switch line.Content {
		case `\ No newline at end of file`:
			if line.OldLine != 0 || line.NewLine != 0 {
				t.Errorf("no-newline marker numbered %d/%d, want 0/0", line.OldLine, line.NewLine)
			}
		case " after":
			// kept=1, old tail/new tail=2, so this is line 3 on both sides.
			// The marker sitting between them must not have taken a number.
			if line.OldLine != 3 || line.NewLine != 3 {
				t.Errorf("line after the marker = %d/%d, want 3/3", line.OldLine, line.NewLine)
			}
		}
	}
}
