package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/layout"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/validation"
	"github.com/andyrewlee/amux/internal/vterm"
)

// DialogID constants
const (
	DialogAddProject      = "add_project"
	DialogCreateWorktree  = "create_worktree"
	DialogDeleteWorktree  = "delete_worktree"
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
	config   *config.Config
	registry *data.Registry
	metadata *data.MetadataStore

	// State
	projects         []data.Project
	activeWorktree   *data.Worktree
	activeProject    *data.Project
	focusedPane      messages.PaneType
	showWelcome      bool
	monitorMode      bool
	monitorFilter    string // "" means "All", otherwise filter by project key (repo path)
	monitorLayoutKey string
	monitorCanvas    *compositor.Canvas

	// UI Components
	layout          *layout.Manager
	dashboard       *dashboard.Model
	center          *center.Model
	sidebar         *sidebar.Model
	sidebarTerminal *sidebar.TerminalModel
	dialog          *common.Dialog
	filePicker      *common.FilePicker
	settingsDialog  *common.SettingsDialog

	// Overlays
	helpOverlay *common.HelpOverlay
	toast       *common.ToastModel

	// Dialog context
	dialogProject  *data.Project
	dialogWorktree *data.Worktree

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

	// Lifecycle
	ready    bool
	quitting bool
	err      error

	// Prefix mode (leader key)
	prefixActive bool
	prefixToken  int

	// Terminal capabilities
	keyboardEnhancements tea.KeyboardEnhancementsMsg

	// Perf tracking
	lastInputAt         time.Time
	pendingInputLatency bool

	// Chrome caches for layer-based rendering
	dashboardChrome *compositor.ChromeCache
	centerChrome    *compositor.ChromeCache
	sidebarChrome   *compositor.ChromeCache
}

func (a *App) markInput() {
	a.lastInputAt = time.Now()
	a.pendingInputLatency = true
}

// New creates a new App instance
func New() (*App, error) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		return nil, err
	}

	// Ensure directories exist
	if err := cfg.Paths.EnsureDirectories(); err != nil {
		return nil, err
	}

	registry := data.NewRegistry(cfg.Paths.RegistryPath)
	metadata := data.NewMetadataStore(cfg.Paths.MetadataRoot)
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

	app := &App{
		config:          cfg,
		registry:        registry,
		metadata:        metadata,
		scripts:         scripts,
		statusManager:   statusManager,
		fileWatcher:     fileWatcher,
		fileWatcherCh:   fileWatcherCh,
		fileWatcherErr:  fileWatcherErr,
		layout:          layout.NewManager(),
		dashboard:       dashboard.New(),
		center:          center.New(cfg),
		sidebar:         sidebar.New(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		helpOverlay:     common.NewHelpOverlay(),
		toast:           common.NewToastModel(),
		focusedPane:     messages.PaneDashboard,
		showWelcome:     true,
		keymap:          DefaultKeyMap(),
		dashboardChrome: &compositor.ChromeCache{},
		centerChrome:    &compositor.ChromeCache{},
		sidebarChrome:   &compositor.ChromeCache{},
	}
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
		a.startFileWatcher(),
	}
	if a.fileWatcherErr != nil {
		cmds = append(cmds, a.toast.ShowWarning("File watching disabled; git status may be stale"))
	}
	return tea.Batch(cmds...)
}

// startGitStatusTicker returns a command that ticks every 3 seconds for git status refresh
func (a *App) startGitStatusTicker() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return messages.GitStatusTick{}
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

