package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/keymap"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/keymapeditor"
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
	DialogSelectAssistant = "select_assistant"
	DialogQuit            = "quit"
)

const leaderTimeout = time.Second

type leaderTimeoutMsg struct{}

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

	// Overlays
	helpOverlay  *common.HelpOverlay
	keymapEditor *keymapeditor.Editor
	toast        *common.ToastModel

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
	keymap        keymap.KeyMap
	styles        common.Styles

	// Lifecycle
	ready    bool
	quitting bool
	err      error

	leaderPending   bool
	leaderPendingAt time.Time
}

// New creates a new App instance
func New() (*App, error) {
	cfg, err := config.Load()
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

	km := keymap.New(cfg.KeyMap)

	return &App{
		config:          cfg,
		registry:        registry,
		metadata:        metadata,
		scripts:         scripts,
		statusManager:   statusManager,
		fileWatcher:     fileWatcher,
		fileWatcherCh:   fileWatcherCh,
		fileWatcherErr:  fileWatcherErr,
		layout:          layout.NewManager(),
		dashboard:       dashboard.New(km),
		center:          center.New(cfg, km),
		sidebar:         sidebar.New(km),
		sidebarTerminal: sidebar.NewTerminalModel(km),
		helpOverlay:     common.NewHelpOverlay(km),
		keymapEditor:    keymapeditor.New(km, cfg.KeyMap),
		toast:           common.NewToastModel(),
		focusedPane:     messages.PaneDashboard,
		showWelcome:     true,
		keymap:          km,
		styles:          common.DefaultStyles(),
	}, nil
}

