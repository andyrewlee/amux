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

Three surfacing channels exist; every `messages.Error` is handled in one place
(`App.handleErrorMessage`), which logs it (when `Logged` is unset) and sets
`a.err` — the modal error overlay ("Press any key to dismiss").

- Raw `messages.Error{Err, Context}` → **modal overlay + log**. Used by most
  Cmd-emitted operation failures (agent create, workspace ops, dialog
  validation). The overlay is the visible surface; no toast is emitted.
- `common.ReportError(context, err, toastMessage)` → **modal overlay + error
  toast + log** (`Error{Logged:true}` paired with `Toast{ToastError}`). Use it
  for failures the user must notice even if they dismiss the overlay quickly
  — a failed settings save, a failed workspace setup, missing tmux.
- Bare `messages.Toast` → **transient toast only**. For soft-validation and
  informational notices ("select a workspace first", "project path is
  required", "session disconnected") — neither operation failures nor
  something to route through the error overlay.

The `safecmd.go` panic wrappers intentionally emit `Error{Logged:true}` with
no toast: a panic already owns the overlay.

New code should reach for `ReportError` for user-initiated operation failures;
a raw `messages.Error` is acceptable when the modal alone is the intended
surface. If a lint check ever lands, "raw `messages.Error` emitted with no
visible surface intent" is the smell to flag — today the contract is social.

## Workspace Create → Activate Flow

Lifecycle phases live in `workspaceLifecycleState` (`app/workspace_lifecycle_state.go`):
a workspace is `active` (untracked), `creating`, or `mutating`; transitions go
through the FSM and invalid moves (e.g. create while mutation-in-flight) are
rejected and logged. `mutating` is one shared phase covering delete, shelve,
and restore — the invariant is one workspace mutation at a time per workspace,
with persistence and rescan suppressed while it is set.

1. `messages.CreateWorkspace` (dialog) → `handleCreateWorkspace`
   (`app_input_messages_workspace.go`): validates input, marks the pending
   workspace `creating` via `lifecycle.markCreating`, shows the dashboard
   spinner, and enqueues the async `workspacesvc.Service.CreateWorkspace` cmd.
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
   (`app_input_workspace.go`): marks the workspace `mutating`
   (`lifecycle.markMutating`), shows the dashboard "deleting" spinner
   (`dashboard.SetWorkspaceBusy` with `WorkspaceOpDelete`), and enqueues the
   async `workspacesvc.Service.DeleteWorkspace` cmd. Sessions are NOT killed here:
   a rejected/failed delete must leave live agents intact.
2. The service validates (primary-checkout guard, repo/path checks), writes a
   durable delete tombstone, removes the worktree under the per-repo git lock,
   kills the workspace's tmux sessions only after worktree removal succeeded,
   deletes the branch (failure is a warning), then deletes metadata.
3. `messages.WorkspaceDeleted` → `handleWorkspaceDeleted`: settles the phase,
   drops the workspace from the active set, navigates home when it was the
   active workspace, clears its dirty marker, and reloads projects.
4. `messages.WorkspaceDeleteFailed` → `handleWorkspaceDeleteFailed`: settles
   the phase first, clears the tombstone only when the worktree still exists,
   then requeues persistence (`persistWorkspaceTabs`) — this is why the dirty
   marker is orthogonal to the lifecycle phase and survives `mutating`.

While `mutating` — for delete, shelve, or restore alike — persistence is
suppressed (`persistWorkspaceTabs` and `handlePersistDebounce` consult
`isWorkspaceMutationInFlight`), orphan GC treats the workspace's sessions as
known (`snapshotMutating`), and store mutations are guarded by
`runUnlessWorkspaceMutationInFlight`. Shelve and restore take the same guard
in `handleShelveWorkspace`/`handleRestoreWorkspace`
(`app_input_workspace_shelve.go`), with `WorkspaceOpShelve`/`WorkspaceOpRestore`
spinner labels.

## Workspace Shelve Flow

1. `messages.ShelveWorkspace` (confirm dialog or bulk driver) →
   `handleShelveWorkspace` (`app_input_workspace_shelve.go`): snapshots the
   workspace, takes the shared `mutating` guard
   (`markWorkspaceMutationInFlight`), clears the create barrier and disarms a
   pending auto-launch (same prelude as delete — an agent must not spawn into
   a worktree about to vanish), shows the "shelving" spinner
   (`WorkspaceOpShelve`), and enqueues the async `Service.ShelveWorkspace`
   cmd.
2. The service runs the repo's `archive` hook if configured (best-effort — a
   failed archive never strands the shelve), removes the worktree, and kills
   the workspace's tmux sessions. Unlike delete it keeps the branch and the
   workspace metadata, and sets no tombstone.
