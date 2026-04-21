package dashboard

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/ui/common"
)

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if !m.focused {
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			if cmd := m.handleToolbarClick(msg.X, msg.Y); cmd != nil {
				return m, cmd
			}

			idx, ok := m.rowIndexAt(msg.X, msg.Y)
			if !ok {
				return m, nil
			}
			if idx < 0 || idx >= len(m.rows) {
				return m, nil
			}
			if !isSelectable(m.rows[idx]) {
				return m, nil
			}

			// Check if click is on the delete or duplicate icons
			if idx == m.cursor {
				rowType := m.rows[idx].Type
				if rowType == RowWorkspace {
					borderLeft := 1
					paddingLeft := 0
					contentX := msg.X - borderLeft - paddingLeft
					if contentX >= m.duplicateIconX && contentX < m.duplicateIconX+2 {
						m.toolbarFocused = false
						return m, m.handleDuplicate()
					}
					if contentX >= m.groupIconX && contentX < m.groupIconX+2 {
						m.toolbarFocused = false
						return m, m.handleSetGroup()
					}
					if contentX >= m.deleteIconX && contentX < m.deleteIconX+2 {
						m.toolbarFocused = false
						return m, m.handleDelete()
					}
				}
			}

			if m.rows[idx].Type == RowSectionHeader && m.rows[idx].IsUserGroup {
				m.toolbarFocused = false
				m.cursor = idx
				return m, m.handleToggleCollapse()
			}

			m.toolbarFocused = false
			m.cursor = idx
			return m, m.handleClick()
		}

	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}

		toolbarItems := m.toolbarItems()
		if m.toolbarFocused {
			if len(toolbarItems) == 0 {
				m.toolbarFocused = false
				break
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
				return m, m.previewCurrentRow()
			case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
				// Already at bottom
			case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
				return m, m.toolbarCommand(toolbarItems[m.toolbarIndex].kind)
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			last := m.findSelectableRow(len(m.rows)-1, -1)
			if last != -1 && m.cursor == last && len(toolbarItems) > 0 {
				m.toolbarFocused = true
				m.toolbarIndex = 0
				return m, m.previewCurrentRow()
			} else {
				m.moveCursor(1)
				return m, m.previewCurrentRow()
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.moveCursor(-1)
			return m, m.previewCurrentRow()
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown", "ctrl+d"))):
			delta := m.visibleHeight() / 2
			if delta < 1 {
				delta = 1
			}
			m.moveCursor(delta)
			return m, m.previewCurrentRow()
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgup", "ctrl+u"))):
			delta := m.visibleHeight() / 2
			if delta < 1 {
				delta = 1
			}
			m.moveCursor(-delta)
			return m, m.previewCurrentRow()
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			return m, m.handleEnter()
		case key.Matches(msg, key.NewBinding(key.WithKeys("D"))):
			return m, m.handleDelete()
		case key.Matches(msg, key.NewBinding(key.WithKeys("P"))):
			return m, m.handleSetProfile()
		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			return m, m.handleRename()
		case key.Matches(msg, key.NewBinding(key.WithKeys("S"))):
			return m, m.handleToggleStatus()
		case key.Matches(msg, key.NewBinding(key.WithKeys("R"))):
			return m, func() tea.Msg { return messages.RefreshDashboard{} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			return m, m.handleSetGroup()
		case key.Matches(msg, key.NewBinding(key.WithKeys("+"))):
			return m, m.handleDuplicate()
		case key.Matches(msg, key.NewBinding(key.WithKeys("space", "l", "h"))):
			if cmd := m.handleToggleCollapse(); cmd != nil {
				return m, cmd
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("G", "end"))):
			if idx := m.findSelectableRow(len(m.rows)-1, -1); idx != -1 {
				m.cursor = idx
				return m, m.previewCurrentRow()
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("home"))):
			if idx := m.findSelectableRow(0, 1); idx != -1 {
				m.cursor = idx
				return m, m.previewCurrentRow()
			}
		}

	case SpinnerTickMsg:
		if len(m.creatingWorkspaces) > 0 || len(m.deletingWorkspaces) > 0 || m.hasActiveAgents() {
			m.spinnerFrame++
			cmds = append(cmds, m.tickSpinner())
		} else {
			m.spinnerActive = false
		}

	case messages.WorkspacesLoaded:
		m.SetWorkspaces(msg.Workspaces)

	case messages.GitStatusResult:
		if msg.Err == nil {
			m.statusCache[msg.Root] = msg.Status
		}

	case messages.WorkspaceActivated:
		if msg.Workspace != nil {
			m.activeRoot = msg.Workspace.Root()
			m.MarkRead(string(msg.Workspace.ID()))
			m.moveCursorToRoot(msg.Workspace.Root())
		}

	case messages.WorkspacePreviewed:
		if msg.Workspace != nil {
			m.activeRoot = msg.Workspace.Root()
			// MarkRead is handled by app with a delay to avoid
			// clearing unread when quickly scrolling through workspaces.
		}

	case messages.ShowWelcome:
		m.activeRoot = ""
	}

	return m, common.SafeBatch(cmds...)
}
