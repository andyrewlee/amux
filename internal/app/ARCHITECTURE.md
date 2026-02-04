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
- The Bubble Tea update loop is the single writer for UI state.
- Long-running work must run in `tea.Cmd` goroutines and return a message.
- PTY output and background workers enqueue messages through the external
  message pump which preserves ordering and applies critical backpressure.

3. Render
- `App.View` composes dashboard + center + sidebar layers using layout
  measurements and compositor caches.
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
- Discovery merges on-disk worktrees into the metadata store; metadata is
  authoritative for UI state even when a workspace directory is missing.

## Invariants

- Tab terminal writes occur in the tab actor or the fallback path only.
- Workspace metadata persistence is single-writer per workspace ID.
- External message pump is the only path for PTY output to reach UI state.
- Long-running work happens only in `tea.Cmd` functions, not inline in update.
- UI models mutate only their own state; IO and side effects live in services.
- tmux session state is reconciled via periodic sync or explicit discovery, not
  during rendering.
