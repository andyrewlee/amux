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
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/layout"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/validation"
)

// DialogID constants
const (
	DialogAddProject      = "add_project"
	DialogCreateWorktree  = "create_worktree"
	DialogDeleteWorktree  = "delete_worktree"
	DialogSelectAssistant = "select_assistant"
)

// App is the root Bubbletea model
type App struct {
	// Configuration
	config   *config.Config
	registry *data.Registry
	metadata *data.MetadataStore

	// State
	projects       []data.Project
	activeWorktree *data.Worktree
	activeProject  *data.Project
	focusedPane    messages.PaneType
	showWelcome    bool

	// UI Components
	layout    *layout.Manager
	dashboard *dashboard.Model
	center    *center.Model
	sidebar   *sidebar.Model
	dialog    *common.Dialog

	// Overlays
	helpOverlay *common.HelpOverlay
	toast       *common.ToastModel

	// Dialog context
	dialogProject  *data.Project
	dialogWorktree *data.Worktree

	// Process management
	scripts *process.ScriptRunner

	// Git status management
	statusManager *git.StatusManager
	fileWatcher   *git.FileWatcher

	// Layout
	width, height int
	keymap        KeyMap
	styles        common.Styles

	// Lifecycle
	ready    bool
	quitting bool
	err      error
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

	// Create file watcher (may fail, that's ok)
	fileWatcher, _ := git.NewFileWatcher(nil)

	return &App{
		config:        cfg,
		registry:      registry,
		metadata:      metadata,
		scripts:       scripts,
		statusManager: statusManager,
		fileWatcher:   fileWatcher,
		layout:        layout.NewManager(),
		dashboard:     dashboard.New(),
		center:        center.New(cfg),
		sidebar:       sidebar.New(),
		helpOverlay:   common.NewHelpOverlay(),
		toast:         common.NewToastModel(),
		focusedPane:   messages.PaneDashboard,
		showWelcome:   true,
		keymap:        DefaultKeyMap(),
		styles:        common.DefaultStyles(),
	}, nil
}

// Init initializes the application
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		tea.EnableMouseCellMotion, // Enable mouse support for click-to-focus
		a.loadProjects(),
		a.dashboard.Init(),
		a.center.Init(),
		a.sidebar.Init(),
		a.startGitStatusTicker(),
		a.startFileWatcher(),
	)
}

// startGitStatusTicker returns a command that ticks every 3 seconds for git status refresh
func (a *App) startGitStatusTicker() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return messages.GitStatusTick{}
	})
}

// startFileWatcher starts watching for file changes and returns events
func (a *App) startFileWatcher() tea.Cmd {
	if a.fileWatcher == nil {
		return nil
	}
	return nil // File watcher runs in background, we'll handle it differently
}

