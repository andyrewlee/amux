package app

import (
	"sync"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
)

// lifecyclePhase is a workspace's position in the create/mutate lifecycle.
// Workspaces not present in the phase map are active: loaded, with no
// lifecycle operation in flight.
type lifecyclePhase uint8

const (
	lifecycleActive lifecyclePhase = iota
	// lifecycleCreating: create accepted, worktree/metadata still being built;
	// the workspace is not in the projects list yet.
	lifecycleCreating
	// lifecycleMutating: delete accepted, teardown in flight.
	lifecycleMutating
)

func (p lifecyclePhase) String() string {
	switch p {
	case lifecycleCreating:
		return "creating"
	case lifecycleMutating:
		return "mutating"
	default:
		return "active"
	}
}

// lifecycleTransitionAllowed is the transition table: creating and mutating
// are mutually exclusive, entered only from active, and always allowed to
// settle back to active. Same-phase moves are idempotent no-ops.
func lifecycleTransitionAllowed(from, to lifecyclePhase) bool {
	if from == to {
		return true
	}
	switch from {
	case lifecycleActive:
		return true
	case lifecycleCreating, lifecycleMutating:
		return to == lifecycleActive
	default:
		return false
	}
}

// workspaceLifecycleState holds the workspace create/mutate/persist
// bookkeeping. The phase map is the explicit lifecycle state machine; the
// dirty set is deliberately NOT a phase, because a dirty marker must survive
// a mutation that later fails (the failed-op handler requeues persistence),
// so dirty coexists with mutating.
type workspaceLifecycleState struct {
	// phaseMu guards phases; the mutating phase is read from Cmd/worker
	// goroutines via the App guard helpers.
	phaseMu        sync.RWMutex
	phases         map[string]lifecyclePhase
	mutatingRootID map[string]string
	// creatingRootID bridges root → the ID a create was marked under. The
	// marked form is computed while the worktree dir does not exist, so it
	// can differ from every identity the post-create workspace reproduces —
	// without the bridge the pre-create key leaks in phases forever.
	creatingRootID map[string]string
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
	// createdUntilProjectsLoadToken protects a newly created/activated workspace
	// from older in-flight project snapshots that began before its metadata was
	// visible. Keys include both workspace IDs and root paths.
	createdUntilProjectsLoadToken map[string]projectsLoadToken
	// localSaveMu guards localSavesAt (written from Cmd goroutines).
	localSaveMu  sync.Mutex
	localSavesAt map[string]localWorkspaceSaveMarker
}

func newWorkspaceLifecycleState() workspaceLifecycleState {
	return workspaceLifecycleState{
		phases:                        make(map[string]lifecyclePhase),
		mutatingRootID:                make(map[string]string),
		creatingRootID:                make(map[string]string),
		dirty:                         make(map[string]bool),
		deletedUntilProjectsLoadToken: make(map[string]projectsLoadToken),
		createdUntilProjectsLoadToken: make(map[string]projectsLoadToken),
		localSavesAt:                  make(map[string]localWorkspaceSaveMarker),
	}
}

// transition moves wsID to a new phase, rejecting (and logging) moves the
// transition table does not allow — e.g. mutating → creating.
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
// the transition was accepted (rejected when the workspace is mid-mutation).
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

