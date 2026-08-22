package center

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/perf"
	"github.com/Skowt/medusa/internal/ui/common"
)

// formatScrollPos formats the scroll position for display
func formatScrollPos(offset, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d lines up", offset, total)
}

// View renders the center pane
func (m *Model) View() string {
	defer perf.Time("center_view")()
	var b strings.Builder

	contentWidth := m.contentWidth()
	if contentWidth < 1 {
		contentWidth = 1
	}

	tabs := m.getTabs()

	// Info bar at the very top (only if we have tabs)
	infoBarHeight := m.infoBarHeight()
	if infoBarHeight > 0 {
		b.WriteString(m.renderInfoBar(contentWidth))
		b.WriteString("\n")
		// Track info bar Y position for mouse hit testing (line 0 = top of content)
		m.actionBarY = 0
	}

	// Tab bar (below info bar)
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	// Content
	activeIdx := m.getActiveTabIdx()
	// Auto-select Info tab when there are no agent tabs
	if len(tabs) == 0 && m.workspace != nil {
		m.infoTabActive = true
	}
	if m.infoTabActive {
		b.WriteString(m.renderInfoContent())
	} else if activeIdx < len(tabs) {
		tab := tabs[activeIdx]
		tab.mu.Lock()
		if tab.DiffViewer != nil {
			// Sync focus state with center pane focus
			tab.DiffViewer.SetFocused(m.focused)
			// Render native diff viewer
			b.WriteString(tab.DiffViewer.View())
		} else if tab.Terminal != nil {
			if m.restartingLocked(tab) {
				b.WriteString(m.renderRestartingContent(tab))
			} else {
				tab.Terminal.ShowCursor = m.focused
				// Use VTerm.Render() directly - it uses dirty line caching and delta styles
				b.WriteString(tab.Terminal.Render())
			}

			if status := m.terminalStatusLineLocked(tab); status != "" {
				b.WriteString("\n" + status)
			}
		}
		tab.mu.Unlock()
	}

	// Help bar with styled keys (prefix mode)
	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}

	// Pad to the inner pane height (border excluded), reserving the help lines.
	// buildBorderedPane will use contentHeight = height - 2, so we target that.
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}

	// Build content with help at bottom
	content := b.String()
	helpContent := strings.Join(helpLines, "\n")

	// Count current lines
	contentLines := strings.Split(content, "\n")
	helpLineCount := len(helpLines)

	// Calculate padding needed
	targetContentLines := innerHeight - helpLineCount
	if targetContentLines < 0 {
		targetContentLines = 0
	}

	// Pad or truncate content to targetContentLines
	if len(contentLines) < targetContentLines {
		// Pad with empty lines
		for len(contentLines) < targetContentLines {
			contentLines = append(contentLines, "")
		}
	} else if len(contentLines) > targetContentLines {
		// Truncate
		contentLines = contentLines[:targetContentLines]
	}

	// Combine content and help
	result := strings.Join(contentLines, "\n")
	if helpContent != "" {
		result += "\n" + helpContent
	}

	return result
}

// TabBarView returns the rendered tab bar string.
func (m *Model) TabBarView() string {
	return m.renderTabBar()
}

// HelpLines returns the help lines for the given width, respecting visibility.
func (m *Model) HelpLines(width int) []string {
	if !m.showKeymapHints {
		return nil
	}
	if width < 1 {
		width = 1
	}
	return m.helpLines(width)
}

func (m *Model) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{}

	// Info tab shows settings keybindings instead of agent tab keybindings
	if m.infoTabActive && m.workspace != nil {
		items = append(items,
			m.helpItem("j/k", "navigate"),
			m.helpItem("Enter", "toggle"),
			m.helpItem("r", "rename"),
			m.helpItem("D", "delete"),
		)
		return common.WrapHelpItems(items, contentWidth)
	}

	hasTabs := len(m.getTabs()) > 0
	if m.workspace != nil {
		items = append(items,
			m.helpItem("C-Spc a", "new agent tab"),
		)
	}
	if hasTabs {
		items = append(items,
			m.helpItem("C-Spc x", "close"),
			m.helpItem("C-Spc S", "restart"),
			m.helpItem("C-Spc p", "prev"),
			m.helpItem("C-Spc n", "next"),
			m.helpItem("C-Spc 1-9", "jump tab"),
			m.helpItem("PgUp", "scroll up"),
			m.helpItem("PgDn", "scroll down"),
		)
	}
	return common.WrapHelpItems(items, contentWidth)
}