// Update handles all messages
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer perf.Time("update")()
	var cmds []tea.Cmd
	if perf.Enabled() {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.KeyReleaseMsg, tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.PasteMsg:
			a.markInput()
		}
	}

	// Handle dialog result first (arrives after dialog is hidden)
	if result, ok := msg.(common.DialogResult); ok {
		logging.Info("Received DialogResult: id=%s confirmed=%v", result.ID, result.Confirmed)
		switch result.ID {
		case DialogAddProject, DialogCreateWorktree, DialogDeleteWorktree, DialogRemoveProject, DialogSelectAssistant, "agent-picker", DialogQuit:
			return a, a.handleDialogResult(result)
		}
		// If not an App-level dialog, let it fall through to components
		// Currently only Center uses custom dialogs
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return a, cmd
	}

	// Handle help overlay toggle (highest priority)
	if _, ok := msg.(tea.KeyPressMsg); ok && a.helpOverlay.Visible() {
		// Any key dismisses help
		a.helpOverlay.Hide()
		return a, nil
	}

	// Allow clicking to dismiss help or error overlays
	if mouseMsg, ok := msg.(tea.MouseClickMsg); ok && mouseMsg.Button == tea.MouseLeft {
		if a.helpOverlay.Visible() {
			a.helpOverlay.Hide()
			return a, nil
		}
		if a.err != nil {
			a.err = nil
			return a, nil
		}
	}

	// Handle toast updates
	if _, ok := msg.(common.ToastDismissed); ok {
		newToast, cmd := a.toast.Update(msg)
		a.toast = newToast
		cmds = append(cmds, cmd)
	}

	// Handle dialog input if visible
	if a.dialog != nil && a.dialog.Visible() {
		newDialog, cmd := a.dialog.Update(msg)
		a.dialog = newDialog
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Don't process other input while dialog is open
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return a, tea.Batch(cmds...)
		}
		if _, ok := msg.(tea.PasteMsg); ok {
			return a, tea.Batch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	// Handle file picker if visible
	if a.filePicker != nil && a.filePicker.Visible() {
		newPicker, cmd := a.filePicker.Update(msg)
		a.filePicker = newPicker
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Don't process other input while file picker is open
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return a, tea.Batch(cmds...)
		}
		if _, ok := msg.(tea.PasteMsg); ok {
			return a, tea.Batch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	// Handle settings dialog if visible
	if a.settingsDialog != nil && a.settingsDialog.Visible() {
		newSettings, cmd := a.settingsDialog.Update(msg)
		a.settingsDialog = newSettings
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Don't process other input while settings dialog is open
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return a, tea.Batch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	switch msg := msg.(type) {
	case tea.KeyboardEnhancementsMsg:
		a.keyboardEnhancements = msg
		logging.Info("Keyboard enhancements: disambiguation=%t event_types=%t", msg.SupportsKeyDisambiguation(), msg.SupportsEventTypes())

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		a.layout.Resize(msg.Width, msg.Height)
		a.updateLayout()

	case tea.MouseClickMsg:
		// Handle mouse click events
		if a.monitorMode {
			if msg.Button == tea.MouseLeft {
				a.focusPane(messages.PaneMonitor)
				if a.monitorExitHit(msg.X, msg.Y) {
					a.toggleMonitorMode()
					break
				}
				if filter, ok := a.monitorFilterHit(msg.X, msg.Y); ok {
					a.monitorFilter = filter
					break
				}
				// Click to focus tile (just select, don't exit)
				a.selectMonitorTile(msg.X, msg.Y)
			}
			break
		}

		leftGutter := a.layout.LeftGutter()
		topGutter := a.layout.TopGutter()
		dashWidth := a.layout.DashboardWidth()
		centerWidth := a.layout.CenterWidth()
		gapX := 0
		if a.layout.ShowCenter() {
			gapX = a.layout.GapX()
		}
		centerStart := leftGutter + dashWidth + gapX
		centerEnd := centerStart + centerWidth
		sidebarStart := centerEnd
		sidebarEnd := centerEnd
		if a.layout.ShowSidebar() {
			sidebarStart = centerEnd + gapX
			sidebarEnd = sidebarStart + a.layout.SidebarWidth()
		}
		inSidebarX := a.layout.ShowSidebar() && msg.X >= sidebarStart && msg.X < sidebarEnd
		localY := msg.Y - topGutter

		// Focus pane on left-click press
		if msg.Button == tea.MouseLeft {
			if msg.X < leftGutter {
				a.focusPane(messages.PaneDashboard)
			} else if msg.X < leftGutter+dashWidth {
				// Clicked on dashboard (left bar)
				a.focusPane(messages.PaneDashboard)
			} else if msg.X < centerEnd {
				// Clicked on center pane
				a.focusPane(messages.PaneCenter)
			} else if inSidebarX {
				// Clicked on sidebar - determine top (changes) or bottom (terminal)
				sidebarHeight := a.layout.Height()
				topPaneHeight, _ := sidebarPaneHeights(sidebarHeight)

				// Split point is after top pane
				if localY >= topPaneHeight {
					a.focusPane(messages.PaneSidebarTerminal)
				} else {
					a.focusPane(messages.PaneSidebar)
				}
			}
		}

		if cmd := a.handleCenterPaneClick(msg); cmd != nil {
			cmds = append(cmds, cmd)
			break
		}

		// Forward mouse events to the focused pane
		// This ensures drag events are received even if the mouse leaves the pane bounds
		switch a.focusedPane {
		case messages.PaneDashboard:
			adjusted := msg
			if a.layout != nil {
				adjusted.X -= a.layout.LeftGutter()
				adjusted.Y -= a.layout.TopGutter()
			}
			newDashboard, cmd := a.dashboard.Update(adjusted)
			a.dashboard = newDashboard
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneCenter:
			adjusted := msg
			if a.layout != nil {
				adjusted.Y -= a.layout.TopGutter()
			}
			newCenter, cmd := a.center.Update(adjusted)
			a.center = newCenter
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneSidebarTerminal:
			// Ignore clicks in the gap/right gutter so they don't trigger sidebar actions.
			if inSidebarX {
				newTerm, cmd := a.sidebarTerminal.Update(msg)
				a.sidebarTerminal = newTerm
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		case messages.PaneSidebar:
			adjusted := msg
			if a.layout != nil {
				adjusted.Y = a.adjustSidebarMouseY(adjusted.Y)
			}
			// Ignore clicks in the gap/right gutter so they don't trigger sidebar actions.
			if inSidebarX {
				newSidebar, cmd := a.sidebar.Update(adjusted)
				a.sidebar = newSidebar
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}

	case tea.MouseWheelMsg:
		if a.monitorMode {
			break
		}
		switch a.focusedPane {
		case messages.PaneDashboard:
			adjusted := msg
			if a.layout != nil {
				adjusted.X -= a.layout.LeftGutter()
				adjusted.Y -= a.layout.TopGutter()
			}
			newDashboard, cmd := a.dashboard.Update(adjusted)
			a.dashboard = newDashboard
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneCenter:
			adjusted := msg
			if a.layout != nil {
				adjusted.Y -= a.layout.TopGutter()
			}
			newCenter, cmd := a.center.Update(adjusted)
			a.center = newCenter
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneSidebarTerminal:
			newTerm, cmd := a.sidebarTerminal.Update(msg)
			a.sidebarTerminal = newTerm
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneSidebar:
			adjusted := msg
			if a.layout != nil {
				adjusted.Y = a.adjustSidebarMouseY(adjusted.Y)
			}
			newSidebar, cmd := a.sidebar.Update(adjusted)
			a.sidebar = newSidebar
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tea.MouseMotionMsg:
		if a.monitorMode {
			break
		}
		switch a.focusedPane {
		case messages.PaneDashboard:
			adjusted := msg
			if a.layout != nil {
				adjusted.X -= a.layout.LeftGutter()
				adjusted.Y -= a.layout.TopGutter()
			}
			newDashboard, cmd := a.dashboard.Update(adjusted)
			a.dashboard = newDashboard
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneCenter:
			adjusted := msg
			if a.layout != nil {
				adjusted.Y -= a.layout.TopGutter()
			}
			newCenter, cmd := a.center.Update(adjusted)
			a.center = newCenter
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneSidebarTerminal:
			newTerm, cmd := a.sidebarTerminal.Update(msg)
			a.sidebarTerminal = newTerm
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneSidebar:
			adjusted := msg
			if a.layout != nil {
				adjusted.Y = a.adjustSidebarMouseY(adjusted.Y)
			}
			newSidebar, cmd := a.sidebar.Update(adjusted)
			a.sidebar = newSidebar
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tea.MouseReleaseMsg:
		if a.monitorMode {
			break
		}
		switch a.focusedPane {
		case messages.PaneDashboard:
			adjusted := msg
			if a.layout != nil {
				adjusted.X -= a.layout.LeftGutter()
				adjusted.Y -= a.layout.TopGutter()
			}
			newDashboard, cmd := a.dashboard.Update(adjusted)
			a.dashboard = newDashboard
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneCenter:
			adjusted := msg
			if a.layout != nil {
				adjusted.Y -= a.layout.TopGutter()
			}
			newCenter, cmd := a.center.Update(adjusted)
			a.center = newCenter
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneSidebarTerminal:
			newTerm, cmd := a.sidebarTerminal.Update(msg)
			a.sidebarTerminal = newTerm
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneSidebar:
			adjusted := msg
			if a.layout != nil {
				adjusted.Y = a.adjustSidebarMouseY(adjusted.Y)
			}
			newSidebar, cmd := a.sidebar.Update(adjusted)
			a.sidebar = newSidebar
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tea.PasteMsg:
		// Handle paste in monitor mode - forward to selected tile
		if a.monitorMode && a.focusedPane == messages.PaneMonitor {
			tabs := a.center.MonitorTabs()
			if len(tabs) > 0 {
				idx := a.center.MonitorSelectedIndex(len(tabs))
				if cmd := a.center.HandleMonitorInput(tabs[idx].ID, msg); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			break
		}
		// Non-monitor paste handling falls through to focused pane
		switch a.focusedPane {
		case messages.PaneCenter:
			newCenter, cmd := a.center.Update(msg)
			a.center = newCenter
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneSidebarTerminal:
			newTerm, cmd := a.sidebarTerminal.Update(msg)
			a.sidebarTerminal = newTerm
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case prefixTimeoutMsg:
		if msg.token == a.prefixToken && a.prefixActive {
			a.exitPrefix()
		}

	case tea.KeyPressMsg:
		// Dismiss error on any key
		if a.err != nil {
			a.err = nil
			return a, nil
		}

		// 1. Handle prefix key (Ctrl+Space)
		if a.isPrefixKey(msg) {
			if a.prefixActive {
				// Prefix + Prefix = send literal Ctrl+Space to terminal
				a.sendPrefixToTerminal()
				a.exitPrefix()
				return a, nil
			}
			// Enter prefix mode
			return a, a.enterPrefix()
		}

		// 2. If prefix is active, handle mux commands
		if a.prefixActive {
			// Esc cancels prefix mode without forwarding
			code := msg.Key().Code
			if code == tea.KeyEsc || code == tea.KeyEscape {
				a.exitPrefix()
				return a, nil
			}

			handled, cmd := a.handlePrefixCommand(msg)
			if handled {
				a.exitPrefix()
				return a, cmd
			}
			// Unknown key in prefix mode: exit prefix and pass through
			a.exitPrefix()
			// Fall through to normal handling below
		}

		// 3. Passthrough mode - route keys to focused pane
		// Monitor pane: all keys go to the selected tile's PTY (navigation is via prefix mode)
		if a.focusedPane == messages.PaneMonitor {
			if cmd := a.handleMonitorInput(msg); cmd != nil {
				return a, cmd
			}
			return a, nil
		}

		// Route to focused pane
		switch a.focusedPane {
		case messages.PaneDashboard:
			newDashboard, cmd := a.dashboard.Update(msg)
			a.dashboard = newDashboard
			cmds = append(cmds, cmd)
		case messages.PaneCenter:
			newCenter, cmd := a.center.Update(msg)
			a.center = newCenter
			cmds = append(cmds, cmd)
		case messages.PaneSidebar:
			newSidebar, cmd := a.sidebar.Update(msg)
			a.sidebar = newSidebar
			cmds = append(cmds, cmd)
		case messages.PaneSidebarTerminal:
			newSidebarTerminal, cmd := a.sidebarTerminal.Update(msg)
			a.sidebarTerminal = newSidebarTerminal
			cmds = append(cmds, cmd)
		}

	case messages.ProjectsLoaded:
		a.projects = msg.Projects
		a.dashboard.SetProjects(a.projects)
		// Request git status for all worktrees
		for i := range a.projects {
			for j := range a.projects[i].Worktrees {
				wt := &a.projects[i].Worktrees[j]
				cmds = append(cmds, a.requestGitStatus(wt.Root))
			}
		}

	case messages.WorktreeActivated:
		// Tabs now persist in memory per-worktree, no need to save/restore from disk
		a.activeProject = msg.Project
		a.activeWorktree = msg.Worktree
		a.showWelcome = false
		a.center.SetWorktree(msg.Worktree)
		a.sidebar.SetWorktree(msg.Worktree)
		// Set up sidebar terminal for the worktree
		if termCmd := a.sidebarTerminal.SetWorktree(msg.Worktree); termCmd != nil {
			cmds = append(cmds, termCmd)
		}
		newDashboard, cmd := a.dashboard.Update(msg)
		a.dashboard = newDashboard
		cmds = append(cmds, cmd)

		// Refresh git status for sidebar
		if msg.Worktree != nil {
			cmds = append(cmds, a.requestGitStatus(msg.Worktree.Root))
			// Set up file watching for this worktree
			if a.fileWatcher != nil {
				_ = a.fileWatcher.Watch(msg.Worktree.Root)
			}
		}

	case messages.ShowWelcome:
		a.goHome()

	case messages.ToggleMonitor:
		a.toggleMonitorMode()

	case messages.ToggleHelp:
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()

	case messages.ToggleKeymapHints:
		a.setKeymapHintsEnabled(!a.config.UI.ShowKeymapHints)
		if err := a.config.SaveUISettings(); err != nil {
			cmds = append(cmds, a.toast.ShowWarning("Failed to save keymap setting"))
		}

	case messages.ShowQuitDialog:
		a.showQuitDialog()

	case messages.RefreshDashboard:
		cmds = append(cmds, a.loadProjects())

	case messages.WorktreeCreatedWithWarning:
		// Worktree was created but setup had issues - still refresh and show warning
		a.err = fmt.Errorf("worktree created with warning: %s", msg.Warning)
		if msg.Worktree != nil {
			if cmd := a.dashboard.SetWorktreeCreating(msg.Worktree, false); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, a.loadProjects())

	case messages.WorktreeCreated:
		if msg.Worktree != nil {
			if cmd := a.dashboard.SetWorktreeCreating(msg.Worktree, false); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, a.loadProjects())

	case messages.WorktreeCreateFailed:
		if msg.Worktree != nil {
			if cmd := a.dashboard.SetWorktreeCreating(msg.Worktree, false); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		a.err = msg.Err
		logging.Error("Error in creating worktree: %v", msg.Err)

	case messages.GitStatusResult:
		newDashboard, cmd := a.dashboard.Update(msg)
		a.dashboard = newDashboard
		cmds = append(cmds, cmd)
		// Update sidebar if this is for the active worktree
		if a.activeWorktree != nil && msg.Root == a.activeWorktree.Root {
			a.sidebar.SetGitStatus(msg.Status)
		}

	case messages.ShowAddProjectDialog:
		logging.Info("Showing Add Project file picker")
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/"
		}
		a.filePicker = common.NewFilePicker(DialogAddProject, home, true)
		a.filePicker.SetSize(a.width, a.height)
		a.filePicker.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
		a.filePicker.Show()

	case messages.ShowCreateWorktreeDialog:
		a.dialogProject = msg.Project
		a.dialog = common.NewInputDialog(DialogCreateWorktree, "Create Worktree", "Enter worktree name...")
		a.dialog.SetSize(a.width, a.height)
		a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
		a.dialog.Show()

	case messages.ShowDeleteWorktreeDialog:
		a.dialogProject = msg.Project
		a.dialogWorktree = msg.Worktree
		a.dialog = common.NewConfirmDialog(
			DialogDeleteWorktree,
			"Delete Worktree",
			fmt.Sprintf("Delete worktree '%s' and its branch?", msg.Worktree.Name),
		)
		a.dialog.SetSize(a.width, a.height)
		a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
		a.dialog.Show()

	case messages.ShowRemoveProjectDialog:
		a.dialogProject = msg.Project
		projectName := ""
		if msg.Project != nil {
			projectName = msg.Project.Name
		}
		a.dialog = common.NewConfirmDialog(
			DialogRemoveProject,
			"Remove Project",
			fmt.Sprintf("Remove project '%s' from AMUX? This won't delete any files.", projectName),
		)
		a.dialog.SetSize(a.width, a.height)
		a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
		a.dialog.Show()

	case messages.ShowSelectAssistantDialog:
		if a.activeWorktree != nil {
			a.dialog = common.NewAgentPicker()
			a.dialog.SetSize(a.width, a.height)
			a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
			a.dialog.Show()
		}

	case messages.ShowSettingsDialog:
		a.settingsDialog = common.NewSettingsDialog(
			common.ThemeID(a.config.UI.Theme),
			a.config.UI.ShowKeymapHints,
		)
		a.settingsDialog.SetSize(a.width, a.height)
		a.settingsDialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
		a.settingsDialog.Show()

	case common.ThemePreview:
		// Live preview - apply theme without saving
		common.SetCurrentTheme(msg.Theme)
		a.styles = common.DefaultStyles()
		// Propagate styles to all components
		a.dashboard.SetStyles(a.styles)
		a.sidebar.SetStyles(a.styles)
		a.sidebarTerminal.SetStyles(a.styles)
		a.center.SetStyles(a.styles)
		a.toast.SetStyles(a.styles)
		a.helpOverlay.SetStyles(a.styles)
		if a.filePicker != nil {
			a.filePicker.SetStyles(a.styles)
		}

	case common.SettingsResult:
		a.settingsDialog = nil
		if msg.Confirmed {
			// Apply theme
			common.SetCurrentTheme(msg.Theme)
			a.config.UI.Theme = string(msg.Theme)
			a.styles = common.DefaultStyles()
			// Propagate styles to all components
			a.dashboard.SetStyles(a.styles)
			a.sidebar.SetStyles(a.styles)
			a.sidebarTerminal.SetStyles(a.styles)
			a.center.SetStyles(a.styles)
			a.toast.SetStyles(a.styles)
			a.helpOverlay.SetStyles(a.styles)
			if a.filePicker != nil {
				a.filePicker.SetStyles(a.styles)
			}

			// Apply keymap hints
			a.setKeymapHintsEnabled(msg.ShowKeymapHints)

			// Save settings
			if err := a.config.SaveUISettings(); err != nil {
				cmds = append(cmds, a.toast.ShowWarning("Failed to save settings"))
			}
		}

	case messages.CreateWorktree:
		if msg.Project != nil && msg.Name != "" {
			worktreePath := filepath.Join(
				a.config.Paths.WorktreesRoot,
				msg.Project.Name,
				msg.Name,
			)
			pending := data.NewWorktree(msg.Name, msg.Name, msg.Base, msg.Project.Path, worktreePath)
			if cmd := a.dashboard.SetWorktreeCreating(pending, true); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, a.createWorktree(msg.Project, msg.Name, msg.Base))

	case messages.DeleteWorktree:
		if msg.Worktree != nil {
			if cmd := a.dashboard.SetWorktreeDeleting(msg.Worktree.Root, true); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, a.deleteWorktree(msg.Project, msg.Worktree))

	case messages.AddProject:
		cmds = append(cmds, a.addProject(msg.Path))

	case messages.RemoveProject:
		cmds = append(cmds, a.removeProject(msg.Project))

	case messages.OpenDiff:
		logging.Info("Opening diff: %s", msg.File)
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		cmds = append(cmds, cmd)

	case messages.OpenCommitViewer:
		logging.Info("Opening commit viewer")
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		cmds = append(cmds, cmd)
		a.focusPane(messages.PaneCenter)

	case messages.ViewCommitDiff:
		logging.Info("Viewing commit diff: %s", msg.Hash)
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		cmds = append(cmds, cmd)

	case messages.CloseTab:
		cmd := a.center.CloseActiveTab()
		cmds = append(cmds, cmd)

	case messages.LaunchAgent:
		logging.Info("Launching agent: %s", msg.Assistant)
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		cmds = append(cmds, cmd)
		// Note: Focus will switch to center when TabCreated is received

	case messages.TabCreated:
		logging.Info("Tab created: %s", msg.Name)
		// Start reading from the new PTY
		cmds = append(cmds, a.center.StartPTYReaders())
		// NOW switch focus to center - tab is ready
		if a.monitorMode {
			a.focusPane(messages.PaneMonitor)
		} else {
			a.focusPane(messages.PaneCenter)
		}

	case messages.TabClosed:
		logging.Info("Tab closed: %d", msg.Index)

	case center.PTYOutput, center.PTYTick, center.PTYFlush, center.PTYStopped:
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		cmds = append(cmds, cmd)

	case messages.SidebarPTYOutput, messages.SidebarPTYTick, messages.SidebarPTYFlush, messages.SidebarPTYStopped, sidebar.SidebarTerminalCreated:
		newSidebarTerminal, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newSidebarTerminal
		cmds = append(cmds, cmd)

	case messages.GitStatusTick:
		// Refresh git status for active worktree
		if a.activeWorktree != nil {
			cmds = append(cmds, a.requestGitStatusCached(a.activeWorktree.Root))
		}
		// Continue the ticker
		cmds = append(cmds, a.startGitStatusTicker())

	case messages.FileWatcherEvent:
		// File changed, invalidate cache and refresh
		a.statusManager.Invalidate(msg.Root)
		cmds = append(cmds, a.requestGitStatus(msg.Root))
		// Continue listening for file changes
		cmds = append(cmds, a.startFileWatcher())

	case messages.WorktreeDeleted:
		if msg.Worktree != nil {
			if cmd := a.dashboard.SetWorktreeDeleting(msg.Worktree.Root, false); cmd != nil {
				cmds = append(cmds, cmd)
			}
			if a.statusManager != nil {
				a.statusManager.Invalidate(msg.Worktree.Root)
			}
		}
		cmds = append(cmds, a.loadProjects())

	case messages.ProjectRemoved:
		cmds = append(cmds, a.toast.ShowSuccess("Project removed"))
		cmds = append(cmds, a.loadProjects())

	case messages.WorktreeDeleteFailed:
		if msg.Worktree != nil {
			if cmd := a.dashboard.SetWorktreeDeleting(msg.Worktree.Root, false); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		a.err = msg.Err
		logging.Error("Error in removing worktree: %v", msg.Err)

	case messages.Error:
		a.err = msg.Err
		logging.Error("Error in %s: %v", msg.Context, msg.Err)

	case messages.ThreadExported:
		msgStr := fmt.Sprintf("Thread saved: %s", filepath.Base(msg.Path))
		if msg.Copied {
			msgStr += " (copied)"
		}
		cmds = append(cmds, a.toast.ShowSuccess(msgStr))

	case messages.ThreadExportFailed:
		logging.Error("Thread export failed: %v", msg.Err)
		cmds = append(cmds, a.toast.ShowError(fmt.Sprintf("Export failed: %v", msg.Err)))

	default:
		// Forward unknown messages to center pane (e.g., commit viewer internal messages)
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return a, tea.Batch(cmds...)
}

// handleDialogResult handles dialog completion
func (a *App) handleDialogResult(result common.DialogResult) tea.Cmd {
	project := a.dialogProject
	worktree := a.dialogWorktree
	a.dialog = nil
	a.dialogProject = nil
	a.dialogWorktree = nil
	logging.Debug("Dialog result: id=%s confirmed=%v value=%s", result.ID, result.Confirmed, result.Value)

	if !result.Confirmed {
		logging.Debug("Dialog cancelled")
		return nil
	}

	switch result.ID {
	case DialogAddProject:
		if result.Value != "" {
			path := validation.SanitizeInput(result.Value)
			logging.Info("Adding project from dialog: %s", path)
			if err := validation.ValidateProjectPath(path); err != nil {
				logging.Warn("Project path validation failed: %v", err)
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating project path"}
				}
			}
			return func() tea.Msg {
				return messages.AddProject{Path: path}
			}
		}

	case DialogCreateWorktree:
		if result.Value != "" && project != nil {
			name := validation.SanitizeInput(result.Value)
			if err := validation.ValidateWorktreeName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating worktree name"}
				}
			}
			return func() tea.Msg {
				return messages.CreateWorktree{
					Project: project,
					Name:    name,
					Base:    "HEAD",
				}
			}
		}

	case DialogDeleteWorktree:
		if project != nil && worktree != nil {
			wt := worktree
			return func() tea.Msg {
				return messages.DeleteWorktree{
					Project:  project,
					Worktree: wt,
				}
			}
		}

	case DialogRemoveProject:
		if a.dialogProject != nil {
			project := a.dialogProject
			return func() tea.Msg {
				return messages.RemoveProject{
					Project: project,
				}
			}
		}

	case DialogSelectAssistant, "agent-picker":
		if a.activeWorktree != nil {
			assistant := result.Value
			if err := validation.ValidateAssistant(assistant); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating assistant"}
				}
			}
			wt := a.activeWorktree
			return func() tea.Msg {
				return messages.LaunchAgent{
					Assistant: assistant,
					Worktree:  wt,
				}
			}
		}

	case DialogQuit:
		a.center.Close()
		a.sidebarTerminal.CloseAll()
		a.quitting = true
		return tea.Quit
	}

	return nil
}

func (a *App) showQuitDialog() {
	if a.dialog != nil && a.dialog.Visible() {
		return
	}
	a.dialog = common.NewConfirmDialog(
		DialogQuit,
		"Quit AMUX",
		"Are you sure you want to quit?",
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// Synchronized Output Mode 2026 sequences
// https://gist.github.com/christianparpart/d8a62cc1ab659194337d73e399004036
const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// View renders the application using layer-based composition.
// This uses lipgloss Canvas to compose layers directly, enabling ultraviolet's
// cell-level differential rendering for optimal performance.
func (a *App) View() tea.View {
	defer perf.Time("view")()

	baseView := func() tea.View {
		var view tea.View
		view.AltScreen = true
		view.MouseMode = tea.MouseModeCellMotion
		view.BackgroundColor = common.ColorBackground
		view.ForegroundColor = common.ColorForeground
		view.KeyboardEnhancements.ReportEventTypes = true
		return view
	}

	if a.quitting {
		view := baseView()
		view.SetContent("Goodbye!\n")
		return a.finalizeView(view)
	}

	if !a.ready {
		view := baseView()
		view.SetContent("Loading...")
		return a.finalizeView(view)
	}

	// Monitor mode uses the compositor for a full-screen grid.
	if a.monitorMode {
		return a.finalizeView(a.viewMonitorMode())
	}

	// Use layer-based rendering for normal mode
	return a.finalizeView(a.viewLayerBased())
}

// viewLayerBased renders the application using lipgloss Canvas composition.
// This enables ultraviolet to perform cell-level differential updates.
func (a *App) viewLayerBased() tea.View {
	view := tea.View{
		AltScreen:            true,
		MouseMode:            tea.MouseModeCellMotion,
		BackgroundColor:      common.ColorBackground,
		ForegroundColor:      common.ColorForeground,
		KeyboardEnhancements: tea.KeyboardEnhancements{ReportEventTypes: true},
	}

	// Create canvas at screen dimensions
	canvas := lipgloss.NewCanvas(a.width, a.height)

	// Dashboard pane (leftmost) - use chrome cache
	leftGutter := a.layout.LeftGutter()
	topGutter := a.layout.TopGutter()
	dashWidth := a.layout.DashboardWidth()
	dashHeight := a.layout.Height()
	dashFocused := a.dashboard.Focused()
	dashContent := a.dashboard.View()
	dashView := buildBorderedPane(dashContent, dashWidth, dashHeight, dashFocused)
	dashClamped := clampPane(dashView, dashWidth, dashHeight)

	var dashDrawable *compositor.StringDrawable
	if cached := a.dashboardChrome.Get(dashClamped, dashWidth, dashHeight, dashFocused, leftGutter, topGutter); cached != nil {
		dashDrawable = cached
	} else {
		dashDrawable = compositor.NewStringDrawable(dashClamped, leftGutter, topGutter)
		a.dashboardChrome.Set(dashClamped, dashWidth, dashHeight, dashFocused, leftGutter, topGutter, dashDrawable)
	}
	canvas.Compose(dashDrawable)

	// Center pane
	if a.layout.ShowCenter() {
		centerX := leftGutter + dashWidth + a.layout.GapX()
		centerWidth := a.layout.CenterWidth()
		centerHeight := a.layout.Height()
		centerFocused := a.focusedPane == messages.PaneCenter

		// Check if we can use VTermLayer for direct cell rendering
		if termLayer := a.center.TerminalLayer(); termLayer != nil && a.center.HasTabs() && !a.center.HasCommitViewer() {
			// Get terminal viewport from center model (accounts for borders, tab bar, help lines)
			termOffsetX, termOffsetY, termW, termH := a.center.TerminalViewport()
			termX := centerX + termOffsetX
			termY := topGutter + termOffsetY

			// Draw the pane chrome with borders using buildBorderedPane - use cache
			centerContent := a.center.ViewChromeOnly()
			centerView := buildBorderedPane(centerContent, centerWidth, centerHeight, centerFocused)
			hasSaveDialog := a.center.HasSaveDialog()
			if hasSaveDialog {
				centerView = a.center.OverlayDialog(centerView)
				// Don't cache when dialog is shown (it changes frequently)
				a.centerChrome.Invalidate()
			}
			centerClamped := clampPane(centerView, centerWidth, centerHeight)

			var centerDrawable *compositor.StringDrawable
			if !hasSaveDialog {
				if cached := a.centerChrome.Get(centerClamped, centerWidth, centerHeight, centerFocused, centerX, topGutter); cached != nil {
					centerDrawable = cached
				}
			}
			if centerDrawable == nil {
				centerDrawable = compositor.NewStringDrawable(centerClamped, centerX, topGutter)
				if !hasSaveDialog {
					a.centerChrome.Set(centerClamped, centerWidth, centerHeight, centerFocused, centerX, topGutter, centerDrawable)
				}
			}
			canvas.Compose(centerDrawable)

			// Compose VTermLayer on top for the terminal content
			// Position it within the pane's content area
			positionedTermLayer := &compositor.PositionedVTermLayer{
				VTermLayer: termLayer,
				PosX:       termX,
				PosY:       termY,
				Width:      termW,
				Height:     termH,
			}
			canvas.Compose(positionedTermLayer)
		} else {
			// Fallback to string-based rendering with borders (no caching - content changes)
			a.centerChrome.Invalidate()
			var centerContent string
			if a.center.HasTabs() {
				centerContent = a.center.View()
			} else {
				centerContent = a.renderCenterPaneContent()
			}
			centerView := buildBorderedPane(centerContent, centerWidth, centerHeight, centerFocused)
			if a.center.HasTabs() && a.center.HasSaveDialog() {
				centerView = a.center.OverlayDialog(centerView)
			}
			centerDrawable := compositor.NewStringDrawable(clampPane(centerView, centerWidth, centerHeight), centerX, topGutter)
			canvas.Compose(centerDrawable)
		}
	}

	// Sidebar pane (rightmost) - use chrome cache
	if a.layout.ShowSidebar() {
		sidebarX := leftGutter + a.layout.DashboardWidth()
		if a.layout.ShowCenter() {
			sidebarX += a.layout.GapX() + a.layout.CenterWidth()
		}
		if a.layout.ShowSidebar() {
			sidebarX += a.layout.GapX()
		}
		sidebarWidth := a.layout.SidebarWidth()
		sidebarHeight := a.layout.Height()
		sidebarFocused := a.focusedPane == messages.PaneSidebar || a.focusedPane == messages.PaneSidebarTerminal
		sidebarView := a.renderSidebarPane()
		sidebarClamped := clampPane(sidebarView, sidebarWidth, sidebarHeight)

		var sidebarDrawable *compositor.StringDrawable
		if cached := a.sidebarChrome.Get(sidebarClamped, sidebarWidth, sidebarHeight, sidebarFocused, sidebarX, topGutter); cached != nil {
			sidebarDrawable = cached
		} else {
			sidebarDrawable = compositor.NewStringDrawable(sidebarClamped, sidebarX, topGutter)
			a.sidebarChrome.Set(sidebarClamped, sidebarWidth, sidebarHeight, sidebarFocused, sidebarX, topGutter, sidebarDrawable)
		}
		canvas.Compose(sidebarDrawable)
	}

	// Overlay layers (dialogs, toasts, etc.)
	a.composeOverlays(canvas)

	view.SetContent(syncBegin + canvas.Render() + syncEnd)
	view.Cursor = a.overlayCursor()
	return view
}

// composeOverlays adds overlay layers (dialogs, toasts, help, etc.) to the canvas.
func (a *App) composeOverlays(canvas *lipgloss.Canvas) {
	// Dialog overlay
	if a.dialog != nil && a.dialog.Visible() {
		dialogView := a.dialog.View()
		dialogWidth, dialogHeight := viewDimensions(dialogView)
		x, y := a.centeredPosition(dialogWidth, dialogHeight)
		dialogDrawable := compositor.NewStringDrawable(dialogView, x, y)
		canvas.Compose(dialogDrawable)
	}

	// File picker overlay
	if a.filePicker != nil && a.filePicker.Visible() {
		pickerView := a.filePicker.View()
		pickerWidth, pickerHeight := viewDimensions(pickerView)
		x, y := a.centeredPosition(pickerWidth, pickerHeight)
		pickerDrawable := compositor.NewStringDrawable(pickerView, x, y)
		canvas.Compose(pickerDrawable)
	}

	// Settings dialog overlay
	if a.settingsDialog != nil && a.settingsDialog.Visible() {
		settingsView := a.settingsDialog.View()
		settingsWidth, settingsHeight := viewDimensions(settingsView)
		x, y := a.centeredPosition(settingsWidth, settingsHeight)
		settingsDrawable := compositor.NewStringDrawable(settingsView, x, y)
		canvas.Compose(settingsDrawable)
	}

	// Help overlay (replaces entire content)
	if a.helpOverlay.Visible() {
		helpView := a.helpOverlay.View()
		helpDrawable := compositor.NewStringDrawable(helpView, 0, 0)
		canvas.Compose(helpDrawable)
	}

	// Prefix mode indicator
	if a.prefixActive {
		indicator := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(lipgloss.Color("#7aa2f7")).
			Padding(0, 1).
			Render("PREFIX")
		indicatorWidth := lipgloss.Width(indicator)
		x := a.width - indicatorWidth - 1
		y := a.height - 3
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		prefixDrawable := compositor.NewStringDrawable(indicator, x, y)
		canvas.Compose(prefixDrawable)
	}

	// Toast notification
	if a.toast.Visible() {
		toastView := a.toast.View()
		if toastView != "" {
			toastWidth := lipgloss.Width(toastView)
			x := (a.width - toastWidth) / 2
			y := a.height - 2
			if x < 0 {
				x = 0
			}
			if y < 0 {
				y = 0
			}
			toastDrawable := compositor.NewStringDrawable(toastView, x, y)
			canvas.Compose(toastDrawable)
		}
	}

	// Error overlay
	if a.err != nil {
		errView := a.renderErrorOverlay()
		errWidth, errHeight := viewDimensions(errView)
		x, y := a.centeredPosition(errWidth, errHeight)
		errDrawable := compositor.NewStringDrawable(errView, x, y)
		canvas.Compose(errDrawable)
	}
}

// viewMonitorMode renders monitor mode using layer-based compositing.
func (a *App) viewMonitorMode() tea.View {
	view := tea.View{
		AltScreen:            true,
		MouseMode:            tea.MouseModeCellMotion,
		BackgroundColor:      common.ColorBackground,
		ForegroundColor:      common.ColorForeground,
		KeyboardEnhancements: tea.KeyboardEnhancements{ReportEventTypes: true},
	}

	// Create canvas at screen dimensions
	canvas := lipgloss.NewCanvas(a.width, a.height)

	// Render monitor grid content
	gridContent := a.renderMonitorGrid()
	gridDrawable := compositor.NewStringDrawable(gridContent, 0, 0)
	canvas.Compose(gridDrawable)

	// Render styled toolbar at top (composed separately to support styled buttons)
	toolbarContent := a.monitorHeaderText()
	toolbarDrawable := compositor.NewStringDrawable(toolbarContent, 0, 0)
	canvas.Compose(toolbarDrawable)

	// Compose overlays using the same layer-based approach as normal mode
	a.composeOverlays(canvas)

	view.SetContent(syncBegin + canvas.Render() + syncEnd)
	view.Cursor = a.overlayCursor()
	return view
}

// renderErrorOverlay returns the error overlay content.
func (a *App) renderErrorOverlay() string {
	if a.err == nil {
		return ""
	}
	errStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#f7768e")).
		Padding(1, 2).
		MaxWidth(60)
	return errStyle.Render("Error: " + a.err.Error() + "\n\nPress any key to dismiss.")
}

func (a *App) finalizeView(view tea.View) tea.View {
	if a.pendingInputLatency {
		perf.Record("input_latency", time.Since(a.lastInputAt))
		a.pendingInputLatency = false
	}
	return view
}

func clampPane(view string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		Render(view)
}

func viewDimensions(view string) (width, height int) {
	lines := strings.Split(view, "\n")
	height = len(lines)
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	return width, height
}

func (a *App) centeredPosition(width, height int) (x, y int) {
	x = (a.width - width) / 2
	y = (a.height - height) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

func (a *App) adjustSidebarMouseY(y int) int {
	if a.layout == nil {
		return y
	}
	// Sidebar content starts one row below the top border.
	return y - a.layout.TopGutter() - 1
}

func (a *App) overlayCursor() *tea.Cursor {
	if a.dialog != nil && a.dialog.Visible() {
		if c := a.dialog.Cursor(); c != nil {
			dialogView := a.dialog.View()
			dialogWidth, dialogHeight := viewDimensions(dialogView)
			x, y := a.centeredPosition(dialogWidth, dialogHeight)
			c.X += x
			c.Y += y
			return c
		}
		return nil
	}

	if a.filePicker != nil && a.filePicker.Visible() {
		if c := a.filePicker.Cursor(); c != nil {
			pickerView := a.filePicker.View()
			pickerWidth, pickerHeight := viewDimensions(pickerView)
			x, y := a.centeredPosition(pickerWidth, pickerHeight)
			c.X += x
			c.Y += y
			return c
		}
	}

	return nil
}

func (a *App) centerPaneStyle() lipgloss.Style {
	width := a.layout.CenterWidth()
	height := a.layout.Height()

	style := lipgloss.NewStyle().
		Width(width-2).
		Height(height-2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(common.ColorBorder).
		Padding(0, 1)

	if a.focusedPane == messages.PaneCenter {
		style = style.
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(common.ColorBorderFocused)
	}
	return style
}

// renderCenterPaneContent renders the center pane content when no tabs (raw content, no borders)
func (a *App) renderCenterPaneContent() string {
	if a.showWelcome {
		return a.renderWelcome()
	}

	if a.activeWorktree != nil {
		return a.renderWorktreeInfo()
	}

	return "Select a worktree from the dashboard"
}

func (a *App) centerPaneContentOrigin() (x, y int) {
	if a.layout == nil {
		return 0, 0
	}
	frameX, frameY := a.centerPaneStyle().GetFrameSize()
	gapX := 0
	if a.layout.ShowCenter() {
		gapX = a.layout.GapX()
	}
	return a.layout.LeftGutter() + a.layout.DashboardWidth() + gapX + frameX/2, a.layout.TopGutter() + frameY/2
}

func (a *App) goHome() {
	a.showWelcome = true
	a.activeWorktree = nil
	a.center.SetWorktree(nil)
	a.sidebar.SetWorktree(nil)
	a.sidebar.SetGitStatus(nil)
	_ = a.sidebarTerminal.SetWorktree(nil)
	a.dashboard.ClearActiveRoot()
}

func (a *App) renderMonitorGrid() string {
	if a.width <= 0 || a.height <= 0 {
		return ""
	}

	allTabs := a.center.MonitorTabs()
	tabs := a.filterMonitorTabs(allTabs)
	if len(tabs) == 0 {
		canvas := a.monitorCanvasFor(a.width, a.height)
		canvas.Fill(vterm.Style{Fg: compositor.HexColor(common.HexColor(common.ColorForeground)), Bg: compositor.HexColor(common.HexColor(common.ColorBackground))})
		empty := "No agents running"
		if a.monitorFilter != "" && len(allTabs) > 0 {
			empty = "No agents for " + a.monitorFilterLabel(a.monitorFilter)
		}
		x := (a.width - ansi.StringWidth(empty)) / 2
		y := a.height / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		canvas.DrawText(x, y, empty, vterm.Style{Fg: compositor.HexColor(common.HexColor(common.ColorMuted))})
		return canvas.Render()
	}

	gridX, gridY, gridW, gridH := a.monitorGridArea()
	grid := monitorGridLayout(len(tabs), gridW, gridH)
	if grid.cols == 0 || grid.rows == 0 {
		return ""
	}

	tabSizes := make([]center.TabSize, 0, len(tabs))
	for i, tab := range tabs {
		rect := monitorTileRect(grid, i, gridX, gridY)
		contentW := rect.W - 2
		contentH := rect.H - 3 // border + header line
		if contentW < 1 {
			contentW = 1
		}
		if contentH < 1 {
			contentH = 1
		}
		tabSizes = append(tabSizes, center.TabSize{
			ID:     tab.ID,
			Width:  contentW,
			Height: contentH,
		})
	}

	layoutKey := a.monitorLayoutKeyFor(tabs, gridW, gridH, tabSizes)
	if layoutKey != a.monitorLayoutKey {
		a.center.ResizeTabs(tabSizes)
		a.monitorLayoutKey = layoutKey
	}

	snapshots := a.center.MonitorTabSnapshots()
	snapByID := make(map[center.TabID]center.MonitorTabSnapshot, len(snapshots))
	for _, snap := range snapshots {
		snapByID[snap.ID] = snap
	}

	canvas := a.monitorCanvasFor(a.width, a.height)
	canvas.Fill(vterm.Style{
		Fg: compositor.HexColor(common.HexColor(common.ColorForeground)),
		Bg: compositor.HexColor(common.HexColor(common.ColorBackground)),
	})

	// Header/toolbar is rendered separately in viewMonitorMode to support styled buttons

	projectNames := make(map[string]string, len(a.projects))
	for _, project := range a.projects {
		projectNames[project.Path] = project.Name
	}

	selectedIndex := a.center.MonitorSelectedIndex(len(tabs))

	for idx, tab := range tabs {
		rect := monitorTileRect(grid, idx, gridX, gridY)
		focused := idx == selectedIndex
		if rect.W < 4 || rect.H < 4 {
			continue
		}

		borderStyle := vterm.Style{Fg: compositor.HexColor(common.HexColor(common.ColorBorder))}
		if focused {
			borderStyle.Fg = compositor.HexColor(common.HexColor(common.ColorBorderFocused))
		}
		canvas.DrawBorder(rect.X, rect.Y, rect.W, rect.H, borderStyle, focused)

		innerX := rect.X + 1
		innerY := rect.Y + 1
		innerW := rect.W - 2
		innerH := rect.H - 2
		if innerW < 1 || innerH < 1 {
			continue
		}

		worktreeName := "unknown"
		if tab.Worktree != nil && tab.Worktree.Name != "" {
			worktreeName = tab.Worktree.Name
		}
		projectName := ""
		if tab.Worktree != nil {
			projectName = projectNames[tab.Worktree.Repo]
		}
		if projectName == "" {
			projectName = monitorProjectName(tab.Worktree)
		}

		statusIcon := common.Icons.Idle
		if tab.Running {
			statusIcon = common.Icons.Running
		}

		assistant := tab.Name
		if assistant == "" {
			assistant = tab.Assistant
		}

		cursor := common.Icons.CursorEmpty
		if focused {
			cursor = common.Icons.Cursor
		}
		header := fmt.Sprintf("%s %s %s/%s", cursor, statusIcon, projectName, worktreeName)
		if assistant != "" {
			header += " [" + assistant + "]"
		}

		hStyle := vterm.Style{Fg: compositor.HexColor(common.HexColor(common.ColorForeground)), Bold: true}
		if focused {
			hStyle.Bg = compositor.HexColor(common.HexColor(common.ColorSelection))
		}
		canvas.DrawText(innerX, innerY, header, hStyle)

		contentY := innerY + 1
		contentH := innerH - 1
		if contentH <= 0 {
			continue
		}

		snap, ok := snapByID[tab.ID]
		if !ok || len(snap.Screen) == 0 {
			canvas.DrawText(innerX, contentY, "No active agent", vterm.Style{Fg: compositor.HexColor(common.HexColor(common.ColorMuted))})
			continue
		}

		canvas.DrawScreen(
			innerX,
			contentY,
			innerW,
			contentH,
			snap.Screen,
			snap.CursorX,
			snap.CursorY,
			focused,
			snap.ViewOffset,
			snap.SelActive,
			snap.SelStartX,
			snap.SelStartY,
			snap.SelEndX,
			snap.SelEndY,
		)
	}

	return canvas.Render()
}

func (a *App) monitorHeaderText() string {
	// Build filter buttons
	filters := a.monitorProjectFilters()
	activeStyle := lipgloss.NewStyle().
		Foreground(common.ColorForeground).
		Background(common.ColorBackground).
		Bold(true).
		Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted).
		Background(common.ColorBackground).
		Padding(0, 1)

	var filterButtons strings.Builder
	if a.monitorFilter == "" {
		filterButtons.WriteString(activeStyle.Render("[All]"))
	} else {
		filterButtons.WriteString(inactiveStyle.Render("[All]"))
	}
	for _, filter := range filters {
		btnText := "[" + filter.Label + "]"
		if a.monitorFilter == filter.Key {
			filterButtons.WriteString(activeStyle.Render(btnText))
		} else {
			filterButtons.WriteString(inactiveStyle.Render(btnText))
		}
	}

	exitBtn := inactiveStyle.Render("[Exit]")
	filtersStr := filterButtons.String()
	filtersWidth := ansi.StringWidth(filtersStr)
	exitWidth := ansi.StringWidth(exitBtn)
	padding := a.width - filtersWidth - exitWidth
	if padding < 0 {
		padding = 0
	}
	return filtersStr + strings.Repeat(" ", padding) + exitBtn
}

type monitorProjectFilter struct {
	Key   string
	Label string
}

func (a *App) monitorProjectFilters() []monitorProjectFilter {
	tabs := a.center.MonitorTabs()
	seen := make(map[string]bool)
	filters := make([]monitorProjectFilter, 0, len(tabs))
	for _, tab := range tabs {
		if tab.Worktree == nil {
			continue
		}
		key, label := a.monitorProjectKeyLabel(tab.Worktree)
		if key == "" {
			continue
		}
		if label == "" {
			label = key
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		filters = append(filters, monitorProjectFilter{Key: key, Label: label})
	}

	if len(filters) == 0 {
		return filters
	}

	labelCounts := make(map[string]int, len(filters))
	for _, filter := range filters {
		labelCounts[filter.Label]++
	}

	if len(labelCounts) == len(filters) {
		return filters
	}

	for i, filter := range filters {
		if labelCounts[filter.Label] > 1 {
			suffix := filepath.Base(filepath.Dir(filter.Key))
			if suffix == "" || suffix == "." || suffix == string(filepath.Separator) {
				suffix = filepath.Base(filter.Key)
			}
			if suffix == "" || suffix == filter.Label {
				suffix = filter.Key
			}
			filters[i].Label = fmt.Sprintf("%s (%s)", filter.Label, suffix)
		}
	}

	return filters
}

func (a *App) monitorProjectKeyLabel(wt *data.Worktree) (string, string) {
	if wt == nil {
		return "", ""
	}
	if project := a.projectForWorktree(wt); project != nil {
		key := project.Path
		if key == "" {
			key = wt.Repo
		}
		if key == "" {
			key = wt.Root
		}
		label := project.Name
		if label == "" {
			label = monitorProjectName(wt)
		}
		return key, label
	}
	key := wt.Repo
	if key == "" {
		key = wt.Root
	}
	label := monitorProjectName(wt)
	if label == "" {
		label = key
	}
	return key, label
}

func (a *App) monitorFilterLabel(key string) string {
	if key == "" {
		return "All"
	}
	for _, filter := range a.monitorProjectFilters() {
		if filter.Key == key {
			return filter.Label
		}
	}
	if base := filepath.Base(key); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return key
}

func (a *App) filterMonitorTabs(tabs []center.MonitorTab) []center.MonitorTab {
	if a.monitorFilter == "" {
		return tabs
	}
	var filtered []center.MonitorTab
	for _, tab := range tabs {
		if tab.Worktree == nil {
			continue
		}
		key, _ := a.monitorProjectKeyLabel(tab.Worktree)
		if key == a.monitorFilter {
			filtered = append(filtered, tab)
		}
	}
	return filtered
}

func (a *App) monitorExitHit(x, y int) bool {
	if y != 0 {
		return false
	}
	header := a.monitorHeaderText()
	exitText := "[Exit]"
	headerStripped := ansi.Strip(header)
	idx := strings.Index(headerStripped, exitText)
	if idx < 0 {
		return false
	}
	start := idx
	end := start + len(exitText)
	return x >= start && x < end
}

func (a *App) monitorFilterHit(x, y int) (string, bool) {
	if y != 0 {
		return "", false
	}
	header := a.monitorHeaderText()
	headerStripped := ansi.Strip(header)

	// Check "All" button
	allText := "[All]"
	allIdx := strings.Index(headerStripped, allText)
	if allIdx >= 0 && x >= allIdx && x < allIdx+len(allText) {
		return "", true
	}

	// Check project buttons
	filters := a.monitorProjectFilters()
	for _, filter := range filters {
		btnText := "[" + filter.Label + "]"
		idx := strings.Index(headerStripped, btnText)
		if idx >= 0 && x >= idx && x < idx+len(btnText) {
			return filter.Key, true
		}
	}
	return "", false
}

type monitorGrid struct {
	cols       int
	rows       int
	colWidths  []int
	rowHeights []int
	gapX       int
	gapY       int
}

func monitorGridLayout(count, width, height int) monitorGrid {
	grid := monitorGrid{
		gapX: 1,
		gapY: 1,
	}
	if count <= 0 || width <= 0 || height <= 0 {
		return grid
	}

	minTileWidth := 20
	minTileHeight := 6
	bestCols := 1
	bestScore := -1
	bestArea := -1

	for cols := 1; cols <= count; cols++ {
		rows := (count + cols - 1) / cols
		gridWidth := width - grid.gapX*(cols-1)
		gridHeight := height - grid.gapY*(rows-1)
		if gridWidth <= 0 || gridHeight <= 0 {
			continue
		}

		tileWidth := gridWidth / cols
		tileHeight := gridHeight / rows
		if tileWidth <= 0 || tileHeight <= 0 {
			continue
		}

		score := tileWidth
		if tileHeight < score {
			score = tileHeight
		}
		if tileWidth < minTileWidth || tileHeight < minTileHeight {
			score /= 2
		}
		area := tileWidth * tileHeight
		if score > bestScore || (score == bestScore && area > bestArea) {
			bestScore = score
			bestArea = area
			bestCols = cols
		}
	}

	rows := (count + bestCols - 1) / bestCols
	gridWidth := width - grid.gapX*(bestCols-1)
	if gridWidth < bestCols {
		gridWidth = bestCols
	}
	gridHeight := height - grid.gapY*(rows-1)
	if gridHeight < rows {
		gridHeight = rows
	}

	grid.cols = bestCols
	grid.rows = rows
	grid.colWidths = make([]int, bestCols)
	grid.rowHeights = make([]int, rows)

	baseCol := gridWidth / bestCols
	extraCol := gridWidth % bestCols
	for i := 0; i < bestCols; i++ {
		grid.colWidths[i] = baseCol
		if i < extraCol {
			grid.colWidths[i]++
		}
	}

	baseRow := gridHeight / rows
	extraRow := gridHeight % rows
	for i := 0; i < rows; i++ {
		grid.rowHeights[i] = baseRow
		if i < extraRow {
			grid.rowHeights[i]++
		}
	}

	return grid
}

type monitorRect struct {
	X int
	Y int
	W int
	H int
}

func monitorTileRect(grid monitorGrid, index int, offsetX, offsetY int) monitorRect {
	if grid.cols == 0 || grid.rows == 0 {
		return monitorRect{}
	}
	row := index / grid.cols
	col := index % grid.cols
	if row < 0 || col < 0 || row >= len(grid.rowHeights) || col >= len(grid.colWidths) {
		return monitorRect{}
	}

	x := offsetX
	for i := 0; i < col; i++ {
		x += grid.colWidths[i] + grid.gapX
	}
	y := offsetY
	for i := 0; i < row; i++ {
		y += grid.rowHeights[i] + grid.gapY
	}

	return monitorRect{
		X: x,
		Y: y,
		W: grid.colWidths[col],
		H: grid.rowHeights[row],
	}
}

func (a *App) monitorGridArea() (int, int, int, int) {
	if a.height <= 2 {
		return 0, 0, a.width, a.height
	}
	return 0, 1, a.width, a.height - 1
}

func (a *App) monitorCanvasFor(width, height int) *compositor.Canvas {
	if width <= 0 || height <= 0 {
		width = 1
		height = 1
	}
	if a.monitorCanvas == nil {
		a.monitorCanvas = compositor.NewCanvas(width, height)
	} else if a.monitorCanvas.Width != width || a.monitorCanvas.Height != height {
		a.monitorCanvas.Resize(width, height)
	}
	return a.monitorCanvas
}

func (a *App) monitorLayoutKeyFor(tabs []center.MonitorTab, gridW, gridH int, sizes []center.TabSize) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%dx%d:%d|", gridW, gridH, len(tabs))
	for i, tab := range tabs {
		b.WriteString(string(tab.ID))
		if i < len(sizes) {
			fmt.Fprintf(&b, ":%dx%d", sizes[i].Width, sizes[i].Height)
		}
		b.WriteString("|")
	}
	return b.String()
}

func (a *App) handleMonitorInput(msg tea.KeyPressMsg) tea.Cmd {
	// Monitor mode - type to interact:
	// Enter -> Select and Open (exit monitor and switch to tile)
	// All other keys -> Forward to selected tile's PTY

	if key.Matches(msg, a.keymap.Enter) {
		return a.exitMonitorToSelection()
	}

	// Forward all other keys to selected tile's PTY
	tabs := a.center.MonitorTabs()
	if len(tabs) == 0 {
		return nil
	}
	idx := a.center.MonitorSelectedIndex(len(tabs))
	return a.center.HandleMonitorInput(tabs[idx].ID, msg)
}

func (a *App) activateMonitorSelection() tea.Cmd {
	snapshots := a.center.MonitorSnapshots()
	if len(snapshots) == 0 {
		return nil
	}
	idx := a.center.MonitorSelectedIndex(len(snapshots))
	snap := snapshots[idx]
	if snap.Worktree == nil {
		return nil
	}
	project := a.projectForWorktree(snap.Worktree)
	return func() tea.Msg {
		return messages.WorktreeActivated{Project: project, Worktree: snap.Worktree}
	}
}

func (a *App) exitMonitorToSelection() tea.Cmd {
	cmd := a.activateMonitorSelection()
	a.monitorMode = false
	a.monitorLayoutKey = ""
	a.focusPane(messages.PaneCenter)
	a.updateLayout()
	return cmd
}

func (a *App) projectForWorktree(wt *data.Worktree) *data.Project {
	if wt == nil {
		return nil
	}
	for i := range a.projects {
		project := &a.projects[i]
		if project.Path == wt.Repo {
			return project
		}
		for j := range project.Worktrees {
			if project.Worktrees[j].Root == wt.Root {
				return project
			}
		}
	}
	return nil
}

func (a *App) selectMonitorTile(paneX, paneY int) (int, bool) {
	tabs := a.center.MonitorTabs()
	count := len(tabs)
	if count == 0 {
		return -1, false
	}

	gridX, gridY, gridW, gridH := a.monitorGridArea()
	x := paneX - gridX
	y := paneY - gridY
	if x < 0 || y < 0 || x >= gridW || y >= gridH {
		return -1, false
	}

	grid := monitorGridLayout(count, gridW, gridH)
	if grid.cols == 0 || grid.rows == 0 {
		return -1, false
	}

	col := -1
	for c := 0; c < grid.cols; c++ {
		if x < grid.colWidths[c] {
			col = c
			break
		}
		x -= grid.colWidths[c]
		if c < grid.cols-1 {
			if x < grid.gapX {
				return -1, false
			}
			x -= grid.gapX
		}
	}

	row := -1
	for r := 0; r < grid.rows; r++ {
		if y < grid.rowHeights[r] {
			row = r
			break
		}
		y -= grid.rowHeights[r]
		if r < grid.rows-1 {
			if y < grid.gapY {
				return -1, false
			}
			y -= grid.gapY
		}
	}

	if row < 0 || col < 0 {
		return -1, false
	}

	index := row*grid.cols + col
	if index >= 0 && index < count {
		a.center.SetMonitorSelectedIndex(index, count)
		return index, true
	}
	return -1, false
}

func monitorProjectName(wt *data.Worktree) string {
	if wt == nil {
		return "unknown"
	}
	if wt.Repo != "" {
		return filepath.Base(wt.Repo)
	}
	if wt.Root != "" {
		return filepath.Base(wt.Root)
	}
	return "unknown"
}

// renderWorktreeInfo renders information about the active worktree
func (a *App) renderWorktreeInfo() string {
	wt := a.activeWorktree

	title := a.styles.Title.Render(wt.Name)
	content := title + "\n\n"
	content += fmt.Sprintf("Branch: %s\n", wt.Branch)
	content += fmt.Sprintf("Path: %s\n", wt.Root)

	if a.activeProject != nil {
		content += fmt.Sprintf("Project: %s\n", a.activeProject.Name)
	}

	agentBtn := a.styles.TabPlus.Render("New agent")
	commitsBtn := a.styles.TabPlus.Render("Commits")
	content += "\n" + lipgloss.JoinHorizontal(lipgloss.Bottom, agentBtn, commitsBtn)
	if a.config.UI.ShowKeymapHints {
		content += "\n" + a.styles.Help.Render("C-Spc a:new agent  C-Spc d:commits")
	}

	return content
}

// renderWelcome renders the welcome screen
func (a *App) renderWelcome() string {
	content := a.welcomeContent()

	// Center the content in the pane
	width := a.layout.CenterWidth() - 4 // Account for borders/padding
	height := a.layout.Height() - 4

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (a *App) welcomeContent() string {
	logo, logoStyle := a.welcomeLogo()
	var b strings.Builder
	b.WriteString(logoStyle.Render(logo))
	b.WriteString("\n\n")
	newProject := a.styles.TabPlus.Render("New project")
	settingsBtn := a.styles.TabPlus.Render("Settings")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Left, newProject, "  ", settingsBtn))
	b.WriteString("\n")
	if a.config.UI.ShowKeymapHints {
		b.WriteString(a.styles.Help.Render("Dashboard: j/k to move • Enter to select"))
	}
	return b.String()
}

func centerOffset(container, content int) int {
	if container <= content {
		return 0
	}
	return (container - content) / 2
}

func findButtonRegion(lines []string, button string) (common.HitRegion, bool) {
	buttonLines := strings.Split(button, "\n")
	if len(buttonLines) == 0 {
		return common.HitRegion{}, false
	}
	strippedButtonLines := make([]string, len(buttonLines))
	for i, line := range buttonLines {
		strippedButtonLines[i] = ansi.Strip(line)
		if strippedButtonLines[i] == "" && len(buttonLines) == 1 {
			return common.HitRegion{}, false
		}
	}
	buttonWidth, buttonHeight := viewDimensions(button)

	for i := 0; i+len(buttonLines) <= len(lines); i++ {
		strippedLine := ansi.Strip(lines[i])
		idx := strings.Index(strippedLine, strippedButtonLines[0])
		if idx < 0 {
			continue
		}
		start := ansi.StringWidth(strippedLine[:idx])
		matched := true
		for j := 1; j < len(buttonLines); j++ {
			lineStripped := ansi.Strip(lines[i+j])
			if idx >= len(lineStripped) || !strings.HasPrefix(lineStripped[idx:], strippedButtonLines[j]) {
				matched = false
				break
			}
		}
		if matched {
			return common.HitRegion{
				X:      start,
				Y:      i,
				Width:  buttonWidth,
				Height: buttonHeight,
			}, true
		}
	}

	return common.HitRegion{}, false
}

func (a *App) welcomeLogo() (string, lipgloss.Style) {
	logo := `
 8888b.  88888b.d88b.  888  888 888  888
    "88b 888 "888 "88b 888  888  Y8bd8P
.d888888 888  888  888 888  888   X88K
888  888 888  888  888 Y88b 888 .d8""8b.
"Y888888 888  888  888  "Y88888 888  888`

	logoStyle := lipgloss.NewStyle().
		Foreground(common.ColorPrimary).
		Bold(true)
	return logo, logoStyle
}

func (a *App) handleCenterPaneClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	if a.layout == nil || !a.layout.ShowCenter() || a.center.HasTabs() {
		return nil
	}
	dashWidth := a.layout.DashboardWidth()
	centerWidth := a.layout.CenterWidth()
	gapX := a.layout.GapX()
	if centerWidth <= 0 {
		return nil
	}
	centerStart := a.layout.LeftGutter() + dashWidth + gapX
	centerEnd := centerStart + centerWidth
	if msg.X < centerStart || msg.X >= centerEnd {
		return nil
	}
	contentX, contentY := a.centerPaneContentOrigin()
	localX := msg.X - contentX
	localY := msg.Y - contentY
	if localX < 0 || localY < 0 {
		return nil
	}

	if a.showWelcome {
		return a.handleWelcomeClick(localX, localY)
	}
	if a.activeWorktree != nil {
		return a.handleWorktreeInfoClick(localX, localY)
	}
	return nil
}

func (a *App) handleWelcomeClick(localX, localY int) tea.Cmd {
	content := a.welcomeContent()
	lines := strings.Split(content, "\n")
	contentWidth, contentHeight := viewDimensions(content)

	// Match the width/height used by renderWelcome for centering.
	placeWidth := a.layout.CenterWidth() - 4
	placeHeight := a.layout.Height() - 4
	if placeWidth <= 0 || placeHeight <= 0 {
		return nil
	}

	offsetX := centerOffset(placeWidth, contentWidth)
	offsetY := centerOffset(placeHeight, contentHeight)

	// Both buttons are on the same line, find them by searching for plain text
	for i, line := range lines {
		strippedLine := ansi.Strip(line)

		// Settings button - check first so it's not blocked by New project's region
		settingsText := "Settings"
		if idx := strings.Index(strippedLine, settingsText); idx >= 0 {
			region := common.HitRegion{
				X:      idx + offsetX,
				Y:      i + offsetY,
				Width:  len(settingsText),
				Height: 1,
			}
			if region.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowSettingsDialog{} }
			}
		}

		// New project button
		newProjectText := "New project"
		if idx := strings.Index(strippedLine, newProjectText); idx >= 0 {
			region := common.HitRegion{
				X:      idx + offsetX,
				Y:      i + offsetY,
				Width:  len(newProjectText),
				Height: 1,
			}
			if region.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowAddProjectDialog{} }
			}
		}
	}

	return nil
}

func (a *App) handleWorktreeInfoClick(localX, localY int) tea.Cmd {
	if a.activeWorktree == nil {
		return nil
	}
	content := a.renderWorktreeInfo()
	lines := strings.Split(content, "\n")

	agentBtn := a.styles.TabPlus.Render("New agent")
	if region, ok := findButtonRegion(lines, agentBtn); ok {
		if region.Contains(localX, localY) {
			return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
		}
	}

	commitsBtn := a.styles.TabPlus.Render("Commits")
	if region, ok := findButtonRegion(lines, commitsBtn); ok {
		if region.Contains(localX, localY) {
			wt := a.activeWorktree
			return func() tea.Msg { return messages.OpenCommitViewer{Worktree: wt} }
		}
	}

	return nil
}

// loadProjects loads all registered projects and their worktrees
func (a *App) loadProjects() tea.Cmd {
	return func() tea.Msg {
		paths, err := a.registry.Projects()
		if err != nil {
			return messages.Error{Err: err, Context: "loading projects"}
		}

		var projects []data.Project
		for _, path := range paths {
			if !git.IsGitRepository(path) {
				continue
			}

			project := data.NewProject(path)
			worktrees, err := git.DiscoverWorktrees(project)
			if err != nil {
				continue
			}
			for i := range worktrees {
				wt := &worktrees[i]
				meta, err := a.metadata.Load(wt)
				if err != nil {
					logging.Warn("Failed to load metadata for %s: %v", wt.Root, err)
					continue
				}
				if meta.Base != "" {
					wt.Base = meta.Base
				}
				if meta.Created != "" {
					if createdAt, err := time.Parse(time.RFC3339, meta.Created); err == nil {
						wt.Created = createdAt
					} else if createdAt, err := time.Parse(time.RFC3339Nano, meta.Created); err == nil {
						wt.Created = createdAt
					} else {
						logging.Warn("Failed to parse worktree created time for %s: %v", wt.Root, err)
					}
				}
			}
			project.Worktrees = worktrees
			projects = append(projects, *project)
		}

		return messages.ProjectsLoaded{Projects: projects}
	}
}

// requestGitStatus requests git status for a worktree (always fetches fresh)
func (a *App) requestGitStatus(root string) tea.Cmd {
	return func() tea.Msg {
		status, err := git.GetStatus(root)
		// Update cache directly (no async refresh needed, we just fetched)
		if a.statusManager != nil && err == nil {
			a.statusManager.UpdateCache(root, status)
		}
		return messages.GitStatusResult{
			Root:   root,
			Status: status,
			Err:    err,
		}
	}
}

// requestGitStatusCached requests git status using cache if available
func (a *App) requestGitStatusCached(root string) tea.Cmd {
	// Check cache first
	if a.statusManager != nil {
		if cached := a.statusManager.GetCached(root); cached != nil {
			return func() tea.Msg {
				return messages.GitStatusResult{
					Root:   root,
					Status: cached,
					Err:    nil,
				}
			}
		}
	}
	// Cache miss, fetch fresh
	return a.requestGitStatus(root)
}

// addProject adds a new project to the registry
func (a *App) addProject(path string) tea.Cmd {
	return func() tea.Msg {
		logging.Info("Adding project: %s", path)

		// Expand path
		if len(path) > 0 && path[0] == '~' {
			home, err := os.UserHomeDir()
			if err == nil {
				path = filepath.Join(home, path[1:])
				logging.Debug("Expanded path to: %s", path)
			}
		}

		// Verify it's a git repo
		if !git.IsGitRepository(path) {
			logging.Warn("Path is not a git repository: %s", path)
			return messages.Error{
				Err:     fmt.Errorf("not a git repository: %s", path),
				Context: "adding project",
			}
		}

		// Add to registry
		if err := a.registry.AddProject(path); err != nil {
			logging.Error("Failed to add project to registry: %v", err)
			return messages.Error{Err: err, Context: "adding project"}
		}

		logging.Info("Project added successfully: %s", path)
		return messages.RefreshDashboard{}
	}
}

// createWorktree creates a new git worktree
func (a *App) createWorktree(project *data.Project, name, base string) tea.Cmd {
	return func() (msg tea.Msg) {
		var wt *data.Worktree
		defer func() {
			if r := recover(); r != nil {
				logging.Error("panic in createWorktree: %v", r)
				msg = messages.WorktreeCreateFailed{
					Worktree: wt,
					Err:      fmt.Errorf("create worktree panicked: %v", r),
				}
			}
		}()

		if project == nil || name == "" {
			return messages.WorktreeCreateFailed{
				Err: fmt.Errorf("missing project or worktree name"),
			}
		}

		worktreePath := filepath.Join(
			a.config.Paths.WorktreesRoot,
			project.Name,
			name,
		)

		branch := name
		wt = data.NewWorktree(name, branch, base, project.Path, worktreePath)

		if err := git.CreateWorktree(project.Path, worktreePath, branch, base); err != nil {
			return messages.WorktreeCreateFailed{
				Worktree: wt,
				Err:      err,
			}
		}

		// Wait for .git file to exist (race condition from git worktree add)
		gitPath := filepath.Join(worktreePath, ".git")
		for i := 0; i < 10; i++ {
			if _, err := os.Stat(gitPath); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		meta := &data.Metadata{
			Name:       name,
			Branch:     branch,
			Repo:       project.Path,
			Base:       base,
			Created:    time.Now().Format(time.RFC3339),
			Assistant:  "claude",
			ScriptMode: "nonconcurrent",
			Env:        make(map[string]string),
		}

		if err := a.metadata.Save(wt, meta); err != nil {
			_ = git.RemoveWorktree(project.Path, worktreePath)
			_ = git.DeleteBranch(project.Path, branch)
			return messages.WorktreeCreateFailed{
				Worktree: wt,
				Err:      err,
			}
		}

		// Run setup scripts from .amux/worktrees.json
		if err := a.scripts.RunSetup(wt, meta); err != nil {
			// Don't fail worktree creation, just log the error
			return messages.WorktreeCreatedWithWarning{
				Worktree: wt,
				Warning:  fmt.Sprintf("setup failed: %v", err),
			}
		}

		return messages.WorktreeCreated{Worktree: wt}
	}
}

// deleteWorktree deletes a git worktree
func (a *App) deleteWorktree(project *data.Project, wt *data.Worktree) tea.Cmd {
	// Check if we need to clear active worktree before running async
	clearActive := a.activeWorktree != nil && a.activeWorktree.Root == wt.Root
	if clearActive {
		a.activeWorktree = nil
		a.showWelcome = true
	}

	return func() tea.Msg {
		if wt.IsPrimaryCheckout() {
			return messages.WorktreeDeleteFailed{
				Project:  project,
				Worktree: wt,
				Err:      fmt.Errorf("cannot delete primary checkout"),
			}
		}

		if err := git.RemoveWorktree(project.Path, wt.Root); err != nil {
			return messages.WorktreeDeleteFailed{
				Project:  project,
				Worktree: wt,
				Err:      err,
			}
		}

		_ = git.DeleteBranch(project.Path, wt.Branch)
		_ = a.metadata.Delete(wt)

		return messages.WorktreeDeleted{
			Project:  project,
			Worktree: wt,
		}
	}
}

// removeProject removes a project from the registry (does not delete files).
func (a *App) removeProject(project *data.Project) tea.Cmd {
	if project == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("missing project"), Context: "removing project"}
		}
	}

	if a.activeWorktree != nil && a.activeWorktree.Repo == project.Path {
		a.goHome()
	}

	return func() tea.Msg {
		if err := a.registry.RemoveProject(project.Path); err != nil {
			return messages.Error{Err: err, Context: "removing project"}
		}
		return messages.ProjectRemoved{Path: project.Path}
	}
}

// focusPane changes focus to the specified pane
func (a *App) focusPane(pane messages.PaneType) {
	a.focusedPane = pane
	switch pane {
	case messages.PaneDashboard:
		a.dashboard.Focus()
		a.center.Blur()
		a.sidebar.Blur()
		a.sidebarTerminal.Blur()
	case messages.PaneCenter:
		a.dashboard.Blur()
		a.center.Focus()
		a.sidebar.Blur()
		a.sidebarTerminal.Blur()
	case messages.PaneSidebar:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Focus()
		a.sidebarTerminal.Blur()
	case messages.PaneSidebarTerminal:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Blur()
		a.sidebarTerminal.Focus()
	case messages.PaneMonitor:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Blur()
		a.sidebarTerminal.Blur()
	}
}

func (a *App) toggleMonitorMode() {
	a.monitorMode = !a.monitorMode
	if a.monitorMode {
		a.center.ResetMonitorSelection()
		a.monitorLayoutKey = ""
		a.focusPane(messages.PaneMonitor)
	} else {
		a.monitorLayoutKey = ""
		a.focusPane(messages.PaneDashboard)
	}
	a.updateLayout()
}

// Prefix mode helpers (leader key)

// isPrefixKey returns true if the key is the prefix key
func (a *App) isPrefixKey(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, a.keymap.Prefix)
}

