# Plan 009: Log stack traces in workspacesvc recover and add message/workspace context to app panic logs

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/app/workspacesvc/workspace_service.go internal/app/app_input.go internal/app/app_view.go internal/app/app_lifecycle_cmd.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: dx
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`CreateWorkspace`'s recover is the only panic handler in the codebase that logs the panic value with **no stack trace** — a panic inside worktree/git/metadata code logs `panic in createWorkspace: runtime error...` and nothing else, unactionable. Separately, the `app.Update`/`app.View` panic logs include the stack but not *which message* triggered it or which workspace was active — for panics in shared handlers (~150 message types flow through `a.update`), the stack alone can't disambiguate the trigger. These logs are the primary debugging surface (README documents "debug is the first thing to try"); making them complete is cheap insurance against the next log-only diagnosis.

## Current state

The outlier — `internal/app/workspacesvc/workspace_service.go:83-89`:

```go
	defer func() {
		if r := recover(); r != nil {
			logging.Error("panic in createWorkspace: %v", r)
			msg = messages.WorkspaceCreateFailed{...}
		}
	}()
```

The package does not import `runtime/debug`.

The convention it should match — every other recover site logs `debug.Stack()`:

- `internal/app/app_input.go:26` — `logging.Error("panic in app.Update: %v\n%s", r, debug.Stack())`
- `internal/app/app_view.go:26`, `internal/app/app_lifecycle_cmd.go:43` — same shape
- `internal/ui/common/safecmd.go:22,56`, `internal/supervisor/supervisor.go:284`, `internal/safego/safego.go:35` — same convention

And the missing-context sites (they have stacks but no trigger info):

- `internal/app/app_input.go:25-31` — `app.Update` recover; `msg` (the `tea.Msg`) and `a.activeWorkspace` are in scope.
- `internal/app/app_view.go:25-33` — `app.View` recover; no message exists on this path, but `a.activeWorkspace`/focused pane does — add what's meaningful, skip what isn't.

## Commands you will need

| Purpose    | Command                                        | Expected on success |
|------------|------------------------------------------------|---------------------|
| Build      | `go build ./internal/app ./internal/app/workspacesvc` | exit 0         |
| Unit tests | `go test ./internal/app ./internal/app/workspacesvc -count=1` | all pass |
| Lint       | `make lint`                                    | exit 0              |
| Full gate  | `make devcheck`                                | exit 0              |

## Scope

**In scope**:
- `internal/app/workspacesvc/workspace_service.go` — add `debug.Stack()` to the recover log.
- `internal/app/app_input.go` — add message type + workspace context to the Update panic log.
- `internal/app/app_view.go` — add workspace/pane context to the View panic log.

**Out of scope**:
- Any other recover site — they already log stacks; do not "standardize" their message text.
- `messages.WorkspaceCreateFailed` construction — unchanged.
- A shared panic-log helper — three sites is below the threshold for a new abstraction; keep the edit local. (If a reviewer asks, that's a follow-up, not this plan.)
- plans/026's log-level convention — different concern.

## Git workflow

- Branch: `advisor/009-panic-log-context` off `main`.
- Commit style: `chore: add stack + message context to panic logs`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: workspacesvc stack trace

In `internal/app/workspacesvc/workspace_service.go`, add `"runtime/debug"` to imports and change the log line to:

```go
logging.Error("panic in createWorkspace: %v\n%s", r, debug.Stack())
```

**Verify**: `go build ./internal/app/workspacesvc` → exit 0.

### Step 2: app.Update context

In `internal/app/app_input.go`, extend the recover log to include the message's concrete type and the active workspace. Find how the workspace ID is exposed — look for `a.activeWorkspace` and an ID/string accessor used in nearby logs (`grep -n 'activeWorkspace' internal/app/app_input.go internal/app/app_view.go | head`). Shape:

```go
logging.Error("panic in app.Update (msg=%T, workspace=%s): %v\n%s", msg, wsContext, r, debug.Stack())
```

where `wsContext` is the active workspace's ID/name or `"none"` — use whatever accessor the file already uses for workspace identity in logs; do NOT call methods that do filesystem work (e.g. `ComputedID()` touches disk — use `ws.ID()`/`ws.Name`, check which the neighbors use).

**Verify**: `go build ./internal/app` → exit 0.

### Step 3: app.View context

Same treatment in `internal/app/app_view.go` — there is no `msg` on this path; add `workspace=%s` and, if trivially available without method calls, the focused pane (`a.focusedPane` or equivalent field — check the file's existing field names).

**Verify**: `go build ./internal/app` → exit 0; `go test ./internal/app ./internal/app/workspacesvc -count=1` → all pass.

### Step 4: Full gate

**Verify**: `make devcheck` → exit 0.

## Test plan

- No new tests required — log-only change. If a panic-recovery test exists for `CreateWorkspace` (`grep -rn 'panic\|recover' internal/app/workspacesvc --include='*_test.go'`), extend it to assert the log contains a stack marker — only if a log-capture seam already exists; do not build one for this.
- Verification: `go test ./internal/app ./internal/app/workspacesvc -count=1` → all pass.

## Done criteria

- [ ] `createWorkspace` panic log includes `debug.Stack()`.
- [ ] `app.Update` panic log includes `msg=%T` and workspace context.
- [ ] `app.View` panic log includes workspace context (and focused pane if cheaply available).
- [ ] `go test ./internal/app ./internal/app/workspacesvc -count=1` exits 0; `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- A shared panic-logging helper was introduced since planning (drift) — use it instead of hand-editing.
- `logging.Error` signature changed — adapt.
- The active-workspace accessor requires filesystem access — pick a different identity accessor or skip the field.

## Maintenance notes

- Future recover sites must include `debug.Stack()` — the convention is now uniform; a reviewer can grep `recover()` and check each has it.
- If `msg` values ever carry secrets (none do today — messages are typed structs, env values live in stores), the `%T` type-only format stays safe by construction — never log `%v`/`%+v` of the message payload.
