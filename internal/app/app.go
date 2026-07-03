package app

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/data"
	"github.com/Skowt/medusa/internal/git"
	"github.com/Skowt/medusa/internal/hooks"
	"github.com/Skowt/medusa/internal/logging"
	"github.com/Skowt/medusa/internal/messages"
	"github.com/Skowt/medusa/internal/permissions"
	"github.com/Skowt/medusa/internal/process"
	"github.com/Skowt/medusa/internal/supervisor"
	"github.com/Skowt/medusa/internal/tmux"
	"github.com/Skowt/medusa/internal/ui/center"
	"github.com/Skowt/medusa/internal/ui/common"
	"github.com/Skowt/medusa/internal/ui/compositor"
	"github.com/Skowt/medusa/internal/ui/dashboard"
	"github.com/Skowt/medusa/internal/ui/layout"
	"github.com/Skowt/medusa/internal/ui/sidebar"
	"github.com/Skowt/medusa/internal/update"
)

// DialogID constants
const (
	DialogCreateWorkspace     = "create_workspace"
	DialogDeleteWorkspace     = "delete_workspace"
	DialogCustomizeTab        = "customize_tab"
	DialogQuit                = "quit"
	DialogCleanupTmux         = "cleanup_tmux"
	DialogSetProfile          = "set_profile"
	DialogRenameWorkspace     = "rename_workspace"
	DialogRenameProfile       = "rename_profile"
	DialogCreateProfile       = "create_profile"
	DialogDeleteProfile       = "delete_profile"
	DialogCommit              = "commit"
	DialogSelectBranchMode    = "select_branch_mode"
	DialogCustomBranch        = "custom_branch"
	DialogAddRepos            = "add_repos"
	DialogAddReposToWorkspace = "add_repos_to_workspace"
	DialogSelectRecentRepos   = "select_recent_repos"
	DialogCloseTab            = "close_tab"
	DialogSetProfileForCreate = "set_profile_for_create"
	DialogQuickDuplicate      = "quick_duplicate"
	DialogArchiveWorkspace    = "archive_workspace"
	DialogArchivedWorkspace   = "archived_workspace"
	DialogSetNote             = "set_note"
	DialogSetWorkspaceGroup   = "set_workspace_group"
	DialogSetGroupForCreate   = "set_group_for_create"
	DialogRenameGroup         = "rename_group"
	DialogDeleteGroup         = "delete_group"
)

// Prefix mode constants
const (
	prefixTimeout = 1500 * time.Millisecond
)

// prefixTimeoutMsg is sent when the prefix mode timer expires
type prefixTimeoutMsg struct {
	token int
}

// markReadMsg is sent after a delay to mark a previewed workspace as read.
// The token is compared to the current markReadToken to avoid stale marks.
type markReadMsg struct {
	token int
	wsID  string
}

