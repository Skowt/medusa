package dashboard

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/hooks"
	"github.com/Skowt/medusa/internal/ui/common"
)

const (
	rightSlotWidth = 7 // " + # × " — duplicate, group-edit, delete slots
)

// renderWorkspaceLine1: hook indicator + name + delete icon
func (m *Model) renderWorkspaceLine1(ws *data.Workspace, selected bool, contentWidth int) string {
	indicatorWidth := 2

	wsID := string(ws.ID())

	// Status text for creating/deleting
	statusText := ""
	if m.deletingWorkspaces[ws.Root()] {
		frame := common.SpinnerFrame(m.spinnerFrame)
		pendingStyle := m.styles.StatusPending
		spaceStyle := lipgloss.NewStyle()
		if selected {
			pendingStyle = pendingStyle.Background(common.ColorSelection)
			spaceStyle = spaceStyle.Background(common.ColorSelection)
		}
		statusText = spaceStyle.Render(" ") + pendingStyle.Render(frame+" deleting")
	} else if _, ok := m.creatingWorkspaces[ws.Root()]; ok {
		frame := common.SpinnerFrame(m.spinnerFrame)
		pendingStyle := m.styles.StatusPending
		spaceStyle := lipgloss.NewStyle()
		if selected {
			pendingStyle = pendingStyle.Background(common.ColorSelection)
			spaceStyle = spaceStyle.Background(common.ColorSelection)
		}
		statusText = spaceStyle.Render(" ") + pendingStyle.Render(frame+" creating")
	}

	// Default indicator based on workspace status
	indicator := "●"
	indicatorFg := common.ColorSuccess
	switch ws.Status {
	case data.StatusBlocked:
		indicator = common.Icons.Blocked
		indicatorFg = common.ColorError
	case data.StatusReview:
		indicator = common.Icons.Pending
		indicatorFg = common.ColorSecondary
	case data.StatusMerged:
		indicator = common.Icons.Completed
		indicatorFg = common.ColorPrimary
	}

	// Hook-based activity overrides: spinner while the agent works, warning symbols for notifications
	if hookState, ok := m.hookStates[wsID]; ok {
		switch {
		case hooks.IsActiveEvent(hooks.EventType(hookState)):
			indicator = common.SpinnerFrame(m.spinnerFrame)
			indicatorFg = common.ColorSuccess
		case hookState == "NotificationPermission" || hookState == "NotificationElicitation" || hookState == "PermissionRequest":
			indicator = "!"
			indicatorFg = common.ColorWarning
		}
	}

	// Override for unread workspaces: make indicator and name orange
	isCurrentWorkspace := ws.Root() == m.activeRoot
	hasUnread := m.unreadWorkspaces[wsID] && !isCurrentWorkspace

	if hasUnread {
		indicatorFg = common.ColorWarning
	}

	iconStyle := lipgloss.NewStyle().Foreground(indicatorFg)
	if selected {
		iconStyle = iconStyle.Bold(true).Background(common.ColorSelection)
	}
	renderedIndicator := iconStyle.Render(indicator + " ")

	// Styles
	style := m.styles.WorkspaceRow
	if hasUnread {
		style = lipgloss.NewStyle().Foreground(common.ColorWarning)
	}
	if selected {
		style = lipgloss.NewStyle().Bold(true).Foreground(common.ColorForeground).Background(common.ColorSelection)
	}

	// Prefix is the leading space + rendered indicator (" <indicator> "), width 3.
	prefix := style.Render(" ") + renderedIndicator

	// Right-edge icon slot: " + # × " when selected (7 cols), "       " otherwise (7 cols).
	rightSlot := "       "
	if selected {
		rightSlot = " " + common.Icons.Add + " " + common.Icons.Group + " " + common.Icons.Close + " "
	}

	// Truncate name
	name := ws.Name
	prefixWidth := 2 + indicatorWidth // logical width used for truncation budget
	maxNameWidth := contentWidth - lipgloss.Width(statusText) - rightSlotWidth - prefixWidth
	if maxNameWidth > 0 && lipgloss.Width(name) > maxNameWidth {
		runes := []rune(name)
		for len(runes) > 0 && lipgloss.Width(string(runes)) > maxNameWidth-1 {
			runes = runes[:len(runes)-1]
		}
		name = string(runes) + "…"
	}

	if selected {
		// Measure from the actual rendered prefix so icon click ranges match
		// what's on screen. rightSlot layout is " + # × " — positions relative
		// to nameEnd: 0=space, 1=+, 2=space, 3=#, 4=space, 5=×, 6=space.
		nameEnd := lipgloss.Width(prefix) + lipgloss.Width(style.Render(name))
		m.duplicateIconX = nameEnd + 1
		m.groupIconX = nameEnd + 3
		m.deleteIconX = nameEnd + 5
	}

	return prefix + style.Render(name) + style.Render(rightSlot) + statusText
}

