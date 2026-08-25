package review

import (
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

// changeKind is what happened to one line of an edit buffer.
type changeKind int

const (
	lineSame changeKind = iota
	lineAdded
	lineModified
)

// lineMark describes one line of the *current* buffer against what was opened.
type lineMark struct {
	Kind changeKind
	// RemovedBefore holds the lines deleted immediately above this one, in
	// order. A deletion has no line of its own left in the buffer, so it is
	// carried by the line that closed over it and drawn as its own numberless
	// row — the text is kept rather than a count because "1 line removed" tells
	// the reader that something went without telling them what, which is the
	// one thing they cannot look up.
	RemovedBefore []string
	// Replaced is the line this one was rewritten from, empty unless Kind is
	// lineModified. The pairing that makes a rewrite one change in the editor's
	// gutter would otherwise throw the old text away — and a *diff* of the same
	// edit has to show it, because "-old +new" is what a rewrite looks like
	// there. Same data, two presentations.
	Replaced string
}

// diffLines compares the buffer against what was read, per line.
//
// The alignment is Myers, via go-udiff, rather than hand-rolled. The first
// version here was a quadratic LCS with an area budget, and past that budget it
// gave up and marked the whole changed window as modified — so a file with two
// edits ninety lines apart drew every line between them as changed, because
// 91×91 exceeded the cap. A real diff has no such cliff: linear in space,
// near-linear in practice, no budget to tune and no window to be wrong about.
func diffLines(was, now []string) []lineMark {
	marks := make([]lineMark, len(now))
	if len(now) == 0 {
		return marks
	}

	// go-udiff works on whole documents. Re-joining costs nothing next to the
	// diff itself and keeps the line-slice signature every caller already holds.
	before := strings.Join(was, "\n")
	edits := udiff.Strings(before, strings.Join(now, "\n"))
	if len(edits) == 0 {
		return marks
	}
	// Context is set past the length of both sides so the result covers every
	// line rather than the neighbourhood of each change: the gutter has to say
	// something about lines that did *not* change too.
	ops, err := udiff.ToUnifiedDiff("a", "b", before, edits, len(was)+len(now))
	if err != nil {
		// A diff that cannot be produced must not claim the file is unchanged.
		for i := range marks {
			marks[i] = lineMark{Kind: lineModified}
		}
		return marks
	}
	return applyOps(ops, marks)
}

// applyOps walks the diff's line operations onto per-line marks.
//
// It works a change at a time — a run of deletions followed by a run of
// insertions — rather than op by op, because the two runs have to be compared
// against each other before either can be labelled. See cancelIdentical.
func applyOps(ops udiff.UnifiedDiff, marks []lineMark) []lineMark {
	flat := flatten(ops)
	at := 0

	// carry holds deletions waiting for a surviving line to attach to. They
	// cannot be written to marks eagerly: the line they belong to has not been
	// emitted yet, and writing ahead of it is overwritten the moment it is.
	var carry []string
	emitSame := func() {
		if at < len(marks) {
			marks[at] = lineMark{Kind: lineSame, RemovedBefore: carry}
		}
		carry = nil
		at++
	}
	emit := func(mark lineMark) {
		if at < len(marks) {
			marks[at] = mark
		}
		at++
	}

	for i := 0; i < len(flat); {
		if flat[i].kind == udiff.Equal {
			emitSame()
			i++
			continue
		}
		removed, added, next := gatherChange(flat, i)
		i = next

		head, removed, added, tail := cancelIdentical(removed, added)
		for range head {
			emitSame()
		}
		pending := append(carry, removed...)
		carry = nil
		for range added {
			emit(addedOrModified(&pending))
		}
		// Whatever is left had no insertion to pair with, so it waits for the
		// next surviving line — the first of the unchanged tail, or the line
		// after this change.
		carry = pending
		for range tail {
			emitSame()
		}
	}

	// Deletions past the end of the file have no surviving line at all, so they
	// go on the last one. Dropped instead, truncating a file showed no change —
	// the edit most in need of confirmation.
	if len(carry) > 0 && len(marks) > 0 {
		last := len(marks) - 1
		marks[last].RemovedBefore = append(marks[last].RemovedBefore, carry...)
	}
	return marks
}

// lineOp is one line-level operation, flattened out of the hunk structure.
type lineOp struct {
	kind udiff.OpKind
	text string
}

func flatten(ops udiff.UnifiedDiff) []lineOp {
	var out []lineOp
	for _, hunk := range ops.Hunks {
		for _, line := range hunk.Lines {
			out = append(out, lineOp{line.Kind, strings.TrimSuffix(line.Content, "\n")})
		}
	}
	return out
}

// gatherChange collects one change: every deletion and insertion from i up to
// the next unchanged line. It returns the two runs and where to resume.
func gatherChange(flat []lineOp, i int) (removed, added []string, next int) {
	for ; i < len(flat) && flat[i].kind != udiff.Equal; i++ {
		if flat[i].kind == udiff.Delete {
			removed = append(removed, flat[i].text)
			continue
		}
		added = append(added, flat[i].text)
	}
	return removed, added, i
}

// cancelIdentical splits identical lines off both ends of a change.
//
// An edit script is not unique, and the line-level conversion readily emits one
// where a line is deleted and immediately re-inserted unchanged: inserting a
// line after "b" comes back as delete "b", insert "b", insert "NEW". Taken at
// face value that reports two changed lines where the user added one. Trimming
// the shared head and tail leaves only what actually differs — and is what
// makes the pairing in addedOrModified safe, since everything that reaches it
// genuinely differs.
func cancelIdentical(removed, added []string) (head int, midRemoved, midAdded, tail []string) {
	limit := min(len(removed), len(added))
	for head < limit && removed[head] == added[head] {
		head++
	}
	tailLen := 0
	for tailLen < limit-head &&
		removed[len(removed)-1-tailLen] == added[len(added)-1-tailLen] {
		tailLen++
	}
	return head,
		removed[head : len(removed)-tailLen],
		added[head : len(added)-tailLen],
		added[len(added)-tailLen:]
}

// addedOrModified pairs a new line with a deletion that sits immediately above
// it, turning the pair into one modified line.
//
// A diff has no notion of "changed": editing one character of a line is a
// delete plus an insert, which showed up as a green marker *and* a red removal
// row — two rows and two colours for one keystroke's worth of edit. Pairing
// them is what makes a typo fix read as a typo fix.
//
// Pairing is only safe because cancelIdentical has already taken the unchanged
// lines off both ends of the change: what reaches here genuinely differs.
func addedOrModified(pending *[]string) lineMark {
	if len(*pending) > 0 {
		// The paired removal is the one this line replaces. It is taken off the
		// pending list so the editor's gutter shows one change rather than a
		// deletion followed by an insertion, but kept on the mark so a diff
		// view can still render the "-old" half.
		replaced := (*pending)[0]
		*pending = (*pending)[1:]
		return lineMark{Kind: lineModified, Replaced: replaced}
	}
	return lineMark{Kind: lineAdded}
}

// editCounts totals the three kinds of change separately. A line the user
// rewrote is one modification, not an addition and a deletion: reporting it as
// both doubles every edit and makes changing a character look like changing two
// lines.
type editCounts struct {
	Modified int
	Added    int
	Removed  int
}

// Any reports whether anything changed at all.
func (c editCounts) Any() bool { return c.Modified+c.Added+c.Removed > 0 }

func countEdits(marks []lineMark) editCounts {
	var c editCounts
	for _, mark := range marks {
		switch mark.Kind {
		case lineModified:
			c.Modified++
		case lineAdded:
			c.Added++
		case lineSame:
		}
		c.Removed += len(mark.RemovedBefore)
	}
	return c
}