// enterPrefix enters prefix mode and schedules a timeout
func (a *App) enterPrefix() tea.Cmd {
	a.prefixActive = true
	a.prefixToken++
	token := a.prefixToken
	return tea.Tick(prefixTimeout, func(t time.Time) tea.Msg {
		return prefixTimeoutMsg{token: token}
	})
}

// exitPrefix exits prefix mode
func (a *App) exitPrefix() {
	a.prefixActive = false
}

// handlePrefixCommand handles a key press while in prefix mode
// Returns (handled, cmd)
func (a *App) handlePrefixCommand(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	// Helper to move monitor selection
	moveMonitorTile := func(dx, dy int) {
		tabs := a.center.MonitorTabs()
		if len(tabs) == 0 {
			return
		}
		_, _, gridW, gridH := a.monitorGridArea()
		grid := monitorGridLayout(len(tabs), gridW, gridH)
		if grid.cols > 0 && grid.rows > 0 {
			a.center.MoveMonitorSelection(dx, dy, grid.cols, grid.rows, len(tabs))
		}
	}

	switch {
	// Pane focus / Monitor tile navigation
	case key.Matches(msg, a.keymap.MoveLeft):
		if a.focusedPane == messages.PaneMonitor {
			// Move selection left in grid (like pane focus in normal mode)
			moveMonitorTile(-1, 0)
			return true, nil
		}
		switch a.focusedPane {
		case messages.PaneCenter:
			a.focusPane(messages.PaneDashboard)
		case messages.PaneSidebar, messages.PaneSidebarTerminal:
			if a.monitorMode {
				a.focusPane(messages.PaneMonitor)
			} else {
				a.focusPane(messages.PaneCenter)
			}
		}
		return true, nil

	case key.Matches(msg, a.keymap.MoveRight):
		if a.focusedPane == messages.PaneMonitor {
			// Move selection right in grid (like pane focus in normal mode)
			moveMonitorTile(1, 0)
			return true, nil
		}
		switch a.focusedPane {
		case messages.PaneDashboard:
			if a.monitorMode {
				a.focusPane(messages.PaneMonitor)
			} else {
				a.focusPane(messages.PaneCenter)
			}
		case messages.PaneCenter:
			if a.monitorMode {
				a.focusPane(messages.PaneMonitor)
			} else if a.layout.ShowSidebar() {
				a.focusPane(messages.PaneSidebar)
			}
		}
		return true, nil

	case key.Matches(msg, a.keymap.MoveUp):
		if a.focusedPane == messages.PaneMonitor {
			// Move selection up in grid
			moveMonitorTile(0, -1)
			return true, nil
		}
		if a.focusedPane == messages.PaneSidebarTerminal {
			a.focusPane(messages.PaneSidebar)
		}
		return true, nil

	case key.Matches(msg, a.keymap.MoveDown):
		if a.focusedPane == messages.PaneMonitor {
			// Move selection down in grid
			moveMonitorTile(0, 1)
			return true, nil
		}
		if a.focusedPane == messages.PaneSidebar && a.layout.ShowSidebar() {
			a.focusPane(messages.PaneSidebarTerminal)
		}
		return true, nil

	// Tab management
	case key.Matches(msg, a.keymap.NextTab):
		a.center.NextTab()
		return true, nil

	case key.Matches(msg, a.keymap.PrevTab):
		a.center.PrevTab()
		return true, nil

	// Tab management
	case key.Matches(msg, a.keymap.NewAgentTab):
		if a.activeWorktree != nil {
			return true, func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
		}
		return true, nil

	case key.Matches(msg, a.keymap.CommitViewer):
		if a.activeWorktree != nil {
			wt := a.activeWorktree
			return true, func() tea.Msg { return messages.OpenCommitViewer{Worktree: wt} }
		}
		return true, nil

	case key.Matches(msg, a.keymap.CloseTab):
		cmd := a.center.CloseActiveTab()
		return true, cmd

	case key.Matches(msg, a.keymap.SaveThread):
		newCenter, cmd := a.center.Update(messages.SaveThreadRequest{})
		a.center = newCenter
		return true, cmd

	// Global commands
	case key.Matches(msg, a.keymap.Monitor):
		a.toggleMonitorMode()
		return true, nil

	case key.Matches(msg, a.keymap.Help):
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()
		return true, nil

	case key.Matches(msg, a.keymap.Quit):
		a.showQuitDialog()
		return true, nil

	// Copy mode (scroll in terminal) - targets focused pane
	case key.Matches(msg, a.keymap.CopyMode):
		switch a.focusedPane {
		case messages.PaneCenter:
			a.center.EnterCopyMode()
		case messages.PaneSidebarTerminal:
			a.sidebarTerminal.EnterCopyMode()
		}
		return true, nil

	// Tab numbers 1-9
	case len(msg.Key().Text) > 0:
		runes := []rune(msg.Key().Text)
		if len(runes) != 1 {
			break
		}
		r := runes[0]
		if r >= '1' && r <= '9' {
			index := int(r - '1')
			a.center.SelectTab(index)
			return true, nil
		}
	}

	return false, nil
}