// Init initializes the application
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.EnableMouseCellMotion, // Enable mouse support for click-to-focus
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
	var cmds []tea.Cmd

	// Handle dialog result first (arrives after dialog is hidden)
	if result, ok := msg.(common.DialogResult); ok {
		logging.Info("Received DialogResult: id=%s confirmed=%v", result.ID, result.Confirmed)
		return a, a.handleDialogResult(result)
	}

	// Route input to keymap editor if visible
	if a.keymapEditor != nil && a.keymapEditor.Visible() {
		newEditor, cmd := a.keymapEditor.Update(msg)
		a.keymapEditor = newEditor
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		switch msg.(type) {
		case tea.KeyMsg, tea.MouseMsg:
			return a, tea.Batch(cmds...)
		}
	}

	// Handle help overlay toggle (highest priority)
	if a.helpOverlay.Visible() {
		switch msg.(type) {
		case tea.KeyMsg, tea.MouseMsg:
			a.helpOverlay.Hide()
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
			logging.Debug("Dialog returned command")
			cmds = append(cmds, cmd)
		}

		// Don't process other keys while dialog is open
		if _, ok := msg.(tea.KeyMsg); ok {
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

		// Don't process other keys while file picker is open
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		a.layout.Resize(msg.Width, msg.Height)
		a.updateLayout()

	case tea.MouseMsg:
		// Handle mouse events
		if a.monitorMode {
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				a.focusPane(messages.PaneMonitor)
				a.selectMonitorTile(msg.X, msg.Y)
				cmd := a.exitMonitorToSelection()
				return a, cmd
			}
			break
		}

		dashWidth := a.layout.DashboardWidth()
		centerWidth := a.layout.CenterWidth()
		rightWidth := centerWidth
		if a.layout.ShowSidebar() {
			rightWidth += a.layout.SidebarWidth()
		}

		// Focus pane on click press
		if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonRight) {

			if msg.X < dashWidth {

				// Clicked on dashboard (left bar)

				a.focusPane(messages.PaneDashboard)
			} else if a.monitorMode && msg.X < dashWidth+rightWidth {
				// Clicked on monitor pane
				a.focusPane(messages.PaneMonitor)
				a.selectMonitorTile(msg.X-dashWidth, msg.Y)
			} else if msg.X < dashWidth+centerWidth {

				// Clicked on center pane

				a.focusPane(messages.PaneCenter)

			} else if a.layout.ShowSidebar() {

				// Clicked on sidebar - determine top (changes) or bottom (terminal)

				sidebarLayout := a.sidebarLayoutInfo()

				// Calculate split point (Y offset of terminal)

				// Border(1) + TopHeight + Separator(1 if present)

				splitY := 1 + sidebarLayout.topHeight

				if sidebarLayout.hasSeparator {

					splitY++

				}

				if msg.Y >= splitY {

					a.focusPane(messages.PaneSidebarTerminal)

				} else {

					a.focusPane(messages.PaneSidebar)

				}

			}

		}

		// Forward mouse events to the focused pane

		// This ensures drag events are received even if the mouse leaves the pane bounds

		switch a.focusedPane {

		case messages.PaneDashboard:

			newDashboard, cmd := a.dashboard.Update(msg)

			a.dashboard = newDashboard

			if cmd != nil {

				cmds = append(cmds, cmd)

			}

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

		case messages.PaneSidebar:

			newSidebar, cmd := a.sidebar.Update(msg)

			a.sidebar = newSidebar

			if cmd != nil {

				cmds = append(cmds, cmd)

			}

		}

	case tea.KeyMsg:
		// Dismiss error on any key
		if a.err != nil {
			a.err = nil
			return a, nil
		}

		// Uncomment for key debugging (causes latency due to sync file I/O):
		// logging.Debug("Key: %s, focusedPane=%d, centerHasTabs=%v", msg.String(), a.focusedPane, a.center.HasTabs())
		if handled, cmd := a.handleLeader(msg); handled {
			return a, cmd
		}
		if handled, cmd := a.handleGlobalKeys(msg); handled {
			return a, cmd
		}

		// Monitor pane navigation (tile selection)
		if a.focusedPane == messages.PaneMonitor {
			if a.handleMonitorNavigation(msg) {
				return a, nil
			}
			if key.Matches(msg, a.keymap.MonitorActivate) {
				cmd := a.exitMonitorToSelection()
				return a, cmd
			}
			if key.Matches(msg, a.keymap.MonitorExit) {
				a.toggleMonitorMode()
				return a, nil
			}
			if cmd := a.handleMonitorInput(msg); cmd != nil {
				return a, cmd
			}
			return a, nil
		}

		if a.inTerminalFocus() {
			switch a.focusedPane {
			case messages.PaneCenter:
				logging.Debug("Forwarding key to center pane: %s", msg.String())
				newCenter, cmd := a.center.Update(msg)
				a.center = newCenter
				return a, cmd
			case messages.PaneSidebarTerminal:
				newSidebarTerminal, cmd := a.sidebarTerminal.Update(msg)
				a.sidebarTerminal = newSidebarTerminal
				return a, cmd
			}
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
		case messages.PaneMonitor:
			// No interactive updates yet; use global bindings to navigate.
		}

	case leaderTimeoutMsg:
		if a.leaderPending && time.Since(a.leaderPendingAt) >= leaderTimeout {
			a.leaderPending = false
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
		a.filePicker.Show()

	case messages.ShowCreateWorktreeDialog:
		a.dialogProject = msg.Project
		a.dialog = common.NewInputDialog(DialogCreateWorktree, "Create Worktree", "Enter worktree name...")
		a.dialog.Show()

	case messages.ShowDeleteWorktreeDialog:
		a.dialogProject = msg.Project
		a.dialogWorktree = msg.Worktree
		a.dialog = common.NewConfirmDialog(
			DialogDeleteWorktree,
			"Delete Worktree",
			fmt.Sprintf("Delete worktree '%s' and its branch?", msg.Worktree.Name),
		)
		a.dialog.Show()

	case messages.ShowSelectAssistantDialog:
		if a.activeWorktree != nil {
			a.dialog = common.NewAgentPicker()
			a.dialog.Show()
		}

	case messages.ShowKeymapEditor:
		if a.keymapEditor != nil {
			a.helpOverlay.Hide()
			a.dialog = nil
			a.filePicker = nil
			a.keymapEditor.Show(a.keymap, a.config.KeyMap)
			a.keymapEditor.SetSize(a.width, a.height)
		}

	case messages.ToggleMonitor:
		a.toggleMonitorMode()

	case messages.ToggleHelpOverlay:
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()

	case messages.ShowQuitDialog:
		a.showQuitDialog()

	case messages.KeymapUpdated:
		a.applyKeyMap(msg.Bindings, &cmds)

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
	}

	return a, tea.Batch(cmds...)
}

