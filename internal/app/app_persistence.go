package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Tab persistence is deliberately narrow: only OpenTabs and ActiveTabIndex
// are ever written from this path, through Service.SaveWorkspaceTabs'
// locked field transaction. A debounced snapshot is captured on the Update
// goroutine and its command can run long after — a whole-Workspace save
// would resurrect whatever stale fields that capture happened to carry over
// a concurrent rename/env/script/lifecycle write.
//
// Ordering is by capture sequence (a.lifecycle.persistSeq, minted only on
// Update): a newer capture always wins even if two commands run in reverse
// order, and a superseded write is a benign no-op — never an error and never
// re-dirtied.

// tabPersistSnapshot is the immutable capture a persist command carries out
// of Update: which workspace, which capture generation, the copied tab
// state, and — only for the service's missing-record create path — a clone
// of the workspace itself. Persisting a workspace that has no metadata yet
// is creation, not a field update, so the clone backs a full Save there.
type tabPersistSnapshot struct {
	wsID      string
	seq       uint64
	tabs      []data.TabInfo
	activeIdx int
	fallback  *data.Workspace
}

// captureTabSnapshot mints the next persist sequence and snapshots the
// workspace's tab state. Must run on the Update goroutine — the sequence is
// only meaningful if it is assigned in Update order.
func (a *App) captureTabSnapshot(ws *data.Workspace, wsID string) tabPersistSnapshot {
	tabs, activeIdx := a.center.GetTabsInfoForWorkspace(wsID)
	ws.OpenTabs = tabs
	ws.ActiveTabIndex = activeIdx
	a.lifecycle.persistSeq++
	return tabPersistSnapshot{
		wsID:      wsID,
		seq:       a.lifecycle.persistSeq,
		tabs:      tabs,
		activeIdx: activeIdx,
		fallback:  snapshotWorkspaceForSave(ws),
	}
}

// persistOneTabSnapshot writes one captured snapshot through the service's
// ordered narrow write and returns whether it committed. Runs wherever the
// caller is — Update goroutine (shutdown flush) or a Cmd goroutine
// (debounce) — the service owns the ordering/locking.
func (a *App) persistOneTabSnapshot(snap tabPersistSnapshot) (committed bool, err error) {
	wrote := false
	var saveErr error
	ran := a.runUnlessWorkspaceMutationInFlight(snap.wsID, func() {
		wrote, saveErr = a.workspaceService.SaveWorkspaceTabs(
			snap.fallback, snap.seq, snap.tabs, snap.activeIdx)
	})
	if !ran {
		// Mutation began between capture and execution — nothing was written
		// and nothing failed; the mutation's own resolution path requeues.
		return false, nil
	}
	if saveErr != nil {
		return false, saveErr
	}
	if wrote {
		a.markLocalWorkspaceSaveForID(snap.wsID)
	}
	return wrote, nil
}

// persistAllWorkspacesNow saves all workspace tab state synchronously.
// Called before shutdown to ensure tabs are persisted before they are closed.
// This intentionally skips mutation-in-flight workspaces. Saving during a
// destructive delete can recreate metadata after the delete removes it.
func (a *App) persistAllWorkspacesNow() {
	if a.workspaceService == nil || a.center == nil {
		return
	}
	for _, project := range a.projects {
		for i := range project.Workspaces {
			ws := &project.Workspaces[i]
			wsID := string(ws.ID())
			if a.isWorkspaceMutationInFlight(wsID) {
				continue
			}
			snap := a.captureTabSnapshot(ws, wsID)
			if len(snap.tabs) == 0 && !a.center.HasWorkspaceState(wsID) {
				continue
			}
			// Shutdown waits behind any in-flight narrow write for this
			// workspace — the service's per-ID lock provides that ordering —
			// but never blocks on an unrelated workspace's write.
			if _, err := a.persistOneTabSnapshot(snap); err != nil {
				logging.Error("Failed to persist workspace on shutdown: %v", err)
			}
		}
	}
	// Clear dirty set since we just saved everything
	for k := range a.lifecycle.dirty {
		delete(a.lifecycle.dirty, k)
	}
}

// persistDebounceMsg is sent after the debounce period to trigger actual save.
type persistDebounceMsg struct {
	token persistToken
}