// App is the root Bubbletea model
type App struct {
	// Configuration
	config     *config.Config
	registry   *data.Registry
	workspaces *data.WorkspaceStore

	// State
	allWorkspaces   []*data.Workspace
	recents         *data.RecentsStore
	activeWorkspace *data.Workspace
	gitStatusRR     int // round-robin index for git status polling
	// gitStatusInFlight deduplicates concurrent git.GetStatus calls per workspace
	// root. Large repos (lots of untracked files) can take hundreds of ms per call,
	// and without this guard, overlapping refresh triggers (tab switch, file-watcher
	// event, ticker) pile up subprocesses that land back in the Update loop in a
	// burst and cause visible UI stutter.
	//
	// Accessed from both the Update goroutine (on request) and the fetch goroutine
	// (on clear), so it needs a mutex.
	gitStatusInFlight   map[string]bool
	gitStatusInFlightMu sync.Mutex
	focusedPane         messages.PaneType
	showWelcome         bool
	monitorMode         bool
	monitorFilter       string
	monitorLayoutKey    string
	monitorCanvas       *compositor.Canvas

	// Update state
	updateAvailable  *update.CheckResult // nil if no update or dismissed
	version          string
	commit           string
	buildDate        string
	upgradeRunning   bool
	updateToastShown bool // guards against re-emitting the update-available toast

	// Button focus state for welcome/workspace info screens
	centerBtnFocused bool
	centerBtnIndex   int

	// UI Components
	layout          *layout.Manager
	dashboard       *dashboard.Model
	center          *center.Model
	sidebar         *sidebar.TabbedSidebar
	sidebarTerminal *sidebar.TerminalModel
	dialog          *common.Dialog
	filePicker      *common.FilePicker
	settingsDialog  *common.SettingsDialog
	themeDialog     *common.ThemeDialog
	soundPicker     *common.SoundPicker
	idePicker       *common.IDEPicker

	// Overlays
	helpOverlay     *common.HelpOverlay
	toast           *common.ToastModel
	profileManager  *common.ProfileManager
	creationOverlay *common.ProgressOverlay

	// Dialog context
	dialogWorkspace     *data.Workspace
	dialogDefaultName   string
	dialogWorkspaceRoot string             // For commit dialog
	dialogProfile       string             // For rename/delete profile dialogs
	dialogCloseTabIdx   int                // For close tab confirmation
	dialogRecents       []data.RecentEntry // Snapshot of recents for select dialog

	// Process management
	scripts *process.ScriptRunner

	// Git status management
	statusManager  *git.StatusManager
	fileWatcher    *git.FileWatcher
	fileWatcherCh  chan messages.FileWatcherEvent
	fileWatcherErr error

	// Permission watcher
	permissionWatcher  *permissions.PermissionWatcher
	permWatcherCh      chan messages.PermissionWatcherEvent
	pendingPermissions []common.PendingPermission
	permissionsDialog  *common.PermissionsDialog
	permissionsEditor  *common.PermissionsEditor

	// Layout
	width, height int
	keymap        KeyMap
	styles        common.Styles
	canvas        *lipgloss.Canvas
	// Lifecycle
	ready        bool
	quitting     bool
	err          error
	shutdownOnce sync.Once
	ctx          context.Context
	supervisor   *supervisor.Supervisor
	// Prefix mode (leader key)
	prefixActive bool
	prefixToken  int

	tmuxSyncToken   int
	tmuxOptions     tmux.Options
	tmuxAvailable   bool
	tmuxCheckDone   bool
	tmuxInstallHint string

	// Hooks socket server (Claude Code lifecycle events)
	hooksServer         *hooks.Server
	hookWorkspaceStates map[string]hooks.EventType
	// hookLastStamp records the timestamp (and clear/active kind) of the last
	// hook event applied per workspace, so out-of-order socket delivery can be
	// rejected. See shouldApplyHookEvent.
	hookLastStamp map[string]hookEventStamp

	// Auto-start agent
	pendingAutoLaunch  string // workspace root for post-creation auto-launch
	pendingAgentLaunch string // workspace root for activation auto-launch

	// Profile gate
	pendingProfileLaunch     string
	pendingProfileLaunchRoot string

	// Delayed mark-read for previewed workspaces
	markReadToken int

	// Workspace persistence debounce
	dirtyWorkspaces map[string]bool
	persistToken    int

	// Terminal capabilities
	keyboardEnhancements tea.KeyboardEnhancementsMsg

	// Perf tracking
	lastInputAt         time.Time
	pendingInputLatency bool

	// Chrome caches for layer-based rendering
	dashboardChrome  *compositor.ChromeCache
	centerChrome     *compositor.ChromeCache
	sidebarChrome    *compositor.ChromeCache
	dashboardContent drawableCache
	dashboardBorders borderCache
	sidebarTabBar    drawableCache
	sidebarContent   drawableCache
	sidebarBorders   borderCache
	terminalTabBar   drawableCache
	terminalStatus   drawableCache
	terminalHelp     drawableCache
	terminalBorders  borderCache
	terminalToggleX  int // X position of terminal collapse/expand toggle button
	terminalToggleY  int // Y position of terminal collapse/expand toggle button
	centerTabBar     drawableCache
	centerStatus     drawableCache
	centerActionBar  drawableCache
	centerHelp       drawableCache
	centerBorders    borderCache

	// External message pump (for PTY readers)
	externalMsgs     chan tea.Msg
	externalCritical chan tea.Msg
	externalSender   func(tea.Msg)
	externalOnce     sync.Once
}