// handleDialogResult handles dialog completion
func (a *App) handleDialogResult(result common.DialogResult) tea.Cmd {
	a.dialog = nil
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
		if result.Value != "" && a.dialogProject != nil {
			name := validation.SanitizeInput(result.Value)
			if err := validation.ValidateWorktreeName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating worktree name"}
				}
			}
			project := a.dialogProject
			return func() tea.Msg {
				return messages.CreateWorktree{
					Project: project,
					Name:    name,
					Base:    "HEAD",
				}
			}
		}

	case DialogDeleteWorktree:
		if a.dialogProject != nil && a.dialogWorktree != nil {
			project := a.dialogProject
			wt := a.dialogWorktree
			return func() tea.Msg {
				return messages.DeleteWorktree{
					Project:  project,
					Worktree: wt,
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
	a.dialog.Show()
}

func (a *App) applyKeyMap(bindings map[string][]string, cmds *[]tea.Cmd) {
	clone := cloneBindings(bindings)
	cfg := config.KeyMapConfig{Bindings: clone}
	a.config.KeyMap = cfg
	if err := config.SaveKeyMap(a.config.Paths, cfg); err != nil {
		if cmds != nil {
			*cmds = append(*cmds, a.toast.ShowError(fmt.Sprintf("Failed to save keymap: %v", err)))
		}
	}

	km := keymap.New(cfg)
	a.keymap = km
	a.dashboard.SetKeyMap(km)
	a.center.SetKeyMap(km)
	a.sidebar.SetKeyMap(km)
	a.sidebarTerminal.SetKeyMap(km)
	a.helpOverlay.SetKeyMap(km)
	if a.keymapEditor != nil {
		a.keymapEditor.SetKeyMap(km, cfg)
	}
}

func cloneBindings(bindings map[string][]string) map[string][]string {
	if len(bindings) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(bindings))
	for key, values := range bindings {
		clone := make([]string, len(values))
		copy(clone, values)
		out[key] = clone
	}
	return out
}

// Synchronized Output Mode 2026 sequences
// https://gist.github.com/christianparpart/d8a62cc1ab659194337d73e399004036
const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// View renders the application
func (a *App) View() string {
	if a.quitting {
		return "Goodbye!\n"
	}

	if !a.ready {
		return "Loading..."
	}

	// Monitor mode uses the compositor for a full-screen grid.
	if a.monitorMode {
		content := a.renderMonitorGrid()

		if a.dialog != nil && a.dialog.Visible() {
			content = a.overlayDialog(content)
		}
		if a.filePicker != nil && a.filePicker.Visible() {
			content = a.overlayFilePicker(content)
		}
		if a.keymapEditor != nil && a.keymapEditor.Visible() {
			content = a.overlayKeymapEditor(content)
		} else if a.helpOverlay.Visible() {
			content = a.helpOverlay.View()
		}
		if a.toast.Visible() {
			content = a.overlayToast(content)
		}
		if a.err != nil {
			content = a.overlayError(content)
		}
		return syncBegin + content + syncEnd
	}

	// Render panes
	dashView := a.dashboard.View()

	var centerView string
	if a.center.HasTabs() {
		centerView = a.center.View()
	} else {
		centerView = a.renderCenterPane()
	}

	// Render sidebar as vertical split: file changes (top) + terminal (bottom)
	sidebarView := a.renderSidebarPane()

	// Hard-clamp pane output to allocated size to prevent bleed.
	if a.layout != nil {
		dashView = clampPane(dashView, a.layout.DashboardWidth(), a.layout.Height())
		centerView = clampPane(centerView, a.layout.CenterWidth(), a.layout.Height())
		sidebarView = clampPane(sidebarView, a.layout.SidebarWidth(), a.layout.Height())
	}

	// Combine using layout manager
	var content string
	if a.layout.ShowSidebar() {
		content = lipgloss.JoinHorizontal(lipgloss.Top, dashView, centerView, sidebarView)
	} else if a.layout.ShowCenter() {
		content = lipgloss.JoinHorizontal(lipgloss.Top, dashView, centerView)
	} else {
		content = dashView
	}

	// Overlay dialog if visible
	if a.dialog != nil && a.dialog.Visible() {
		content = a.overlayDialog(content)
	}

	// Overlay file picker if visible
	if a.filePicker != nil && a.filePicker.Visible() {
		content = a.overlayFilePicker(content)
	}

	// Show keymap editor if visible
	if a.keymapEditor != nil && a.keymapEditor.Visible() {
		content = a.overlayKeymapEditor(content)
	} else if a.helpOverlay.Visible() {
		// Show help overlay if visible
		content = a.helpOverlay.View()
	}

	// Show toast notification if visible
	if a.toast.Visible() {
		content = a.overlayToast(content)
	}

	// Show error message if present
	if a.err != nil {
		content = a.overlayError(content)
	}

	// Wrap with synchronized output to prevent flickering
	return syncBegin + content + syncEnd
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

// overlayToast renders a toast notification at the bottom
func (a *App) overlayToast(content string) string {
	toastView := a.toast.View()
	if toastView == "" {
		return content
	}

	// Position toast at bottom center
	lines := strings.Split(content, "\n")
	if len(lines) >= a.height && a.height > 2 {
		// Replace second-to-last line with toast
		toastLine := lipgloss.PlaceHorizontal(a.width, lipgloss.Center, toastView)
		lines[a.height-2] = toastLine
	}

	return strings.Join(lines, "\n")
}

// overlayDialog renders the dialog as a true modal overlay on top of content
func (a *App) overlayDialog(content string) string {
	dialogView := a.dialog.View()
	dialogLines := strings.Split(dialogView, "\n")

	// Calculate dialog dimensions
	dialogHeight := len(dialogLines)
	dialogWidth := 0
	for _, line := range dialogLines {
		if w := lipgloss.Width(line); w > dialogWidth {
			dialogWidth = w
		}
	}

	// Center the dialog (true center)
	x := (a.width - dialogWidth) / 2
	y := (a.height - dialogHeight) / 2

	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	// Split content into lines - preserve exact line count
	contentLines := strings.Split(content, "\n")
	originalLineCount := len(contentLines)

	// Overlay dialog lines onto content using ANSI-aware functions
	for i, dialogLine := range dialogLines {
		contentY := y + i
		if contentY >= 0 && contentY < len(contentLines) {
			bgLine := contentLines[contentY]

			// Get left portion of background (before dialog)
			left := ansi.Truncate(bgLine, x, "")
			// Pad left if needed
			leftWidth := lipgloss.Width(left)
			if leftWidth < x {
				left += strings.Repeat(" ", x-leftWidth)
			}

			// Get right portion of background (after dialog)
			rightStart := x + dialogWidth
			bgWidth := lipgloss.Width(bgLine)
			var right string
			if rightStart < bgWidth {
				right = ansi.TruncateLeft(bgLine, rightStart, "")
			}

			// Compose: left + dialog + right
			contentLines[contentY] = left + dialogLine + right
		}
	}

	// Preserve original line count exactly
	if len(contentLines) > originalLineCount {
		contentLines = contentLines[:originalLineCount]
	}

	return strings.Join(contentLines, "\n")
}

// overlayKeymapEditor renders the keymap editor as a modal overlay on top of content
func (a *App) overlayKeymapEditor(content string) string {
	if a.keymapEditor == nil {
		return content
	}
	editorView := a.keymapEditor.View()
	editorLines := strings.Split(editorView, "\n")

	editorHeight := len(editorLines)
	editorWidth := 0
	for _, line := range editorLines {
		if w := lipgloss.Width(line); w > editorWidth {
			editorWidth = w
		}
	}

	x := (a.width - editorWidth) / 2
	y := (a.height - editorHeight) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	a.keymapEditor.SetOffset(x, y)

	contentLines := strings.Split(content, "\n")
	originalLineCount := len(contentLines)

	for i, editorLine := range editorLines {
		contentY := y + i
		if contentY >= 0 && contentY < len(contentLines) {
			bgLine := contentLines[contentY]

			left := ansi.Truncate(bgLine, x, "")
			leftWidth := lipgloss.Width(left)
			if leftWidth < x {
				left += strings.Repeat(" ", x-leftWidth)
			}

			rightStart := x + editorWidth
			bgWidth := lipgloss.Width(bgLine)
			var right string
			if rightStart < bgWidth {
				right = ansi.TruncateLeft(bgLine, rightStart, "")
			}

			contentLines[contentY] = left + editorLine + right
		}
	}

	if len(contentLines) > originalLineCount {
		contentLines = contentLines[:originalLineCount]
	}

	return strings.Join(contentLines, "\n")
}

// overlayFilePicker renders the file picker as a modal overlay
func (a *App) overlayFilePicker(content string) string {
	pickerView := a.filePicker.View()
	pickerLines := strings.Split(pickerView, "\n")

	// Calculate picker dimensions
	pickerHeight := len(pickerLines)
	pickerWidth := 0
	for _, line := range pickerLines {
		if w := lipgloss.Width(line); w > pickerWidth {
			pickerWidth = w
		}
	}

	// Center the picker
	x := (a.width - pickerWidth) / 2
	y := (a.height - pickerHeight) / 2

	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	// Split content into lines
	contentLines := strings.Split(content, "\n")
	originalLineCount := len(contentLines)

	// Overlay picker lines onto content
	for i, pickerLine := range pickerLines {
		contentY := y + i
		if contentY >= 0 && contentY < len(contentLines) {
			bgLine := contentLines[contentY]
			bgWidth := lipgloss.Width(bgLine)

			// Build the line: left part + picker line + right part
			var left string
			if x > 0 {
				left = ansi.Truncate(bgLine, x, "")
			}

			rightStart := x + lipgloss.Width(pickerLine)
			var right string
			if rightStart < bgWidth {
				right = ansi.TruncateLeft(bgLine, rightStart, "")
			}

			contentLines[contentY] = left + pickerLine + right
		}
	}

	if len(contentLines) > originalLineCount {
		contentLines = contentLines[:originalLineCount]
	}

	return strings.Join(contentLines, "\n")
}

// overlayError renders an error banner at the bottom of the screen
func (a *App) overlayError(content string) string {
	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#e06c75")).
		Bold(true).
		Padding(0, 2).
		Width(a.width)

	errMsg := fmt.Sprintf(" Error: %s (press any key to dismiss)", a.err.Error())
	errBanner := errStyle.Render(errMsg)

	// Place error at the bottom
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && a.height > 1 {
		// Replace last line with error banner
		if len(lines) >= a.height {
			lines[a.height-1] = errBanner
		} else {
			lines = append(lines, errBanner)
		}
	}

	return strings.Join(lines, "\n")
}

// renderCenterPane renders the center pane content when no tabs
func (a *App) renderCenterPane() string {
	width := a.layout.CenterWidth()
	height := a.layout.Height()

	style := lipgloss.NewStyle().
		Width(width-2).
		Height(height-2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#5c6370")).
		Padding(0, 1)

	if a.focusedPane == messages.PaneCenter {
		style = style.BorderForeground(lipgloss.Color("#61afef"))
	}

	if a.showWelcome {
		return style.Render(a.renderWelcome())
	}

	if a.activeWorktree != nil {
		return style.Render(a.renderWorktreeInfo())
	}

	return style.Render("Select a worktree from the dashboard")
}

func (a *App) goHome() {
	a.showWelcome = true
	a.activeWorktree = nil
	a.center.SetWorktree(nil)
	a.sidebar.SetWorktree(nil)
	a.sidebar.SetGitStatus(nil)
	_ = a.sidebarTerminal.SetWorktree(nil)
}

func (a *App) renderMonitorGrid() string {
	if a.width <= 0 || a.height <= 0 {
		return ""
	}

	tabs := a.center.MonitorTabs()
	if len(tabs) == 0 {
		canvas := a.monitorCanvasFor(a.width, a.height)
		canvas.Fill(vterm.Style{Fg: compositor.HexColor(string(common.ColorForeground)), Bg: compositor.HexColor(string(common.ColorBackground))})
		empty := "No agents running"
		x := (a.width - ansi.StringWidth(empty)) / 2
		y := a.height / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		canvas.DrawText(x, y, empty, vterm.Style{Fg: compositor.HexColor(string(common.ColorMuted))})
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
		Fg: compositor.HexColor(string(common.ColorForeground)),
		Bg: compositor.HexColor(string(common.ColorBackground)),
	})

	headerStyle := vterm.Style{Fg: compositor.HexColor(string(common.ColorMuted))}
	navHint := keymap.PairHint(a.keymap.MonitorLeft, a.keymap.MonitorRight) + " " + keymap.PairHint(a.keymap.MonitorUp, a.keymap.MonitorDown)
	openHint := keymap.PrimaryKey(a.keymap.MonitorActivate)
	exitHint := keymap.PrimaryKey(a.keymap.MonitorExit)
	canvas.DrawText(0, 0, fmt.Sprintf("Monitor: %s move • %s open • %s exit", navHint, openHint, exitHint), headerStyle)

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

		borderStyle := vterm.Style{Fg: compositor.HexColor(string(common.ColorBorder))}
		if focused {
			borderStyle.Fg = compositor.HexColor(string(common.ColorBorderFocused))
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

		hStyle := vterm.Style{Fg: compositor.HexColor(string(common.ColorForeground)), Bold: true}
		if focused {
			hStyle.Bg = compositor.HexColor(string(common.ColorSelection))
		}
		canvas.DrawText(innerX, innerY, header, hStyle)

		contentY := innerY + 1
		contentH := innerH - 1
		if contentH <= 0 {
			continue
		}

		snap, ok := snapByID[tab.ID]
		if !ok || len(snap.Screen) == 0 {
			canvas.DrawText(innerX, contentY, "No active agent", vterm.Style{Fg: compositor.HexColor(string(common.ColorMuted))})
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
		)
	}

	return canvas.Render()
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

func (a *App) handleMonitorNavigation(msg tea.KeyMsg) bool {
	tabs := a.center.MonitorTabs()
	if len(tabs) == 0 {
		return false
	}

	_, _, gridW, gridH := a.monitorGridArea()
	grid := monitorGridLayout(len(tabs), gridW, gridH)
	if grid.cols == 0 || grid.rows == 0 {
		return false
	}

	switch {
	case key.Matches(msg, a.keymap.MonitorLeft):
		a.center.MoveMonitorSelection(-1, 0, grid.cols, grid.rows, len(tabs))
		return true
	case key.Matches(msg, a.keymap.MonitorRight):
		a.center.MoveMonitorSelection(1, 0, grid.cols, grid.rows, len(tabs))
		return true
	case key.Matches(msg, a.keymap.MonitorUp):
		a.center.MoveMonitorSelection(0, -1, grid.cols, grid.rows, len(tabs))
		return true
	case key.Matches(msg, a.keymap.MonitorDown):
		a.center.MoveMonitorSelection(0, 1, grid.cols, grid.rows, len(tabs))
		return true
	}
	return false
}

func (a *App) handleMonitorInput(msg tea.KeyMsg) tea.Cmd {
	tabs := a.center.MonitorTabs()
	if len(tabs) == 0 {
		return nil
	}
	idx := a.center.MonitorSelectedIndex(len(tabs))
	if idx < 0 || idx >= len(tabs) {
		return nil
	}
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

func (a *App) selectMonitorTile(paneX, paneY int) {
	tabs := a.center.MonitorTabs()
	count := len(tabs)
	if count == 0 {
		return
	}

	gridX, gridY, gridW, gridH := a.monitorGridArea()
	x := paneX - gridX
	y := paneY - gridY
	if x < 0 || y < 0 || x >= gridW || y >= gridH {
		return
	}

	grid := monitorGridLayout(count, gridW, gridH)
	if grid.cols == 0 || grid.rows == 0 {
		return
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
				return
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
				return
			}
			y -= grid.gapY
		}
	}

	if row < 0 || col < 0 {
		return
	}

	index := row*grid.cols + col
	if index >= 0 && index < count {
		a.center.SetMonitorSelectedIndex(index, count)
	}
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

	content += "\n" + a.styles.Help.Render("Press "+keymap.LeaderSequenceHint(a.keymap, a.keymap.TabNew)+" to launch an agent")

	return content
}

// renderWelcome renders the welcome screen
func (a *App) renderWelcome() string {
	logo := `
 8888b.  88888b.d88b.  888  888 888  888
    "88b 888 "888 "88b 888  888  Y8bd8P
.d888888 888  888  888 888  888   X88K
888  888 888  888  888 Y88b 888 .d8""8b.
"Y888888 888  888  888  "Y88888 888  888`

	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7aa2f7")).
		Bold(true)

	// Quick start section
	quickStart := `
┌─ Quick Start ─────────────────────┐
│  Enter   Open selected row        │
│  Leader+n Launch AI agent         │
│  Alt+?   Show all shortcuts       │
└───────────────────────────────────┘`

	quickStartStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#565f89"))

	// Build the welcome screen content
	var b strings.Builder
	b.WriteString(logoStyle.Render(logo))
	b.WriteString("\n\n")
	b.WriteString(quickStartStyle.Render(quickStart))

	// Center the content in the pane
	width := a.layout.CenterWidth() - 4 // Account for borders/padding
	height := a.layout.Height() - 4

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, b.String())
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

func (a *App) inTerminalFocus() bool {
	switch a.focusedPane {
	case messages.PaneCenter:
		return a.center.HasTabs()
	case messages.PaneSidebarTerminal:
		return true
	default:
		return false
	}
}

func (a *App) handleLeader(msg tea.KeyMsg) (bool, tea.Cmd) {
	if a.leaderPending {
		a.leaderPending = false
		return true, a.runLeaderCommand(msg)
	}
	if key.Matches(msg, a.keymap.Leader) {
		a.leaderPending = true
		a.leaderPendingAt = time.Now()
		return true, tea.Tick(leaderTimeout, func(time.Time) tea.Msg {
			return leaderTimeoutMsg{}
		})
	}
	return false, nil
}

func (a *App) handleGlobalKeys(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keymap.FocusLeft):
		a.focusLeft()
		return true, nil
	case key.Matches(msg, a.keymap.FocusRight):
		a.focusRight()
		return true, nil
	case key.Matches(msg, a.keymap.FocusUp):
		a.focusUp()
		return true, nil
	case key.Matches(msg, a.keymap.FocusDown):
		a.focusDown()
		return true, nil
	case key.Matches(msg, a.keymap.MonitorToggle):
		a.toggleMonitorMode()
		return true, nil
	case key.Matches(msg, a.keymap.Home):
		a.goHome()
		a.focusPane(messages.PaneDashboard)
		return true, nil
	case key.Matches(msg, a.keymap.Help):
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()
		return true, nil
	case key.Matches(msg, a.keymap.KeymapEditor):
		return true, func() tea.Msg { return messages.ShowKeymapEditor{} }
	case key.Matches(msg, a.keymap.Quit):
		a.showQuitDialog()
		return true, nil
	case key.Matches(msg, a.keymap.ScrollUpHalf):
		a.scrollFocusedTerminal(1)
		return true, nil
	case key.Matches(msg, a.keymap.ScrollDownHalf):
		a.scrollFocusedTerminal(-1)
		return true, nil
	}
	return false, nil
}

