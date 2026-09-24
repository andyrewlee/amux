package messages

import (
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

// FileWatcherEvent is sent when a watched file changes
type FileWatcherEvent struct {
	Root string
}

// StateWatcherEvent is sent when amux state files change on disk.
type StateWatcherEvent struct {
	Reason string
	Paths  []string
}

// SidebarPTYOutput contains PTY output for sidebar terminal
type SidebarPTYOutput struct {
	WorkspaceID string
	TabID       string
	Data        []byte
}

// SidebarPTYFlush applies buffered PTY output for sidebar terminal
type SidebarPTYFlush struct {
	WorkspaceID string
	TabID       string
}

// SidebarPTYStopped signals that the sidebar PTY read loop has stopped
type SidebarPTYStopped struct {
	WorkspaceID string
	TabID       string
	Err         error
}

// MarkCriticalExternalMsg marks SidebarPTYStopped as a critical external
// message: a stopped PTY must never be evicted from the lossy queue.
func (SidebarPTYStopped) MarkCriticalExternalMsg() {}

// SidebarSelectionScrollTick is sent by the tick loop to continue
// auto-scrolling during mouse-drag selection past viewport edges.
type SidebarSelectionScrollTick struct {
	WorkspaceID string
	TabID       string
	Gen         uint64
	Seq         uint64
}

// BranchChangesLoaded carries the result of an async BranchChangesVsBase
// fetch triggered by toggling branch mode on. It is routed back into the
// sidebar explicitly by internal/app (see app_input.go), since Bubbletea has
// no generic message broadcast.
type BranchChangesLoaded struct {
	Root    string
	LoadID  int
	Changes []git.Change
	Err     error
}

// AheadBehindLoaded carries the result of an async AheadBehind fetch,
// triggered on workspace switch, manual refresh ("g"), and after a commit.
type AheadBehindLoaded struct {
	Root   string
	LoadID int
	Ahead  int
	Behind int
	Err    error
}

// UpdateCheckComplete is sent when the background update check finishes
type UpdateCheckComplete struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	ReleaseNotes    string
	Err             error
}

// TriggerUpgrade is sent when the user requests an upgrade
type TriggerUpgrade struct{}

// UpgradeComplete is sent when the upgrade finishes
type UpgradeComplete struct {
	NewVersion string
	Err        error
}

// OpenFileInVim requests opening a file in vim in the center pane
type OpenFileInVim struct {
	Path      string
	Workspace *data.Workspace
}

// AttachRunSession requests an interactive center tab attached to an existing
// run session (newest alive, resolved by the caller). The tab borrows the
// session: no tags are stamped and closing detaches instead of killing.
type AttachRunSession struct {
	Workspace   *data.Workspace
	SessionName string
}
