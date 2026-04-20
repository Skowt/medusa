package dashboard

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/messages"
)

// handleKeypress routes a KeyPressMsg to the right action.
// Returns (handled, cmd): handled=true means the switch in Update should return immediately.
func (m *Model) handleKeypress(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	toolbarItems := m.toolbarItems()
	if m.toolbarFocused {
		return m.handleToolbarKeypress(msg, toolbarItems)
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
		last := m.findSelectableRow(len(m.rows)-1, -1)
		if last != -1 && m.cursor == last && len(toolbarItems) > 0 {
			m.toolbarFocused = true
			m.toolbarIndex = 0
			return true, m.previewCurrentRow()
		}
		m.moveCursor(1)
		return true, m.previewCurrentRow()
	case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
		m.moveCursor(-1)
		return true, m.previewCurrentRow()
	case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown", "ctrl+d"))):
		delta := m.visibleHeight() / 2
		if delta < 1 {
			delta = 1
		}
		m.moveCursor(delta)
		return true, m.previewCurrentRow()
	case key.Matches(msg, key.NewBinding(key.WithKeys("pgup", "ctrl+u"))):
		delta := m.visibleHeight() / 2
		if delta < 1 {
			delta = 1
		}
		m.moveCursor(-delta)
		return true, m.previewCurrentRow()
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		if row := m.currentRow(); row != nil && row.Type == RowGroupHeader {
			return true, m.handleToggleGroup()
		}
		return true, m.handleEnter()
	case key.Matches(msg, key.NewBinding(key.WithKeys("l", "right"))):
		if row := m.currentRow(); row != nil && row.Type == RowGroupHeader && !row.GroupExpanded {
			return true, m.handleToggleGroup()
		}
		return false, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("h", "left"))):
		// If on an expanded group header, collapse it.
		if row := m.currentRow(); row != nil && row.Type == RowGroupHeader && row.GroupExpanded {
			return true, m.handleToggleGroup()
		}
		// If on a grouped workspace, jump up to the parent group header and collapse it.
		if row := m.currentRow(); row != nil && row.Type == RowWorkspace && row.GroupName != "" {
			if idx := m.findParentGroupHeader(m.cursor); idx != -1 {
				m.cursor = idx
				return true, m.handleToggleGroup()
			}
		}
		return false, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("N"))):
		return true, m.handleCreateGroup()
	case key.Matches(msg, key.NewBinding(key.WithKeys("D"))):
		if row := m.currentRow(); row != nil && row.Type == RowGroupHeader {
			return true, m.handleDeleteGroup()
		}
		return true, m.handleDelete()
	case key.Matches(msg, key.NewBinding(key.WithKeys("P"))):
		return true, m.handleSetProfile()
	case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
		if row := m.currentRow(); row != nil {
			switch row.Type {
			case RowWorkspace:
				return true, m.handleRename()
			case RowGroupHeader:
				return true, m.handleRenameGroup()
			}
		}
		return false, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
		if row := m.currentRow(); row != nil && row.Type == RowWorkspace {
			return true, m.handleAssignWorkspaceGroup()
		}
		if idx := m.findSelectableRow(0, 1); idx != -1 {
			m.cursor = idx
			return true, m.previewCurrentRow()
		}
		return false, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("S"))):
		return true, m.handleToggleStatus()
	case key.Matches(msg, key.NewBinding(key.WithKeys("R"))):
		return true, func() tea.Msg { return messages.RefreshDashboard{} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("G"))):
		if idx := m.findSelectableRow(len(m.rows)-1, -1); idx != -1 {
			m.cursor = idx
			return true, m.previewCurrentRow()
		}
		return false, nil
	}
	return false, nil
}

// handleToolbarKeypress routes keys when the toolbar is focused.
func (m *Model) handleToolbarKeypress(msg tea.KeyPressMsg, toolbarItems []toolbarItem) (bool, tea.Cmd) {
	if len(toolbarItems) == 0 {
		m.toolbarFocused = false
		return false, nil
	}
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("left", "h"))):
		m.toolbarIndex = (m.toolbarIndex - 1 + len(toolbarItems)) % len(toolbarItems)
	case key.Matches(msg, key.NewBinding(key.WithKeys("right", "l"))):
		m.toolbarIndex = (m.toolbarIndex + 1) % len(toolbarItems)
	case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
		m.toolbarFocused = false
		if last := m.findSelectableRow(len(m.rows)-1, -1); last != -1 {
			m.cursor = last
		}
		return true, m.previewCurrentRow()
	case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
		// Already at bottom; no-op.
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		return true, m.toolbarCommand(toolbarItems[m.toolbarIndex].kind)
	}
	return true, nil
}