func (a *App) runLeaderCommand(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, a.keymap.TabNext):
		a.center.NextTab()
	case key.Matches(msg, a.keymap.TabPrev):
		a.center.PrevTab()
	case key.Matches(msg, a.keymap.TabNew):
		if a.activeWorktree != nil {
			return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
		}
	case key.Matches(msg, a.keymap.TabClose):
		return a.center.CloseActiveTab()
	}
	return nil
}

func (a *App) focusLeft() {
	switch a.focusedPane {
	case messages.PaneCenter:
		a.focusPane(messages.PaneDashboard)
	case messages.PaneSidebar, messages.PaneSidebarTerminal:
		a.focusPane(messages.PaneCenter)
	case messages.PaneMonitor:
		a.focusPane(messages.PaneDashboard)
	}
}

func (a *App) focusRight() {
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
	case messages.PaneMonitor:
		// No-op; monitor is the rightmost pane
	}
}

func (a *App) focusUp() {
	if !a.monitorMode && a.focusedPane == messages.PaneSidebarTerminal {
		a.focusPane(messages.PaneSidebar)
	}
}

func (a *App) focusDown() {
	if !a.monitorMode && a.focusedPane == messages.PaneSidebar && a.layout.ShowSidebar() {
		a.focusPane(messages.PaneSidebarTerminal)
	}
}

