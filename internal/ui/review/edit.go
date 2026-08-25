package review

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
)

// editBuffer is one file open for hand editing.
//
// The agent may still be running while the user edits, so the buffer records
// what the file looked like when it was opened and refuses to write over a file
// that has moved since. Silently clobbering the agent's work is the worst thing
// this feature could do, and it would be invisible: the user would see their
// own text land and never know what it replaced.
type editBuffer struct {
	path string // absolute
	// original is the file exactly as read, and shown is what the textarea
	// holds — the two differ because the widget expands every tab to spaces on
	// the way in and offers no way to turn that off (its sanitizer is on an
	// unexported field of an internal package).
	//
	// Keeping both is what makes saving safe. Comparing against shown is the
	// only way to tell a real edit from the widget's own rewrite, and holding
	// original means an untouched line can be written back byte-identical
	// instead of tab-expanded. Without this, opening a Go file or a Makefile
	// and saving would silently convert its indentation — and for a Makefile
	// that is not a style change, it stops the file working.
	original string
	shown    string
	area     textarea.Model
	// tabIndented records that the file indents with tabs, so lines the user
	// *did* edit get tabs back rather than the spaces they were typed over.
	tabIndented bool
	// base is the file's committed content, tab-expanded to match the buffer.
	// Diffing against it live is what keeps the "changed since HEAD" markers
	// pinned to the lines they belong to: the git diff's line numbers are fixed
	// at load, so the moment the user inserts a line every marker below points
	// at whatever slid into its slot.
	base    string
	hasBase bool

	size    int64
	modTime time.Time

	// marks are recomputed only when the content changes. They are read
	// several times per frame — the gutter, the header, the rebuilt diff — and
	// each recomputation is a full diff of the file, so without this a single
	// scroll event ran four or five of them.
	cached marksCache
}

// marksCache holds the diffs for one buffer value.
type marksCache struct {
	value string
	valid bool
	own   []lineMark // against what was opened: the user's edits
	base  []lineMark // against the commit: everything changed in the workspace
}

// noWrapWidth is wide enough that no source line wraps in the model. Wrapping
// is a display concern, and this view does its own clipping.
const noWrapWidth = 1 << 14

// textareaTabWidth is how many spaces the widget substitutes for a tab.
const textareaTabWidth = 4

// expandTabs mirrors the widget's own substitution so the two can be compared.
func expandTabs(s string) string {
	return strings.ReplaceAll(s, "\t", strings.Repeat(" ", textareaTabWidth))
}

// restoreIndent converts a line's leading spaces back into tabs. Only leading
// whitespace is touched: spaces used to align a trailing comment or a struct
// literal are the author's and must survive untouched.
func restoreIndent(line string) string {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	tabs := i / textareaTabWidth
	if tabs == 0 {
		return line
	}
	return strings.Repeat("\t", tabs) + line[tabs*textareaTabWidth:]
}

// usesTabIndent reports whether any line of the file is tab-indented.
func usesTabIndent(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "\t") {
			return true
		}
	}
	return false
}

// newEditBuffer reads a file into a fresh buffer, stamping the identity the
// save-time check compares against.
func newEditBuffer(path, base string, hasBase bool, height int) (*editBuffer, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	area := textarea.New()
	area.ShowLineNumbers = true
	area.CharLimit = 0
	// The widget defaults to MaxHeight 99 and MaxWidth 500, and once a value
	// reaches MaxHeight it makes Enter a *silent* no-op — so in any file over
	// 99 lines, which is most of them, pressing Enter simply did nothing and
	// said nothing. MaxWidth truncates long lines just as quietly. Both are
	// meant for a small input box, not a file, so both are cleared.
	area.MaxHeight = 0
	area.MaxWidth = 0
	// The widget is sized past any real line length so it never soft-wraps.
	// This view clips instead, and wrapping inside the model breaks it in two
	// ways: CursorDown moves by *visual* row, so stepping to a line counts
	// wrapped rows and stops short — jumping to line 120 of a file with long
	// comments landed on line 13 — and Column() stops being the logical column.
	area.SetWidth(noWrapWidth)
	area.SetHeight(height)
	area.SetValue(string(content))
	area.MoveToBegin()
	return &editBuffer{
		path:        path,
		original:    string(content),
		shown:       area.Value(),
		base:        expandTabs(base),
		hasBase:     hasBase,
		tabIndented: usesTabIndent(string(content)),
		area:        area,
		size:        info.Size(),
		modTime:     info.ModTime(),
	}, nil
}

// Dirty reports whether the user has changed anything.
//
// The comparison is against shown, not original: the widget's tab expansion
// makes every tab-indented file differ from its own contents the moment it is
// opened, so comparing with original reports an untouched Go file as edited.
func (b *editBuffer) Dirty() bool {
	return b != nil && b.area.Value() != b.shown
}

// EditCounts reports what the user has changed against what was opened.
func (b *editBuffer) EditCounts() editCounts { return countEdits(b.Marks()) }

// Marks labels every line of the buffer against what was opened — the user's
// own edits in this session.
//
// Derived from the content rather than tracked alongside it: the textarea owns
// every edit path — typing, deleting, pasting, moving lines — and a tally kept
// beside it would drift out of step with whichever of those it missed. The
// cache keeps that from costing a diff per read.
func (b *editBuffer) Marks() []lineMark {
	if b == nil {
		return nil
	}
	b.refresh()
	return b.cached.own
}

