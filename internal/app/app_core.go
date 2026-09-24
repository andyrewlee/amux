package app

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/supervisor"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
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
	DialogRenameWorkspace = "rename_workspace"
	DialogCommitWorkspace = "commit_workspace"
	DialogMergeWorkspace  = "merge_workspace"
	DialogMergeConflict   = "merge_conflict"
	DialogTrustScripts    = "trust_scripts"
	DialogShelveWorkspace = "shelve_workspace"
	// DialogBulkShelveWorkspace confirms shelving the dashboard's marked
	// workspace set as one operation — replaces N per-row confirms, not the
	// per-row op itself.
	DialogBulkShelveWorkspace = "bulk_shelve_workspace"
	// DialogBulkRestoreWorkspace confirms restoring the dashboard's marked
	// shelved rows as one sequential drain.
	DialogBulkRestoreWorkspace = "bulk_restore_workspace"
	// DialogBulkPurgeWorkspace is the typed confirm for permanently deleting
	// the dashboard's marked shelved rows — the input must equal the count.
	DialogBulkPurgeWorkspace = "bulk_purge_workspace"
	DialogRemoveProject      = "remove_project"
	DialogQuit               = "quit"
	DialogCleanupTmux        = "cleanup_tmux"
	DialogSaveTranscript     = "save_transcript"
)

// prefixTimeoutMsg is sent when the prefix mode timer expires.
type prefixTimeoutMsg struct {
	token int
}

// App is the root Bubbletea model.
type App struct {
	// Configuration
	config           *config.Config
	workspaceService *workspacesvc.Service
	gitStatus        GitStatusService
	tmuxService      TmuxOps
	updateService    UpdateService

	// Git-status request dedup: at most one refresh subprocess per
	// root in flight; requests arriving during one coalesce into a single
	// follow-up. All three maps are touched only on the Update goroutine —
	// the worker Cmds never read them.
	gitStatusInFlight    map[string]bool
	gitStatusPending     map[string]bool
	gitStatusPendingFull map[string]bool

	// Limits
	maxAttachedAgentTabs    int
	maxAttachedTerminalTabs int

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
	// overlays holds the bespoke (non-registry) modal dialogs and their
	// per-dialog context — see app_overlays.go for the struct and the
	// table-driven input chain that replaced per-field wrappers.
	overlays overlayState
	// pendingOverlayOpens defers modal overlay opens that arrive while
	// another overlay is visible — see app_overlay_arbiter.go.
	pendingOverlayOpens []func()
	projectEnvStore     *data.ProjectEnvStore
	// lastRunScriptRoot/lastRunScriptAlive track the active workspace's run
	// state across run-script status results so a running→stopped flip with
	// a non-zero exit surfaces as a toast instead of a silent badge-off.
	lastRunScriptRoot  string
	lastRunScriptAlive bool
	// runScriptStatusInFlight suppresses a second status check while one is
	// still running — the check shells tmux, so a wedged server would
	// otherwise pile up goroutines one per tick.
	runScriptStatusInFlight bool

	// Overlays
	toast *common.ToastModel
	// overlayGeom caches the overlay dimensions measured by the last
	// composeOverlays pass; cursor placement and mouse hit-tests reuse it
	// rather than re-rendering views the compose pass already produced.
	overlayGeom overlayGeometry

	// dlg holds per-dialog scratch set at dialog-open. A dialog result carries
	// the dlg snapshot captured at emit time (boundDialogResultMsg), so the
	// context applied is the producing instance's — never the context of a
	// dialog that replaced it before the result landed.
	dlg dialogContext
	// dialogSeq is a monotonically increasing counter stamped by
	// presentDialog on each shown app dialog; dialogOpenSeq records the seq of
	// the currently-open instance. A bound result whose seq no longer matches
	// dialogOpenSeq is stale (its dialog was replaced) and is dropped.
	dialogSeq     int
	dialogOpenSeq int
	// pendingWorkspaceCreate carries the create-workspace→agent-picker
	// handoff: it is written inside a dialog result and consumed by the NEXT
	// dialog's result (or its cancel), so it cannot live in dlg — a blanket
	// clear at each result would wipe it before consumption.
	pendingWorkspaceCreate pendingWorkspaceCreateState
	// Assistants chosen during workspace creation, keyed by workspace ID and
	// launched once that workspace is activated, so creating a workspace lands
	// the user in a live agent tab instead of making them re-pick the same
	// assistant. Keyed rather than a single slot because creations are async and
	// unordered: two started back to back are both in flight, and a single slot
	// would let the second overwrite the first, leaving it with no agent.
	pendingLaunchAssistants map[string]string

	// bulk drives a marked-set lifecycle op (shelve, restore, purge) one
	// workspace at a time through the literal per-row handler — see
	// app_bulk_shelve.go.
	bulk bulkOpState

	// Git write-back seams. All nil in production (each falls back to the real
	// git.* function); tests install fakes to assert the dialog→git wiring
	// without a real repo.
	commitAllFn        func(context.Context, string, string) error
	mergeBranchFn      func(context.Context, string, string) error
	abortMergeFn       func(context.Context, string) error
	checkedOutBranchFn func(string) (string, error)
	// copyToClipboardFn is the transcript-export sink seam: nil in
	// production (falls back to common.CopyToClipboardWithLog); tests stub
	// it so the copy path never shells out to pbcopy mid-test.
	copyToClipboardFn func(text, label string)
	localBaseBranchFn func(repoPath, base string) string

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
	prefixActive   bool
	prefixToken    int
	prefixSequence []string

	// tmuxActivity holds tmux activity-scan bookkeeping (tokens, coalescing,
	// shared-scan ownership, per-session hysteresis).
	tmuxActivity    tmuxActivityState
	tmuxOptions     tmux.Options
	tmuxAvailable   bool
	tmuxCheckDone   bool
	projectsLoaded  bool
	tmuxInstallHint string
	instanceID      string // Immutable after init; safe for read-only access from Cmd goroutines.

	// lifecycle holds workspace create/delete/persist bookkeeping.
	lifecycle workspaceLifecycleState

	// Terminal capabilities
	keyboardEnhancements tea.KeyboardEnhancementsMsg

	// Perf tracking
	lastInputAt         time.Time
	pendingInputLatency bool

	// renderCache holds the chrome/drawable caches for layer-based rendering.
	renderCache renderCacheState

	// External message pump (for PTY readers)
	externalMsgs     chan tea.Msg
	externalCritical chan tea.Msg
	externalSender   func(tea.Msg)
	externalOnce     sync.Once
}

// dialogContext is per-dialog scratch: written when a dialog is shown, read
// in handleDialogResult, then cleared wholesale so nothing leaks into the
// next dialog's lifecycle.
type dialogContext struct {
	project          *data.Project
	workspace        *data.Workspace
	trustScriptsHash string
	// bulkTargets carries the confirmed bulk-op set from the dialog to
	// its result handler.
	bulkTargets []bulkTarget
	// mergeBase is the local base branch the merge confirmation resolved and
	// verified, carried to the confirm handler so it reports the same branch
	// the user was shown.
	mergeBase string
	// transcript carries the pane transcript captured when the save-transcript
	// dialog opened — the terminal keeps scrolling while the dialog is up, so
	// the bytes must be snapshotted at open, not read again on confirm.
	transcript string
}

// pendingWorkspaceCreateState is the create-workspace→agent-picker handoff:
// written by the create-workspace result, consumed by the agent-picker
// result or its cancel path. Crosses two dialogs by design — see App.dlg.
type pendingWorkspaceCreateState struct {
	project *data.Project
	name    string
	base    string
}
