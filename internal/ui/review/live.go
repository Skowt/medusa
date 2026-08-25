package review

import (
	"strings"

	"github.com/Skowt/medusa/internal/git"
)

// displayDiff is the diff every view of a file must read.
//
// One accessor rather than entry.Diff at each call site: the pane, its row list
// and its header have to agree on what they are showing, and a view still
// reading the on-disk diff while the cursor navigates a rebuilt one puts the
// selection on a different line than the one under it.
func (m *Model) displayDiff(entry *fileEntry) *git.DiffResult {
	if live := m.liveDiff(entry); live != nil {
		return live
	}
	return entry.Diff
}

// liveDiff rebuilds a file's diff from its unsaved edit buffer, or returns nil
// when there is nothing to rebuild.
//
// The git diff a file loads with describes what is on disk, so a buffer the
// user has typed into and not saved is invisible in it — leaving the editor
// showed the diff exactly as it was before they started, which reads as the
// edits having been thrown away. Rebuilding from the buffer is what makes the
// two views agree.
//
// It is diffed against the committed content, which the buffer already holds
// (see editBuffer.base), so this costs no git call per frame.
func (m *Model) liveDiff(entry *fileEntry) *git.DiffResult {
	buf := m.edits[entry.Path()]
	if buf == nil || !buf.Dirty() {
		return nil
	}
	// Cached on the content it was built from. Rebuilding it per call meant
	// synthesizing the whole file's diff several times a frame, since every
	// view of it goes through displayDiff.
	value := buf.area.Value()
	if m.liveCache.path == entry.Path() && m.liveCache.value == value {
		return m.liveCache.diff
	}
	diff := synthDiff(entry.Path(), strings.Split(value, "\n"), buf.BaseMarks())
	m.liveCache = liveDiffCache{path: entry.Path(), value: value, diff: diff}
	return diff
}

// liveDiffCache holds the rebuilt diff for one buffer value.
type liveDiffCache struct {
	path  string
	value string
	diff  *git.DiffResult
}

// synthDiff turns per-line marks into the DiffResult shape the pane renders,
// so a rebuilt diff and a git one draw through exactly the same code.
//
// Everything is emitted as a single hunk covering the file. Real hunks exist to
// keep a diff small on the wire; here the whole file is already in memory, and
// splitting it would mean hiding context the reader is mid-edit in.
func synthDiff(path string, now []string, marks []lineMark) *git.DiffResult {
	result := &git.DiffResult{Path: path}

	oldLine, newLine := 1, 1
	for i, line := range now {
		var mark lineMark
		if i < len(marks) {
			mark = marks[i]
		}
		for _, gone := range mark.RemovedBefore {
			result.Lines = append(result.Lines, git.DiffLine{
				Kind: git.DiffLineDelete, Content: "-" + gone, OldLine: oldLine,
			})
			oldLine++
		}
		switch mark.Kind {
		case lineModified:
			// A rewrite is one change in the editor's gutter but two lines in a
			// diff: dropping the "-old" half here would make the rewritten line
			// look like a pure insertion and lose what it replaced.
			result.Lines = append(result.Lines, git.DiffLine{
				Kind: git.DiffLineDelete, Content: "-" + mark.Replaced, OldLine: oldLine,
			})
			oldLine++
			result.Lines = append(result.Lines, git.DiffLine{
				Kind: git.DiffLineAdd, Content: "+" + line, NewLine: newLine,
			})
		case lineAdded:
			result.Lines = append(result.Lines, git.DiffLine{
				Kind: git.DiffLineAdd, Content: "+" + line, NewLine: newLine,
			})
		case lineSame:
			result.Lines = append(result.Lines, git.DiffLine{
				Kind: git.DiffLineContext, Content: " " + line,
				OldLine: oldLine, NewLine: newLine,
			})
			oldLine++
		}
		newLine++
	}

	result.Hunks = []git.Hunk{{
		OldStart: 1, OldCount: oldLine - 1,
		NewStart: 1, NewCount: len(now),
		StartLine: 0,
		Header:    "@@ working tree, including your unsaved edits @@",
	}}
	result.Lines = append([]git.DiffLine{{
		Kind: git.DiffLineHeader, Content: result.Hunks[0].Header,
	}}, result.Lines...)
	result.Empty = len(result.Lines) == 1
	return result
}

// liveCounts returns a file's added and deleted line counts, from the unsaved
// buffer when there is one so the file list moves as the user types.
func (m *Model) liveCounts(entry *fileEntry) (added, deleted int) {
	diff := m.displayDiff(entry)
	if diff == nil {
		return 0, 0
	}
	if diff == entry.Diff {
		return entry.Added, entry.Deleted
	}
	return diff.AddedLines(), diff.DeletedLines()
}