// sendPrefixToTerminal sends a literal Ctrl-Space (NUL) to the focused terminal
func (a *App) sendPrefixToTerminal() {
	if a.focusedPane == messages.PaneCenter {
		a.center.SendToTerminal("\x00")
	} else if a.focusedPane == messages.PaneSidebarTerminal {
		a.sidebarTerminal.SendToTerminal("\x00")
	}
}

// updateLayout updates component sizes based on window size
func (a *App) updateLayout() {
	a.dashboard.SetSize(a.layout.DashboardWidth(), a.layout.Height())

	centerWidth := a.layout.CenterWidth()
	if a.monitorMode && a.layout.ShowCenter() {
		centerWidth += a.layout.SidebarWidth()
	}
	a.center.SetSize(centerWidth, a.layout.Height())
	leftGutter := a.layout.LeftGutter()
	topGutter := a.layout.TopGutter()
	gapX := 0
	if a.layout.ShowCenter() {
		gapX = a.layout.GapX()
	}
	a.center.SetOffset(leftGutter + a.layout.DashboardWidth() + gapX) // Set X offset for mouse coordinate conversion
	a.center.SetCanFocusRight(a.layout.ShowSidebar())
	a.dashboard.SetCanFocusRight(a.layout.ShowCenter())

	// New two-pane sidebar structure: each pane has its own border
	sidebarWidth := a.layout.SidebarWidth()
	sidebarHeight := a.layout.Height()

	// Each pane gets half the height (borders touch)
	topPaneHeight, bottomPaneHeight := sidebarPaneHeights(sidebarHeight)

	// Content dimensions inside each pane (subtract border + padding)
	// Border: 2 (top + bottom), Padding: 2 (left + right from Pane style)
	contentWidth := sidebarWidth - 2 - 2 // border + padding
	if contentWidth < 1 {
		contentWidth = 1
	}
	topContentHeight := topPaneHeight - 2 // border only (no vertical padding in Pane style)
	if topContentHeight < 1 {
		topContentHeight = 1
	}
	bottomContentHeight := bottomPaneHeight - 2
	if bottomContentHeight < 1 {
		bottomContentHeight = 1
	}

	a.sidebar.SetSize(contentWidth, topContentHeight)
	a.sidebarTerminal.SetSize(contentWidth, bottomContentHeight)

	// Calculate and set offsets for sidebar terminal mouse handling
	// X: Dashboard + Center + Border(1) + Padding(1)
	sidebarX := leftGutter + a.layout.DashboardWidth()
	if a.layout.ShowCenter() {
		sidebarX += a.layout.GapX() + a.layout.CenterWidth()
	}
	if a.layout.ShowSidebar() {
		sidebarX += a.layout.GapX()
	}
	termOffsetX := sidebarX + 2

	// Y: Top pane height (including its border) + Bottom pane border(1)
	termOffsetY := topGutter + topPaneHeight + 1
	a.sidebarTerminal.SetOffset(termOffsetX, termOffsetY)

	if a.dialog != nil {
		a.dialog.SetSize(a.width, a.height)
	}
	if a.filePicker != nil {
		a.filePicker.SetSize(a.width, a.height)
	}
	if a.settingsDialog != nil {
		a.settingsDialog.SetSize(a.width, a.height)
	}
}

