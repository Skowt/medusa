package center

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	appPty "github.com/Skowt/medusa/internal/pty"
	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/ui/compositor"
	"github.com/Skowt/medusa/internal/ui/diff"
	"github.com/Skowt/medusa/internal/vterm"
)

// TabID is a unique identifier for a tab that survives slice reordering
type TabID string

// tabIDCounter is used to generate unique tab IDs
var tabIDCounter uint64

// generateTabID creates a new unique tab ID
func generateTabID() TabID {
	id := atomic.AddUint64(&tabIDCounter, 1)
	return TabID(fmt.Sprintf("tab-%d", id))
}

// SelectionState tracks mouse selection state for copy/paste
type SelectionState struct {
	Active    bool // Selection in progress (mouse button down)?
	StartX    int  // Start column (terminal coordinates)
	StartLine int  // Start row (absolute line number, 0 = first scrollback line)
	EndX      int  // End column
	EndLine   int  // End row (absolute line number)
}

// Tab represents a single tab in the center pane
type Tab struct {
	ID              TabID // Unique identifier that survives slice reordering
	Name            string
	Assistant       string
	Workspace       *data.Workspace
	Agent           *appPty.Agent
	SessionName     string
	ClaudeSessionID string
	Detached        bool
	Terminal        *vterm.VTerm // Virtual terminal emulator with scrollback
	DiffViewer      *diff.Model  // Native diff viewer (replaces PTY-based viewer)
	mu              sync.Mutex   // Protects Terminal
	closed          uint32
	closing         uint32
	Running         bool // Whether the agent is actively running
	readerActive    bool // Guard to ensure only one PTY read loop per tab
	// Buffer PTY output to avoid rendering partial screen updates.

	pendingOutput     []byte
	flushScheduled    bool
	lastOutputAt      time.Time
	HookState         string
	Unread            bool
	flushPendingSince time.Time
	ptyRows           int
	ptyCols           int
	ptyMsgCh          chan tea.Msg
	readerCancel      chan struct{}
	// Mouse selection state
	Selection             SelectionState
	selectionGen          uint64
	selectionScrollDir    int
	selectionScrollActive bool
	lastClickTime         time.Time // for double-click detection
	lastClickX            int
	lastClickLine         int

	ptyTraceFile       *os.File
	ptyTraceBytes      int
	ptyTraceClosed     bool
	ptyRestartBackoff  time.Duration
	ptyHeartbeat       int64
	ptyRestartCount    int
	ptyRestartSince    time.Time
	autoRestartAttempt int // tracks auto-restart attempts after session death

	// Per-tab agent settings (configured at tab creation time)
	Isolated                 bool
	AllowUnsandboxedCommands bool
	PermissionMode           string
	Fullscreen               bool // Claude fullscreen TUI mode: mouse is forwarded to Claude.
	FrameRendering           bool // App repaints complete frames; it owns paging but not necessarily mouse input.
	// Codex per-tab policies, persisted so a restart relaunches the tab the
	// way it was started. Empty for every other assistant.
	CodexSandbox string
	CodexAuto    bool

	// Script tabs retain the full shell command (env-prefixed) so Restart
	// can relaunch the same process in a fresh tmux session.
	ScriptFullCmd string

	// Snapshot cache for VTermLayer - avoid recreating snapshot when terminal unchanged
	cachedSnap       *compositor.VTermSnapshot
	cachedVersion    uint64
	cachedShowCursor bool
	monitorSnapAt    time.Time
	monitorDirty     bool
}

func (t *Tab) isClosed() bool {
	if t == nil {
		return true
	}
	return atomic.LoadUint32(&t.closed) == 1 || atomic.LoadUint32(&t.closing) == 1
}

func (t *Tab) markClosing() {
	if t == nil {
		return
	}
	atomic.StoreUint32(&t.closing, 1)
}

func (t *Tab) markClosed() {
	if t == nil {
		return
	}
	atomic.StoreUint32(&t.closed, 1)
	atomic.StoreUint32(&t.closing, 1)
}

