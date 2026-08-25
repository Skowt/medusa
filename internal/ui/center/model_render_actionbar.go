package center

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// getBaseBranchDisplay returns the base branch string for info bar display.
// Shows origin/main for remote refs, or "branch (local)" for local branches.
func (m *Model) getBaseBranchDisplay() string {
	if m.workspace == nil {
		return "main"
	}
	if m.workspace.Base() != "" && m.workspace.Base() != "HEAD" {
		base := m.workspace.Base()
		if strings.HasPrefix(base, "origin/") {
			return base
		}
		return base + " (local)"
	}
	return "main"
}

// renderInfoBar renders the info bar with workspace details and action buttons.
// Layout: [branch info] │ [path] [IDE] … right-aligned [Review Changes]
// Also renders a subtle separator line below.
func (m *Model) renderInfoBar(width int) string {
	m.actionBarHits = m.actionBarHits[:0]

	if m.workspace == nil || width < 20 {
		return ""
	}

	ws := m.workspace

	// Styles
	mutedStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	branchStyle := lipgloss.NewStyle().Foreground(common.ColorInfo)
	pathStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	separatorStyle := lipgloss.NewStyle().Foreground(common.ColorSurface2)

	branchLabel := ws.Branch()
	branchWidth := max(lipgloss.Width(branchLabel), copyBadgeMinWidth)
	branchLabel = m.copyLabel(copyTargetBranch, branchLabel, branchWidth)
	baseBranchDisplay := m.getBaseBranchDisplay()
	var branchInfo string
	var branchX int
	if ws.IsMainBranch() {
		branchInfo = branchStyle.Render(branchLabel)
	} else {
		prefix := mutedStyle.Render(baseBranchDisplay) + mutedStyle.Render(" ← ")
		branchX = lipgloss.Width(prefix)
		branchInfo = prefix + branchStyle.Render(branchLabel)
	}
	m.actionBarHits = append(m.actionBarHits, actionBarButton{
		kind:   actionBarCopyBranch,
		region: common.HitRegion{X: branchX, Y: 0, Width: branchWidth, Height: 1},
	})

	// Calculate left side content
	separator := separatorStyle.Render(" │ ")
	separatorWidth := lipgloss.Width(separator)

	// IDE button (styled to match branch name)
	ideBtn := branchStyle.Render("[IDE]")
	ideBtnWidth := lipgloss.Width(ideBtn)

	// Review button, offered only while the worktree is dirty.
	reviewBtn := ""
	reviewBtnWidth := 0
	if m.gitDirty {
		reviewBtn = lipgloss.NewStyle().Foreground(common.ColorPrimary).
			Bold(true).Render("[Review Changes]")
		reviewBtnWidth = lipgloss.Width(reviewBtn) + 1
	}

	// Build path info (shortened)
	reservedForLeft := lipgloss.Width(branchInfo) + separatorWidth + 1 + ideBtnWidth + reviewBtnWidth
	availableForPath := width - reservedForLeft
	if availableForPath < 10 {
		availableForPath = 10
	}

	pathInfo := shortenPath(ws.Root(), availableForPath)
	pathWidth := max(lipgloss.Width(pathInfo), copyBadgeMinWidth)
	pathInfo = m.copyLabel(copyTargetWorkdir, pathInfo, pathWidth)
	pathRendered := pathStyle.Render(pathInfo)

	// Left content: branch │ path [IDE]. The review button is not part of it —
	// it is right-aligned below, away from the three controls that describe the
	// workspace, because it is the one that acts on the work.
	leftContent := branchInfo + separator + pathRendered + " " + ideBtn

	// The displayed path itself is the copy target.
	pathX := lipgloss.Width(branchInfo + separator)
	m.actionBarHits = append(m.actionBarHits, actionBarButton{
		kind:   actionBarCopyDir,
		label:  "Path",
		region: common.HitRegion{X: pathX, Y: 0, Width: pathWidth, Height: 1},
	})

	// Track IDE button hit region after the path.
	ideBtnX := pathX + lipgloss.Width(pathInfo) + 1
	m.actionBarHits = append(m.actionBarHits, actionBarButton{
		kind:  actionBarOpenIDE,
		label: "IDE",
		region: common.HitRegion{
			X:      ideBtnX,
			Y:      0,
			Width:  ideBtnWidth,
			Height: 1,
		},
	})

	// Build the main line, with the review button pinned to the right edge.
	//
	// The left side is clipped first when the two would collide: the path is
	// already abbreviated and can lose a little more, whereas a button pushed
	// past the edge is simply gone — which is the failure mode that is hardest
	// to notice, since nothing is left to hint it should be there.
	mainLine := leftContent
	if reviewBtn != "" {
		reviewWidth := lipgloss.Width(reviewBtn)
		room := width - reviewWidth - 1
		if room < 0 {
			room = 0
		}
		left := lipgloss.NewStyle().MaxWidth(room).Render(leftContent)
		gap := room - lipgloss.Width(left) + 1
		if gap < 1 {
			gap = 1
		}
		mainLine = left + strings.Repeat(" ", gap) + reviewBtn

		m.actionBarHits = append(m.actionBarHits, actionBarButton{
			kind:  actionBarReviewChanges,
			label: "Review Changes",
			region: common.HitRegion{
				X:      lipgloss.Width(mainLine) - reviewWidth,
				Y:      0,
				Width:  reviewWidth,
				Height: 1,
			},
		})
	}

	// Add a subtle separator line below (using dim box-drawing character)
	separatorLine := separatorStyle.Render(strings.Repeat("─", width))

	return mainLine + "\n" + separatorLine
}