func (a *App) scrollFocusedTerminal(delta int) {
	switch a.focusedPane {
	case messages.PaneCenter:
		if a.center.HasTabs() {
			a.center.ScrollHalf(delta)
		}
	case messages.PaneSidebarTerminal:
		a.sidebarTerminal.ScrollHalf(delta)
	}
}

// updateLayout updates component sizes based on window size
func (a *App) updateLayout() {
	a.dashboard.SetSize(a.layout.DashboardWidth(), a.layout.Height())
	a.dashboard.SetOffset(2, 1)

	centerWidth := a.layout.CenterWidth()
	if a.monitorMode && a.layout.ShowCenter() {
		centerWidth += a.layout.SidebarWidth()
	}
	a.center.SetSize(centerWidth, a.layout.Height())
	a.center.SetOffset(a.layout.DashboardWidth()) // Set X offset for mouse coordinate conversion

	sidebarLayout := a.sidebarLayoutInfo()
	a.sidebar.SetSize(sidebarLayout.bodyWidth, sidebarLayout.topHeight)
	a.sidebarTerminal.SetSize(sidebarLayout.bodyWidth, sidebarLayout.bottomHeight)

	// Sidebar top section mouse handling offset
	topOffsetX := a.layout.DashboardWidth() + a.layout.CenterWidth() + 3
	a.sidebar.SetOffset(topOffsetX, 1)

	// Calculate and set offsets for sidebar terminal mouse handling
	// X: Dashboard + Center + Border(1) + Padding(1) + Gutter(1)
	termOffsetX := a.layout.DashboardWidth() + a.layout.CenterWidth() + 3

	// Y: Border(1) + TopHeight
	termOffsetY := 1 + sidebarLayout.topHeight
	if sidebarLayout.hasSeparator {
		termOffsetY++ // + Separator(1)
	}
	a.sidebarTerminal.SetOffset(termOffsetX, termOffsetY)

	if a.dialog != nil {
		a.dialog.SetSize(a.width, a.height)
	}
	if a.keymapEditor != nil {
		a.keymapEditor.SetSize(a.width, a.height)
	}
}

