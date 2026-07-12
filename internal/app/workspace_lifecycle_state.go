package app

import (
	"sync"

	"github.com/andyrewlee/amux/internal/logging"
)

// lifecyclePhase is a workspace's position in the create/delete lifecycle.
// Workspaces not present in the phase map are active: loaded, with no
// lifecycle operation in flight.
type lifecyclePhase uint8

const (
	lifecycleActive lifecyclePhase = iota
	// lifecycleCreating: create accepted, worktree/metadata still being built;
	// the workspace is not in the projects list yet.
	lifecycleCreating
	// lifecycleDeleting: delete accepted, teardown in flight.
	lifecycleDeleting
)

func (p lifecyclePhase) String() string {
	switch p {
	case lifecycleCreating:
		return "creating"
	case lifecycleDeleting:
		return "deleting"
	default:
		return "active"
	}
}

// lifecycleTransitionAllowed is the transition table: creating and deleting
// are mutually exclusive, entered only from active, and always allowed to
// settle back to active. Same-phase moves are idempotent no-ops.
func lifecycleTransitionAllowed(from, to lifecyclePhase) bool {
	if from == to {
		return true
	}
	switch from {
	case lifecycleActive:
		return true
	case lifecycleCreating, lifecycleDeleting:
		return to == lifecycleActive
	default:
		return false
	}
}

// workspaceLifecycleState holds the workspace create/delete/persist
// bookkeeping. The phase map is the explicit lifecycle state machine; the
// dirty set is deliberately NOT a phase, because a dirty marker must survive
// a delete that later fails (the failed-delete handler requeues persistence),
// so dirty coexists with deleting.
type workspaceLifecycleState struct {
	// phaseMu guards phases; the deleting phase is read from Cmd/worker
	// goroutines via the App guard helpers.
	phaseMu        sync.RWMutex
	phases         map[string]lifecyclePhase
	deletingRootID map[string]string
	// dirty tracks workspaces with unsaved tab state (persist debounce).
	// Touched only from App.Update handlers (single writer).
	dirty map[string]bool
	// persistToken is the current persist-debounce generation.
	persistToken persistToken
	// projectsLoadToken is the next load generation to issue; lastApplied is
	// the highest applied, so handleProjectsLoaded can drop stale reloads.
	projectsLoadToken            projectsLoadToken
	lastAppliedProjectsLoadToken projectsLoadToken
	// deletedUntilProjectsLoadToken keeps a successfully deleted workspace hidden
	// from project-load snapshots until the post-delete reload has applied.
	// Keys include both workspace IDs and root paths because ID normalization can
	// change after the worktree path is removed.
	deletedUntilProjectsLoadToken map[string]projectsLoadToken
	// localSaveMu guards localSavesAt (written from Cmd goroutines).
	localSaveMu  sync.Mutex
	localSavesAt map[string]localWorkspaceSaveMarker
}

func newWorkspaceLifecycleState() workspaceLifecycleState {
	return workspaceLifecycleState{
		phases:                        make(map[string]lifecyclePhase),
		deletingRootID:                make(map[string]string),
		dirty:                         make(map[string]bool),
		deletedUntilProjectsLoadToken: make(map[string]projectsLoadToken),
		localSavesAt:                  make(map[string]localWorkspaceSaveMarker),
	}
}