// Update handles all messages
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle dialog result first (arrives after dialog is hidden)
	if result, ok := msg.(common.DialogResult); ok {
		logging.Info("Received DialogResult: id=%s confirmed=%v", result.ID, result.Confirmed)
		return a, a.handleDialogResult(result)
	}

	// Handle help overlay toggle (highest priority)
	if keyMsg, ok := msg.(tea.KeyMsg); ok && a.helpOverlay.Visible() {
		// Any key dismisses help
		a.helpOverlay.Hide()
		_ = keyMsg // consume the key
		return a, nil
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

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		a.layout.Resize(msg.Width, msg.Height)
		a.updateLayout()

	case tea.MouseMsg:
		// Handle mouse events
		dashWidth := a.layout.DashboardWidth()
		centerWidth := a.layout.CenterWidth()

		// Focus pane on left-click press
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if msg.X < dashWidth {
				// Clicked on dashboard (left bar)
				a.focusPane(messages.PaneDashboard)
			} else if msg.X < dashWidth+centerWidth {
				// Clicked on center pane
				a.focusPane(messages.PaneCenter)
			} else if a.layout.ShowSidebar() {
				// Clicked on sidebar
				a.focusPane(messages.PaneSidebar)
			}
		}

		// Forward mouse events to center pane for selection handling
		if msg.X >= dashWidth && msg.X < dashWidth+centerWidth {
			newCenter, cmd := a.center.Update(msg)
			a.center = newCenter
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tea.KeyMsg:
		// Handle quit first
		if key.Matches(msg, a.keymap.Quit) {
			a.center.Close()
			a.quitting = true
			return a, tea.Quit
		}

		// Dismiss error on any key
		if a.err != nil {
			a.err = nil
			return a, nil
		}

		logging.Debug("Key: %s, focusedPane=%d, centerHasTabs=%v", msg.String(), a.focusedPane, a.center.HasTabs())

		// When focused on center pane with terminal, handle navigation keys
		if a.focusedPane == messages.PaneCenter {
			// Check for global navigation keys BEFORE forwarding to terminal
			// These must be intercepted or user gets stuck in terminal
			switch {
			case key.Matches(msg, a.keymap.MoveLeft):
				// From center, move left to dashboard
				a.focusPane(messages.PaneDashboard)
				return a, nil
			case key.Matches(msg, a.keymap.MoveRight):
				// From center, move right to sidebar (if visible)
				if a.layout.ShowSidebar() {
					a.focusPane(messages.PaneSidebar)
				}
				return a, nil
			case key.Matches(msg, a.keymap.Quit):
				a.center.Close()
				a.quitting = true
				return a, tea.Quit
			}

			// When we have active tabs, forward all other keys to the terminal
			if a.center.HasTabs() {
				logging.Debug("Forwarding key to center pane: %s", msg.String())
				newCenter, cmd := a.center.Update(msg)
				a.center = newCenter
				return a, cmd
			}
			logging.Debug("Center has no tabs, not forwarding")
		}

		// Global keybindings (only when NOT in terminal mode)
		// Relative vim-style navigation: Ctrl+H = left, Ctrl+L = right
		switch {
		case key.Matches(msg, a.keymap.MoveLeft):
			switch a.focusedPane {
			case messages.PaneCenter:
				a.focusPane(messages.PaneDashboard)
			case messages.PaneSidebar:
				a.focusPane(messages.PaneCenter)
			}
		case key.Matches(msg, a.keymap.MoveRight):
			switch a.focusedPane {
			case messages.PaneDashboard:
				a.focusPane(messages.PaneCenter)
			case messages.PaneCenter:
				if a.layout.ShowSidebar() {
					a.focusPane(messages.PaneSidebar)
				}
			}
		case key.Matches(msg, a.keymap.Home):
			a.showWelcome = true
			a.activeWorktree = nil
		case key.Matches(msg, a.keymap.NewAgentTab):
			if a.activeWorktree != nil {
				return a, func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("?"))):
			// Toggle help overlay
			a.helpOverlay.SetSize(a.width, a.height)
			a.helpOverlay.Toggle()
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
		a.showWelcome = true
		a.activeWorktree = nil

	case messages.RefreshDashboard:
		cmds = append(cmds, a.loadProjects())

	case messages.WorktreeCreatedWithWarning:
		// Worktree was created but setup had issues - still refresh and show warning
		a.err = fmt.Errorf("worktree created with warning: %s", msg.Warning)
		cmds = append(cmds, a.loadProjects())

	case messages.GitStatusResult:
		newDashboard, cmd := a.dashboard.Update(msg)
		a.dashboard = newDashboard
		cmds = append(cmds, cmd)
		// Update sidebar if this is for the active worktree
		if a.activeWorktree != nil && msg.Root == a.activeWorktree.Root {
			a.sidebar.SetGitStatus(msg.Status)
		}

	case messages.ShowAddProjectDialog:
		logging.Info("Showing Add Project dialog")
		a.dialog = common.NewInputDialog(DialogAddProject, "Add Project", "Enter repository path...")
		a.dialog.Show()

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

	case messages.CreateWorktree:
		cmds = append(cmds, a.createWorktree(msg.Project, msg.Name, msg.Base))

	case messages.DeleteWorktree:
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
		a.focusPane(messages.PaneCenter)

	case center.PTYOutput, center.PTYTick, center.PTYFlush, center.PTYStopped:
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
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
	}

	return nil
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

	// Render panes
	dashView := a.dashboard.View()

	var centerView string
	if a.center.HasTabs() {
		centerView = a.center.View()
	} else {
		centerView = a.renderCenterPane()
	}

	sidebarView := a.sidebar.View()

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

	// Show help overlay if visible
	if a.helpOverlay.Visible() {
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

	content += "\n" + a.styles.Help.Render("Press Ctrl+T to launch an agent")

	return content
}

