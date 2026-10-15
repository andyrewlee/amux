# Architecture

This document describes the core runtime flow of amux and the invariants that
keep Bubble Tea, tmux, and PTY state consistent.

## App Lifecycle

1. Init
- `app.New` wires config, registries, stores, services, UI models, and the
  supervisor.
- `App.Init` schedules async commands to load projects, start tickers, start
  watchers, and kick off update checks.

2. Message Pump
- The Bubble Tea update loop is the single writer for UI state, with one
  named exception: the center tab actor is a second writer for per-tab
  terminal state, serialized under `tab.mu` (and mirrored by the sidebar's
  `ts.mu`-guarded writer queue).
- Long-running work must run in `tea.Cmd` goroutines and return a message.
- PTY output and background workers enqueue messages through the external
  message pump which preserves ordering and applies critical backpressure.

3. Render
- `App.View` composes dashboard + center + sidebar layers using layout
  measurements and compositor caches.
- `App.View` reuses the complete prior frame for render-neutral PTY buffering
  messages. The frame key covers everything those messages can change behind
  `App.Update`'s back: the active center/sidebar vterm content versions, the
  active tab's OSC title version (title updates never move the content version),
  and center tab activity - the tab bar's working highlights, which the actor
  sets for background tabs too, plus the active chat tab's cursor-trust window.
  Inactive terminal *content* deliberately does not participate.
- Anything else that a render-neutral message mutates must join that key or the
  screen will hold a stale frame until an unrelated message arrives.
- Render is derived from state only; no side effects.

4. Shutdown
- `App.Shutdown` stops the supervisor, closes watchers, tears down PTYs, and
  ensures workspace state is persisted before exit.

## PTY Pipeline

PTY output travels through a single, ordered path:
1. PTY reader (tab actor / sidebar PTY reader)
2. External message pump (`App.enqueueExternalMsg`)
3. Bubble Tea update loop
4. vterm mutation + compositor snapshot
5. Render layers

This makes PTY delivery observable, debounced, and safe for UI state.

## tmux Lifecycle and Tagging

- Sessions are created with tags that identify amux ownership and workspace.
- Discovery scans tmux for tagged sessions, then hydrates missing tabs/terminals.
- Sync reconciles session state to persisted tab metadata.
- GC removes orphaned sessions not associated with any known workspace.

## Workspace Persistence and Discovery

- Workspace metadata (tabs, active index) is persisted per workspace ID.
- Persistence is debounced and single-writer (Bubble Tea update loop only).
- `Service.SaveWorkspaceTabs` owns the tab-persistence lifecycle guard: the
  atomic mutation check wraps the whole ordered save (write lock + store
  transaction + missing-record create) exactly once. Lock order is workspace
  lifecycle guard → per-ID tab write lock → store transaction. No caller may
  wrap `SaveWorkspaceTabs` in the lifecycle guard — re-acquiring the phase
  read lock inside the guarded write deadlocks behind a queued writer.
- Discovery merges on-disk worktrees into the metadata store; metadata is
  authoritative for UI state even when a workspace directory is missing.

## Panic Recovery

A panic must never freeze a subsystem for the rest of the session. The
contract has five layers:

- **Update/View recover.** `App.Update` (`app_input.go`) and `App.View`
  (`app_view.go`) wrap their bodies in recover: the panic is logged with a
  stack, `a.err` is set (surfaced through the error overlay), and the frame
  cache is invalidated. The dropped cmd set is then partially rebuilt:
  periodic chains re-arm only in tail position, so a panic before the tail
  kills the chain for the session. `rearmCmdForPanic` (`app_rearm.go`) maps
  each periodic-chain message to its starter command, mirroring the
  handler's own tail semantics (stale-token guards included); per-tab PTY
  chains delegate to each model's `RearmForPanic`. Every dispatched
  message must be classified — `TestRearmCmdForPanic_ClassifiesEveryDispatchedMessage`
  walks the dispatch table and fails on an unclassified type.
- **Advisory-latch reset.** `resetAdvisoryLatchesAfterPanic`
  (`app_panic_latches.go`) clears single-flight marks the panicking Update
  armed for cmds that never ran (git status, run-script status, upgrade,
  tmux activity scan), aborts staged dialog handoffs
  (`clearPendingHandoffs`), and abandons any in-flight bulk batch
  (`a.bulk.Abandon`). The function documents deliberate exclusions —
  lifecycle mutation marks, pending-launch picks, obligation markers —
  where a false clear admits concurrency or strands user intent, both
  worse than a stranded flag.
- **Cmd-level panic boundaries.** `common.SafeCmd`/`SafeBatch` confine a
  cmd panic to a `messages.Error`; single-flight producers instead wrap
  with `panicAsMsg`/`wrapLifecycleCmd` (`app_panic_results.go`,
  `app_lifecycle_cmd.go`) so the panic emits the producer's own typed
  failure — releasing the in-flight guard and advancing a bulk drain,
  neither of which a bare `messages.Error` would do.
- **Actor per-event recover.** The center tab actor wraps each event in
  its own recover (`tab_actor.go`): a panicking event is logged and
  dropped rather than killing the actor goroutine, then
  `reconcileActorEventPanic` cleans per-event state and
  `rearmActorChainAfterPanic` restarts the chain the event drives.
- **`withMu` discipline.** Pane-mutex sections run through `withMu`/
  `withMuVal` (`internal/ui/{center,sidebar}/mutex_guard.go`) — the
  deferred unlock means a panic cannot leave `tab.mu`/`ts.mu` held, which
  would deadlock the actor restart and every later reader.

## Invariants

- Tab terminal writes occur in the tab actor or the fallback path only.
- Workspace metadata persistence is single-writer per workspace ID.
- External message pump is the only path for PTY output to reach UI state.
- Long-running work happens only in `tea.Cmd` functions, not inline in update.
- UI models mutate only their own state; IO and side effects live in services.
- tmux session state is reconciled via periodic sync or explicit discovery, not
  during rendering.
