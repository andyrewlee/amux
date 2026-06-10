package app

// Distinct types for the App's dedup/generation counters, so the compiler
// rejects cross-assignment between unrelated token streams (e.g. comparing an
// activity-scan token against a persist token). Each token increments when a
// new generation is issued; handlers drop messages carrying a stale token.

// activityScanToken identifies a tmux activity ticker/scan generation.
type activityScanToken int

// projectsLoadToken identifies a projects-load generation; out-of-order
// reloads are dropped by comparing against the last applied token. The token
// crosses the messages package boundary as a plain int
// (messages.ProjectsLoaded.LoadToken).
type projectsLoadToken int

// persistToken identifies a workspace-persist debounce generation.
type persistToken int
