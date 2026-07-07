package center

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// handleActionBarClickFromMsg converts screen coordinates and checks for action bar clicks.
func (m *Model) handleActionBarClickFromMsg(msg tea.MouseClickMsg) tea.Cmd {
	// Convert screen coordinates to content coordinates
	const (
		borderTop   = 1
		borderLeft  = 1
		paddingLeft = 1
	)

	contentX := msg.X - m.offsetX - borderLeft - paddingLeft
	contentY := msg.Y - borderTop

	if contentX < 0 || contentY < 0 {
		return nil
	}

	return m.handleActionBarClick(contentX, contentY)
}

// handleInfoContentClick handles clicks on [Copy] and [Rename] buttons in the info tab.
func (m *Model) handleInfoContentClick(msg tea.MouseClickMsg) tea.Cmd {
	if m.infoContent == "" || m.workspace == nil {
		return nil
	}

	const (
		borderTop   = 1
		borderLeft  = 1
		paddingLeft = 1
	)

	localX := msg.X - m.offsetX - borderLeft - paddingLeft
	localY := msg.Y - borderTop

	// Info content starts after: info bar + tab bar (1) + separator (1) + leading \n from renderInfoContent
	infoBarHeight := m.infoBarHeight()
	contentStartY := infoBarHeight + 2 + 1 // tab bar + separator + leading \n
	infoY := localY - contentStartY

	if localX < 0 || infoY < 0 {
		return nil
	}

	lines := strings.Split(m.infoContent, "\n")
	if infoY >= len(lines) {
		return nil
	}

	// Find clickable buttons dynamically by scanning the clicked line
	if infoY >= len(lines) {
		return nil
	}

	ws := m.workspace
	stripped := ansi.Strip(lines[infoY])

	type button struct {
		prefix string // line must contain this prefix to match
		text   string
		action func() tea.Msg
	}
	buttons := []button{
		{"Branch:", "[Copy]", func() tea.Msg {
			if err := common.CopyToClipboard(ws.Branch()); err != nil {
				return messages.Toast{Message: "Failed to copy: " + err.Error(), Level: messages.ToastError}
			}
			return messages.Toast{Message: "Copied branch to clipboard", Level: messages.ToastInfo}
		}},
		{"Branch:", "[Rename]", func() tea.Msg {
			return messages.ShowRenameWorkspaceDialog{Workspace: ws}
		}},
		{"Path:", "[Copy]", func() tea.Msg {
			if err := common.CopyToClipboard(ws.Root()); err != nil {
				return messages.Toast{Message: "Failed to copy: " + err.Error(), Level: messages.ToastError}
			}
			return messages.Toast{Message: "Copied path to clipboard", Level: messages.ToastInfo}
		}},
	}

	for _, btn := range buttons {
		if !strings.Contains(stripped, btn.prefix) {
			continue
		}
		idx := strings.Index(stripped, btn.text)
		if idx < 0 {
			continue
		}
		// Byte-offset → column. The branch name or displayed path preceding
		// the button can contain multi-byte runes (accented chars, CJK,
		// emoji), so idx is a byte index and must be converted to display
		// columns through lipgloss.Width before comparing against localX.
		region := common.HitRegion{
			X:      lipgloss.Width(stripped[:idx]),
			Y:      infoY,
			Width:  lipgloss.Width(btn.text),
			Height: 1,
		}
		if region.Contains(localX, infoY) {
			return btn.action
		}
	}

	return nil
}
