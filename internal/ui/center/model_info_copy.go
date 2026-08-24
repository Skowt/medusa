package center

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Skowt/medusa/internal/ui/common"
)

// InfoCopyField identifies one of the Info tab's click-to-copy values.
type InfoCopyField int

const (
	// InfoCopyBranch is the Info tab's branch value.
	InfoCopyBranch InfoCopyField = iota
	// InfoCopyPath is the Info tab's workspace path value.
	InfoCopyPath
)

// infoCopyRow describes where a field sits on its line. The renderer prints
// label and the hit test locates the value by it, so both read the same string
// — a label the two disagree on is a value that renders but cannot be clicked.
type infoCopyRow struct {
	label    string // padded label printed immediately before the value
	trailing string // marker printed one space after the value, "" when the value ends the line
	target   copyTarget
}

var infoCopyRows = map[InfoCopyField]infoCopyRow{
	InfoCopyBranch: {label: "Branch: ", trailing: "[Rename]", target: copyTargetInfoBranch},
	InfoCopyPath:   {label: "Path:   ", target: copyTargetInfoPath},
}

// Label returns the padded label the Info tab must print before this field's
// value.
func (f InfoCopyField) Label() string {
	return infoCopyRows[f].label
}

// InfoCopyValue renders one Info tab value as a click-to-copy field: the value
// itself, a "click to copy" badge while the pointer is over it, or "✓ copied"
// for a moment after a click. styled reports that the returned string already
// carries its own styling and must be written as-is.
//
// The field is padded to a fixed width so swapping a badge in cannot move what
// follows it — the branch line ends in [Rename].
func (m *Model) InfoCopyValue(field InfoCopyField, value string) (string, bool) {
	target := infoCopyRows[field].target
	label := value
	var badge lipgloss.Style
	styled := false
	switch {
	case m.copyFeedbackActive(target):
		label = " ✓ copied "
		badge = lipgloss.NewStyle().
			Foreground(common.ColorSuccess).
			Background(common.ColorSurface1).
			Bold(true)
		styled = true
	case m.copyHoverActive && m.copyHover == target:
		label = " click to copy "
		badge = lipgloss.NewStyle().
			Foreground(common.ColorInfo).
			Background(common.ColorSurface1)
		styled = true
	}
	width := max(lipgloss.Width(value), copyBadgeMinWidth)
	pad := strings.Repeat(" ", max(0, width-lipgloss.Width(label)))
	if !styled {
		return label + pad, false
	}
	return badge.Render(label) + pad, true
}

// infoContentOriginY returns the content row the Info tab's first line occupies,
// in pane-local coordinates: the info bar, the tab bar, its separator, and the
// leading newline renderInfoContent adds.
func (m *Model) infoContentOriginY() int {
	return m.infoBarHeight() + 2 + 1
}

// infoCopyRegion returns the hit region of field's value, in Info-tab content
// coordinates. The region is derived from the rendered line so it tracks
// whatever width the value or its badge currently occupies.
func (m *Model) infoCopyRegion(field InfoCopyField) (common.HitRegion, bool) {
	row, ok := infoCopyRows[field]
	if !ok || m.infoContent == "" {
		return common.HitRegion{}, false
	}
	for i, line := range strings.Split(m.infoContent, "\n") {
		stripped := ansi.Strip(line)
		// Anchored at column 0: a workspace note is free text and could well
		// contain "Path:", but notes are always printed behind a cursor prefix.
		if !strings.HasPrefix(stripped, row.label) {
			continue
		}
		start := lipgloss.Width(row.label)
		end := lipgloss.Width(stripped)
		if row.trailing != "" {
			t := strings.Index(stripped, row.trailing)
			if t < 0 {
				return common.HitRegion{}, false
			}
			// One space separates the padded value from the marker.
			end = lipgloss.Width(stripped[:t]) - 1
		}
		if end <= start {
			return common.HitRegion{}, false
		}
		return common.HitRegion{X: start, Y: i, Width: end - start, Height: 1}, true
	}
	return common.HitRegion{}, false
}

// infoCopyHit reports which Info tab value covers the given content coordinates.
func (m *Model) infoCopyHit(localX, infoY int) (InfoCopyField, bool) {
	for _, field := range []InfoCopyField{InfoCopyBranch, InfoCopyPath} {
		if region, ok := m.infoCopyRegion(field); ok && region.Contains(localX, infoY) {
			return field, true
		}
	}
	return 0, false
}

// infoCopyCommand copies the field's full value — the untruncated path, not the
// ~-shortened one on screen — and starts its feedback badge.
func (m *Model) infoCopyCommand(field InfoCopyField) tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	target := infoCopyRows[field].target
	switch field {
	case InfoCopyBranch:
		return m.copyWithFeedback(target, m.workspace.Branch())
	case InfoCopyPath:
		return m.copyWithFeedback(target, m.workspace.Root())
	}
	return nil
}
