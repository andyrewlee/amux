package app

import (
	"context"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/supervisor"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/layout"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// New creates a new App instance.
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
	workspaces.SetDefaultAssistant(cfg.ResolvedDefaultAssistant())
	scripts := process.NewScriptRunner(cfg.PortStart, cfg.PortRangeSize)
	workspaceService := newWorkspaceService(registry, workspaces, scripts, cfg.Paths.WorkspacesRoot, cfg.ResolvedDefaultAssistant())

	// Create status manager (callback will be nil, we use it for caching only)
	statusManager := git.NewStatusManager(nil)
	gitStatus := newGitStatusService(statusManager)

	tmuxSvc := newTmuxService(nil)
	updateSvc := newUpdateService(version, commit, date)

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

	// Create state watcher event channel
	stateWatcherCh := make(chan messages.StateWatcherEvent, 10)

	// Create state watcher with callback that sends to channel
	stateWatcher, stateWatcherErr := newStateWatcher(cfg.Paths.RegistryPath, cfg.Paths.MetadataRoot, func(reason string, paths []string) {
		select {
		case stateWatcherCh <- messages.StateWatcherEvent{Reason: reason, Paths: paths}:
		default:
			// Channel full, drop event (will catch on next change)
		}
	})
	if stateWatcherErr != nil {
		logging.Warn("State watcher disabled: %v", stateWatcherErr)
		stateWatcher = nil
	}

	ctx := context.Background()
	app := &App{
		config:                 cfg,
		workspaceService:       workspaceService,
		gitStatus:              gitStatus,
		tmuxService:            tmuxSvc,
		updateService:          updateSvc,
		fileWatcher:            fileWatcher,
		fileWatcherCh:          fileWatcherCh,
		fileWatcherErr:         fileWatcherErr,
		stateWatcher:           stateWatcher,
		stateWatcherCh:         stateWatcherCh,
		stateWatcherErr:        stateWatcherErr,
		layout:                 layout.NewManager(),
		dashboard:              dashboard.New(),
		center:                 center.New(cfg),
		sidebar:                sidebar.NewTabbedSidebar(),
		sidebarTerminal:        sidebar.NewTerminalModel(),
		helpOverlay:            common.NewHelpOverlay(),
		toast:                  common.NewToastModel(),
		focusedPane:            messages.PaneDashboard,
		showWelcome:            true,
		keymap:                 DefaultKeyMap(),
		dashboardChrome:        &compositor.ChromeCache{},
		centerChrome:           &compositor.ChromeCache{},
		sidebarChrome:          &compositor.ChromeCache{},
		version:                version,
		commit:                 commit,
		buildDate:              date,
		externalMsgs:           make(chan tea.Msg, externalMsgBuffer),
		externalCritical:       make(chan tea.Msg, externalCriticalBuffer),
		ctx:                    ctx,
		tmuxOptions:            tmuxOpts,
		tmuxActiveWorkspaceIDs: make(map[string]bool),
		sessionActivityStates:  make(map[string]*sessionActivityState),
		dirtyWorkspaces:        make(map[string]bool),
		localWorkspaceSavesAt:  make(map[string]localWorkspaceSaveMarker),
		creatingWorkspaceIDs:   make(map[string]bool),
		maxAttachedAgentTabs:   maxAttachedAgentTabsFromEnv(),
	}
	app.instanceID = newInstanceID()
	app.supervisor = supervisor.New(ctx)
	app.installSupervisorErrorHandler()
	// Route PTY messages through the app-level pump.
	app.center.SetMsgSink(app.enqueueExternalMsg)
	app.sidebarTerminal.SetMsgSink(app.enqueueExternalMsg)
	app.center.SetInstanceID(app.instanceID)
	app.sidebarTerminal.SetInstanceID(app.instanceID)
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
	// Propagate tmux config to components
	app.center.SetTmuxConfig(tmuxOpts.ServerName, tmuxOpts.ConfigPath)
	app.sidebarTerminal.SetTmuxConfig(tmuxOpts.ServerName, tmuxOpts.ConfigPath)
	app.supervisor.Start("center.tab_actor", app.center.RunTabActor, supervisor.WithRestartPolicy(supervisor.RestartAlways))
	if app.gitStatus != nil {
		app.supervisor.Start("git.status_manager", app.gitStatus.Run)
	}
	if fileWatcher != nil {
		app.supervisor.Start("git.file_watcher", fileWatcher.Run, supervisor.WithBackoff(supervisorBackoff))
	}
	if stateWatcher != nil {
		app.supervisor.Start("app.state_watcher", stateWatcher.Run, supervisor.WithBackoff(supervisorBackoff))
	}
	return app, nil
}

