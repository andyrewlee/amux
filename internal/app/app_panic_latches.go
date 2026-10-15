package app

// resetAdvisoryLatchesAfterPanic clears the single-flight marks that gate
// off-loop producers. Every mark names a tea.Cmd the marking Update was
// about to return; a panicking Update discards that whole cmd set, so any
// mark it set refers to a producer that never ran — leaving it latched
// freezes the associated feature (status column, run indicator, upgrade,
// activity scan) until restart.
//
// Clearing marks set by EARLIER Updates is also safe: those name producers
// that are genuinely running, whose result messages will land and clear the
// mark anyway — a false clear at worst allows one duplicate read
// (idempotent). The asymmetry is deliberate: a duplicate refresh is cheap;
// a permanently frozen indicator is not.
//
// Overlap caveat: clearing scanInFlight while a scan genuinely runs
// permits a second concurrent scan — tolerable ONLY because missBySession
// is mutex-guarded (tmuxActivityState.missMu). Never add another
// goroutine-side mutable field to the scan path without the same
// protection.
//
// Deliberate exclusions — do NOT clear these here:
//   - lifecycle mutation marks (markWorkspaceMutationInFlight): they
//     serialize worktree add/remove, not idempotent reads — a false clear
//     admits a concurrent mutation, strictly worse than a stranded flag
//     (which wedges only that one workspace's lifecycle ops).
//   - pendingLaunchAssistants: keyed by workspace ID and consumed at
//     activation — clearing strands a picked assistant silently.
//   - tmuxActivity.rescanPending, overlays.settingsUIPersistPending:
//     obligation markers ("work still owed") that self-heal when set.
//   - pendingInputLatency: a timing probe that self-heals on the next
//     keypress.
func (a *App) resetAdvisoryLatchesAfterPanic() {
	clear(a.gitStatusInFlight)
	// The pending maps pair with gitStatusInFlight: a cleared in-flight
	// with a surviving pending mark yields one spurious follow-up refresh
	// per entry on the next result. Cheap, but the maps also linger —
	// drop both while the reset already touches this structure.
	clear(a.gitStatusPending)
	clear(a.gitStatusPendingFull)
	a.runScriptStatusInFlight = false
	a.upgradeRunning = false
	a.tmuxActivity.scanInFlight = false

	// Staged dialog handoffs cross two dialog results; a panic between arm
	// and consume leaves a stale binding the next unrelated dialog would
	// consume (e.g. an agent pick silently creating a workspace). Abort
	// them — the user re-issues the flow.
	a.clearPendingHandoffs()

	// A panic inside advanceBulk strands the batch: queue non-empty and
	// headID armed forever, rejecting every later bulk op. Abandon the
	// batch — the in-flight head op still completes through its own
	// message (bulkFinished no-ops on the cleared headID, so a foreign
	// completion cannot corrupt a future batch), and its marks come down
	// here since finishBulk will never run.
	if a.bulk.active() {
		if a.dashboard != nil {
			a.dashboard.ClearMarks(a.bulk.markIDs)
		}
		a.bulk = bulkOpState{}
	}
}