type drawableCache struct {
	content  string
	x, y     int
	drawable *compositor.StringDrawable
}

func (c *drawableCache) get(content string, x, y int) *compositor.StringDrawable {
	if content == "" {
		c.content = ""
		c.drawable = nil
		return nil
	}
	if c.drawable != nil && c.content == content && c.x == x && c.y == y {
		return c.drawable
	}
	c.content = content
	c.x = x
	c.y = y
	c.drawable = compositor.NewStringDrawable(content, x, y)
	return c.drawable
}

type borderCache struct {
	x, y      int
	width     int
	height    int
	focused   bool
	themeID   common.ThemeID
	drawables []*compositor.StringDrawable
}

func (c *borderCache) get(x, y, width, height int, focused bool) []*compositor.StringDrawable {
	themeID := common.GetCurrentTheme().ID
	if c.drawables != nil &&
		c.x == x && c.y == y &&
		c.width == width && c.height == height &&
		c.focused == focused &&
		c.themeID == themeID {
		return c.drawables
	}
	c.x = x
	c.y = y
	c.width = width
	c.height = height
	c.focused = focused
	c.themeID = themeID
	c.drawables = borderDrawables(x, y, width, height, focused)
	return c.drawables
}

func (a *App) markInput() {
	a.lastInputAt = time.Now()
	a.pendingInputLatency = true
}

// New creates a new App instance
func New(version, commit, date string) (*App, error) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		return nil, err
	}
	applyTmuxEnvFromConfig(cfg, false)
	tmuxOpts := tmux.DefaultOptions()

	// Ensure directories exist
	if err := cfg.Paths.EnsureDirectories(); err != nil {
		return nil, err
	}

	registry := data.NewRegistry(cfg.Paths.RegistryPath)
	workspaces := data.NewWorkspaceStore(cfg.Paths.MetadataRoot)
	recents := data.NewRecentsStore(cfg.Paths.RecentsPath)
	scripts := process.NewScriptRunner(cfg.PortStart, cfg.PortRangeSize)

	// Create status manager (callback will be nil, we use it for caching only)
	statusManager := git.NewStatusManager(nil)

	// Create file watcher event channel
	fileWatcherCh := make(chan messages.FileWatcherEvent, 10)

	// Create file watcher with callback that sends to channel
	fileWatcher, fileWatcherErr := git.NewFileWatcher(func(root string) {
		select {
		case fileWatcherCh <- messages.FileWatcherEvent{Root: root}:
		default:
			// Channel full, drop event (will catch on next change)
		}
	})
	if fileWatcherErr != nil {
		logging.Warn("File watcher disabled: %v", fileWatcherErr)
		fileWatcher = nil
	}

	// Create permission watcher event channel
	permWatcherCh := make(chan messages.PermissionWatcherEvent, 10)

	ctx := context.Background()
	app := &App{
		config:              cfg,
		registry:            registry,
		workspaces:          workspaces,
		recents:             recents,
		scripts:             scripts,
		statusManager:       statusManager,
		fileWatcher:         fileWatcher,
		fileWatcherCh:       fileWatcherCh,
		fileWatcherErr:      fileWatcherErr,
		permWatcherCh:       permWatcherCh,
		layout:              layout.NewManager(),
		dashboard:           dashboard.New(),
		center:              center.New(cfg),
		sidebar:             sidebar.NewTabbedSidebar(),
		sidebarTerminal:     sidebar.NewTerminalModel(),
		helpOverlay:         common.NewHelpOverlay(),
		toast:               common.NewToastModel(),
		focusedPane:         messages.PaneDashboard,
		showWelcome:         true,
		keymap:              DefaultKeyMap(),
		dashboardChrome:     &compositor.ChromeCache{},
		centerChrome:        &compositor.ChromeCache{},
		sidebarChrome:       &compositor.ChromeCache{},
		version:             version,
		commit:              commit,
		buildDate:           date,
		externalMsgs:        make(chan tea.Msg, 4096),
		externalCritical:    make(chan tea.Msg, 512),
		ctx:                 ctx,
		tmuxOptions:         tmuxOpts,
		hookWorkspaceStates: make(map[string]hooks.EventType),
		hookLastStamp:       make(map[string]hookEventStamp),
		dirtyWorkspaces:     make(map[string]bool),
	}
	app.supervisor = supervisor.New(ctx)
	app.installSupervisorErrorHandler()
	// Route PTY messages through the app-level pump.
	app.center.SetMsgSink(app.enqueueExternalMsg)
	app.sidebarTerminal.SetMsgSink(app.enqueueExternalMsg)
	// Apply saved theme before creating styles
	common.SetCurrentTheme(common.ThemeID(cfg.UI.Theme))
	app.styles = common.DefaultStyles()
	// Propagate styles to all components (they were created with default theme)
	app.dashboard.SetStyles(app.styles)
	app.sidebar.SetStyles(app.styles)
	app.sidebarTerminal.SetStyles(app.styles)
	app.center.SetStyles(app.styles)
	app.toast.SetStyles(app.styles)
	app.helpOverlay.SetStyles(app.styles)
	app.dashboard.SetCollapsedGroups(cfg.UI.CollapsedGroups)
	app.layout.SetSidebarHidden(cfg.UI.HideSidebar)
	app.layout.SetTerminalHidden(cfg.UI.HideTerminal)
	app.setKeymapHintsEnabled(cfg.UI.ShowKeymapHints)
	// Propagate tmux config to components
	app.center.SetTmuxConfig(tmuxOpts.ServerName, tmuxOpts.ConfigPath)
	app.supervisor.Start("center.tab_actor", app.center.RunTabActor, supervisor.WithRestartPolicy(supervisor.RestartAlways))
	if app.statusManager != nil {
		app.supervisor.Start("git.status_manager", app.statusManager.Run)
	}
	if fileWatcher != nil {
		app.supervisor.Start("git.file_watcher", fileWatcher.Run, supervisor.WithBackoff(500*time.Millisecond))
	}

	// Create permission watcher if global permissions is enabled
	if cfg.UI.GlobalPermissions {
		app.initPermissionWatcher()
	}

	// Inject hooks into all profiles and start the hooks socket server
	_ = config.InjectHooksIntoAllProfiles(cfg.Paths.ProfilesRoot, cfg.Paths.HooksDir)
	app.initHooksServer()

	// Initialize focus state on all components (dashboard is the default focus)
	app.focusPane(messages.PaneDashboard)

	return app, nil
}

