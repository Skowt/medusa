package dashboard

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/ui/common"
)

// SpinnerTickMsg is sent to update the spinner animation
type SpinnerTickMsg struct{}

// spinnerInterval is how often the spinner updates
const spinnerInterval = 80 * time.Millisecond

// RowType identifies the type of row in the dashboard
type RowType int

const (
	RowHome RowType = iota
	RowWorkspace       // workspace entry
	RowCreate          // "+ New Workspace"
	RowSpacer
	RowSectionHeader   // status group header
	RowQuickDuplicate  // "+ Quick Duplicate"
)

// Row represents a single row in the dashboard
type Row struct {
	Type         RowType
	Workspace    *data.Workspace
	Label        string         // for RowSectionHeader
	GroupRepos   []data.RepoRef // for RowQuickDuplicate
	GroupProfile string         // for RowQuickDuplicate
}

// toolbarButtonKind identifies toolbar buttons
type toolbarButtonKind int

const (
	toolbarHelp toolbarButtonKind = iota
	toolbarMonitor
	toolbarSettings
)

// toolbarButton tracks a clickable button in the toolbar
type toolbarButton struct {
	kind   toolbarButtonKind
	region common.HitRegion
}

// Model is the Bubbletea model for the dashboard pane
type Model struct {
	// Data
	workspaces  []*data.Workspace
	rows        []Row
	activeRoot  string // Currently active workspace root
	statusCache map[string]*git.StatusResult

	// UI state
	cursor          int
	focused         bool
	width           int
	height          int
	scrollOffset    int
	canFocusRight   bool
	showKeymapHints bool
	toolbarHits     []toolbarButton // Clickable toolbar buttons
	toolbarY        int             // Y position of toolbar in content coordinates
	toolbarFocused  bool            // Whether toolbar actions are focused
	toolbarIndex    int             // Focused toolbar action index
	deleteIconX     int             // X position of delete "x" icon for currently selected row

	// Loading state
	creatingWorkspaces map[string]*data.Workspace // Workspaces currently being created
	deletingWorkspaces map[string]bool            // Workspaces currently being deleted
	spinnerFrame       int                        // Current spinner animation frame
	spinnerActive      bool                       // Whether spinner ticks are active

	// Agent activity state
	activeWorkspaceIDs   map[string]bool // Workspace IDs with active agents
	workspaceAgentStates map[string]int  // Workspace ID -> agent state (0=idle, 1=running, 2=active)
	unreadWorkspaces     map[string]bool // Workspace IDs with unread changes
	tmuxConfirmedActive  map[string]bool // Workspace IDs confirmed active by tmux

	// Styles
	styles common.Styles
}

// New creates a new dashboard model
func New() *Model {
	return &Model{
		workspaces:         []*data.Workspace{},
		rows:               []Row{},
		statusCache:        make(map[string]*git.StatusResult),
		creatingWorkspaces: make(map[string]*data.Workspace),
		deletingWorkspaces: make(map[string]bool),
		activeWorkspaceIDs:   make(map[string]bool),
		workspaceAgentStates: make(map[string]int),
		unreadWorkspaces:     make(map[string]bool),
		tmuxConfirmedActive:  make(map[string]bool),
		cursor:             0,
		focused:            true,
		styles:             common.DefaultStyles(),
	}
}

// SetActiveWorkspaces updates the set of workspaces with active agents.
func (m *Model) SetActiveWorkspaces(active map[string]bool) {
	m.activeWorkspaceIDs = active
}

// SetTmuxConfirmedActive updates the set of workspace IDs confirmed as genuinely
// active by the tmux "esc to interrupt" detection.
func (m *Model) SetTmuxConfirmedActive(active map[string]bool) bool {
	viewedWSID := m.selectedWorkspaceID()

	newUnread := false
	for wsID := range m.tmuxConfirmedActive {
		if !active[wsID] && !m.unreadWorkspaces[wsID] && wsID != viewedWSID {
			m.unreadWorkspaces[wsID] = true
			newUnread = true
		}
	}
	m.tmuxConfirmedActive = active
	return newUnread
}

// SetWorkspaceAgentStates updates the agent state map for workspaces.
func (m *Model) SetWorkspaceAgentStates(states map[string]int) tea.Cmd {
	m.workspaceAgentStates = states
	return m.startSpinnerIfNeeded()
}

// selectedWorkspaceID returns the workspace ID of the currently selected row.
func (m *Model) selectedWorkspaceID() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	row := m.rows[m.cursor]
	if row.Type == RowWorkspace && row.Workspace != nil {
		return string(row.Workspace.ID())
	}
	return ""
}

// MarkRead clears the unread flag for a workspace.
func (m *Model) MarkRead(wsID string) {
	delete(m.unreadWorkspaces, wsID)
}

// InvalidateStatus removes a workspace's cached status.
func (m *Model) InvalidateStatus(root string) {
	delete(m.statusCache, root)
}

// SetCanFocusRight controls whether focus-right hints should be shown.
func (m *Model) SetCanFocusRight(can bool) {
	m.canFocusRight = can
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *Model) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
}