// Init initializes the application.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.loadProjects(),
		a.dashboard.Init(),
		a.center.Init(),
		a.sidebar.Init(),
		a.sidebarTerminal.Init(),
		a.startGitStatusTicker(),
		a.startPTYWatchdog(),
		a.startTmuxActivityTicker(),
		a.triggerTmuxActivityScan(),
		a.startTmuxSyncTicker(),
		a.checkTmuxAvailable(),
		a.startFileWatcher(),
		a.startStateWatcher(),
		a.checkForUpdates(),
	}
	if a.fileWatcherErr != nil {
		cmds = append(cmds, a.toast.ShowWarning("File watching disabled; git status may be stale"))
	}
	if a.stateWatcherErr != nil {
		cmds = append(cmds, a.toast.ShowWarning("Workspace sync disabled; other instances may be stale"))
	}
	return common.SafeBatch(cmds...)
}

// checkForUpdates starts a background check for updates.
func (a *App) checkForUpdates() tea.Cmd {
	return func() tea.Msg {
		if a.updateService == nil {
			return messages.UpdateCheckComplete{}
		}
		result, err := a.updateService.Check()
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

// tmuxAvailableResult is sent after checking tmux availability.
type tmuxAvailableResult struct {
	available   bool
	installHint string
}

func (a *App) checkTmuxAvailable() tea.Cmd {
	return func() tea.Msg {
		if a.tmuxService == nil {
			return tmuxAvailableResult{available: false, installHint: "tmux service unavailable"}
		}
		if err := a.tmuxService.EnsureAvailable(); err != nil {
			return tmuxAvailableResult{available: false, installHint: a.tmuxService.InstallHint()}
		}
		return tmuxAvailableResult{available: true}
	}
}

// startGitStatusTicker returns a command that ticks every 3 seconds for git status refresh.
func (a *App) startGitStatusTicker() tea.Cmd {
	return common.SafeTick(gitStatusTickInterval, func(t time.Time) tea.Msg {
		return messages.GitStatusTick{}
	})
}

// startPTYWatchdog ticks periodically to ensure PTY readers are running.
func (a *App) startPTYWatchdog() tea.Cmd {
	return common.SafeTick(ptyWatchdogInterval, func(time.Time) tea.Msg {
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
	value := strings.TrimSpace(os.Getenv("AMUX_TMUX_SYNC_INTERVAL"))
	if value == "" {
		return tmuxSyncDefaultInterval
	}
	interval, err := time.ParseDuration(value)
	if err != nil || interval <= 0 {
		logging.Warn("Invalid AMUX_TMUX_SYNC_INTERVAL=%q; using %s", value, tmuxSyncDefaultInterval)
		return tmuxSyncDefaultInterval
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

// startFileWatcher starts watching for file changes and returns events.
func (a *App) startFileWatcher() tea.Cmd {
	if a.fileWatcher == nil || a.fileWatcherCh == nil {
		return nil
	}
	return func() tea.Msg {
		return <-a.fileWatcherCh
	}
}

// startStateWatcher waits for state change notifications.
func (a *App) startStateWatcher() tea.Cmd {
	if a.stateWatcher == nil || a.stateWatcherCh == nil {
		return nil
	}
	return func() tea.Msg {
		return <-a.stateWatcherCh
	}
}
