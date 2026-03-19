package center

import (
	"os"
	"path/filepath"
	"strings"

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
// Layout: [branch info] │ [path] [Copy] [IDE]
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

	// Build branch info: "origin/main ← feature-branch" or just "main" if on main
	baseBranchDisplay := m.getBaseBranchDisplay()
	var branchInfo string
	if ws.IsMainBranch() {
		branchInfo = branchStyle.Render(ws.Branch())
	} else {
		branchInfo = mutedStyle.Render(baseBranchDisplay) + mutedStyle.Render(" ← ") + branchStyle.Render(ws.Branch())
	}

	// Calculate left side content
	separator := separatorStyle.Render(" │ ")
	separatorWidth := lipgloss.Width(separator)

	// Copy and IDE buttons (styled to match branch name)
	copyBtn := branchStyle.Render("[Copy]")
	copyBtnWidth := lipgloss.Width(copyBtn)
	ideBtn := branchStyle.Render("[IDE]")
	ideBtnWidth := lipgloss.Width(ideBtn)

	// Build path info (shortened)
	reservedForLeft := lipgloss.Width(branchInfo) + separatorWidth + 1 + copyBtnWidth + 1 + ideBtnWidth // +1 for spaces
	availableForPath := width - reservedForLeft
	if availableForPath < 10 {
		availableForPath = 10
	}

	pathInfo := shortenPath(ws.Root(), availableForPath)
	pathRendered := pathStyle.Render(pathInfo)

	// Left content: branch │ path [Copy] [IDE]
	leftContent := branchInfo + separator + pathRendered + " " + copyBtn + " " + ideBtn

	// Track Copy button hit region
	copyBtnX := lipgloss.Width(branchInfo + separator + pathRendered + " ")
	m.actionBarHits = append(m.actionBarHits, actionBarButton{
		kind:  actionBarCopyDir,
		label: "Copy",
		region: common.HitRegion{
			X:      copyBtnX,
			Y:      0,
			Width:  copyBtnWidth,
			Height: 1,
		},
	})

	// Track IDE button hit region (after Copy button)
	ideBtnX := copyBtnX + copyBtnWidth + 1 // +1 for space
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

	// Build the main line
	mainLine := leftContent

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
	case actionBarCopyDir:
		return func() tea.Msg {
			return messages.ActionBarCopyDir{WorkspaceRoot: ws.Root()}
		}
	case actionBarOpenIDE:
		return func() tea.Msg {
			return messages.ActionBarOpenIDE{WorkspaceRoot: ws.Root()}
		}
	}
	return nil
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