// renderInfoContent renders the content for the Info tab.
func (m *Model) renderInfoContent() string {
	if m.infoContent != "" {
		return "\n" + m.infoContent
	}
	return "\nNo workspace information available."
}

// TerminalViewport returns the terminal content area coordinates relative to the pane.
// Returns (x, y, width, height) where the terminal content should be rendered.
// This is for layer-based rendering positioning within the bordered pane.
// Uses terminalMetrics() as the single source of truth for geometry.
func (m *Model) TerminalViewport() (x, y, width, height int) {
	tm := m.terminalMetrics()
	return tm.ContentStartX, tm.ContentStartY, tm.Width, tm.Height
}

// terminalStatusLineLocked returns the status line for the active terminal.
// Caller must hold tab.mu.
func (m *Model) terminalStatusLineLocked(tab *Tab) string {
	if tab == nil || tab.Terminal == nil {
		return ""
	}
	if tab.Terminal.IsScrolled() {
		offset, total := tab.Terminal.GetScrollInfo()
		scrollStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorInfo)
		return scrollStyle.Render(" SCROLL: " + formatScrollPos(offset, total) + " ")
	}
	if m.restartingLocked(tab) {
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorInfo).
			Render(" RESTARTING ")
	}
	if tab.Running && !tab.Detached {
		return ""
	}
	status := ""
	hint := ""
	if tab.Detached {
		status = " DETACHED "
	} else if !tab.Running {
		if tab.autoRestartAttempt > 0 && tab.autoRestartAttempt <= tabAutoRestartMax {
			status = fmt.Sprintf(" RESTARTING (%d/%d) ", tab.autoRestartAttempt, tabAutoRestartMax)
		} else {
			status = " STOPPED "
			hint = " Ctrl-a S to restart "
		}
	}
	statusStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(common.ColorBackground).
		Background(common.ColorInfo)
	if tab.Detached {
		statusStyle = statusStyle.Background(common.ColorWarning)
	} else if !tab.Running {
		statusStyle = statusStyle.Background(common.ColorError)
	}
	result := statusStyle.Render(status)
	if hint != "" {
		hintStyle := lipgloss.NewStyle().
			Foreground(common.ColorMuted)
		result += " " + hintStyle.Render(hint)
	}
	return result
}

// activeTerminalStatusLine returns the status line for the active terminal.
func (m *Model) activeTerminalStatusLine() string {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return ""
	}
	tab := tabs[activeIdx]
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return m.terminalStatusLineLocked(tab)
}

// ActiveTerminalStatusLine returns the status line for the active terminal.
func (m *Model) ActiveTerminalStatusLine() string {
	return m.activeTerminalStatusLine()
}

// restartingMaxDisplay bounds how long the restarting placeholder is shown.
// It is cleared as soon as the new agent paints; the bound only covers an
// agent that never paints at all, so the tab falls back to its real state
// instead of claiming a restart is still in progress forever.
const restartingMaxDisplay = 15 * time.Second

// restartingLocked reports whether tab is between agents and should be shown
// as restarting. Caller must hold tab.mu.
func (m *Model) restartingLocked(tab *Tab) bool {
	if tab == nil || !tab.restarting {
		return false
	}
	if !tab.restartingSince.IsZero() && time.Since(tab.restartingSince) > restartingMaxDisplay {
		tab.restarting = false
		return false
	}
	return true
}

// clearRestartingIfPaintedLocked ends the restarting state once the new agent
// has drawn something. The replacement's tmux client repaints an empty pane
// well before the agent itself boots, so arrival of output is not enough — the
// screen has to actually hold something. Caller must hold tab.mu.
func clearRestartingIfPaintedLocked(tab *Tab) {
	if tab == nil || !tab.restarting || tab.Terminal == nil {
		return
	}
	if !tab.Terminal.ScreenIsBlank() {
		tab.restarting = false
	}
}

// renderRestartingContent fills the terminal viewport with a restart notice.
// Caller must hold tab.mu.
func (m *Model) renderRestartingContent(tab *Tab) string {
	tm := m.terminalMetrics()
	width, height := tm.Width, tm.Height
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	label := lipgloss.NewStyle().
		Foreground(common.ColorMuted).
		Render("Restarting " + restartLabel(tab) + "…")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, label)
}

// restartLabel names what is being restarted, for the placeholder.
func restartLabel(tab *Tab) string {
	name := strings.TrimSpace(tab.Assistant)
	if name == "" {
		return "agent"
	}
	if name == "script" {
		return "script"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
