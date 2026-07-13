package common

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	// Horizontal padding inside the box border.
	progressPadX = 3
	// Text width the box always reserves, so short runs don't render a cramped box.
	progressMinText = 36
	// Widest the text may grow before the detail is truncated instead.
	progressMaxText = 60
	// Spinner or check mark plus the two spaces after it.
	progressPrefix = 3
	// A detail narrower than this is dropped rather than shown as "(...)".
	progressMinDetail = 4
)

// ProgressOverlay renders a centered modal showing step-based progress with a spinner.
type ProgressOverlay struct {
	title        string
	steps        []string
	currentStep  int
	detail       string // shown in brackets after the active step
	spinnerFrame int
	visible      bool
	width        int
	height       int
}

// NewProgressOverlay creates a new progress overlay with the given title and step labels.
func NewProgressOverlay(title string, steps []string) *ProgressOverlay {
	return &ProgressOverlay{
		title:   title,
		steps:   steps,
		visible: true,
	}
}

// Show makes the overlay visible.
func (p *ProgressOverlay) Show() { p.visible = true }

// Hide hides the overlay.
func (p *ProgressOverlay) Hide() { p.visible = false }

// Visible returns whether the overlay is visible.
func (p *ProgressOverlay) Visible() bool { return p.visible }

// AdvanceStep moves to the next step, clearing the detail.
func (p *ProgressOverlay) AdvanceStep() {
	p.detail = ""
	if p.currentStep < len(p.steps)-1 {
		p.currentStep++
	}
}

// SetStepDetail sets the bracketed detail text shown after the active step.
func (p *ProgressOverlay) SetStepDetail(detail string) { p.detail = detail }

// TickSpinner advances the spinner frame.
func (p *ProgressOverlay) TickSpinner() { p.spinnerFrame++ }

// SetSize stores the terminal dimensions for centering.
func (p *ProgressOverlay) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// View renders the overlay box.
func (p *ProgressOverlay) View() string {
	if !p.visible {
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	checkStyle := lipgloss.NewStyle().
		Foreground(ColorSuccess)

	spinnerStyle := lipgloss.NewStyle().
		Foreground(ColorPrimary)

	detailStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	futureStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	// Labels get at most this much room after the three-cell spinner/check
	// prefix; anything longer is truncated rather than wrapped.
	labelRoom := p.maxTextWidth() - progressPrefix
	detail := p.fittedDetail(labelRoom)

	var lines []string
	lines = append(lines, titleStyle.Render(p.title))
	textWidth := lipgloss.Width(p.title)

	for i, step := range p.steps {
		label := truncateToWidth(step, labelRoom)
		var line string
		if i < p.currentStep {
			// Completed
			line = checkStyle.Render(Icons.Clean) + "  " + label
		} else if i == p.currentStep {
			// Active — append detail in brackets if set
			stepText := label
			if detail != "" {
				stepText += " " + detailStyle.Render("("+detail+")")
			}
			line = spinnerStyle.Render(SpinnerFrame(p.spinnerFrame)) + "  " + stepText
		} else {
			// Future
			line = futureStyle.Render("   " + label)
		}
		lines = append(lines, line)
		textWidth = max(textWidth, lipgloss.Width(line))
	}

	content := strings.Join(lines, "\n")

	textWidth = min(max(textWidth, progressMinText), p.maxTextWidth())

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, progressPadX).
		Width(textWidth + 2*progressPadX + 2)

	return boxStyle.Render(content)
}

// maxTextWidth is the widest the box's text may be: bounded by the terminal so
// the modal always fits, and by progressMaxText so one long detail can't stretch
// the box across a wide screen.
func (p *ProgressOverlay) maxTextWidth() int {
	limit := progressMaxText
	if p.width > 0 {
		// Leave room for the border and the padding on both sides.
		limit = min(limit, p.width-2*progressPadX-2)
	}
	return max(limit, progressPrefix+1)
}

// fittedDetail shrinks the active step's detail (a repo name) so the step stays
// on one line instead of wrapping, and drops it entirely when there's no room.
func (p *ProgressOverlay) fittedDetail(labelRoom int) string {
	if p.detail == "" || p.currentStep >= len(p.steps) {
		return ""
	}
	label := truncateToWidth(p.steps[p.currentStep], labelRoom)
	room := labelRoom - lipgloss.Width(label) - lipgloss.Width(" ()")
	if room < progressMinDetail {
		return ""
	}
	return truncateToWidth(p.detail, room)
}