// persistSaveFailedMsg is returned by the debounced-save Cmd goroutine when one
// or more workspace saves fail. a.lifecycle.dirty is App state and must be
// mutated only on the Update loop (see workspaceLifecycleState.dirty's doc
// comment: "Touched only from App.Update handlers (single writer)"), so the
// goroutine cannot re-mark a workspace dirty itself. It reports the failure
// via this message instead; handlePersistSaveFailed does the actual re-dirty
// on the Update loop.
type persistSaveFailedMsg struct {
	workspaceIDs []string
}

// persistWorkspaceTabs marks a workspace dirty and schedules a debounced save.
func (a *App) persistWorkspaceTabs(wsID string) tea.Cmd {
	if wsID == "" {
		return nil
	}
	if a.isWorkspaceMutationInFlight(wsID) {
		return nil
	}
	a.lifecycle.markDirty(wsID)
	a.lifecycle.persistToken++
	token := a.lifecycle.persistToken
	return common.SafeTick(persistDebounce, func(t time.Time) tea.Msg {
		return persistDebounceMsg{token: token}
	})
}

func (a *App) migrateDirtyWorkspaceID(oldID, newID string) {
	if oldID == "" || newID == "" || oldID == newID {
		return
	}
	if a.lifecycle.dirty == nil || !a.lifecycle.dirty[oldID] {
		return
	}
	a.lifecycle.dirty[newID] = true
	delete(a.lifecycle.dirty, oldID)
}

// persistActiveWorkspaceTabs is a convenience that persists the active workspace's tabs.
func (a *App) persistActiveWorkspaceTabs() tea.Cmd {
	if a.activeWorkspace == nil {
		return nil
	}
	return a.persistWorkspaceTabs(string(a.activeWorkspace.ID()))
}

func (a *App) handlePersistDebounce(msg persistDebounceMsg) tea.Cmd {
	// Ignore stale tokens (newer persist request superseded this one)
	if msg.token != a.lifecycle.persistToken {
		return nil
	}
	if a.center == nil || a.workspaceService == nil {
		return nil
	}
	if len(a.lifecycle.dirty) == 0 {
		return nil
	}

	// Collect immutable tab snapshots for all dirty workspaces — capture
	// sequence minted here, on Update, so command order can't reorder writes.
	var snapshots []tabPersistSnapshot
	processed := make(map[string]bool, len(a.lifecycle.dirty))
	for wsID := range a.lifecycle.dirty {
		if a.isWorkspaceMutationInFlight(wsID) {
			// Keep dirty marker while delete is in flight. If delete fails, the
			// marker must remain so pending workspace state can still be saved.
			continue
		}
		ws := a.findWorkspaceByID(wsID)
		if ws == nil {
			processed[wsID] = true
			continue
		}
		snapshots = append(snapshots, a.captureTabSnapshot(ws, wsID))
		processed[wsID] = true
	}
	// Clear only workspaces processed above; keep in-flight delete markers dirty.
	for wsID := range processed {
		delete(a.lifecycle.dirty, wsID)
	}

	if len(snapshots) == 0 {
		return nil
	}
	return func() tea.Msg {
		var failedIDs []string
		for _, snap := range snapshots {
			_, err := a.persistOneTabSnapshot(snap)
			if err != nil {
				logging.Error("Failed to save workspace tabs: %v", err)
				// Do not touch a.lifecycle.dirty here — this runs in a Cmd
				// goroutine, not on the Update loop. Report the failure via a
				// message so handlePersistSaveFailed can re-dirty safely.
				failedIDs = append(failedIDs, snap.wsID)
			}
		}
		if len(failedIDs) == 0 {
			return nil
		}
		return persistSaveFailedMsg{workspaceIDs: failedIDs}
	}
}

// handlePersistSaveFailed re-marks workspaces dirty after their debounced save
// failed, so the next debounce (or clean shutdown) retries the save. This is
// the Update-loop counterpart to the persistSaveFailedMsg emitted from the
// save goroutine above: persistWorkspaceTabs mutates a.lifecycle.dirty, and it
// must only ever be called from here, not from that goroutine.
func (a *App) handlePersistSaveFailed(msg persistSaveFailedMsg) tea.Cmd {
	var cmds []tea.Cmd
	for _, wsID := range msg.workspaceIDs {
		if cmd := a.persistWorkspaceTabs(wsID); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return common.SafeBatch(cmds...)
}
