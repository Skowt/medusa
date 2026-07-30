package dashboard

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/ui/common"
)

// SpinnerTickMsg is sent to update the spinner animation
type SpinnerTickMsg struct{}

// spinnerInterval is how often the spinner updates
const spinnerInterval = 80 * time.Millisecond

// RowType identifies the type of row in the dashboard
type RowType int

const (
	RowHome      RowType = iota
	RowWorkspace         // workspace entry
	RowCreate            // "+ New Workspace"
	RowSpacer
	RowSectionHeader // section header (user group, archived, orphans)
)

// Row represents a single row in the dashboard
type Row struct {
	Type        RowType
	Workspace   *data.Workspace
	Label       string // for RowSectionHeader: group label, or "archived" / "archived-footer" / "orphans"
	IsUserGroup bool   // true for user-defined group headers (interactive)
	Collapsed   bool   // only meaningful when IsUserGroup && Type == RowSectionHeader
	MemberCount int    // number of hidden children when Collapsed; 0 otherwise
}

// wsButtonAction identifies a workspace action button.
type wsButtonAction int

const (
	btnDuplicate wsButtonAction = iota
	btnGroup
	btnArchive
)

// wsButtonHit is the clickable hit box of one action button on the selected
// workspace row. line is the row-relative display line; x0..x1 is the
// content-relative X range [x0, x1).
type wsButtonHit struct {
	action wsButtonAction
	line   int
	x0, x1 int
}

// toolbarButtonKind identifies toolbar buttons
type toolbarButtonKind int

const (
	toolbarHelp toolbarButtonKind = iota
	toolbarMonitor
	toolbarSettings
	toolbarSkillUsage
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
	wsButtonHits    []wsButtonHit   // clickable action buttons of the currently selected workspace row
	collapsedGroups map[string]bool // Group label ("" = Ungrouped) → collapsed

	// Loading state
	creatingWorkspaces map[string]*data.Workspace // Workspaces currently being created
	deletingWorkspaces map[string]bool            // Workspaces currently being deleted
	spinnerFrame       int                        // Current spinner animation frame
	spinnerActive      bool                       // Whether spinner ticks are active

	// Agent activity state
	activeWorkspaceIDs   map[string]bool   // Workspace IDs with active agents
	workspaceAgentStates map[string]int    // Workspace ID -> agent state (0=idle, 1=running, 2=active)
	unreadWorkspaces     map[string]bool   // Workspace IDs with unread changes
	hookStates           map[string]string // Workspace ID -> last hook event type

	// Styles
	styles common.Styles
}

// New creates a new dashboard model
func New() *Model {
	return &Model{
		workspaces:           []*data.Workspace{},
		rows:                 []Row{},
		statusCache:          make(map[string]*git.StatusResult),
		creatingWorkspaces:   make(map[string]*data.Workspace),
		deletingWorkspaces:   make(map[string]bool),
		activeWorkspaceIDs:   make(map[string]bool),
		workspaceAgentStates: make(map[string]int),
		unreadWorkspaces:     make(map[string]bool),
		hookStates:           make(map[string]string),
		collapsedGroups:      make(map[string]bool),
		cursor:               0,
		focused:              true,
		styles:               common.DefaultStyles(),
	}
}

// SetActiveWorkspaces updates the set of workspaces with active agents.
func (m *Model) SetActiveWorkspaces(active map[string]bool) {
	m.activeWorkspaceIDs = active
}

// SetHookStates updates the per-workspace hook event type for indicator rendering.
func (m *Model) SetHookStates(states map[string]string) {
	m.hookStates = states
}

// MarkUnread flags a workspace as having unread agent output (orange
// highlight) and reports whether it was newly flagged — the caller plays the
// notification sound only then, so one attention event produces at most one
// ping. The workspace the user is currently looking at is skipped: they are
// already watching it.
func (m *Model) MarkUnread(wsID string) bool {
	if wsID == "" || m.unreadWorkspaces[wsID] || wsID == m.selectedWorkspaceID() {
		return false
	}
	m.unreadWorkspaces[wsID] = true
	return true
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

// SetCollapsedGroups replaces the collapsed-groups map (called from App when config loads).
func (m *Model) SetCollapsedGroups(cg map[string]bool) {
	if cg == nil {
		cg = make(map[string]bool)
	}
	m.collapsedGroups = cg
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

	// Determine archived section boundaries
	archivedStart := m.archivedSectionStart()

	mainRowEnd := len(m.rows)
	if archivedStart >= 0 {
		mainRowEnd = archivedStart
	}

	// Pre-render archived section to measure its true height
	var archivedBuf strings.Builder
	if archivedStart >= 0 {
		for i := archivedStart; i < len(m.rows); i++ {
			line := m.renderRow(m.rows[i], i == m.cursor && !m.toolbarFocused)
			archivedBuf.WriteString(line)
			archivedBuf.WriteString("\n")
		}
	}
	archivedRendered := archivedBuf.String()
	archivedHeight := strings.Count(archivedRendered, "\n")

	// The main (non-archived) rows get a scrollable region;
	// the archived section is always pinned to the bottom.
	mainVisibleHeight := visibleHeight - archivedHeight
	if mainVisibleHeight < 1 {
		mainVisibleHeight = 1
	}

	// Adjust scroll offset for cursor within main rows
	cursorLine := m.cursorLineOffset()
	if m.cursor < mainRowEnd {
		if cursorLine < m.scrollOffset {
			m.scrollOffset = cursorLine
		}
		cursorBottom := cursorLine + m.rowLineCount(m.cursor) - 1
		if cursorBottom >= m.scrollOffset+mainVisibleHeight {
			m.scrollOffset = cursorBottom - mainVisibleHeight + 1
		}
	} else {
		// Cursor is in archived section; keep main scroll at bottom
		mainTotalLines := 0
		for i := 0; i < mainRowEnd; i++ {
			mainTotalLines += m.rowLineCount(i)
		}
		maxOffset := mainTotalLines - mainVisibleHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.scrollOffset > maxOffset {
			m.scrollOffset = maxOffset
		}
	}

	// Render main (non-archived) rows with scrolling
	lineOffset := 0
	for i := 0; i < mainRowEnd; i++ {
		lines := m.rowLineCount(i)
		if lineOffset+lines <= m.scrollOffset {
			lineOffset += lines
			continue
		}
		if lineOffset >= m.scrollOffset+mainVisibleHeight {
			break
		}
		line := m.renderRow(m.rows[i], i == m.cursor && !m.toolbarFocused)
		b.WriteString(line)
		b.WriteString("\n")
		lineOffset += lines
	}

	// Clip main content to mainVisibleHeight so it never overflows into
	// the archived section. A multi-line row that starts within bounds but
	// extends past mainVisibleHeight can cause the buffer to be too tall.
	mainContentHeight := strings.Count(b.String(), "\n")
	if mainContentHeight > mainVisibleHeight {
		mainLines := strings.SplitN(b.String(), "\n", mainVisibleHeight+1)
		if len(mainLines) > mainVisibleHeight {
			mainLines = mainLines[:mainVisibleHeight]
		}
		b.Reset()
		b.WriteString(strings.Join(mainLines, "\n"))
		b.WriteString("\n")
		mainContentHeight = mainVisibleHeight
	}

	// Pad between main content and archived section
	padding := visibleHeight - mainContentHeight - archivedHeight
	if padding > 0 {
		b.WriteString(strings.Repeat("\n", padding))
	}

	// Append pre-rendered archived section
	b.WriteString(archivedRendered)

	m.toolbarY = innerHeight - toolbarHeight - helpHeight

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