3. `messages.WorkspaceShelved` → `handleWorkspaceShelved`: any archive warning
   becomes a toast; `finishWorkspaceShelved` drops ID-keyed state under the
   stamped pre-removal identity set (marking the row deleted-until-reload,
   clearing activity/dirty markers, releasing the port, unwatching files),
   navigates home when it was the active workspace, removes the loaded
   project entry, and clears the spinner. The message is also fanned into
   center and sidebar-terminal `Update` so tabs whose sessions were reaped
   come down — otherwise a late `PtyTabCreateResult` could file a live
   session the orphan GC skips. Then `loadProjects` and
   `bulkShelveFinished(true)` advance the batch when one is draining.
4. `messages.WorkspaceShelveFailed` → `handleWorkspaceShelveFailed`: clears
   the guard and spinner, reloads projects, and requeues
   `persistWorkspaceTabs` — a debounced save skipped mid-flight by the guard
   must not strand the dirty marker (the same compensation the delete-failure
   path runs). `bulkShelveFinished(false)` counts the item and drains on.

## Workspace Restore Flow

1. `messages.RestoreWorkspace` (dashboard Enter on a shelved row) →
   `handleRestoreWorkspace`: snapshots the workspace and takes the shared
   `mutating` guard — a restore is a lifecycle mutation too, and without the
   guard a second Enter (or a shelve/purge landing mid-restore) would run a
   concurrent worktree add/remove against the same root. Shows the
   "restoring" spinner (`WorkspaceOpRestore`) and enqueues the async
   `Service.RestoreWorkspace` cmd.
2. The service recreates the worktree from the kept branch.
3. `messages.WorkspaceRestored` → `handleWorkspaceRestored`: releases the
   guard, clears the spinner, enqueues `runSetupAsync` (a restored worktree
   is a fresh worktree — same reasoning as create), shows a success toast,
   and reloads projects so the row re-surfaces as a normal workspace.
4. `messages.WorkspaceRestoreFailed` → `handleWorkspaceRestoreFailed`:
   releases the guard and spinner so the shelved row can be retried, runs the
   same `persistWorkspaceTabs` re-dirty compensation as shelve failure, and
   reports the error.

## Bulk Shelve Flow

1. `messages.ShowBulkShelveWorkspaceDialog` (dashboard `S` with marks) →
   `handleShowBulkShelveWorkspaceDialog` (`app_bulk_shelve.go`): converts the
   marked items to `bulkShelveTarget`s (each carries its project — marks can
   span projects — and its `MetadataID` as a mark-clearing key) and opens a
   single confirm dialog listing the names.
2. On confirm, `dialogResultBulkShelveWorkspace` → `startBulkShelve`: a
   second batch while one drains is rejected rather than queued (overwriting
   state would orphan the in-flight head). Initializes `bulkShelveState` and
   calls `advanceBulkShelve`.
3. `advanceBulkShelve` pops one target and drives it through the literal
   `handleShelveWorkspace` — worktree removals therefore never run in
   parallel, and each item gets the full guarded/spinner path. A guard
   rejection returns no cmds: the target is counted failed and skipped
   immediately rather than stalling the drain.
4. Each `WorkspaceShelved`/`WorkspaceShelveFailed` calls
   `bulkShelveFinished`: only a completion matching `headID` (the in-flight
   `MetadataID`) counts and advances — a foreign completion from a manual
   shelve interleaving belongs to its own flow and cannot corrupt the
   accounting.
5. Queue empty → `finishBulkShelve`: clears the batch members' dashboard
   marks and emits one summary toast ("Shelved N of M"). Per-row "Shelved X"
   toasts are suppressed while `bulkShelve.active()` — the batch summary
   replaces N row toasts.