// shortenPath shortens a path to fit within maxLen characters.
// It replaces the home directory with ~ for more readable paths.
func shortenPath(path string, maxLen int) string {
	// First, try to replace home directory with ~
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if strings.HasPrefix(path, home) {
			path = "~" + strings.TrimPrefix(path, home)
		}
	}

	if len(path) <= maxLen {
		return path
	}

	// Take last parts of the path to fit within maxLen
	parts := strings.Split(path, string(filepath.Separator))
	result := ""
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if part == "" {
			continue
		}
		if result == "" {
			result = part
		} else {
			candidate := part + string(filepath.Separator) + result
			if len(candidate)+4 > maxLen { // +4 for ".../"
				break
			}
			result = candidate
		}
	}

	// Add ellipsis prefix if we truncated
	if !strings.HasPrefix(path, result) && !strings.HasPrefix(result, "~") {
		result = "..." + string(filepath.Separator) + result
	}

	return result
}

// actionBarCommand returns the command for the given button kind.
func (m *Model) actionBarCommand(kind actionBarButtonKind) tea.Cmd {
	if m.workspace == nil {
		return nil
	}

	ws := m.workspace
	switch kind {
	case actionBarCopyBranch:
		return m.copyWithFeedback(copyTargetBranch, ws.Branch())
	case actionBarCopyDir:
		return m.copyWithFeedback(copyTargetWorkdir, ws.Root())
	case actionBarOpenIDE:
		return func() tea.Msg {
			return messages.ActionBarOpenIDE{WorkspaceRoot: ws.Root()}
		}
	case actionBarReviewChanges:
		return func() tea.Msg {
			return messages.OpenReviewChanges{WorkspaceID: string(ws.ID())}
		}
	}
	return nil
}

const copyFeedbackDuration = 1500 * time.Millisecond

func (m *Model) copyFeedbackActive(target copyTarget) bool {
	return m.copyFeedback != nil && m.copyFeedback[target] != 0
}

func (m *Model) copyLabel(target copyTarget, value string, width int) string {
	label := value
	var badge lipgloss.Style
	styled := false
	if m.copyFeedbackActive(target) {
		label = " ✓ copied "
		badge = lipgloss.NewStyle().
			Foreground(common.ColorSuccess).
			Background(common.ColorSurface1).
			Bold(true)
		styled = true
	} else if m.copyHoverActive && m.copyHover == target {
		label = " click to copy "
		badge = lipgloss.NewStyle().
			Foreground(common.ColorInfo).
			Background(common.ColorSurface1)
		styled = true
	}
	labelWidth := lipgloss.Width(label)
	if labelWidth > width {
		width = labelWidth
	}
	left := (width - labelWidth) / 2
	if !styled {
		return strings.Repeat(" ", left) + label + strings.Repeat(" ", width-labelWidth-left)
	}
	return strings.Repeat(" ", left) + badge.Render(label) + strings.Repeat(" ", width-labelWidth-left)
}

const copyBadgeMinWidth = len(" click to copy ")

func (m *Model) copyWithFeedback(target copyTarget, value string) tea.Cmd {
	write := m.clipboardWrite
	if write == nil {
		write = common.CopyToClipboard
	}
	if err := write(value); err != nil {
		return func() tea.Msg {
			return messages.Toast{Message: "Failed to copy: " + err.Error(), Level: messages.ToastError}
		}
	}
	if m.copyFeedback == nil {
		m.copyFeedback = make(map[copyTarget]uint64)
	}
	m.copySequence++
	generation := m.copySequence
	m.copyFeedback[target] = generation
	return common.SafeTick(copyFeedbackDuration, func(time.Time) tea.Msg {
		return copyFeedbackExpired{target: target, generation: generation}
	})
}

// infoBarHeight returns the height of the info bar (2 if visible: content + separator, 0 otherwise).
func (m *Model) infoBarHeight() int {
	if m.workspace == nil {
		return 0
	}
	return 2 // Main line + separator line
}

// InfoBarView returns the rendered info bar string for layer-based rendering.
func (m *Model) InfoBarView(width int) string {
	if m.infoBarHeight() == 0 {
		return ""
	}
	return m.renderInfoBar(width)
}

// InfoBarHeight returns the info bar height (exported for app rendering).
func (m *Model) InfoBarHeight() int {
	return m.infoBarHeight()
}

// SetInfoBarY sets the Y position of the info bar for mouse hit testing.
func (m *Model) SetInfoBarY(y int) {
	m.actionBarY = y
}

// handleInfoBarClick checks if a click is on an info bar button and returns the appropriate command.
func (m *Model) handleInfoBarClick(contentX, contentY int) tea.Cmd {
	if m.infoBarHeight() == 0 {
		return nil
	}

	// Check if click is within the info bar area (first line only, not separator)
	if contentY != m.actionBarY {
		return nil
	}

	// Check button hits
	for _, hit := range m.actionBarHits {
		if hit.region.Contains(contentX, 0) {
			return m.actionBarCommand(hit.kind)
		}
	}
	return nil
}

func (m *Model) handleActionBarClick(contentX, contentY int) tea.Cmd {
	return m.handleInfoBarClick(contentX, contentY)
}
