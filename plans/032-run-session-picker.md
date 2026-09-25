# Plan 032: Add a run-session picker for concurrent `run` sessions

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/process/run_session.go internal/app/app_workspace_scripts.go internal/ui/ internal/messages/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: LOW-MED
- **Depends on**: plans/004 (suffix naming fix — the picker's session set should be read after naming is correct; technically independent, better landed after)
- **Category**: direction
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`script_mode: concurrent` mints `amux-ws-<id>-run-2`, `-3`, … sessions, but the UI reaches exactly one: `RunScriptOutput` tails `names[len(names)-1]` and `RunScriptAttachTarget` returns only the newest *alive* session. Every run after the first is unreachable in-app — no tail, no attach, no per-run exit status — without dropping to raw `tmux`, and dead sessions persist via `remain-on-exit` as unexplained litter. The primitives all exist (`Find`/`Status`/`Tail`/`Kill` on `RunSessionHost`); the missing piece is a chooser.

## Current state

- `internal/process/run_session.go:130-133` — concurrent naming.
- `:216` — `RunScriptOutput` tails last session only; `:241-258` — `RunScriptAttachTarget` newest alive only.
- `run_session.go:18-33` — `RunSessionHost` interface: `Find`/`Status`/`Tail`/`Kill` all present (read it for exact signatures).
- `internal/app/app_workspace_scripts.go:124-171` (`R` output viewer) and `:411-447` (`a` attach) — the two call sites that currently take the newest.
- `internal/tmux/detached_session.go:19-22` — `remain-on-exit on` means dead sessions keep `pane_dead_status` — the picker can show exit codes via `Status`.
- `internal/process/scripts_stop.go:21-30` — Stop-all kills every found session — the "reap all" path already exists.
- UI precedent for choosers: check `internal/ui/common` for a list/picker component and how other dialogs enumerate sessions (e.g. sidebar session attach list — `buildSidebarSessionAttachInfos` at `app_tmux_discover.go:~230` builds rows with alive state — reuse its row shape or the underlying `sidebar.SessionAttachInfo` type).

## Commands you will need

| Purpose    | Command                                     | Expected on success |
|------------|---------------------------------------------|---------------------|
| Build      | `go build ./internal/app ./internal/process ./internal/ui/...` | exit 0 |
| Unit tests | `go test ./internal/app ./internal/process -count=1` | all pass |
| Harness    | `go run ./cmd/amux-harness -dump-frame /tmp/pick.txt` with a mode that reaches the picker (check `-h`) | renders |
| Full gate  | `make devcheck`                             | exit 0              |

## Scope

**In scope**:
- A chooser overlay listing a workspace's run sessions (name, alive/exit-status, created age — whatever `Status`/`Find` returns cheaply).
- `R` (output) and `a` (attach) flows: when >1 session exists, open the chooser; 0-1 keep current fast path.
- Optional verbs in the chooser: `x`/`d` kill selected session (dead-session litter is the maintenance win) — include only if the `Kill` path exists and the overlay conventions support a destructive key with confirm.
- Tests.

**Out of scope**:
- Changing session naming (plans/004 owns it).
- A picker for non-run sessions (agent tabs have their own surface).
- Auto-reaping dead sessions — this plan exposes them; reaping policy is separate.
- `RunScriptOutputAndStatus` refactor — keep its combined-fetch optimization; the picker may call it or the host directly.

## Git workflow

- Branch: `advisor/032-run-session-picker` off `main`.
- Commit style: `feat: pick among concurrent run sessions for output/attach`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Read the surfaces

Read `RunSessionHost` interface, `runSessionsHosted`, the `R`/`a` handlers in `app_workspace_scripts.go`, and the existing overlay/dialog infrastructure (`internal/app/app_dialogs_registry.go`, `app_overlay_arbiter.go` — the chooser must arbitrate through the same overlay system; check how existing pickers — e.g. the workspace delete confirm or file picker — are built: `grep -rln 'Dialog\|Overlay' internal/ui/common`).

**Verify**: you can write the row-producing function signature: `(sessions []runSessionEntry, err error)` where each entry carries name + alive + exit status.

### Step 2: Enumerate sessions with status

Add a service method (app-side or `ScriptRunner` — pick the layer `RunScriptOutput` lives in): list `findRunSessions(ws)` → `Status` each → return rows sorted by parsed suffix (base first, then -N ascending — NOT lexical, `run-10` must follow `run-9`; parse the `-N` suffix for ordering — same helper shape as plans/004's `nextRunSuffix` parsing, share it if it landed).

**Verify**: unit test with fake host — 3 sessions (alive, dead-exit-7, dead-exit-0) → rows sorted + statuses.

### Step 3: Chooser overlay

Build the picker following the nearest existing overlay pattern. Behavior: `↑`/`↓`/`j`/`k` select, Enter opens the action (Tail for `R`, attach for `a` — or both verbs shown if the overlay supports per-row actions), Esc closes. Keep it read-mostly — kill-session verb only if the confirm pattern exists already.

**Verify**: harness dump shows the picker rendering; `go test` on its Update.

### Step 4: Wire `R`/`a`

When `len(sessions) <= 1` keep the current fast path (no picker flash for the common case); `> 1` opens the chooser targeting tail or attach per the originating key.

**Verify**: `go test ./internal/app -run 'RunScript|Picker' -v` → pass; `make devcheck` → exit 0.

## Test plan

- New: session enumeration w/ status + suffix ordering; picker navigation/selection messages; `R`/`a` with 1 vs N sessions.
- Structural pattern: existing overlay/dialog tests (`internal/app/*dialog*_test.go`) and the run-script integration tests.
- Edge: zero sessions → current behavior; sessions dying between enumerate and select → status refreshed or error surfaces.

## Done criteria

- [ ] With multiple run sessions, `R`/`a` present a chooser; each shows name + alive/exit status.
- [ ] Single-session workspaces keep the instant path.
- [ ] Suffix ordering is numeric (`run-10` after `run-9`).
- [ ] `go test ./internal/app ./internal/process -count=1` exits 0; `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The overlay/dialog system can't host a list-picker shape without a new component — report the gap rather than building a bespoke one.
- `RunSessionHost.Status` per session is a subprocess each and enumerating N sessions is expensive — batch via a `list-sessions -F` row (tmux primitives exist — `FindRunSessions` returns them) or cap enumeration; if it needs a new tmux primitive, report.
- plans/004 changed naming semantics incompatibly — re-derive the sort.
- Attach can't target arbitrary run sessions (`RunScriptAttachTarget` is newest-alive-only by contract) — extend it to take a name, or report the interface change needed.

## Maintenance notes

- Dead-session rows showing exit codes are the debug win — keep the status column accurate (`pane_dead_status` via `Status`, not guesswork).
- If per-run exit reporting ever lands as badges/toasts, the picker stays the manual inspection surface — don't merge the two.
- Reviewer focus: the picker must arbitrate through the overlay arbiter (no parallel modal paths) and keep the single-session fast path.