// BaseMarks labels every line against the committed file: everything the
// workspace has changed, the agent's work and the user's together.
//
// This is computed from the buffer rather than read off the git diff for the
// reason the git diff cannot serve: its line numbers were fixed when the window
// opened, so inserting a line leaves every marker below it pointing one row too
// high. An untracked file has no base, so all of it is new.
func (b *editBuffer) BaseMarks() []lineMark {
	if b == nil {
		return nil
	}
	b.refresh()
	return b.cached.base
}

// refresh recomputes both diffs when the buffer's content has moved, and does
// nothing when it has not. One string comparison stands in for the two diffs.
func (b *editBuffer) refresh() {
	value := b.area.Value()
	if b.cached.valid && b.cached.value == value {
		return
	}
	now := strings.Split(value, "\n")

	own := []lineMark(nil)
	if value != b.shown {
		own = diffLines(strings.Split(b.shown, "\n"), now)
	}

	base := make([]lineMark, len(now))
	if b.hasBase {
		base = diffLines(strings.Split(b.base, "\n"), now)
	} else {
		// Nothing committed to compare against: every line of the file is new.
		for i := range base {
			base[i] = lineMark{Kind: lineAdded}
		}
	}
	b.cached = marksCache{value: value, valid: true, own: own, base: base}
}

// CursorColumn returns the caret's rune offset within its logical line.
//
// Column, not LineInfo().ColumnOffset: the latter is the offset within the
// *visual* row, so on any soft-wrapped line it puts the caret back at the start
// of the wrap instead of where the user is typing.
func (b *editBuffer) CursorColumn() int {
	if b == nil {
		return 0
	}
	return b.area.Column()
}

// Stale reports whether the file changed underneath the buffer since it was
// opened. Size and mtime together are what git itself uses to decide a file is
// untouched, and they cost one stat rather than a re-read.
func (b *editBuffer) Stale() bool {
	info, err := os.Stat(b.path)
	if err != nil {
		// Gone or unreadable counts as changed: there is nothing safe to
		// overwrite, and recreating a file the agent deleted would be wrong.
		return true
	}
	return info.Size() != b.size || !info.ModTime().Equal(b.modTime)
}

// Save writes the buffer back, refusing if the file moved under it. A clean
// buffer is a no-op so an opened-but-untouched file never trips the guard.
func (b *editBuffer) Save() error {
	if !b.Dirty() {
		return nil
	}
	if b.Stale() {
		return fmt.Errorf("%s changed on disk since you opened it", b.path)
	}
	info, err := os.Stat(b.path)
	if err != nil {
		return err
	}
	content := b.merged()
	if err := os.WriteFile(b.path, []byte(content), info.Mode().Perm()); err != nil {
		return err
	}
	// Re-stamp so a second save in the same session compares against what we
	// just wrote rather than against the pre-edit file.
	b.original = content
	b.shown = expandTabs(content)
	if updated, err := os.Stat(b.path); err == nil {
		b.size, b.modTime = updated.Size(), updated.ModTime()
	}
	return nil
}

// merged rebuilds the file to write, undoing the widget's tab expansion.
//
// The rule is that a line the user did not touch is written back exactly as it
// was read — matched against shown, so the widget's own rewrite never counts as
// a change. That is what keeps editing one line of a Go file from reindenting
// the other four hundred, and what keeps a Makefile working: its tabs are
// syntax, not style, and silently spacing them out breaks every recipe.
//
// A line that *was* edited cannot be recovered that way, so its leading spaces
// are folded back into tabs when the file indents with tabs. Only the leading
// run is touched: spaces aligning a trailing comment are the author's.
func (b *editBuffer) merged() string {
	was := strings.Split(b.shown, "\n")
	raw := strings.Split(b.original, "\n")
	now := strings.Split(b.area.Value(), "\n")

	out := make([]string, len(now))
	for i, line := range now {
		switch {
		case i < len(was) && line == was[i] && i < len(raw):
			out[i] = raw[i] // untouched: byte-identical
		case b.tabIndented:
			out[i] = restoreIndent(line)
		default:
			out[i] = line
		}
	}
	return strings.Join(out, "\n")
}

// FocusLine puts the cursor on a 1-based line of the file.
//
// Editing opens from a diff row the user was already looking at, so landing at
// the top of the file would make them hunt for the place they just chose — in a
// long file that is most of the cost of the edit. Lines past the end clamp to
// the last one rather than doing nothing, since a diff can name a line the
// working tree no longer has.
//
// Only the caret moves here. Scrolling is renderEditPane's job (editScrollTop),
// because the widget's own viewport takes its content during View and silently
// clamps any offset set before the first render — which left the caret parked
// on line 120 with the pane still showing line 1.
func (b *editBuffer) FocusLine(line int) {
	if b == nil || line <= 1 {
		if b != nil {
			b.area.MoveToBegin()
		}
		return
	}
	b.area.MoveToBegin()
	for i := 1; i < line; i++ {
		before := b.area.Line()
		b.area.CursorDown()
		if b.area.Line() == before {
			return // hit the last line
		}
	}
}

// SetHeight resizes the editing area. There is no width to set: the model is
// pinned wide so it never wraps, and this view clips — see newEditBuffer.
func (b *editBuffer) SetHeight(height int) {
	if b == nil {
		return
	}
	b.area.SetHeight(height)
}
