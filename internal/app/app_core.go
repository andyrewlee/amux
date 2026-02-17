package app

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/supervisor"
	"github.com/andyrewlee/amux/internal/tmux"
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
	DialogCleanupTmux     = "cleanup_tmux"
)

// prefixTimeoutMsg is sent when the prefix mode timer expires.
type prefixTimeoutMsg struct {
	token int
}

// App is the root Bubbletea model.
type App struct {
	// Configuration
	config           *config.Config
	workspaceService *workspaceService
	gitStatus        GitStatusService
	tmuxService      *tmuxService
	updateService    UpdateService

	// Limits
	maxAttachedAgentTabs int

	// State
	projects        []data.Project
	activeWorkspace *data.Workspace
	activeProject   *data.Project
	focusedPane     messages.PaneType
	showWelcome     bool

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
	// Pending workspace creation context while selecting assistant.
	pendingWorkspaceProject *data.Project
	pendingWorkspaceName    string
	pendingWorkspaceBase    string

	// Git status management
	fileWatcher     *git.FileWatcher
	fileWatcherCh   chan messages.FileWatcherEvent
	fileWatcherErr  error
	stateWatcher    *stateWatcher
	stateWatcherCh  chan messages.StateWatcherEvent
	stateWatcherErr error

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

	tmuxSyncToken             int
	tmuxActivityToken         int
	tmuxActivityScanInFlight  bool
	tmuxActivityRescanPending bool
	tmuxOptions               tmux.Options
	tmuxAvailable             bool
	tmuxCheckDone             bool
	projectsLoaded            bool
	tmuxInstallHint           string
	tmuxActiveWorkspaceIDs    map[string]bool
	sessionActivityStates     map[string]*activity.SessionState // Per-session hysteresis state
	instanceID                string
	lastTerminalGCRun         time.Time

	// Workspace persistence debounce
	dirtyWorkspaces       map[string]bool
	persistToken          int
	localWorkspaceSaveMu  sync.Mutex
	localWorkspaceSavesAt map[string]localWorkspaceSaveMarker

	// Workspaces in creation flow (not yet loaded into projects list)
	creatingWorkspaceIDs map[string]bool

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