const (
	sidebarBorderWidth        = 1
	sidebarPaddingX           = 1
	sidebarGutterWidth        = 1
	sidebarSeparatorMinHeight = 3
)

type sidebarLayoutInfo struct {
	innerWidth    int
	contentWidth  int
	bodyWidth     int
	contentHeight int
	topHeight     int
	bottomHeight  int
	hasSeparator  bool
}

func (a *App) sidebarLayoutInfo() sidebarLayoutInfo {
	outerWidth := a.layout.SidebarWidth()
	outerHeight := a.layout.Height()

	innerWidth := outerWidth - (sidebarBorderWidth * 2)
	if innerWidth < 1 {
		innerWidth = 1
	}

	contentWidth := innerWidth - (sidebarPaddingX * 2)
	if contentWidth < 1 {
		contentWidth = 1
	}

	bodyWidth := contentWidth - sidebarGutterWidth
	if bodyWidth < 1 {
		bodyWidth = 1
	}

	contentHeight := outerHeight - (sidebarBorderWidth * 2)
	if contentHeight < 1 {
		contentHeight = 1
	}

	available := contentHeight
	hasSeparator := false
	if available >= sidebarSeparatorMinHeight {
		hasSeparator = true
		available--
	}

	topHeight := available / 2
	bottomHeight := available - topHeight
	if available > 0 {
		if topHeight < 1 {
			topHeight = 1
			bottomHeight = available - topHeight
		}
		if bottomHeight < 1 {
			bottomHeight = 1
			topHeight = available - bottomHeight
		}
	}

	return sidebarLayoutInfo{
		innerWidth:    innerWidth,
		contentWidth:  contentWidth,
		bodyWidth:     bodyWidth,
		contentHeight: contentHeight,
		topHeight:     topHeight,
		bottomHeight:  bottomHeight,
		hasSeparator:  hasSeparator,
	}
}

