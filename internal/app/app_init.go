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
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/supervisor"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/layout"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/ui/theme"
)

// newFileWatcherFn and newStateWatcherFn are construction seams for the file and
// state watchers. They default to the real constructors and exist only so tests
// can force a construction failure and pin that the app degrades gracefully (nil
// watchers, disabled flags set) instead of panicking. Production never reassigns
// them.
var (
	newFileWatcherFn  = git.NewFileWatcher
	newStateWatcherFn = newStateWatcher
)

// newAppShell constructs an App with its UI components built and wired in the
// same order the real app uses, but with no services attached: no supervisor,
// no watchers, no tmux/git/update services, and no background commands. New
// layers the services on top; the headless harness uses the shell directly so
// component construction and ordering changes are exercised by harness runs.
func newAppShell(cfg *config.Config) *App {
	// Explicit one-time package setup (no package init side effects): install
	// the default theme and arm perf profiling from the environment.
	theme.Init()
	perf.Init()
	app := &App{
		config:               cfg,
		layout:               layout.NewManager(),
		dashboard:            dashboard.New(),
		center:               center.New(cfg),
		sidebar:              sidebar.NewTabbedSidebar(),
		sidebarTerminal:      sidebar.NewTerminalModel(),
		toast:                common.NewToastModel(),
		focusedPane:          messages.PaneDashboard,
		keymap:               DefaultKeyMap(),
		renderCache:          newRenderCacheState(),
		tmuxActivity:         newTmuxActivityState(),
		lifecycle:            newWorkspaceLifecycleState(),
		maxAttachedAgentTabs: maxAttachedAgentTabsFromEnv(),
	}
	app.styles = common.DefaultStyles()
	// Propagate styles to all components (they may have been created with a
	// different current theme). filePicker is nil at construction, so its
	// nil-guarded branch in propagateStyles is intentionally skipped here.
	app.propagateStyles()
	if cfg != nil {
		app.setKeymapHintsEnabled(cfg.UI.ShowKeymapHints)
		app.dashboard.SetNotifyOnDone(cfg.UI.NotifyOnDone)
	}
	return app
}

// propagateStyles fans the current a.styles out to every UI component that
// accepts styles. Each component is nil-guarded so the same helper is correct
// both at construction (where filePicker is nil and is intentionally skipped)
// and after a live theme change (where filePicker may be present). Only
// components exposing SetStyles are included; the modal dialog and settings
// dialog do not and are deliberately omitted.
func (a *App) propagateStyles() {
	if a.dashboard != nil {
		a.dashboard.SetStyles(a.styles)
	}
	if a.sidebar != nil {
		a.sidebar.SetStyles(a.styles)
	}
	if a.sidebarTerminal != nil {
		a.sidebarTerminal.SetStyles(a.styles)
	}
	if a.center != nil {
		a.center.SetStyles(a.styles)
	}
	if a.toast != nil {
		a.toast.SetStyles(a.styles)
	}
	if a.filePicker != nil {
		a.filePicker.SetStyles(a.styles)
	}
}