// renderWorkspaceLine2: profile · git changes · created day
func (m *Model) renderWorkspaceLine2(ws *data.Workspace, selected bool, contentWidth int) string {
	bg := lipgloss.NewStyle()
	if selected {
		bg = bg.Background(common.ColorSelection)
	}

	mutedStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	if selected {
		mutedStyle = mutedStyle.Background(common.ColorSelection)
	}

	arrowStyle := lipgloss.NewStyle().Foreground(common.ColorSurface2)
	if selected {
		arrowStyle = arrowStyle.Background(common.ColorSelection)
	}

	indent := bg.Render(" ") + arrowStyle.Render("└ ")

	var parts []string

	// Repo chip: "medusa" (single), "medusa +N" (multi), or omitted (no repos).
	if chip := m.renderRepoChip(ws); chip != "" {
		parts = append(parts, mutedStyle.Render(chip))
	}

	// Profile name
	profileName := "Default"
	if ws.Profile != "" {
		profileName = ws.Profile
	}
	parts = append(parts, mutedStyle.Render(profileName))

	// Git changes summary
	root := ws.PrimaryWorktreeRoot()
	if status, ok := m.statusCache[root]; ok && status != nil && !status.Clean {
		gitSummary := formatGitSummary(status)
		if gitSummary != "" {
			gitStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
			if selected {
				gitStyle = gitStyle.Background(common.ColorSelection)
			}
			parts = append(parts, gitStyle.Render(gitSummary))
		}
	} else {
		parts = append(parts, mutedStyle.Render("Clean"))
	}

	// Created day (e.g. "Mon")
	if !ws.Created.IsZero() {
		parts = append(parts, mutedStyle.Render(ws.Created.Format("Mon")))
	}

	sep := mutedStyle.Render(" · ")
	return indent + strings.Join(parts, sep)
}

// renderWorkspaceLine3 renders a muted third line listing all repos.
// Only called when the row is selected and the workspace has >=2 repos.
func (m *Model) renderWorkspaceLine3(ws *data.Workspace, contentWidth int) string {
	if len(ws.Repos) < 2 {
		return ""
	}
	names := make([]string, len(ws.Repos))
	for i, r := range ws.Repos {
		names[i] = r.Name
	}
	sort.Strings(names)

	bg := lipgloss.NewStyle().Background(common.ColorSelection)
	mutedStyle := lipgloss.NewStyle().Foreground(common.ColorMuted).Background(common.ColorSelection)
	arrowStyle := lipgloss.NewStyle().Foreground(common.ColorSurface2).Background(common.ColorSelection)

	indent := bg.Render(" ") + arrowStyle.Render("└ ")
	label := mutedStyle.Render(strings.Join(names, ", "))

	line := indent + label
	return padWithBg(line, contentWidth, bg)
}

// renderRepoChip returns the line-2 repo chip for a workspace.
// - 0 repos: "".
// - 1 repo: "medusa".
// - >=2 repos: "N Repos" (the full list is shown on line 3 when selected).
func (m *Model) renderRepoChip(ws *data.Workspace) string {
	if len(ws.Repos) == 0 {
		return ""
	}
	if len(ws.Repos) == 1 {
		return ws.Repos[0].Name
	}
	return fmt.Sprintf("%d Repos", len(ws.Repos))
}

// formatGitSummary returns a short summary of git changes, e.g. "3M 2A 1?"
func formatGitSummary(status *git.StatusResult) string {
	if status == nil || status.Clean {
		return ""
	}
	var parts []string
	staged := len(status.Staged)
	unstaged := len(status.Unstaged)
	untracked := len(status.Untracked)
	if staged > 0 {
		parts = append(parts, fmt.Sprintf("%d+", staged))
	}
	if unstaged > 0 {
		parts = append(parts, fmt.Sprintf("%dM", unstaged))
	}
	if untracked > 0 {
		parts = append(parts, fmt.Sprintf("%d?", untracked))
	}
	return strings.Join(parts, " ")
}