// renderSidebarPane renders the sidebar as a vertical split with file changes and terminal
func (a *App) renderSidebarPane() string {
	layout := a.sidebarLayoutInfo()

	topFocused := a.focusedPane == messages.PaneSidebar
	bottomFocused := a.focusedPane == messages.PaneSidebarTerminal
	sidebarFocused := topFocused || bottomFocused

	topView := ""
	if layout.topHeight > 0 {
		topView = renderSidebarSection(a.sidebar.View(), layout.contentWidth, layout.topHeight, topFocused)
	}
	bottomView := ""
	if layout.bottomHeight > 0 {
		bottomView = renderSidebarSection(a.sidebarTerminal.View(), layout.contentWidth, layout.bottomHeight, bottomFocused)
	}

	var parts []string
	if topView != "" {
		parts = append(parts, topView)
	}
	if layout.hasSeparator && topView != "" && bottomView != "" {
		separator := lipgloss.NewStyle().
			Foreground(common.ColorBorder).
			Render(strings.Repeat("─", layout.contentWidth))
		parts = append(parts, separator)
	}
	if bottomView != "" {
		parts = append(parts, bottomView)
	}

	content := lipgloss.JoinVertical(lipgloss.Top, parts...)
	style := a.styles.Pane
	if sidebarFocused {
		style = a.styles.FocusedPane
	}

	return style.Width(layout.innerWidth).Render(content)
}

func renderSidebarSection(content string, width, height int, focused bool) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if width <= sidebarGutterWidth {
		return lipgloss.NewStyle().Width(width).Height(height).Render(content)
	}

	contentWidth := width - sidebarGutterWidth
	normalized := lipgloss.NewStyle().Width(contentWidth).Height(height).Render(content)
	lines := strings.Split(normalized, "\n")
	gutter := " "
	gutterStyle := lipgloss.NewStyle()
	if focused {
		gutter = "▌"
		gutterStyle = gutterStyle.Foreground(common.ColorBorderFocused)
	}

	for i := range lines {
		if focused {
			lines[i] = gutterStyle.Render(gutter) + lines[i]
		} else {
			lines[i] = gutter + lines[i]
		}
	}

	return strings.Join(lines, "\n")
}