func (a *App) setKeymapHintsEnabled(enabled bool) {
	if a.config != nil {
		a.config.UI.ShowKeymapHints = enabled
	}
	a.dashboard.SetShowKeymapHints(enabled)
	a.center.SetShowKeymapHints(enabled)
	a.sidebar.SetShowKeymapHints(enabled)
	a.sidebarTerminal.SetShowKeymapHints(enabled)
	if a.dialog != nil {
		a.dialog.SetShowKeymapHints(enabled)
	}
	if a.filePicker != nil {
		a.filePicker.SetShowKeymapHints(enabled)
	}
}

// renderSidebarPane renders the sidebar as two stacked panes: file changes (top) and terminal (bottom)
func (a *App) renderSidebarPane() string {
	outerWidth := a.layout.SidebarWidth()
	outerHeight := a.layout.Height()

	// Split height evenly between the two panes (borders touch)
	paneHeight, bottomPaneHeight := sidebarPaneHeights(outerHeight)

	topFocused := a.focusedPane == messages.PaneSidebar
	bottomFocused := a.focusedPane == messages.PaneSidebarTerminal

	// Build top pane manually with guaranteed border
	topView := buildBorderedPane(a.sidebar.View(), outerWidth, paneHeight, topFocused)

	// Build bottom pane manually with guaranteed border
	bottomView := buildBorderedPane(a.sidebarTerminal.View(), outerWidth, bottomPaneHeight, bottomFocused)

	// Stack the two panes vertically
	return lipgloss.JoinVertical(lipgloss.Top, topView, bottomView)
}

