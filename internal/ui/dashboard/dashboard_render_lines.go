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
	nameIndent   = 3 // width of the leading " ● " prefix on line 1
	footerIndent = 3 // left indent of the action footer; equals width of " └ "
)

// detailIndent returns the " └ " tree connector prefix used by every detail
// line hanging off a workspace name (metadata, repo list, action footer). The
// connector is drawn in a color that stays visible on the selection
// background so it is not swallowed when the row is selected.
func detailIndent(selected bool) string {
	bg := lipgloss.NewStyle()
	arrowFg := common.ColorSurface2
	if selected {
		bg = bg.Background(common.ColorSelection)
		arrowFg = common.ColorMuted // visible against the selection background
	}
	arrowStyle := lipgloss.NewStyle().Foreground(arrowFg)
	if selected {
		arrowStyle = arrowStyle.Background(common.ColorSelection)
	}
	return bg.Render(" ") + arrowStyle.Render("└ ")
}

// wsButtonDefs is the ordered set of action buttons and their labels.
var wsButtonDefs = []struct {
	action wsButtonAction
	label  string
}{
	{btnDuplicate, "[dupe]"},
	{btnGroup, "[group]"},
	{btnArchive, "[archive]"},
}

// workspacePending reports whether the workspace is mid-create or mid-delete
// (its row shows a spinner + status text instead of wrapping the name).
func (m *Model) workspacePending(ws *data.Workspace) bool {
	if m.deletingWorkspaces[ws.Root()] {
		return true
	}
	_, creating := m.creatingWorkspaces[ws.Root()]
	return creating
}

// nameChunks returns the display lines of the workspace name (text only, no
// styling). An unselected or pending row gets a single ellipsized line; a
// selected row wraps up to maxNameLines. Both the renderer and rowLineCount
// call this so their line counts cannot drift.
func (m *Model) nameChunks(ws *data.Workspace, selected bool, contentWidth int) []string {
	width := contentWidth - nameIndent
	if width < 1 {
		width = 1
	}
	if !selected || m.workspacePending(ws) {
		return []string{truncateRunes([]rune(ws.Name), width)}
	}
	return wrapName(ws.Name, width, maxNameLines)
}

// footerButtonHits returns each button's footer-relative hit box (line 0).
func footerButtonHits() []wsButtonHit {
	x := footerIndent
	hits := make([]wsButtonHit, 0, len(wsButtonDefs))
	for _, b := range wsButtonDefs {
		w := lipgloss.Width(b.label)
		hits = append(hits, wsButtonHit{action: b.action, line: 0, x0: x, x1: x + w})
		x += w + 1 // one space between buttons
	}
	return hits
}

// renderFooterLine renders the action button row for a selected active row.
// The footer only shows on the selected row, so it always uses the selected
// tree connector and aligns its buttons under the metadata text above.
func (m *Model) renderFooterLine() string {
	bg := lipgloss.NewStyle().Background(common.ColorSelection)
	btnStyle := lipgloss.NewStyle().Foreground(common.ColorMuted).Background(common.ColorSelection)
	parts := []string{detailIndent(true)}
	for i, b := range wsButtonDefs {
		if i > 0 {
			parts = append(parts, bg.Render(" "))
		}
		parts = append(parts, btnStyle.Render(b.label))
	}
	return strings.Join(parts, "")
}

// renderWorkspaceNameLines renders the styled name line(s): the indicator +
// first name chunk (plus any status text), then indented continuation lines.
func (m *Model) renderWorkspaceNameLines(ws *data.Workspace, selected bool, contentWidth int) []string {
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
		case hookState == "NotificationElicitation":
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

	chunks := m.nameChunks(ws, selected, contentWidth)
	contPrefixStyle := lipgloss.NewStyle()
	if selected {
		contPrefixStyle = contPrefixStyle.Background(common.ColorSelection)
	}
	contPrefix := contPrefixStyle.Render(strings.Repeat(" ", nameIndent))

	lines := make([]string, 0, len(chunks))
	lines = append(lines, prefix+style.Render(chunks[0])+statusText)
	for _, c := range chunks[1:] {
		lines = append(lines, contPrefix+style.Render(c))
	}
	return lines
}

// renderWorkspaceLine2: profile · git changes · created day
func (m *Model) renderWorkspaceLine2(ws *data.Workspace, selected bool, contentWidth int) string {
	mutedStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	if selected {
		mutedStyle = mutedStyle.Background(common.ColorSelection)
	}

	indent := detailIndent(selected)

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

	indent := detailIndent(true)
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