// SetStyles updates the component's styles.
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
}

// Init initializes the dashboard
func (m *Model) Init() tea.Cmd {
	return nil
}

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
			if !isSelectable(m.rows[idx].Type) {
				return m, nil
			}

			// Check if click is on the delete icon
			if idx == m.cursor {
				rowType := m.rows[idx].Type
				if rowType == RowWorkspace {
					borderLeft := 1
					paddingLeft := 0
					contentX := msg.X - borderLeft - paddingLeft
					if contentX >= m.deleteIconX && contentX < m.deleteIconX+3 {
						m.toolbarFocused = false
						return m, m.handleDelete()
					}
				}
			}

			m.toolbarFocused = false
			m.cursor = idx
			return m, m.handleEnter()
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
			if m.cursor >= 0 && m.cursor < len(m.rows) {
				if m.rows[m.cursor].Type == RowWorkspace {
					return m, m.handleRename()
				}
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("R"))):
			return m, func() tea.Msg { return messages.RefreshDashboard{} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("G"))):
			if idx := m.findSelectableRow(len(m.rows)-1, -1); idx != -1 {
				m.cursor = idx
				return m, m.previewCurrentRow()
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
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
			m.MarkRead(string(msg.Workspace.ID()))
		}

	case messages.ShowWelcome:
		m.activeRoot = ""
	}

	return m, common.SafeBatch(cmds...)
}

// View renders the dashboard
func (m *Model) View() string {
	var b strings.Builder

	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	helpHeight := m.helpLineCount()
	toolbarHeight := m.toolbarHeight()
	visibleHeight := innerHeight - toolbarHeight - helpHeight
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	// Adjust scroll offset for multi-line rows
	cursorLine := m.cursorLineOffset()
	if cursorLine < m.scrollOffset {
		m.scrollOffset = cursorLine
	}
	cursorBottom := cursorLine + m.rowLineCount(m.cursor) - 1
	if cursorBottom >= m.scrollOffset+visibleHeight {
		m.scrollOffset = cursorBottom - visibleHeight + 1
	}

	// Render rows accounting for multi-line
	lineOffset := 0
	for i, row := range m.rows {
		lines := m.rowLineCount(i)
		if lineOffset+lines <= m.scrollOffset {
			lineOffset += lines
			continue
		}
		if lineOffset >= m.scrollOffset+visibleHeight {
			break
		}
		line := m.renderRow(row, i == m.cursor && !m.toolbarFocused)
		b.WriteString(line)
		b.WriteString("\n")
		lineOffset += lines
	}

	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := innerHeight - toolbarHeight - helpHeight
	if targetHeight < 0 {
		targetHeight = 0
	}
	padding := targetHeight - contentHeight + 1
	if padding > 0 {
		b.WriteString(strings.Repeat("\n", padding))
		m.toolbarY = targetHeight
	} else {
		m.toolbarY = contentHeight - 1
	}

	toolbar := m.renderToolbar()
	b.WriteString(toolbar)

	if m.showKeymapHints {
		contentWidth := m.width - 3
		if contentWidth < 1 {
			contentWidth = 1
		}
		helpLines := m.helpLines(contentWidth)
		if len(helpLines) > 0 {
			b.WriteString("\n")
			b.WriteString(strings.Join(helpLines, "\n"))
		}
	}

	return b.String()
}

// SetSize sets the dashboard size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.clampScrollOffset()
}

// Focus sets the focus state
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the dashboard is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetWorkspaces sets the workspace list
func (m *Model) SetWorkspaces(workspaces []*data.Workspace) {
	m.workspaces = workspaces
	m.rebuildRows()
	// Keep cursor on the active workspace after re-arrangement
	if m.activeRoot != "" {
		m.moveCursorToRoot(m.activeRoot)
	}
	m.clampScrollOffset()
}

// ScrollInfo returns the scroll state needed to render a scrollbar overlay.
func (m *Model) ScrollInfo() (scrollOffset, totalLines, visible int) {
	total := 0
	for i := range m.rows {
		total += m.rowLineCount(i)
	}
	return m.scrollOffset, total, m.visibleHeight()
}

// visibleHeight returns the number of visible lines in the dashboard
func (m *Model) visibleHeight() int {
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	helpHeight := m.helpLineCount()
	toolbarHeight := m.toolbarHeight()
	visibleHeight := innerHeight - toolbarHeight - helpHeight
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	return visibleHeight
}

// cursorLineOffset returns the line offset of the cursor position
func (m *Model) cursorLineOffset() int {
	offset := 0
	for i := 0; i < m.cursor && i < len(m.rows); i++ {
		offset += m.rowLineCount(i)
	}
	return offset
}

// ClearActiveRoot resets the active workspace selection to "Home".
func (m *Model) ClearActiveRoot() {
	m.activeRoot = ""
}

// moveCursorToRoot moves the dashboard cursor to the row matching the given root.
func (m *Model) moveCursorToRoot(root string) {
	for i, row := range m.rows {
		if row.Type == RowWorkspace && row.Workspace != nil && row.Workspace.Root() == root {
			m.cursor = i
			return
		}
	}
}
