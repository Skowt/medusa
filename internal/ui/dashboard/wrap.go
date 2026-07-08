package dashboard

import "charm.land/lipgloss/v2"

const (
	// maxNameLines caps how many lines a wrapped workspace name may occupy on
	// the selected row.
	maxNameLines = 3
	// hyphenLookback is how far back from a hard wrap we look for a hyphen to
	// break on, so slug names break at "-" rather than mid-token.
	hyphenLookback = 8
)

// wrapName splits name into at most maxLines display lines, each no wider than
// width columns. It prefers to break just after a hyphen within the last
// hyphenLookback columns of a line. If the name still overflows the last
// allowed line, that line is ellipsized. Width is measured with lipgloss.Width.
func wrapName(name string, width, maxLines int) []string {
	if width < 1 {
		width = 1
	}
	if maxLines < 1 {
		maxLines = 1
	}
	runes := []rune(name)
	var lines []string
	for len(runes) > 0 {
		if len(lines) == maxLines-1 {
			lines = append(lines, truncateRunes(runes, width))
			return lines
		}
		cut := lineCut(runes, width)
		lines = append(lines, string(runes[:cut]))
		runes = runes[cut:]
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}
	return lines
}

// lineCut returns how many leading runes fit within width columns, preferring a
// hyphen boundary within the last hyphenLookback columns of the fitted span.
func lineCut(runes []rune, width int) int {
	n := 0
	for n < len(runes) && lipgloss.Width(string(runes[:n+1])) <= width {
		n++
	}
	if n == 0 {
		n = 1 // always make progress
	}
	if n >= len(runes) {
		return len(runes)
	}
	for i := n - 1; i >= 0 && i >= n-hyphenLookback; i-- {
		if runes[i] == '-' {
			return i + 1
		}
	}
	return n
}

// truncateRunes returns the longest prefix of runes fitting within width
// columns, appending an ellipsis when truncation occurs.
func truncateRunes(runes []rune, width int) string {
	if lipgloss.Width(string(runes)) <= width {
		return string(runes)
	}
	const ell = "…"
	budget := width - lipgloss.Width(ell)
	if budget < 0 {
		budget = 0
	}
	n := 0
	for n < len(runes) && lipgloss.Width(string(runes[:n+1])) <= budget {
		n++
	}
	return string(runes[:n]) + ell
}
