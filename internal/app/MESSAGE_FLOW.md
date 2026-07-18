# Message Flow and Taxonomy

This document defines message boundaries used by the app and clarifies which
messages may originate outside the Bubble Tea update loop.

## Taxonomy

### External Messages

External messages are produced by goroutines, IO, or long-running commands.
They must enter the app through the external message pump, never by direct
state mutation.

Examples:
- PTY output (`center.PTYOutput`, `messages.SidebarPTYOutput`)
- File/state watcher events (`messages.FileWatcherEvent`, `messages.StateWatcherEvent`)
- Background supervisor errors (`messages.Error` from workers)
- tmux discovery/sync results

Rules:
- External messages are enqueued via `App.enqueueExternalMsg`.
- External messages never mutate state directly; they are handled in `Update`.

### Internal Messages

Internal messages are produced by UI interactions or by commands triggered
inside the update loop.

Examples:
- Key/mouse input
- Dialog results
- UI-only actions (focus changes, toggles, local commands)

Rules:
- Internal messages may be generated synchronously in Update.
- Long-running work must still be wrapped in a `tea.Cmd`.

## Command Discipline

- Anything that touches disk, runs external commands, or waits on IO belongs in
  a `tea.Cmd`.
- Update handlers should be quick state transitions plus command scheduling.
- If work might block, wrap it in a command and return a message.

## Error Reporting

- Use `common.ReportError(...)` (or a thin wrapper) to log + toast + emit `messages.Error`.
- `messages.Error` is handled in one place (`App.handleErrorMessage`) to keep error UX consistent.
- Every *failure* of a user-initiated operation or a subsystem — e.g. a failed
  settings save, a failed workspace setup, or a missing tmux — must go through
  `ReportError` so it is logged and routed to `handleErrorMessage`.
- Not every toast is an error: soft-validation and informational toasts are
  intentionally *outside* `ReportError`. Bare `toast.ShowWarning`/`ShowError`
  for preconditions ("select a workspace first", "project path is required") or
  state notices ("session disconnected", "file watching disabled") stay as
  toasts, since they are neither failures of an operation nor something to route
  through the error overlay.

## Workspace Create → Activate Flow

Lifecycle phases live in `workspaceLifecycleState` (`app/workspace_lifecycle_state.go`):
a workspace is `active` (untracked), `creating`, or `deleting`; transitions go
through the FSM and invalid moves (e.g. create while delete-in-flight) are
rejected and logged.

1. `messages.CreateWorkspace` (dialog) → `handleCreateWorkspace`
   (`app_input_messages_workspace.go`): validates input, marks the pending
   workspace `creating` via `lifecycle.markCreating`, shows the dashboard
   spinner, and enqueues the async `workspaceService.CreateWorkspace` cmd.
2. The service creates the worktree, waits for `.git`, and saves metadata.
   Any failure after the worktree exists rolls the worktree/branch back and
   returns `messages.WorkspaceCreateFailed` — never `WorkspaceCreated` — so no
   setup or reload runs for a workspace that no longer exists.
3. `messages.WorkspaceCreated` → `handleWorkspaceCreated`
   (`app_input_workspace.go`): settles the phase back to active
   (`lifecycle.clearCreating`), clears the spinner, enqueues `runSetupAsync`
   and `loadProjects`.
4. `messages.ProjectsLoaded` → `handleProjectsLoaded`: applies the freshest
   load generation only (stale `LoadToken`s are dropped) and rebinds the
   active selection.
5. `messages.WorkspaceActivated` → `handleWorkspaceActivated`: sets the active
   workspace, discovers/restores tabs, starts git status + file watching.

While `creating`, a projects reload that does not yet contain the workspace
must not clear the phase — only `WorkspaceCreated`/`WorkspaceCreateFailed`
settle it (see `TestLifecycleCreateWhileProjectsLoading`).

## Workspace Delete Flow

1. `messages.DeleteWorkspace` (confirm dialog) → `handleDeleteWorkspace`
   (`app_input_workspace.go`): marks the workspace `deleting`
   (`lifecycle.markDeleting`), shows the dashboard spinner, and enqueues the
   async `workspaceService.DeleteWorkspace` cmd. Sessions are NOT killed here:
   a delete rejected by validation must leave live agents intact.
2. The service validates (primary-checkout guard, repo/path checks), stops
   managed scripts, kills the workspace's tmux sessions, kills orphaned
   service process groups still referencing the worktree and verifies none
   survive (`teardownWorkspaceProcessesForDelete` — a failure here aborts the
   delete before anything touches the worktree), writes a durable delete
   tombstone, removes the worktree under the per-repo git lock, deletes the
   branch (failure is a warning), then deletes metadata. Teardown running
   BEFORE removal is deliberate: a post-validation failure has already lost
   its sessions (recoverable), while removing a worktree under live writers
   orphans service stacks to PID 1 with a deleted cwd (which is not).
3. `messages.WorkspaceDeleted` → `handleWorkspaceDeleted`: settles the phase,
   drops the workspace from the active set, navigates home when it was the
   active workspace, clears its dirty marker, and reloads projects.
4. `messages.WorkspaceDeleteFailed` → `handleWorkspaceDeleteFailed`: settles
   the phase first, clears the tombstone only when the worktree still exists,
   then requeues persistence (`persistWorkspaceTabs`) — this is why the dirty
   marker is orthogonal to the lifecycle phase and survives `deleting`.

While `deleting`, persistence is suppressed (`persistWorkspaceTabs` and
`handlePersistDebounce` consult `isWorkspaceDeleteInFlight`), orphan GC treats
the workspace's sessions as known (`snapshotDeleting`), and store mutations are
guarded by `runUnlessWorkspaceDeleteInFlight`.