func sidebarPaneHeights(total int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	top := total / 2
	bottom := total - top

	// Prefer keeping both panes visible when there's room.
	if total >= 6 {
		if top < 3 {
			top = 3
			bottom = total - top
		}
		if bottom < 3 {
			bottom = 3
			top = total - bottom
		}
		return top, bottom
	}

	// In tight spaces, keep the terminal visible by shrinking the top pane first.
	if total >= 3 && bottom < 3 {
		bottom = 3
		top = total - bottom
		if top < 0 {
			top = 0
		}
		return top, bottom
	}

	if top > total {
		top = total
	}
	if bottom < 0 {
		bottom = 0
	}
	return top, bottom
}

// buildBorderedPane creates a bordered pane with exact dimensions, manually drawing the border
func buildBorderedPane(content string, width, height int, focused bool) string {
	if width < 3 || height < 3 {
		return ""
	}

	borderColor := common.ColorBorder
	// Border characters - use thick borders for focused panes
	topLeft, topRight, bottomLeft, bottomRight := "╭", "╮", "╰", "╯"
	horizontal, vertical := "─", "│"
	if focused {
		borderColor = common.ColorBorderFocused
		// Use thick border characters for focused pane
		topLeft, topRight, bottomLeft, bottomRight = "┏", "┓", "┗", "┛"
		horizontal, vertical = "━", "┃"
	}
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Content area dimensions (inside border and padding)
	contentWidth := width - 4   // 2 for border, 2 for padding
	contentHeight := height - 2 // 2 for border (top + bottom)
	if contentWidth < 1 {
		contentWidth = 1
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Truncate and pad content to exact size
	lines := strings.Split(content, "\n")
	if len(lines) > contentHeight {
		lines = lines[:contentHeight]
	}
	// Pad with empty lines if needed
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}
	// Truncate each line to width and pad (ANSI-aware to preserve styled content)
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w > contentWidth {
			// Truncate using ANSI-aware function to preserve escape sequences
			lines[i] = ansi.Truncate(line, contentWidth, "")
		} else if w < contentWidth {
			// Pad with spaces
			lines[i] = line + strings.Repeat(" ", contentWidth-w)
		}
	}

	// Build the box
	var result strings.Builder
	innerWidth := width - 2 // width inside left/right borders

	// Top border
	result.WriteString(borderStyle.Render(topLeft + strings.Repeat(horizontal, innerWidth) + topRight))
	result.WriteString("\n")

	// Content lines with side borders and padding
	for _, line := range lines {
		result.WriteString(borderStyle.Render(vertical))
		result.WriteString(" ") // left padding
		result.WriteString(line)
		result.WriteString(" ") // right padding
		result.WriteString(borderStyle.Render(vertical))
		result.WriteString("\n")
	}

	// Bottom border
	result.WriteString(borderStyle.Render(bottomLeft + strings.Repeat(horizontal, innerWidth) + bottomRight))

	return result.String()
}
