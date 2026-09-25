# Plan 026: Establish and apply a Warn-vs-Error log-level convention

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/logging/logger.go LINTING.md AGENTS.md internal/app/workspacesvc/workspace_service_shelve.go internal/app/app_tmux_gc.go internal/app/app_persistence.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: dx
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`AMUX_LOG_LEVEL=error` is documented (README:292) as a verbosity knob, but nearly every *real operation failure* logs at `logging.Warn` — shelve failures, orphan-GC kill failures, shutdown persist failures — while `logging.Error` is used mostly for panics and init failures (22 Error vs 162 Warn call sites). A user setting `error` to quiet the log sees only panics: the failures they'd most want to see vanish. The convention is unwritten anywhere — first decide it, then re-level the clearest offenders.

## Current state

- `internal/logging/logger.go` — `Debug`/`Info`/`Warn`/`Error` levels; `log()` writes under mutex. Adjacent issues found at audit: `:209` silently drops writer errors (`_, _ =`), and `:69` uses `slog.Debug` (stderr) instead of the file logger, so a log-prune failure never lands in the log.
- Example real-failure-at-Warn sites: `internal/app/workspacesvc/workspace_service_shelve.go:47` ("workspace shelve failed"), `internal/app/app_tmux_gc.go:206` ("orphan GC: failed to kill session"), `internal/app/app_persistence.go:36` ("Failed to persist workspace on shutdown").
- `logging.Error` sites: `internal/app/app_input.go:26`, `app_view.go`, `safecmd.go` — mostly panic/init paths.
- Full Warn census: `grep -rn 'logging\.Warn' internal/ --include='*.go' | wc -l` (~162 at audit); the re-leveling must be a judgment pass, not a bulk sed.

The convention decision this plan must encode (write it into LINTING.md — the documented lint/policy home — or AGENTS.md; pick whichever already carries conventions):

- **Error** = a user-visible operation failed and the app continued (the user should know: shelve failed, kill failed, persist failed, copy failed).
- **Warn** = degraded/retrying/self-healing or informational-urgent (subprocess kill during revalidation, backlog valve, recoverable fallbacks).
- Debug/Info unchanged.

Note the honest ambiguity: some Warns are self-healing (GC retry loops) and correctly Warn — the re-level is "real failures the user asked for that failed", not "anything non-nil".

## Commands you will need

| Purpose    | Command                                      | Expected on success |
|------------|----------------------------------------------|---------------------|
| Build      | `go build ./...`                             | exit 0              |
| Unit tests | `go test ./internal/... -count=1` (touched pkgs) | all pass          |
| Lint       | `make lint && make lint-strict-new`          | exit 0              |
| Full gate  | `make devcheck`                              | exit 0              |

## Scope

**In scope**:
- `LINTING.md` (or AGENTS.md) — the convention text.
- The specific Warn→Error re-levels the convention selects — expected handful (the three cited + grepped siblings), NOT a mass rewrite.
- `internal/logging/logger.go:209,69` — optional small fixes if in-scope-adjacent and trivially safe (writer error → best-effort stderr note; prune `slog.Debug` → file-log or drop). Fold only if <20 lines total.

**Out of scope**:
- A level taxonomy bigger than the four existing levels — no new levels, no structured-logger rewrite.
- plans/024's mutex fast-path — orthogonal.
- Re-leveling Debug/Info sites.

## Git workflow

- Branch: `advisor/026-log-level-convention` off `main`.
- Commit style: `chore: define and apply warn/error log-level convention`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Write the convention

Add the two-bullet convention (Error vs Warn above) to `LINTING.md` under a new short section or AGENTS.md — pick the file that already states conventions (LINTING.md is lint policy; AGENTS.md is contributor workflow — put it where "how to call the logger" guidance would live; check both files first).

**Verify**: doc reads as two sentences + one example each — no essay.

### Step 2: Census and re-level the real failures

`grep -rn 'logging\.Warn' internal/ --include='*.go' | grep -iv 'retry\|fallback\|degrad\|skipping\|could not read\|ignoring'` — from that list, re-level the sites where a user-requested operation definitively failed (shelve/kill/persist/delete/copy/report failures). Each change is one word — but each site must be read: if the call is inside a retry/self-heal path (GC sweep continuing, watcher resync), it stays Warn. Target: the clearest ~10-20 sites, conservative.

**Verify**: `go build ./...` → exit 0; `go test` on touched packages → pass.

### Step 3: Optional logger fixes (only if trivial)

`logger.go:209` — writer error: append a `fmt.Fprintf(os.Stderr, ...)` best-effort note or leave; `logger.go:69` — route the prune failure through `Warn` on the file logger (the logger exists by then? check init order — if it fires before init, stderr is correct and just needs the comment updated). Fold only if each is <10 lines and doesn't change behavior beyond the intended note.

**Verify**: `go test ./internal/logging -count=1` → pass.

### Step 4: Gates

**Verify**: `make devcheck` → exit 0; `make lint-strict-new` → exit 0.

## Test plan

- No new tests for level changes (log lines aren't behavior). If a logger test asserts levels, keep it honest.
- Verification: `make devcheck` + eyeball `AMUX_LOG_LEVEL=error` behavior change (manual — the point of the fix).

## Done criteria

- [ ] Warn-vs-Error convention documented in the conventions file.
- [ ] Real-operation failures (shelve/kill/persist class) log at Error; self-healing paths stay Warn.
- [ ] `grep` shows the re-leveled set is deliberate, not bulk (diff review).
- [ ] `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The "Error = user-visible failure" convention conflicts with an existing documented convention you find in the repo — align to the documented one instead.
- A site marked Warn is inside a path that *retries and self-heals on the next tick* — it stays Warn; don't mass-edit.
- Re-leveling turns out to be >40 sites — the convention is producing noise, not signal; reduce to the worst offenders and report the proportion.

## Maintenance notes

- New `logging.Warn` calls should be checked against the convention at review time — that's why the doc exists; the grep above is the audit query to reuse.
- `AMUX_LOG_LEVEL=error` is now meaningful; if users complain error is too loud, the convention's boundary (user-initiated op vs background self-heal) is what to re-check, not the levels.
