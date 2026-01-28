package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/github"
	"github.com/andyrewlee/amux/internal/linear"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/supervisor"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/tracker"
	"github.com/andyrewlee/amux/internal/ui/board"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/diffview"
	"github.com/andyrewlee/amux/internal/ui/drawer"
	"github.com/andyrewlee/amux/internal/ui/inspector"
	"github.com/andyrewlee/amux/internal/ui/layout"
	"github.com/andyrewlee/amux/internal/ui/preview"
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
	DialogCleanupTmux     = "cleanup_tmux"
)

// Prefix mode constants
const (
	prefixTimeout = 700 * time.Millisecond
)

// prefixTimeoutMsg is sent when the prefix mode timer expires
type prefixTimeoutMsg struct {
	token int
}

// AuxMode identifies auxiliary pane mode.
type AuxMode int

const (
	AuxNone AuxMode = iota
	AuxPreview
	AuxDiff
)

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
	board           *board.Model
	center          *center.Model
	inspector       *inspector.Model
	drawer          *drawer.Model
	sidebar         *sidebar.TabbedSidebar
	sidebarTerminal *sidebar.TerminalModel
	dashboard       *dashboard.Model
	diffView        *diffview.Model
	previewView     *preview.Model
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

	// Linear
	linearConfig  *linear.Config
	linearService *linear.Service
	linearAdapter *tracker.LinearAdapter
	webhookCh     chan messages.WebhookEvent
	webhookCancel context.CancelFunc
	boardIssues   []linear.Issue
	selectedIssue *linear.Issue
	auxMode       AuxMode
	drawerOpen    bool
	githubConfig  *github.Config

	statePickerIssueID    string
	statePickerStates     []linear.State
	commentIssueID        string
	attemptPickerIssueID  string
	attemptPickerBranches []string
	diffIssueID           string
	diffComments          map[string][]reviewComment
	diffCommentFile       string
	diffCommentSide       string
	diffCommentLine       int
	changeBaseIssueID     string
	prStatuses            map[string]prInfo
	prCommentIssueID      string
	prCommentOptions      []string
	createIssueTeams      []issueTeamOption
	issueMenuIssueID      string
	issueMenuActions      []issueMenuAction
	editIssueID           string
	editIssueTitle        string
	editIssueDescription  string
	renameBranchIssueID   string
	subtaskParentIssueID  string
	accountFilterValues   []string
	projectFilterOptions  []projectFilterOption
	labelFilterValues     []string
	recentFilterValues    []int
	pendingAgentMessages  map[string]string
	authMissingAccounts   []string
	oauthAccountValues    []string
	activityLog           []common.ActivityEntry
	activitySeq           int
	approvals             map[string]*approvalState
	approvalsTickerActive bool
	agentActivityIDs      map[string]string
	agentProfileIssueID   string
	scriptActivityIDs     map[string]string
	processRecords        map[string]*processRecord
	nextActions           map[string]nextActionSummary
	scriptOutputCh        chan messages.ScriptOutput

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

	tmuxSyncToken          int
	tmuxActivityToken      int
	tmuxOptions            tmux.Options
	tmuxAvailable          bool
	tmuxCheckDone          bool
	tmuxInstallHint        string
	tmuxActiveWorkspaceIDs map[string]bool
	sessionActivityStates  map[string]*sessionActivityState // Per-session hysteresis state

	// Workspace persistence debounce
	dirtyWorkspaces map[string]bool
	persistToken    int

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
	applyTmuxEnvFromConfig(cfg, false)
	tmuxOpts := tmux.DefaultOptions()

	// Run migrations before ensuring directories
	migrationResult := cfg.Paths.RunMigrations()
	if migrationResult.HasMigrations() {
		logging.Info("Path migration completed successfully")
	}
	if migrationResult.Error != nil {
		logging.Warn("Path migration encountered errors: %v", migrationResult.Error)
		legacyWorkspacesRoot := filepath.Join(cfg.Paths.Home, "worktrees")
		if info, err := os.Stat(legacyWorkspacesRoot); err == nil && info.IsDir() {
			logging.Warn("Falling back to legacy workspaces path: %s", legacyWorkspacesRoot)
			cfg.Paths.WorkspacesRoot = legacyWorkspacesRoot
		}

		legacyMetadataRoot := filepath.Join(cfg.Paths.Home, "worktrees-metadata")
		if info, err := os.Stat(legacyMetadataRoot); err == nil && info.IsDir() {
			logging.Warn("Falling back to legacy metadata path: %s", legacyMetadataRoot)
			cfg.Paths.MetadataRoot = legacyMetadataRoot
		}
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

	linearConfig, err := linear.LoadConfig(cfg.Paths.LinearConfigPath)
	if err != nil {
		return nil, err
	}
	linearCache := linear.NewCache(cfg.Paths.CacheRoot)
	linearService := linear.NewService(linearConfig, linearCache)
	githubConfig, err := github.LoadConfig(cfg.Paths.GitHubConfigPath)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	app := &App{
		config:                 cfg,
		registry:               registry,
		workspaces:             workspaces,
		scripts:                scripts,
		statusManager:          statusManager,
		fileWatcher:            fileWatcher,
		fileWatcherCh:          fileWatcherCh,
		fileWatcherErr:         fileWatcherErr,
		layout:                 layout.NewManager(),
		board:                  board.New(),
		dashboard:              dashboard.New(),
		center:                 center.New(cfg),
		inspector:              inspector.New(),
		drawer:                 drawer.New(),
		sidebar:                sidebar.NewTabbedSidebar(),
		sidebarTerminal:        sidebar.NewTerminalModel(),
		diffView:               diffview.New(),
		previewView:            preview.New(),
		helpOverlay:            common.NewHelpOverlay(),
		toast:                  common.NewToastModel(),
		focusedPane:            messages.PaneDashboard,
		showWelcome:            true,
		keymap:                 DefaultKeyMap(),
		styles:                 common.DefaultStyles(),
		linearConfig:           linearConfig,
		linearService:          linearService,
		linearAdapter:          tracker.NewLinearAdapter(linearService),
		auxMode:                AuxNone,
		diffComments:           make(map[string][]reviewComment),
		prStatuses:             make(map[string]prInfo),
		githubConfig:           githubConfig,
		pendingAgentMessages:   make(map[string]string),
		approvals:              make(map[string]*approvalState),
		agentActivityIDs:       make(map[string]string),
		scriptActivityIDs:      make(map[string]string),
		processRecords:         make(map[string]*processRecord),
		nextActions:            make(map[string]nextActionSummary),
		scriptOutputCh:         make(chan messages.ScriptOutput, 200),
		dashboardChrome:        &compositor.ChromeCache{},
		centerChrome:           &compositor.ChromeCache{},
		sidebarChrome:          &compositor.ChromeCache{},
		version:                version,
		commit:                 commit,
		buildDate:              date,
		externalMsgs:           make(chan tea.Msg, 4096),
		externalCritical:       make(chan tea.Msg, 512),
		ctx:                    ctx,
		tmuxOptions:            tmuxOpts,
		tmuxActiveWorkspaceIDs: make(map[string]bool),
		sessionActivityStates:  make(map[string]*sessionActivityState),
		dirtyWorkspaces:        make(map[string]bool),
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
	app.board.SetStyles(app.styles)
	app.inspector.SetStyles(app.styles)
	app.drawer.SetStyles(app.styles)
	app.sidebarTerminal.SetStyles(app.styles)
	app.center.SetStyles(app.styles)
	app.toast.SetStyles(app.styles)
	app.helpOverlay.SetStyles(app.styles)
	app.setKeymapHintsEnabled(cfg.UI.ShowKeymapHints)
	// Propagate tmux config to components
	app.center.SetTmuxConfig(tmuxOpts.ServerName, tmuxOpts.ConfigPath)
	app.sidebarTerminal.SetTmuxConfig(tmuxOpts.ServerName, tmuxOpts.ConfigPath)
	app.supervisor.Start("center.tab_actor", app.center.RunTabActor, supervisor.WithRestartPolicy(supervisor.RestartAlways))
	if app.statusManager != nil {
		app.supervisor.Start("git.status_manager", app.statusManager.Run)
	}
	if fileWatcher != nil {
		app.supervisor.Start("git.file_watcher", fileWatcher.Run, supervisor.WithBackoff(500*time.Millisecond))
	}
	app.updateAuthStatus()
	return app, nil
}

// Init initializes the application
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.loadProjects(),
		a.board.Init(),
		a.center.Init(),
		a.inspector.Init(),
		a.drawer.Init(),
		a.diffView.Init(),
		a.previewView.Init(),
		a.sidebarTerminal.Init(),
		a.startGitStatusTicker(),
		a.startPTYWatchdog(),
		a.startTmuxActivityTicker(),
		a.triggerTmuxActivityScan(),
		a.startTmuxSyncTicker(),
		a.checkTmuxAvailable(),
		a.startFileWatcher(),
		a.checkForUpdates(),
		a.listenScriptOutput(),
		a.refreshBoard(false),
	}
	if cmd := a.startWebhookServer(); cmd != nil {
		cmds = append(cmds, cmd)
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

// tmuxAvailableResult is sent after checking tmux availability
type tmuxAvailableResult struct {
	available   bool
	installHint string
}

func (a *App) checkTmuxAvailable() tea.Cmd {
	return func() tea.Msg {
		if err := tmux.EnsureAvailable(); err != nil {
			return tmuxAvailableResult{available: false, installHint: tmux.InstallHint()}
		}
		return tmuxAvailableResult{available: true}
	}
}

// IsTmuxAvailable returns whether tmux is installed and available.
func (a *App) IsTmuxAvailable() bool {
	return a.tmuxAvailable
}

func (a *App) startWebhookServer() tea.Cmd {
	if a.linearConfig == nil || len(a.linearConfig.WebhookSecrets) == 0 {
		return nil
	}
	if a.webhookCh == nil {
		a.webhookCh = make(chan messages.WebhookEvent, 50)
	}
	addr := os.Getenv("AMUX_LINEAR_WEBHOOK_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.webhookCancel = cancel
	server := linear.NewWebhookServer(addr, a.linearConfig.WebhookSecrets, func(event linear.WebhookEvent) {
		select {
		case a.webhookCh <- messages.WebhookEvent{
			Account: event.Account,
			Type:    event.Type,
			Action:  event.Action,
			Data:    event.Data,
		}:
		default:
		}
	})
	go func() {
		if err := server.Start(ctx); err != nil {
			logging.Warn("Linear webhook server stopped: %v", err)
		}
	}()
	return a.listenWebhook()
}

func (a *App) listenWebhook() tea.Cmd {
	if a.webhookCh == nil {
		return nil
	}
	return func() tea.Msg {
		return <-a.webhookCh
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

// startTmuxSyncTicker returns a command that ticks for tmux session reconciliation.
func (a *App) startTmuxSyncTicker() tea.Cmd {
	a.tmuxSyncToken++
	token := a.tmuxSyncToken
	return common.SafeTick(a.tmuxSyncInterval(), func(time.Time) tea.Msg {
		return messages.TmuxSyncTick{Token: token}
	})
}

func (a *App) tmuxSyncInterval() time.Duration {
	const defaultInterval = 7 * time.Second
	value := strings.TrimSpace(os.Getenv("AMUX_TMUX_SYNC_INTERVAL"))
	if value == "" {
		return defaultInterval
	}
	interval, err := time.ParseDuration(value)
	if err != nil || interval <= 0 {
		logging.Warn("Invalid AMUX_TMUX_SYNC_INTERVAL=%q; using %s", value, defaultInterval)
		return defaultInterval
	}
	return interval
}

func applyTmuxEnvFromConfig(cfg *config.Config, force bool) {
	if cfg == nil {
		return
	}
	if force {
		setEnvOrUnset("AMUX_TMUX_SERVER", cfg.UI.TmuxServer)
		setEnvOrUnset("AMUX_TMUX_CONFIG", cfg.UI.TmuxConfigPath)
		setEnvOrUnset("AMUX_TMUX_SYNC_INTERVAL", cfg.UI.TmuxSyncInterval)
		return
	}
	setEnvIfNonEmpty("AMUX_TMUX_SERVER", cfg.UI.TmuxServer)
	setEnvIfNonEmpty("AMUX_TMUX_CONFIG", cfg.UI.TmuxConfigPath)
	setEnvIfNonEmpty("AMUX_TMUX_SYNC_INTERVAL", cfg.UI.TmuxSyncInterval)
}

func (a *App) tmuxSyncWorkspaces() []*data.Workspace {
	if a.monitorMode {
		var targets []*data.Workspace
		for i := range a.projects {
			project := &a.projects[i]
			if a.monitorFilter != "" && project.Path != a.monitorFilter {
				continue
			}
			for j := range project.Workspaces {
				targets = append(targets, &project.Workspaces[j])
			}
		}
		return targets
	}
	if a.activeWorkspace != nil {
		return []*data.Workspace{a.activeWorkspace}
	}
	return nil
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

func (a *App) listenScriptOutput() tea.Cmd {
	if a.scriptOutputCh == nil {
		return nil
	}
	return func() tea.Msg {
		return <-a.scriptOutputCh
	}
}