// Init initializes the application
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.loadWorkspaces(),
		a.dashboard.Init(),
		a.center.Init(),
		a.sidebar.Init(),
		a.sidebarTerminal.Init(),
		a.startGitStatusTicker(),
		a.startPTYWatchdog(),
		a.startTmuxSyncTicker(),
		a.checkTmuxAvailable(),
		a.startFileWatcher(),
		a.startPermissionWatcher(),
		a.checkForUpdates(),
	}
	if a.fileWatcherErr != nil {
		cmds = append(cmds, a.toast.ShowWarning("File watching disabled; git status may be stale"))
	}
	return a.safeBatch(cmds...)
}

// Shutdown releases resources that may outlive the Bubble Tea program.
func (a *App) Shutdown() {
	a.shutdownOnce.Do(func() {
		// Close terminals and scripts first. The supervisor's tab actor
		// may be blocked on a PTY write (a raw syscall that ignores
		// context cancellation). Closing the PTY file descriptors here
		// unblocks those writes so the supervisor can stop cleanly.
		if a.center != nil {
			a.center.Close()
		}
		if a.sidebarTerminal != nil {
			a.sidebarTerminal.CloseAll()
		}
		if a.scripts != nil {
			a.scripts.StopAll()
		}
		if a.supervisor != nil {
			a.supervisor.Stop()
		}
		if a.fileWatcher != nil {
			_ = a.fileWatcher.Close()
		}
		if a.permissionWatcher != nil {
			_ = a.permissionWatcher.Close()
		}
		if a.hooksServer != nil {
			_ = a.hooksServer.Close()
		}
	})
}