// Model is the Bubbletea model for the center pane
type Model struct {
	// State
	workspace            *data.Workspace
	tabsByWorkspace      map[string][]*Tab // tabs per workspace ID
	activeTabByWorkspace map[string]int    // active tab index per workspace
	// Workspaces whose persisted tabs were already restored. Running tabs are
	// recreated asynchronously, so tabsByWorkspace alone can't guard against a
	// second restore arriving before creation lands (e.g. WorkspacesLoaded
	// racing WorkspaceActivated after an unarchive).
	restoredWorkspaces map[string]struct{}
	// restoreOrder remembers, per workspace, the position each tab held when
	// the workspace was last saved, keyed by tmux session name (or display
	// name for a persisted tab that never got a session). Restoring is
	// asynchronous — a tab lands when its agent finishes attaching, and Codex
	// and Claude take different amounts of time — so without this the tab bar
	// came back in completion order rather than creation order.
	restoreOrder         map[string]map[string]int
	focused              bool
	canFocusRight        bool
	monitorMode          bool
	monitorSnapshotCache map[TabID]MonitorTabSnapshot
	monitorSnapshotNext  int
	monitorSnapCh        chan monitorSnapshotRequest
	monitorSnapCancel    func()
	monitorSnapHeartbeat int64
	monitorActiveID      TabID
	tabsRevision         uint64
	monitorTabsRevision  uint64
	monitorTabsCache     []*Tab
	agentManager         *appPty.AgentManager
	monitor              MonitorModel
	msgSink              func(tea.Msg)
	tabEvents            chan tabEvent
	tabActorReady        uint32
	tabActorHeartbeat    int64

	// Info tab (virtual tab for workspace info)
	infoTabActive bool
	infoContent   string
	infoCursor    int

	// Tab strip horizontal scroll (index of the leftmost visible agent tab)
	tabScrollOffset int

	// lastRenderedActiveID is the ID of the active agent tab at the previous
	// render, or "" when the Info tab is active, there are no tabs, or the
	// workspace just changed. renderTabBar asks visibleTabs to pull the
	// viewport to the active tab only when this changes.
	//
	// Compared by ID, not index: closing the active tab shifts a different
	// tab into the same index, and that is a changed active tab. TabID is
	// documented to survive slice reordering, which is exactly what this needs.
	lastRenderedActiveID TabID

	// Layout
	width           int
	height          int
	offsetX         int // X offset from screen left (dashboard width)
	bottomPadding   int // Extra rows to reserve at the bottom (e.g. when terminal pane is hidden)
	showKeymapHints bool

	// Animation
	spinnerFrame int // Current frame for activity spinner animation

	// Config
	config     *config.Config
	styles     common.Styles
	tabHits    []tabHit
	tmuxConfig tmuxConfig

	// Action bar state
	actionBarHits   []actionBarButton
	actionBarY      int // Y position of action bar within content
	copyFeedback    map[copyTarget]uint64
	copySequence    uint64
	clipboardWrite  func(string) error
	copyHover       copyTarget
	copyHoverActive bool
}

// tmuxConfig holds tmux-related configuration
type tmuxConfig struct {
	ServerName string
	ConfigPath string
}

func (m *Model) getTmuxOptions() tmux.Options {
	opts := tmux.DefaultOptions()
	if m.tmuxConfig.ServerName != "" {
		opts.ServerName = m.tmuxConfig.ServerName
	}
	if m.tmuxConfig.ConfigPath != "" {
		opts.ConfigPath = m.tmuxConfig.ConfigPath
	}
	return opts
}

// SetTmuxConfig updates the tmux configuration.
func (m *Model) SetTmuxConfig(serverName, configPath string) {
	m.tmuxConfig.ServerName = serverName
	m.tmuxConfig.ConfigPath = configPath
}

type tabHitKind int

const (
	tabHitTab tabHitKind = iota
	tabHitClose
	tabHitPlus
	tabHitInfo
	tabHitPrev
	tabHitNext
	tabHitNote
	tabHitSessionID
)

// actionBarButtonKind identifies which action bar button was clicked
type actionBarButtonKind int

const (
	actionBarCopyBranch actionBarButtonKind = iota
	actionBarCopyDir
	actionBarOpenIDE
)

type copyTarget int

const (
	copyTargetBranch copyTarget = iota
	copyTargetWorkdir
	copyTargetSessionID
	copyTargetInfoBranch
	copyTargetInfoPath
)

type copyFeedbackExpired struct {
	target     copyTarget
	generation uint64
}

// actionBarButton stores hit region info for an action bar button
type actionBarButton struct {
	kind   actionBarButtonKind
	label  string
	region common.HitRegion
}

type tabHit struct {
	kind   tabHitKind
	index  int
	region common.HitRegion
}

