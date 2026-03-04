package app

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/ui/common"
)

func (a *App) centerPaneStyle() lipgloss.Style {
	width := a.layout.CenterWidth()
	height := a.layout.Height()

	style := lipgloss.NewStyle().
		Width(width-2).
		Height(height-2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(common.ColorBorder).
		Padding(0, 1)

	if a.focusedPane == messages.PaneCenter {
		style = style.
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(common.ColorBorderFocused)
	}
	return style
}

// renderCenterPaneContent renders the center pane content when no tabs (raw content, no borders)
func (a *App) renderCenterPaneContent() string {
	if a.showWelcome {
		return a.renderWelcome()
	}

	if a.activeWorkspace != nil {
		return a.renderWorkspaceInfo()
	}

	return "Select a worktree from the dashboard"
}

func (a *App) centerPaneContentOrigin() (x, y int) {
	if a.layout == nil {
		return 0, 0
	}
	frameX, frameY := a.centerPaneStyle().GetFrameSize()
	gapX := 0
	if a.layout.ShowCenter() {
		gapX = a.layout.GapX()
	}
	return a.layout.LeftGutter() + a.layout.DashboardWidth() + gapX + frameX/2, a.layout.TopGutter() + frameY/2
}

func (a *App) goHome() {
	a.showWelcome = true
	a.activeWorkspace = nil
	a.center.SetWorkspace(nil)
	a.sidebar.SetWorkspace(nil)
	a.sidebar.SetGitStatus(nil)
	_ = a.sidebarTerminal.SetWorkspace(nil)
	a.dashboard.ClearActiveRoot()
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
}

// renderWorkspaceInfo renders information about the active workspace (for center pane and Info tab)
func (a *App) renderWorkspaceInfo() string {
	ws := a.activeWorkspace

	label := lipgloss.NewStyle().Foreground(common.ColorMuted)
	value := lipgloss.NewStyle().Foreground(common.ColorForeground)
	on := lipgloss.NewStyle().Foreground(common.ColorSuccess)
	off := lipgloss.NewStyle().Foreground(common.ColorMuted)
	danger := lipgloss.NewStyle().Foreground(common.ColorError)
	cursor := a.center.InfoCursor()
	cursorStyle := lipgloss.NewStyle().Foreground(common.ColorPrimary)

	prefix := func(idx int) string {
		if idx == cursor {
			return cursorStyle.Render("▸ ")
		}
		return "  "
	}

	copyHint := lipgloss.NewStyle().Foreground(common.ColorMuted)

	// Shorten path with ~ for home directory
	displayPath := ws.Root()
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(displayPath, home) {
		displayPath = "~" + displayPath[len(home):]
	}

	var b strings.Builder
	b.WriteString(label.Render("Branch: ") + value.Render(ws.Branch()) + " " + copyHint.Render("[Copy]") + " " + copyHint.Render("[Rename]") + "\n")
	b.WriteString(label.Render("Path:   ") + value.Render(displayPath) + " " + copyHint.Render("[Copy]") + "\n")

	// Settings section
	b.WriteString("\n" + label.Render("Settings") + "\n")

	// Status
	statusStr := "In Progress"
	statusStyle := on
	switch ws.Status {
	case data.StatusStarted, data.StatusNone:
		statusStr = "In Progress"
		statusStyle = on
	case data.StatusBlocked:
		statusStr = "Blocked"
		statusStyle = danger
	case data.StatusMerged:
		statusStr = "Complete"
		statusStyle = lipgloss.NewStyle().Foreground(common.ColorPrimary)
	case data.StatusArchived:
		statusStr = "Archived"
		statusStyle = off
	}
	b.WriteString(prefix(0) + label.Render("Status:  ") + statusStyle.Render(statusStr) + "\n")

	// Profile
	profileStr := "Default"
	if ws.Profile != "" {
		profileStr = ws.Profile
	}
	b.WriteString(prefix(1) + label.Render("Profile: ") + value.Render(profileStr) + "\n")

	// Repos
	if ws.IsMultiRepo() {
		b.WriteString("\n" + label.Render("Repos:") + "\n")
		for _, repo := range ws.Repos {
			baseInfo := ""
			for _, wt := range ws.Worktrees {
				if wt.Base != "" {
					baseInfo = label.Render(fmt.Sprintf(" [%s]", wt.Base))
					break
				}
			}
			b.WriteString(fmt.Sprintf("  %s%s\n", repo.Name, baseInfo))
		}
	}

	// Edit Repos (only for multi-repo workspaces)
	if ws.IsMultiRepo() {
		b.WriteString(prefix(2) + label.Render(fmt.Sprintf("Edit Repos: (%d repos)", len(ws.Repos))) + "\n")
	}

	// Git Changes section
	b.WriteString("\n" + label.Render("Git Changes") + "\n")
	var gitStatus *git.StatusResult
	if a.statusManager != nil {
		gitStatus = a.statusManager.GetLastKnown(ws.PrimaryWorktreeRoot())
	}
	if gitStatus == nil || gitStatus.Clean {
		b.WriteString("  " + on.Render("Working tree clean") + "\n")
	} else {
		renderChanges := func(header string, changes []git.Change) {
			if len(changes) == 0 {
				return
			}
			b.WriteString("  " + label.Render(fmt.Sprintf("%s (%d)", header, len(changes))) + "\n")
			for _, c := range changes {
				b.WriteString("    " + label.Render(c.KindString()) + " " + value.Render(c.Path) + "\n")
			}
		}
		renderChanges("Staged", gitStatus.Staged)
		renderChanges("Unstaged", gitStatus.Unstaged)
		renderChanges("Untracked", gitStatus.Untracked)
	}

	return b.String()
}

// renderWelcome renders the welcome screen
func (a *App) renderWelcome() string {
	content := a.welcomeContent()

	// Center the content in the pane
	width := a.layout.CenterWidth() - 4 // Account for borders/padding
	height := a.layout.Height() - 2

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (a *App) welcomeContent() string {
	logo, logoStyle := a.welcomeLogo()
	var b strings.Builder
	b.WriteString(logoStyle.Render(logo))
	b.WriteString("\n\n")

	activeStyle := lipgloss.NewStyle().Foreground(common.ColorForeground).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)

	addProjectStyle := inactiveStyle
	settingsStyle := inactiveStyle
	if a.centerBtnFocused {
		if a.centerBtnIndex == 0 {
			addProjectStyle = activeStyle
		} else if a.centerBtnIndex == 1 {
			settingsStyle = activeStyle
		}
	}
	addProject := addProjectStyle.Render("[+ Add Workspace]")
	settingsBtn := settingsStyle.Render("[Settings]")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Left, addProject, "  ", settingsBtn))
	b.WriteString("\n")
	if a.config.UI.ShowKeymapHints {
		b.WriteString(a.styles.Help.Render("Dashboard: j/k to move • Enter to select"))
	}
	return b.String()
}

func (a *App) welcomeLogo() (string, lipgloss.Style) {
	logo := `
                            888
                            888
88888b.d88b.   .d88b.     .d888 888  888 .d8888b   8888b.
888 "888 "88b d8P  Y8b d88" 888 888  888 88K          "88b
888  888  888 88888888 888  888 888  888 "Y8888b. .d888888
888  888  888 Y8b.     Y88b 888 Y88b 888      X88 888  888
888  888  888  "Y8888   "Y88888  "Y88888  88888P' "Y888888`

	logoStyle := lipgloss.NewStyle().
		Foreground(common.ColorPrimary).
		Bold(true)
	return logo, logoStyle
}