// renderWelcome renders the welcome screen
func (a *App) renderWelcome() string {
	logo := `

   __ _ _ __ ___  _   ___  __
  / _' | '_ ' _ \| | | \ \/ /
 | (_| | | | | | | |_| |>  <
  \__,_|_| |_| |_|\__,_/_/\_\
`

	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#98c379")).
		Bold(true).
		Render(logo)

	stats := fmt.Sprintf("\n%d projects registered", len(a.projects))
	statsStyled := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#61afef")).
		Render(stats)

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#5c6370")).
		Render("\n\nctrl+q: quit • j/k: navigate • enter: select • a: add project")

	return title + statsStyled + help
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
		// Update cache
		if a.statusManager != nil && err == nil {
			a.statusManager.RequestRefresh(root)
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
	return func() tea.Msg {
		worktreePath := filepath.Join(
			a.config.Paths.WorktreesRoot,
			project.Name,
			name,
		)

		branch := name

		if err := git.CreateWorktree(project.Path, worktreePath, branch, base); err != nil {
			return messages.Error{Err: err, Context: "creating worktree"}
		}

		// Wait for .git file to exist (race condition from git worktree add)
		gitPath := filepath.Join(worktreePath, ".git")
		for i := 0; i < 10; i++ {
			if _, err := os.Stat(gitPath); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		wt := data.NewWorktree(name, branch, base, project.Path, worktreePath)
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
			return messages.Error{Err: err, Context: "saving metadata"}
		}

		// Run setup scripts from .amux/worktrees.json
		if err := a.scripts.RunSetup(wt, meta); err != nil {
			// Don't fail worktree creation, just log the error
			return messages.WorktreeCreatedWithWarning{
				Worktree: wt,
				Warning:  fmt.Sprintf("setup failed: %v", err),
			}
		}

		return messages.RefreshDashboard{}
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
			return messages.Error{
				Err:     fmt.Errorf("cannot delete primary checkout"),
				Context: "deleting worktree",
			}
		}

		if err := git.RemoveWorktree(project.Path, wt.Root); err != nil {
			return messages.Error{Err: err, Context: "removing worktree"}
		}

		_ = git.DeleteBranch(project.Path, wt.Branch)
		_ = a.metadata.Delete(wt)

		return messages.RefreshDashboard{}
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
	case messages.PaneCenter:
		a.dashboard.Blur()
		a.center.Focus()
		a.sidebar.Blur()
	case messages.PaneSidebar:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Focus()
	}
}

// updateLayout updates component sizes based on window size
func (a *App) updateLayout() {
	a.dashboard.SetSize(a.layout.DashboardWidth(), a.layout.Height())
	a.center.SetSize(a.layout.CenterWidth(), a.layout.Height())
	a.center.SetOffset(a.layout.DashboardWidth()) // Set X offset for mouse coordinate conversion
	a.sidebar.SetSize(a.layout.SidebarWidth(), a.layout.Height())
	if a.dialog != nil {
		a.dialog.SetSize(a.width, a.height)
	}
}