// transition moves wsID to a new phase, rejecting (and logging) moves the
// transition table does not allow — e.g. deleting → creating.
func (w *workspaceLifecycleState) transition(wsID string, to lifecyclePhase) bool {
	if wsID == "" {
		return false
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	if w.phases == nil {
		w.phases = make(map[string]lifecyclePhase)
	}
	return w.transitionLocked(wsID, to)
}

func (w *workspaceLifecycleState) transitionLocked(wsID string, to lifecyclePhase) bool {
	from := w.phases[wsID]
	if !lifecycleTransitionAllowed(from, to) {
		logging.Warn("workspace lifecycle: rejected transition %s -> %s for workspace %s", from, to, wsID)
		return false
	}
	if to == lifecycleActive {
		delete(w.phases, wsID)
	} else {
		w.phases[wsID] = to
	}
	return true
}

// phase returns the workspace's current lifecycle phase.
func (w *workspaceLifecycleState) phase(wsID string) lifecyclePhase {
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	return w.phases[wsID]
}

// markCreating records a workspace as create-in-flight. It reports whether
// the transition was accepted (rejected when the workspace is mid-delete).
func (w *workspaceLifecycleState) markCreating(wsID string) bool {
	return w.transition(wsID, lifecycleCreating)
}

// clearCreating settles a creating workspace back to active. A workspace in
// any other phase is left untouched.
func (w *workspaceLifecycleState) clearCreating(wsID string) {
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	if w.phases[wsID] == lifecycleCreating {
		delete(w.phases, wsID)
	}
}

// markDeleting sets or clears the delete-in-flight phase for a workspace.
// Setting is rejected while the workspace is mid-create; clearing only
// settles a deleting workspace (it never stomps another phase).
func (w *workspaceLifecycleState) markDeleting(wsID string, deleting bool) bool {
	if deleting {
		return w.transition(wsID, lifecycleDeleting)
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	if w.phases[wsID] == lifecycleDeleting {
		delete(w.phases, wsID)
	}
	return true
}

func (w *workspaceLifecycleState) markDeletingWorkspace(wsID, root string, deleting bool) bool {
	if root == "" {
		return w.markDeleting(wsID, deleting)
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	if w.phases == nil {
		w.phases = make(map[string]lifecyclePhase)
	}
	if w.deletingRootID == nil {
		w.deletingRootID = make(map[string]string)
	}
	if deleting {
		if !w.transitionLocked(wsID, lifecycleDeleting) {
			return false
		}
		w.deletingRootID[root] = wsID
		return true
	}
	if markedID := w.deletingRootID[root]; markedID != "" {
		delete(w.phases, markedID)
		delete(w.deletingRootID, root)
	}
	if w.phases[wsID] == lifecycleDeleting {
		delete(w.phases, wsID)
	}
	return true
}

// isDeleting reports whether a workspace is currently delete-in-flight.
func (w *workspaceLifecycleState) isDeleting(wsID string) bool {
	if wsID == "" {
		return false
	}
	return w.phase(wsID) == lifecycleDeleting
}

func (w *workspaceLifecycleState) isDeletingWorkspace(wsID, root string) bool {
	if wsID == "" && root == "" {
		return false
	}
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	if wsID != "" && w.phases[wsID] == lifecycleDeleting {
		return true
	}
	if root != "" {
		markedID := w.deletingRootID[root]
		return markedID != "" && w.phases[markedID] == lifecycleDeleting
	}
	return false
}

// snapshotPhase returns a copy of the IDs currently in the given phase. The
// RLock is required because callers like collectKnownWorkspaceIDs run on the
// Update goroutine while the map is also mutated from worker goroutines.
func (w *workspaceLifecycleState) snapshotPhase(phase lifecyclePhase) map[string]bool {
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	var out map[string]bool
	for id, p := range w.phases {
		if p != phase {
			continue
		}
		if out == nil {
			out = make(map[string]bool)
		}
		out[id] = true
	}
	return out
}

// snapshotDeleting returns a copy of the IDs currently delete-in-flight.
func (w *workspaceLifecycleState) snapshotDeleting() map[string]bool {
	return w.snapshotPhase(lifecycleDeleting)
}

// snapshotCreating returns a copy of the IDs currently create-in-flight.
func (w *workspaceLifecycleState) snapshotCreating() map[string]bool {
	return w.snapshotPhase(lifecycleCreating)
}

// runUnlessDeleting runs fn while holding the shared phase lock only when
// wsID is not currently delete-in-flight. Holding the lock across fn keeps
// the check and side effect atomic with respect to markDeleting.
func (w *workspaceLifecycleState) runUnlessDeleting(wsID string, fn func()) bool {
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()

	if wsID == "" || w.phases[wsID] == lifecycleDeleting {
		return false
	}
	if fn != nil {
		fn()
	}
	return true
}

func (w *workspaceLifecycleState) markDeletedUntilProjectsLoad(wsID, root string, token projectsLoadToken) {
	if token == 0 || (wsID == "" && root == "") {
		return
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	if w.deletedUntilProjectsLoadToken == nil {
		w.deletedUntilProjectsLoadToken = make(map[string]projectsLoadToken)
	}
	if wsID != "" {
		w.deletedUntilProjectsLoadToken[wsID] = token
	}
	if root != "" {
		w.deletedUntilProjectsLoadToken[root] = token
	}
}

func (w *workspaceLifecycleState) shouldFilterDeletedWorkspace(wsID, root string, loadToken projectsLoadToken) bool {
	if wsID == "" && root == "" {
		return false
	}
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	if wsID != "" && w.phases[wsID] == lifecycleDeleting {
		return true
	}
	if root != "" {
		markedID := w.deletingRootID[root]
		if markedID != "" && w.phases[markedID] == lifecycleDeleting {
			return true
		}
	}
	for _, identity := range []string{wsID, root} {
		if identity == "" {
			continue
		}
		until, ok := w.deletedUntilProjectsLoadToken[identity]
		if ok && (loadToken == 0 || loadToken <= until) {
			return true
		}
	}
	return false
}

func (w *workspaceLifecycleState) clearDeletedProjectLoadBarriersThrough(loadToken projectsLoadToken, loadedIdentities map[string]bool) {
	if loadToken == 0 {
		return
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	for wsID, until := range w.deletedUntilProjectsLoadToken {
		if until <= loadToken && !loadedIdentities[wsID] {
			delete(w.deletedUntilProjectsLoadToken, wsID)
		}
	}
}

// markDirty records a workspace as having unsaved tab state.
func (w *workspaceLifecycleState) markDirty(wsID string) {
	if wsID == "" {
		return
	}
	if w.dirty == nil {
		w.dirty = make(map[string]bool)
	}
	w.dirty[wsID] = true
}
