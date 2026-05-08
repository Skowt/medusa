package common

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// LineBuilder accumulates rendered rows for a framed view (typically a modal
// dialog) while keeping hit regions aligned with the actually-drawn rows.
//
// Dialogs that set an explicit width via lipgloss.Style.Width will soft-wrap
// any line that exceeds their content width. A naive Y counter based on the
// number of calls to Append would therefore drift away from the rendered rows,
// leaving click regions pointing at the wrong line. LineBuilder solves this by
// pre-wrapping each appended fragment at the style's content width and
// advancing its Y counter by the resulting row count.
//
// The View, Regions, and Size methods all derive from the same underlying
// slice of post-wrap rows, so hit-tests, dialog bounds, and compositor
// placement cannot disagree.
type LineBuilder struct {
	style   lipgloss.Style
	total   int
	content int
	rows    []string
	regions []HitRegion
}

// NewLineBuilder returns a builder that renders its content through style,
// interpreting totalWidth as the final rendered width of the framed view
// (matching the convention of lipgloss.Style.Width in v2). The builder wraps
// each appended fragment at totalWidth minus the style's frame so that no
// further wrapping happens at render time.
func NewLineBuilder(style lipgloss.Style, totalWidth int) *LineBuilder {
	frameX, _ := style.GetFrameSize()
	content := totalWidth - frameX
	if content < 1 {
		content = 1
	}
	return &LineBuilder{style: style, total: totalWidth, content: content}
}

// Append adds content to the builder. If id is non-empty, a HitRegion covering
// every row the content occupies (after wrapping) is recorded under that id,
// spanning the full inner width. Embedded \n characters within content are
// honoured as explicit line breaks; each resulting fragment is wrapped
// independently.
func (b *LineBuilder) Append(id, content string) {
	startY := len(b.rows)
	for i, chunk := range strings.Split(content, "\n") {
		if i > 0 && chunk == "" {
			b.rows = append(b.rows, "")
			continue
		}
		wrapped := lipgloss.NewStyle().Width(b.content).Render(chunk)
		b.rows = append(b.rows, strings.Split(wrapped, "\n")...)
	}
	if id != "" {
		b.regions = append(b.regions, HitRegion{
			ID:     id,
			X:      0,
			Y:      startY,
			Width:  b.content,
			Height: len(b.rows) - startY,
		})
	}
}

// Blank appends a visually empty row. Equivalent to Append("", "") but reads
// more clearly at call sites.
func (b *LineBuilder) Blank() {
	b.rows = append(b.rows, "")
}

// AddRegion records an extra HitRegion at content-local coordinates. Use for
// rows with multiple click targets (OK/Cancel buttons, select chevrons) where
// Append's default full-row region isn't granular enough.
func (b *LineBuilder) AddRegion(id string, x, y, width, height int) {
	if id == "" || width <= 0 || height <= 0 {
		return
	}
	b.regions = append(b.regions, HitRegion{
		ID:     id,
		X:      x,
		Y:      y,
		Width:  width,
		Height: height,
	})
}

// CurrentRow returns the index of the next row that will be appended. Useful
// to capture the Y of a row that's about to be appended so multiple AddRegion
// calls can target the same row without recomputing coordinates.
func (b *LineBuilder) CurrentRow() int {
	return len(b.rows)
}

// AppendRaw appends content without any width-based wrapping. Use when the
// caller has already wrapped lines (e.g. for indented descriptions where
// lipgloss's Width+MarginLeft combo is unreliable). Multi-line content is
// split on \n into separate rows. If id is non-empty, a HitRegion covering
// the appended rows is recorded under that id.
func (b *LineBuilder) AppendRaw(id, content string) {
	startY := len(b.rows)
	b.rows = append(b.rows, strings.Split(content, "\n")...)
	if id != "" {
		b.regions = append(b.regions, HitRegion{
			ID:     id,
			X:      0,
			Y:      startY,
			Width:  b.content,
			Height: len(b.rows) - startY,
		})
	}
}

// View returns the fully framed, post-wrap rendering of the accumulated rows.
func (b *LineBuilder) View() string {
	return b.style.Render(strings.Join(b.rows, "\n"))
}

// Regions returns the hit regions recorded so far, in the order they were
// appended. Coordinates are content-local (i.e. relative to the top-left of
// the inner content area, after subtracting the frame offset).
func (b *LineBuilder) Regions() []HitRegion {
	out := make([]HitRegion, len(b.regions))
	copy(out, b.regions)
	return out
}

// RegionByID returns the HitRegion recorded for the given id. Ok is false if
// no region with that id exists.
func (b *LineBuilder) RegionByID(id string) (HitRegion, bool) {
	for _, r := range b.regions {
		if r.ID == id {
			return r, true
		}
	}
	return HitRegion{}, false
}

// Size returns the total rendered dimensions of View, including the frame
// contributed by the outer style. Callers should use these values to centre
// the dialog on screen so the click handler and compositor agree on placement.
func (b *LineBuilder) Size() (w, h int) {
	_, frameY := b.style.GetFrameSize()
	return b.total, len(b.rows) + frameY
}

// ContentOffset returns the top-left corner of the content area within the
// framed view, measured in cells from the view's top-left.
func (b *LineBuilder) ContentOffset() (x, y int) {
	frameX, frameY := b.style.GetFrameSize()
	return frameX / 2, frameY / 2
}

// Rows returns the number of content rows (post-wrap, excluding frame).
func (b *LineBuilder) Rows() int {
	return len(b.rows)
}

// ContentWidth returns the inner content width that this builder wraps to
// (the framed view width minus the style's horizontal frame).
func (b *LineBuilder) ContentWidth() int {
	return b.content
}