func (m *Model) paneWidth() int {
	if m.width < 1 {
		return 1
	}
	return m.width
}

func (m *Model) contentWidth() int {
	frameX, _ := m.styles.Pane.GetFrameSize()
	width := m.paneWidth() - frameX
	if width < 1 {
		return 1
	}
	return width
}

// ContentWidth returns the content width inside the pane.
func (m *Model) ContentWidth() int {
	return m.contentWidth()
}

// TerminalMetrics holds the computed geometry for the terminal content area.
// This is the single source of truth for terminal positioning and sizing.
type TerminalMetrics struct {
	// For mouse hit-testing (screen coordinates to terminal coordinates)
	ContentStartX int // X offset from pane left edge (border + padding)
	ContentStartY int // Y offset from pane top edge (border + tab bar)

	// Terminal dimensions
	Width  int // Terminal width in columns
	Height int // Terminal height in rows
}

// terminalMetrics computes the terminal content area geometry.
// It preserves the original layout constants while accounting for dynamic help lines and info bar.
func (m *Model) terminalMetrics() TerminalMetrics {
	// These values match the original working implementation
	const (
		borderLeft   = 1
		paddingLeft  = 1
		borderTop    = 1
		tabBarHeight = 2 // tab line + separator line
		baseOverhead = 5 // borders (2) + tab bar (2) + status line reserve (1)
	)

	width := m.contentWidth()
	if width < 1 {
		width = 1
	}
	if width < 10 {
		width = 80
	}
	helpLineCount := 0
	if m.showKeymapHints {
		helpLineCount = len(m.helpLines(width))
	}
	infoBarHeight := m.infoBarHeight()
	height := m.height - baseOverhead - helpLineCount - infoBarHeight - m.bottomPadding
	if height < 5 {
		height = 24
	}

	// Terminal starts after: border + info bar + tab bar
	// Order is: info bar (at top) → tab bar → terminal
	contentStartY := borderTop + infoBarHeight + tabBarHeight

	return TerminalMetrics{
		ContentStartX: borderLeft + paddingLeft,
		ContentStartY: contentStartY,
		Width:         width,
		Height:        height,
	}
}

// New creates a new center pane model
func New(cfg *config.Config) *Model {
	return &Model{
		tabsByWorkspace:      make(map[string][]*Tab),
		activeTabByWorkspace: make(map[string]int),
		restoredWorkspaces:   make(map[string]struct{}),
		restoreOrder:         make(map[string]map[string]int),
		config:               cfg,
		agentManager:         appPty.NewAgentManager(cfg),
		styles:               common.DefaultStyles(),
		tabEvents:            make(chan tabEvent, 4096),
	}
}

// SetCanFocusRight controls whether focus-right hints should be shown.
func (m *Model) SetCanFocusRight(can bool) {
	m.canFocusRight = can
}

// SetMonitorMode controls whether monitor-mode optimizations are active.
func (m *Model) SetMonitorMode(enabled bool) {
	m.monitorMode = enabled
	if !enabled {
		m.StopMonitorSnapshots()
		m.monitorSnapshotCache = nil
		m.monitorSnapshotNext = 0
	} else if m.monitorSnapshotCache == nil {
		m.monitorSnapshotCache = make(map[TabID]MonitorTabSnapshot)
	}
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *Model) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
}

// SetStyles updates the component's styles (for theme changes).
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
	// Propagate to all viewers in tabs
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab != nil {
				tab.mu.Lock()
				applyTerminalTheme(tab.Terminal)
				if tab.DiffViewer != nil {
					tab.DiffViewer.SetStyles(styles)
				}
				tab.mu.Unlock()
			}
		}
	}
}

// SetMsgSink sets a callback for PTY messages.
func (m *Model) SetMsgSink(sink func(tea.Msg)) {
	m.msgSink = sink
}

// TabEvents returns a channel for actor-style tab mutations.
func (m *Model) TabEvents() chan tabEvent {
	return m.tabEvents
}

func (m *Model) isTabActorReady() bool {
	return atomic.LoadUint32(&m.tabActorReady) == 1
}

func (m *Model) setTabActorReady() {
	atomic.StoreUint32(&m.tabActorReady, 1)
}

func (m *Model) noteTabActorHeartbeat() {
	atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().UnixNano())
	if atomic.LoadUint32(&m.tabActorReady) == 0 {
		atomic.StoreUint32(&m.tabActorReady, 1)
	}
}
