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
func (a *App) resetAdvisoryLatchesAfterPanic() {
	clear(a.gitStatusInFlight)
	a.runScriptStatusInFlight = false
	a.upgradeRunning = false
	a.tmuxActivity.scanInFlight = false
}
