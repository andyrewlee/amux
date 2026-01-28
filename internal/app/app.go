package app

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/supervisor"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/layout"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/update"
)

// DialogID constants
const (
	DialogAddProject      = "add_project"
	DialogCreateWorkspace = "create_workspace"
	DialogDeleteWorkspace = "delete_workspace"
	DialogRemoveProject   = "remove_project"
	DialogSelectAssistant = "select_assistant"
	DialogQuit            = "quit"
)

// Prefix mode constants
const (
	prefixTimeout = 700 * time.Millisecond
)

// prefixTimeoutMsg is sent when the prefix mode timer expires
type prefixTimeoutMsg struct {
	token int
}

// App is the root Bubbletea model
type App struct {
	// Configuration
	config     *config.Config
	registry   *data.Registry
	workspaces *data.WorkspaceStore

	// State
	projects         []data.Project
	activeWorkspace  *data.Workspace
	activeProject    *data.Project
	focusedPane      messages.PaneType
	showWelcome      bool
	monitorMode      bool
	monitorFilter    string // "" means "All", otherwise filter by project key (repo path)
	monitorLayoutKey string
	monitorCanvas    *compositor.Canvas

	// Update state
	updateAvailable *update.CheckResult // nil if no update or dismissed
	version         string
	commit          string
	buildDate       string
	upgradeRunning  bool

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

	// Overlays
	helpOverlay *common.HelpOverlay
	toast       *common.ToastModel

	// Dialog context
	dialogProject   *data.Project
	dialogWorkspace *data.Workspace

	// Process management
	scripts *process.ScriptRunner

	// Git status management
	statusManager  *git.StatusManager
	fileWatcher    *git.FileWatcher
	fileWatcherCh  chan messages.FileWatcherEvent
	fileWatcherErr error

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

	// Terminal capabilities
	keyboardEnhancements tea.KeyboardEnhancementsMsg

	// Perf tracking
	lastInputAt         time.Time
	pendingInputLatency bool

	// Chrome caches for layer-based rendering
	dashboardChrome      *compositor.ChromeCache
	centerChrome         *compositor.ChromeCache
	sidebarChrome        *compositor.ChromeCache
	dashboardContent     drawableCache
	dashboardBorders     borderCache
	sidebarTopTabBar     drawableCache
	sidebarTopContent    drawableCache
	sidebarBottomContent drawableCache
	sidebarBottomTabBar  drawableCache
	sidebarBottomStatus  drawableCache
	sidebarBottomHelp    drawableCache
	sidebarTopBorders    borderCache
	sidebarBottomBorders borderCache
	centerTabBar         drawableCache
	centerStatus         drawableCache
	centerHelp           drawableCache
	centerBorders        borderCache

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

	// Ensure directories exist
	if err := cfg.Paths.EnsureDirectories(); err != nil {
		return nil, err
	}

	registry := data.NewRegistry(cfg.Paths.RegistryPath)
	workspaces := data.NewWorkspaceStore(cfg.Paths.MetadataRoot)
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

	ctx := context.Background()
	app := &App{
		config:           cfg,
		registry:         registry,
		workspaces:       workspaces,
		scripts:          scripts,
		statusManager:    statusManager,
		fileWatcher:      fileWatcher,
		fileWatcherCh:    fileWatcherCh,
		fileWatcherErr:   fileWatcherErr,
		layout:           layout.NewManager(),
		dashboard:        dashboard.New(),
		center:           center.New(cfg),
		sidebar:          sidebar.NewTabbedSidebar(),
		sidebarTerminal:  sidebar.NewTerminalModel(),
		helpOverlay:      common.NewHelpOverlay(),
		toast:            common.NewToastModel(),
		focusedPane:      messages.PaneDashboard,
		showWelcome:      true,
		keymap:           DefaultKeyMap(),
		dashboardChrome:  &compositor.ChromeCache{},
		centerChrome:     &compositor.ChromeCache{},
		sidebarChrome:    &compositor.ChromeCache{},
		version:          version,
		commit:           commit,
		buildDate:        date,
		externalMsgs:     make(chan tea.Msg, 1024),
		externalCritical: make(chan tea.Msg, 256),
		ctx:              ctx,
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
	app.setKeymapHintsEnabled(cfg.UI.ShowKeymapHints)
	app.supervisor.Start("center.tab_actor", app.center.RunTabActor, supervisor.WithRestartPolicy(supervisor.RestartAlways))
	if app.statusManager != nil {
		app.supervisor.Start("git.status_manager", app.statusManager.Run)
	}
	if fileWatcher != nil {
		app.supervisor.Start("git.file_watcher", fileWatcher.Run, supervisor.WithBackoff(500*time.Millisecond))
	}
	return app, nil
}

// Init initializes the application
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.loadProjects(),
		a.dashboard.Init(),
		a.center.Init(),
		a.sidebar.Init(),
		a.sidebarTerminal.Init(),
		a.startGitStatusTicker(),
		a.startPTYWatchdog(),
		a.startFileWatcher(),
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
		if a.supervisor != nil {
			a.supervisor.Stop()
		}
		if a.fileWatcher != nil {
			_ = a.fileWatcher.Close()
		}
		if a.center != nil {
			a.center.Close()
		}
		if a.sidebarTerminal != nil {
			a.sidebarTerminal.CloseAll()
		}
		if a.scripts != nil {
			a.scripts.StopAll()
		}
	})
}

// checkForUpdates starts a background check for updates.
func (a *App) checkForUpdates() tea.Cmd {
	return func() tea.Msg {
		updater := update.NewUpdater(a.version, a.commit, a.buildDate)
		result, err := updater.Check()
		if err != nil {
			logging.Warn("Update check failed: %v", err)
			return messages.UpdateCheckComplete{Err: err}
		}
		return messages.UpdateCheckComplete{
			CurrentVersion:  result.CurrentVersion,
			LatestVersion:   result.LatestVersion,
			UpdateAvailable: result.UpdateAvailable,
			ReleaseNotes:    result.ReleaseNotes,
			Err:             nil,
		}
	}
}

// startGitStatusTicker returns a command that ticks every 3 seconds for git status refresh
func (a *App) startGitStatusTicker() tea.Cmd {
	return common.SafeTick(3*time.Second, func(t time.Time) tea.Msg {
		return messages.GitStatusTick{}
	})
}

// startPTYWatchdog ticks periodically to ensure PTY readers are running.
func (a *App) startPTYWatchdog() tea.Cmd {
	return common.SafeTick(5*time.Second, func(time.Time) tea.Msg {
		return messages.PTYWatchdogTick{}
	})
}

// startFileWatcher starts watching for file changes and returns events
func (a *App) startFileWatcher() tea.Cmd {
	if a.fileWatcher == nil || a.fileWatcherCh == nil {
		return nil
	}
	return func() tea.Msg {
		return <-a.fileWatcherCh
	}
}
