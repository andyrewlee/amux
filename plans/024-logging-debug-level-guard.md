# Plan 024: Guard per-keystroke `logging.Debug` calls behind a cheap level check

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/logging/logger.go internal/ui/center/model_input_keys.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: perf
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`sendKeyToTerminal` calls `logging.Debug` up to three times per keypress, and `log()` takes `defaultLogger.mu` *before* checking the level filter — so every filtered-out debug call still serializes on the global mutex that wraps the file write. Filtered logs still pay the lock; worse, any goroutine blocked on a slow file write stalls keystroke handling on the UI goroutine. Low probability, real mechanism — a fast-path level check is a few lines.

## Current state

`internal/logging/logger.go:198-210` (`log`):

```go
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()

	if !defaultLogger.enabled || level < defaultLogger.level {
		return
	}

	timestamp := ...
	msg := fmt.Sprintf(format, args...)
	...
	_, _ = defaultLogger.writer.Write([]byte(line))
```

Call sites in the hot path: `internal/ui/center/model_input_keys.go:19` (entry log), `:207`, `:210` (`sendKeyToTerminal` — fires per keypress).

The level is set once at init (`SetLevel`/init path — `grep -n 'level' internal/logging/logger.go | head -20` to confirm whether level can change at runtime; if it can, the fast-path check must still be safe — a plain bool/atomic read races benignly since a stale check only costs or skips a debug line).

## Commands you will need

| Purpose    | Command                                       | Expected on success |
|------------|-----------------------------------------------|---------------------|
| Build      | `go build ./internal/logging ./internal/ui/center` | exit 0           |
| Unit tests | `go test ./internal/logging ./internal/ui/center -count=1` | all pass |
| Lint       | `make lint`                                   | exit 0              |
| Full gate  | `make devcheck`                               | exit 0              |

## Scope

**In scope**:
- `internal/logging/logger.go` — the level fast-path (preferred fix point: fix once for all callers).
- `internal/ui/center/model_input_keys.go` — only if the logger-level fix can't apply (fallback: guard call sites).

**Out of scope**:
- Changing log levels, format, or the mutex discipline on the write itself — the fix is only about where the level check sits.
- Other Debug call sites — if the fix lands in `log()` it covers them all; do not sprinkle guards repo-wide.
- plans/026's level-convention decision.

## Git workflow

- Branch: `advisor/024-logging-debug-guard` off `main`.
- Commit style: `perf: check log level before taking logger mutex`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Determine the level's mutability

`grep -n 'SetLevel\|level =' internal/logging/logger.go` — if level is set once and never mutated, a plain early check is trivially safe. If mutable, make it an `atomic.Int32`/`atomic.Bool` enabled+level read (check what Go version/`sync/atomic` typed atomics are in use — Go 1.26 has them; grep the file for existing atomic use).

**Verify**: you've confirmed which shape is safe.

### Step 2: Add the fast path

Preferred — inside `log()` before the mutex:

```go
	if !defaultLogger.levelEnabled(level) {   // atomic or immutable read
		return
	}
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	...existing body (keep the in-lock check too — belt and suspenders against a raced level change)...
```

`levelEnabled` returns `enabled && level >= current`. Keep the post-lock check — it's still the authoritative gate; the fast path only skips the lock acquisition.

Fallback if `log()` shape makes this awkward: export `DebugEnabled() bool` and guard `sendKeyToTerminal`'s call sites — less complete but zero-risk; prefer the log() fix.

**Verify**: `go build ./internal/logging` → exit 0.

### Step 3: Tests

`internal/logging/logger_test.go` (or wherever logger tests live — `ls internal/logging/*_test.go`): assert (a) filtered call doesn't write (existing behavior, likely covered), (b) a `DebugEnabled`/`levelEnabled` check returns correct values at set levels, (c) if atomics introduced — a `SetLevel` flip mid-test toggles the fast path. Don't contrive a mutex-contention test; the mechanism is the fix, not an observable.

**Verify**: `go test ./internal/logging -count=1 -v` → pass.

### Step 4: Gates

**Verify**: `go test ./internal/logging ./internal/ui/center -count=1` → pass; `make devcheck` → exit 0.

## Test plan

- Level-gate unit tests (Step 3); existing logger tests must pass unchanged.
- Verification: focused `go test` + `make devcheck`.

## Done criteria

- [ ] Filtered log calls return before acquiring `defaultLogger.mu`.
- [ ] The write path under the mutex is unchanged; post-lock check retained.
- [ ] `go test ./internal/logging ./internal/ui/center -count=1` exits 0; `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The logger was restructured (no `defaultLogger` singleton / mutex shape changed) — adapt to the new shape or report.
- Level is mutable through a channel/API that requires the mutex for correctness — then the fast path needs the atomic approach; if atomics are rejected by the codebase's lint config, use the `DebugEnabled` call-site fallback.
- `sendKeyToTerminal` no longer logs per key — the finding may be moot; still worth the fast-path since other Debug sites exist, but re-verify necessity.

## Maintenance notes

- Any NEW logging call in a per-keystroke/per-frame path gets the fast path for free once it lives in `log()` — that was the point of fixing it there.
- If a reviewer asks for a benchmark: `go test -bench` on `log()` filtered-vs-unfiltered is easy to add but not required.
- plans/026 (level convention) is orthogonal — it decides *which* level each message uses; this plan only makes filtered calls cheap.
