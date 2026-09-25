# Plan 005: Replace fixed pacing sleeps in shared e2e helpers with pollable observables

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/e2e/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: tests
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

The shared e2e helpers inject keystrokes separated by *fixed* sleeps (`prefixSettleDelay=50ms`, `dialogInputSettle=150ms`). On a loaded CI runner the app can take longer than the sleep to process the leader byte or the navigation key — the next keystroke then lands in the wrong mode (`q` typed as plain input; Enter confirming the dialog's default "No"), and the test burns 15–30s to a timeout. This is precisely the mechanism behind the two flakes already seen in CI (`TestRunSessionStatusReportsNonzeroExit`, `TestDragSelectUpAutoScrollsWhileRepainting` on the apt lane). The constants' own comments claim "no screen observable exists" — that's stale: the prefix palette renders whenever `prefixActive` and has a golden frame, so the helpers can poll instead of sleep.

## Current state

`internal/e2e/timeouts.go:22-34`:

```go
	// prefixInterKeyDelay spaces the keys of a leader-key sequence far enough
	// apart for the app's prefix-mode state machine to observe each key.
	prefixInterKeyDelay = 15 * time.Millisecond

	// prefixSettleDelay lets the app register prefix mode after the leader
	// byte before the command key arrives. No screen observable exists for
	// "prefix mode is armed", so this stays a fixed pacing sleep.
	prefixSettleDelay = 50 * time.Millisecond

	// dialogInputSettle lets a dialog consume one navigation key before the
	// next arrives. Selection state is styling-only (not visible in
	// ScreenASCII), so there is nothing to poll.
	dialogInputSettle = 150 * time.Millisecond
```

Consumers (each transits the fixed sleeps):

- `internal/e2e/persistence_test.go:104-124` — `sendPrefixCommand`/`sendPrefixSequence` sleep `prefixSettleDelay` after the NUL leader byte, used by `quitApp`, `createAgentTab`, `createSidebarTerminalTab`, and transitively the shelve, persistence, trust, and agent tests.
- `internal/e2e/helpers_test.go:75,79`, `internal/e2e/script_trust_test.go:75`, `internal/e2e/lifecycle_shelve_test.go:176` — `dialogInputSettle` between the "h" selection key and Enter inside `deleteSelectedWorkspace`/`approveTrustDialog`/`confirmDialog`.

The observables that make polling possible:

- `internal/app/app_view_overlays.go:~111` — `if a.prefixActive { palette := a.renderPrefixPalette() ... }` renders a palette strip at the bottom of the screen. `internal/app/testdata/golden/overlay_prefix.frame` is the golden capture — open it and pick a fragment of palette text that is (a) present whenever the palette renders and (b) not present on a normal screen.
- Dialog selection state: the comment says selection is styling-only — verify against the actual render (`confirmDialog` dialogs render Yes/No with the selected option highlighted; check `internal/app/testdata/golden/` for a delete/confirm frame, or dump one with `go run ./cmd/amux-harness -mode center -frames 1 -warmup 0 -dump-frame /tmp/frame.txt` per AGENTS.md). If truly unobservable in `ScreenASCII`, use the *retry-until-observable* pattern instead of a state poll — `lifecycle_shelve_test.go:104-113` already demonstrates a bounded retry loop for dialogs.
- Existing polling helpers: `waitForUIContains`-style helpers live in `internal/e2e/helpers_test.go` (search for `waitFor` in that package) — reuse rather than inventing a new poll primitive.

Repo conventions for e2e tests: all tests call a `skipIfNoTmux`-style guard; PTY interaction helpers live in `internal/e2e/pty.go`/`helpers_test.go`; timeouts are named constants in `timeouts.go` — keep new waits consistent with that file (named constant + explanatory comment).

## Commands you will need

| Purpose      | Command                                        | Expected on success |
|--------------|------------------------------------------------|---------------------|
| Build        | `go build ./internal/e2e`                      | exit 0              |
| E2E tests    | `go test ./internal/e2e -count=1`              | all pass (or every test skips with the documented tmux-missing reason) |
| Specific     | `go test ./internal/e2e -run 'Persistence|Shelve|Trust' -count=1 -v` | pass |
| Lint         | `make lint`                                    | exit 0              |
| Full gate    | `make devcheck`                                | exit 0              |

## Scope

**In scope**:
- `internal/e2e/timeouts.go` — retire or repurpose the two sleeps.
- `internal/e2e/helpers_test.go` — the shared wait/poll helpers.
- `internal/e2e/persistence_test.go` — `sendPrefixCommand`/`sendPrefixSequence`.
- `internal/e2e/script_trust_test.go`, `internal/e2e/lifecycle_shelve_test.go` — the dialog helpers.
- Any other `internal/e2e/*_test.go` that sleeps `prefixSettleDelay`/`dialogInputSettle` (`grep -rn 'prefixSettleDelay\|dialogInputSettle' internal/e2e` for the full list).

**Out of scope**:
- `internal/app/app_view_overlays.go` or any production code — observables already exist; do not add instrumentation for the tests' benefit.
- Other deadline-based polls in `internal/tmux`, `internal/app`, `internal/app/workspacesvc` tests — covered by plans/010.
- The two already-known flaky tests' own timeouts (`waitForSessionStatus`, drag-scroll) — the helpers they depend on are the point of this plan; a separate mechanical sweep of remaining sites is plans/010.
- `prefixInterKeyDelay` (15ms between keys *within* a sequence) — a keystroke-pacing concern, not a mode-settle concern; leave it (real terminals pace bytes anyway).

## Git workflow

- Branch: `advisor/005-e2e-pacing-observables` off `main`.
- Commit style: `test: replace fixed e2e pacing sleeps with observable waits`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add `waitForPrefixPalette` and confirm the palette fragment

1. Read `internal/app/testdata/golden/overlay_prefix.frame` and `a.renderPrefixPalette` (`internal/app/app_view_overlays.go` or wherever it renders) to choose a stable ASCII fragment — e.g. a header/label in the palette that no normal screen produces.
2. In `internal/e2e/helpers_test.go`, add `waitForPrefixPalette(t, inst)` using the existing `waitForUIContains`-style poll (bounded by the enclosing gesture's timeout — e.g. `workspaceAgentTimeout`-scale or a new `prefixArmTimeout = 5s`), failing with the current screen dump like the other waiters do.

**Verify**: `go build ./internal/e2e` → exit 0.

### Step 2: Route the prefix helpers through the observable wait

In `sendPrefixCommand`/`sendPrefixSequence` (`internal/e2e/persistence_test.go:104-124`), replace `time.Sleep(prefixSettleDelay)` with `waitForPrefixPalette` *after* sending the leader byte. Keep `prefixInterKeyDelay` between subsequent keys. If a caller sends a sequence where the palette is *dismissed* by the second key before polling (check `sendPrefixSequence` semantics — a multi-key sequence may open+close the palette between polls), poll only after the leader byte or use a latch: `paletteSeen` via a short poll window inside the sequence.

**Verify**: `go test ./internal/e2e -run 'Persistence|Quit' -count=1 -v` → pass (or documented skip without tmux).

### Step 3: Route the dialog helpers through a bounded observable/retry

For `deleteSelectedWorkspace`, `approveTrustDialog`, `confirmDialog`:

- First check whether the selected option renders detectably in `ScreenASCII` (dump a frame: `go run ./cmd/amux-harness -mode center -frames 1 -warmup 0 -dump-frame /tmp/frame.txt` or inspect golden dialog frames). If it does → poll for it.
- If it doesn't → replace the single fixed sleep with the bounded-retry idiom already used at `internal/e2e/lifecycle_shelve_test.go:104-113` (send key, observe expected post-state, retry the keystroke on timeout up to N times).

**Verify**: `go test ./internal/e2e -run 'Shelve|Trust|Delete' -count=1 -v` → pass.

### Step 4: Retire or repurpose the constants

If no callers remain, delete `prefixSettleDelay`/`dialogInputSettle` from `timeouts.go`. If a residual caller legitimately needs a fixed pace (e.g. pure byte-arrival pacing with no observable), keep the constant with a corrected comment naming the residual use — do not keep the stale "no observable exists" claim.

**Verify**: `grep -rn 'prefixSettleDelay\|dialogInputSettle' internal/e2e` → only justified residual hits or none. `make devcheck` → exit 0.

### Step 5: Soak the changed paths

**Verify**: `go test ./internal/e2e -count=2` → two consecutive green runs (the flake class being fixed is load-sensitive; a doubled run is cheap signal).

## Test plan

- This plan IS test changes. The "new tests" are the new wait helpers' failure modes — they must dump the screen on timeout like sibling waiters.
- Confirm no helper now *succeeds* by accident: temporarily slow the app under test? No — do not modify production; rely on the two consecutive green runs + code review.
- Verification: `go test ./internal/e2e -count=2` → green twice.

## Done criteria

- [ ] No `time.Sleep` remains on the leader-byte→command-key path or the dialog selection→Enter path (polling or bounded retry instead).
- [ ] `prefixSettleDelay`/`dialogInputSettle` deleted or re-documented for a justified residual use.
- [ ] `go test ./internal/e2e -count=2` exits 0 twice consecutively (or documented tmux-missing skips).
- [ ] `make devcheck` exits 0.
- [ ] No production files modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The prefix palette does NOT render in the test PTY's captured screen (e.g. palette renders only above a terminal-size threshold) — pick a different observable (e.g. the workspace-tab state the palette's command produces) or report.
- A helper's post-key observable is ambiguous across themes/dialog types — narrow the fragment to ASCII structure, not theme text.
- The bounded-retry idiom at `lifecycle_shelve_test.go:104-113` no longer exists — find the current dialog-retry helper (`grep -rn 'retry\|attempt' internal/e2e`) or write the loop inline.
- Any *production* code change appears necessary — this plan is test-only; report instead.

## Maintenance notes

- New prefix commands / dialog flows added to e2e tests must reuse `waitForPrefixPalette`/the dialog-retry helper — never reintroduce fixed sleeps; the pattern is now "send → observe → proceed".
- If the palette's rendered text changes, `waitForPrefixPalette`'s fragment must be updated in step with it — the fragment choice is the fragile point; a reviewer should check it's theme-stable.
- The apt CI lane (`tmux-e2e (apt)` in `.github/workflows/ci.yml`) is where these flakes manifest; after merge, watch for the two historical flakes (`TestRunSessionStatusReportsNonzeroExit`, `TestDragSelectUpAutoScrollsWhileRepainting`) — they should stop recurring.