// New creates a new App instance.
func New(version, commit, date string) (*App, error) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		return nil, err
	}
	applyTmuxEnvFromConfig(cfg)
	tmuxOpts := tmux.DefaultOptions()

	// Ensure directories exist
	if err := cfg.Paths.EnsureDirectories(); err != nil {
		return nil, err
	}

	registry := data.NewRegistry(cfg.Paths.RegistryPath)
	workspaces := data.NewWorkspaceStore(cfg.Paths.MetadataRoot)
	scripts := process.NewScriptRunner(cfg.PortStart, cfg.PortRangeSize)
	workspaceService := newWorkspaceService(registry, workspaces, scripts, cfg.Paths.WorkspacesRoot)

	// Create status manager (used for synchronous status caching only).
	statusManager := git.NewStatusManager()
	gitStatus := newGitStatusService(statusManager)

	var tmuxSvc TmuxOps = tmuxOps{}
	updateSvc := newUpdateService(version, commit, date)

	// Create file watcher event channel
	fileWatcherCh := make(chan messages.FileWatcherEvent, 10)

	// Create file watcher with callback that sends to channel
	fileWatcher, fileWatcherErr := newFileWatcherFn(func(root string) {
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
	stateWatcher, stateWatcherErr := newStateWatcherFn(cfg.Paths.RegistryPath, cfg.Paths.MetadataRoot, func(reason string, paths []string) {
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

	// Apply saved theme before creating components and styles.
	common.SetCurrentTheme(common.ThemeID(cfg.UI.Theme))

	ctx := context.Background()
	app := newAppShell(cfg)
	app.workspaceService = workspaceService
	app.gitStatus = gitStatus
	app.tmuxService = tmuxSvc
	app.updateService = updateSvc
	app.fileWatcher = fileWatcher
	app.fileWatcherCh = fileWatcherCh
	app.fileWatcherErr = fileWatcherErr
	app.stateWatcher = stateWatcher
	app.stateWatcherCh = stateWatcherCh
	app.stateWatcherErr = stateWatcherErr
	app.showWelcome = true
	app.version = version
	app.commit = commit
	app.buildDate = date
	app.externalMsgs = make(chan tea.Msg, externalMsgBuffer)
	app.externalCritical = make(chan tea.Msg, externalCriticalBuffer)
	app.ctx = ctx
	app.tmuxOptions = tmuxOpts
	app.instanceID = newInstanceID()
	app.supervisor = supervisor.New(ctx)
	app.installSupervisorErrorHandler()
	// Route PTY messages through the app-level pump.
	app.center.SetMsgSinkTry(app.tryEnqueueExternalMsg)
	app.sidebarTerminal.SetMsgSink(app.enqueueExternalMsg)
	app.center.SetInstanceID(app.instanceID)
	app.sidebarTerminal.SetInstanceID(app.instanceID)
	// Propagate tmux config to components
	app.center.SetTmuxOptions(tmuxOpts)
	app.sidebarTerminal.SetTmuxOptions(tmuxOpts)
	app.supervisor.Start("center.tab_actor", app.center.RunTabActor, supervisor.WithRestartPolicy(supervisor.RestartAlways))
	if fileWatcher != nil {
		app.supervisor.Start("git.file_watcher", fileWatcher.Run, supervisor.WithBackoff(supervisorBackoff))
	}
	if stateWatcher != nil {
		app.supervisor.Start("app.state_watcher", stateWatcher.Run, supervisor.WithBackoff(supervisorBackoff))
	}

	// Let the service's load/rescan path consult the App's delete-in-flight guard
	// so it can skip workspaces that are being deleted (used by the rescan guard).
	workspaceService.deleteInFlight = app.isWorkspaceDeleteInFlight
	workspaceService.deleteInFlightGuard = app.runUnlessWorkspaceDeleteInFlight
	// Let the delete path tear down workspace tmux sessions after worktree
	// removal succeeds, without killing live sessions for failed deletes.
	workspaceService.killWorkspaceSessions = app.killWorkspaceSessionsSync

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
		a.startOrphanGCTicker(),
		a.startTmuxActivityTicker(),
		a.triggerTmuxActivityScan(),
		a.startTmuxSyncTicker(),
		a.checkTmuxAvailable(),
		a.startFileWatcher(),
		a.startStateWatcher(),
		a.checkForUpdates(),
	}
	cmds = append(cmds, a.watcherWarningCmds()...)
	return common.SafeBatch(cmds...)
}

// watcherWarningCmds returns the warning-toast commands for any watcher that
// failed to construct, in a fixed order: file watcher first, then state watcher.
// It is split out of Init so the warning-queuing behavior is directly testable
// (Init folds these into a tea.Batch, whose contents cannot be inspected). The
// slice has exactly one entry per set watcher-err flag and is empty when both
// watchers came up cleanly.
func (a *App) watcherWarningCmds() []tea.Cmd {
	var cmds []tea.Cmd
	if a.fileWatcherErr != nil {
		cmds = append(cmds, a.toast.ShowWarning("File watching disabled; git status may be stale"))
	}
	if a.stateWatcherErr != nil {
		cmds = append(cmds, a.toast.ShowWarning("Workspace sync disabled; other instances may be stale"))
	}
	return cmds
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

// startOrphanGCTicker returns a command that ticks periodically to clean up orphaned tmux sessions.
func (a *App) startOrphanGCTicker() tea.Cmd {
	return common.SafeTick(orphanGCInterval, func(time.Time) tea.Msg {
		return messages.OrphanGCTick{}
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
	a.tmuxActivity.syncToken++
	token := a.tmuxActivity.syncToken
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
	if interval < tmuxSyncMinInterval {
		logging.Warn("AMUX_TMUX_SYNC_INTERVAL=%q is below minimum %s; using %s", value, tmuxSyncMinInterval, tmuxSyncMinInterval)
		return tmuxSyncMinInterval
	}
	return interval
}

func applyTmuxEnvFromConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	setEnvIfNonEmpty(config.WorkspacesRootEnvVar, cfg.Paths.WorkspacesRoot)
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
