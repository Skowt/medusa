package center

import (
	"charm.land/lipgloss/v2"
)

// minNoteWidth is the fewest cells a non-empty note may be allocated before
// the tab viewport gets any width. It is a floor on the cap, not on the
// allocation: a three-cell note takes three cells, not eight.
const minNoteWidth = 8

// noteWidth returns the cells to reserve for the workspace note in the tab
// bar, or 0 when there is no note. A set note always receives at least
// min(displayWidth(note), minNoteWidth) cells so it is never fully hidden.
func noteWidth(note string, contentWidth int) int {
	if note == "" || contentWidth <= 0 {
		return 0
	}

	cap := contentWidth / 3
	if cap < minNoteWidth {
		cap = minNoteWidth
	}

	w := lipgloss.Width(note)
	if w > cap {
		return cap
	}
	return w
}

// arrowWidth is the cells consumed by one scroll arrow plus its trailing space.
const arrowWidth = 2

// tabViewport describes which agent tabs are visible in the scrollable strip.
type tabViewport struct {
	start, end int // visible range, [start, end)
	showPrev   bool
	showNext   bool
}

// visibleTabs picks the largest run of whole tabs starting at offset that fits
// in avail cells, reserving arrowWidth for each arrow it decides to show.
//
// Tabs are never partially drawn: close-button hit regions are anchored from a
// tab's right edge, so a clipped tab would place a clickable × outside the
// viewport.
//
// When active >= 0 and the returned range is non-empty, that range is
// guaranteed to contain active, which is what makes keyboard tab-cycling
// scroll the viewport into view.
func visibleTabs(widths []int, avail, offset, active int) tabViewport {
	n := len(widths)
	if n == 0 {
		return tabViewport{}
	}

	// Clamp the requested offset into range.
	if offset < 0 {
		offset = 0
	}
	if offset > n-1 {
		offset = n - 1
	}

	// Pull the viewport to the active tab. Scrolling left is a direct
	// assignment; scrolling right needs the run computed backwards from
	// active so that active is the last whole tab that fits.
	if active >= 0 && active < n {
		if active < offset {
			offset = active
		} else {
			for fitRun(widths, avail, offset, n) <= active {
				if offset >= active {
					break
				}
				offset++
			}
		}
	}

	end := fitRun(widths, avail, offset, n)

	return tabViewport{
		start:    offset,
		end:      end,
		showPrev: offset > 0,
		showNext: end < n,
	}
}

// fitRun returns the exclusive end index of the largest run of whole tabs
// starting at start that fits in avail, accounting for the arrows that such a
// run would require.
func fitRun(widths []int, avail, start, n int) int {
	budget := avail
	if start > 0 {
		budget -= arrowWidth // showPrev
	}

	// First pass: assume no next arrow.
	end := start
	used := 0
	for end < n && used+widths[end] <= budget {
		used += widths[end]
		end++
	}
	if end == n {
		return end
	}

	// A next arrow is needed. Re-fit against the smaller budget.
	budget -= arrowWidth
	end = start
	used = 0
	for end < n && used+widths[end] <= budget {
		used += widths[end]
		end++
	}
	return end
}