// markMutating sets or clears the mutation-in-flight phase for a workspace.
// Setting is rejected while the workspace is mid-create AND while it is
// already mutating — every caller is a lifecycle dispatch guard, so a
// repeated mark is a re-entry, not an idempotent refresh. Clearing only
// settles a mutating workspace (it never stomps another phase).
func (w *workspaceLifecycleState) markMutating(wsID string, mutating bool) bool {
	if mutating {
		w.phaseMu.Lock()
		defer w.phaseMu.Unlock()
		if w.phases == nil {
			w.phases = make(map[string]lifecyclePhase)
		}
		if w.phases[wsID] == lifecycleMutating {
			return false
		}
		return w.transitionLocked(wsID, lifecycleMutating)
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	if w.phases[wsID] == lifecycleMutating {
		delete(w.phases, wsID)
	}
	return true
}

func (w *workspaceLifecycleState) markMutatingWorkspace(wsID, root string, mutating bool) bool {
	if root == "" {
		return w.markMutating(wsID, mutating)
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	if w.phases == nil {
		w.phases = make(map[string]lifecyclePhase)
	}
	if w.mutatingRootID == nil {
		w.mutatingRootID = make(map[string]string)
	}
	if mutating {
		if w.phases[wsID] == lifecycleMutating {
			// Already in flight under this identity — the dispatch must be
			// rejected, not treated as an idempotent re-mark (the generic
			// transition table allows same-phase moves, which is exactly the
			// re-entry this guard exists to stop).
			return false
		}
		if !w.transitionLocked(wsID, lifecycleMutating) {
			return false
		}
		w.mutatingRootID[root] = wsID
		return true
	}
	if markedID := w.mutatingRootID[root]; markedID != "" {
		delete(w.phases, markedID)
		delete(w.mutatingRootID, root)
	}
	if w.phases[wsID] == lifecycleMutating {
		delete(w.phases, wsID)
	}
	return true
}

// isMutating reports whether a workspace is currently mutation-in-flight.
func (w *workspaceLifecycleState) isMutating(wsID string) bool {
	if wsID == "" {
		return false
	}
	return w.phase(wsID) == lifecycleMutating
}

// isMutatingLocked is the caller-holds-phaseMu core of every mutation probe:
// direct ID hit plus the root bridge (the mark stamped under one ID form is
// findable through the root when a sibling form drifts).
func (w *workspaceLifecycleState) isMutatingLocked(wsID, root string) bool {
	if wsID != "" && w.phases[wsID] == lifecycleMutating {
		return true
	}
	if root != "" {
		markedID := w.mutatingRootID[root]
		return markedID != "" && w.phases[markedID] == lifecycleMutating
	}
	return false
}

func (w *workspaceLifecycleState) isMutatingWorkspace(wsID, root string) bool {
	if wsID == "" && root == "" {
		return false
	}
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	return w.isMutatingLocked(wsID, root)
}

// isMutatingWorkspaceIDs probes the workspace's FULL identity set plus the
// root bridge — a mutation marked under any form (pre/post-drift ComputedID,
// MetadataID, storeID) is found regardless of which form this workspace
// value resolves to right now.
func (w *workspaceLifecycleState) isMutatingWorkspaceIDs(ws *data.Workspace) bool {
	if ws == nil {
		return false
	}
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	for _, id := range data.WorkspaceIdentityStrings(ws) {
		if w.isMutatingLocked(id, "") {
			return true
		}
	}
	return w.isMutatingLocked("", ws.Root)
}

// runUnlessMutatingWorkspaceIDs is the set-wide counterpart of
// runUnlessMutating: the check and fn run under the same phaseMu hold, so a
// mutation marked mid-flight between the probe and the store write can't
// slip past. Used by the service's rescan/import guards.
func (w *workspaceLifecycleState) runUnlessMutatingWorkspaceIDs(ws *data.Workspace, fn func()) bool {
	if ws == nil {
		return false
	}
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	for _, id := range data.WorkspaceIdentityStrings(ws) {
		if w.isMutatingLocked(id, "") {
			return false
		}
	}
	if w.isMutatingLocked("", ws.Root) {
		return false
	}
	if fn != nil {
		fn()
	}
	return true
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

// snapshotMutating returns a copy of the IDs currently mutation-in-flight.
func (w *workspaceLifecycleState) snapshotMutating() map[string]bool {
	return w.snapshotPhase(lifecycleMutating)
}

// snapshotCreating returns a copy of the IDs currently create-in-flight.
func (w *workspaceLifecycleState) snapshotCreating() map[string]bool {
	return w.snapshotPhase(lifecycleCreating)
}

// runUnlessMutating runs fn while holding the shared phase lock only when
// wsID is not currently mutation-in-flight. Holding the lock across fn keeps
// the check and side effect atomic with respect to markMutating.
func (w *workspaceLifecycleState) runUnlessMutating(wsID string, fn func()) bool {
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()

	if wsID == "" || w.phases[wsID] == lifecycleMutating {
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

func (w *workspaceLifecycleState) markCreatedUntilProjectsLoad(wsID, root string, token projectsLoadToken) {
	if token == 0 || (wsID == "" && root == "") {
		return
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	if w.createdUntilProjectsLoadToken == nil {
		w.createdUntilProjectsLoadToken = make(map[string]projectsLoadToken)
	}
	if wsID != "" {
		w.createdUntilProjectsLoadToken[wsID] = token
	}
	if root != "" {
		w.createdUntilProjectsLoadToken[root] = token
	}
}

func (w *workspaceLifecycleState) shouldRetainCreatedWorkspace(wsID, root string, loadToken projectsLoadToken) bool {
	if loadToken == 0 || (wsID == "" && root == "") {
		return false
	}
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	for _, identity := range []string{wsID, root} {
		if identity == "" {
			continue
		}
		if until, ok := w.createdUntilProjectsLoadToken[identity]; ok && loadToken <= until {
			return true
		}
	}
	return false
}

func (w *workspaceLifecycleState) clearCreatedProjectLoadBarriersThrough(loadToken projectsLoadToken, loadedIdentities map[string]bool) {
	if loadToken == 0 {
		return
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	for identity, until := range w.createdUntilProjectsLoadToken {
		if loadToken < until {
			continue
		}
		if loadedIdentities[identity] || loadToken > until {
			delete(w.createdUntilProjectsLoadToken, identity)
		}
	}
}

func (w *workspaceLifecycleState) clearCreatedProjectLoadBarrier(wsID, root string) {
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	delete(w.createdUntilProjectsLoadToken, wsID)
	delete(w.createdUntilProjectsLoadToken, root)
}

func (w *workspaceLifecycleState) shouldFilterDeletedWorkspace(wsID, root string, loadToken projectsLoadToken) bool {
	if wsID == "" && root == "" {
		return false
	}
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	if wsID != "" && w.phases[wsID] == lifecycleMutating {
		return true
	}
	if root != "" {
		markedID := w.mutatingRootID[root]
		if markedID != "" && w.phases[markedID] == lifecycleMutating {
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
