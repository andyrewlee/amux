# Plan 014: Tighten two weak test assertions — unsorted `FindRunSessions` compare and msgpump floor-of-one

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/tmux/detached_session_test.go internal/app/app_msgpump_race_test.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: tests
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

Two tests assert too little: `TestFindRunSessionsScopesTagsAndNamespace` asserts positional ordering (`got[0]=="find-a" && got[1]=="find-b"`) on `FindRunSessions`' unsorted output — it passes only because tmux currently happens to return alphabetical order; a tmux/config change flakes it. And `TestExternalMsgPumpConcurrent` enqueues ~17k messages then asserts `delivered >= 1` — a pump that delivers exactly one message then wedges passes; the functional assertion can't catch ordering violations, near-total loss, or critical-queue starvation (the race-coverage value under `-race` remains, but the functional check is vestigial).

## Current state

`internal/tmux/detached_session_test.go:117`:

```go
	if len(got) != 2 || got[0] != "find-a" || got[1] != "find-b" {
		t.Fatalf("FindRunSessions() = %v, want [find-a find-b]", got)
	}
```

The package's own convention sorts first — `tmux_sessions_test.go:71,138,175` call `sort.Strings(got)` before comparing.

`internal/app/app_msgpump_race_test.go:29-52`: 8 producers × 2000 + 2 producers × 500 enqueue into `externalMsgs`(cap 256)/`externalCritical`(cap 64), then:

```go
	testutil.WaitForAtomic(t, func() int64 { return atomic.LoadInt64(&delivered) }, 1, 2*time.Second)
```

— a floor of one. The pump drops non-critical overflow by design (`tryEnqueueExternalMsg` counts drops via `perf.Count`), so "all 17k delivered" is NOT the correct assertion either — the correct tightenings are: (a) every **critical** message sent before `close` is delivered (critical queue never enqueued more than cap-64 at once? — check the test's pacing; if 1000 critical enqueues exceed cap 64 they also drop, so the assert must match the drop contract: all critical messages *that weren't dropped* — i.e. delivered ≥ some derived bound, or producers pace themselves below capacity); (b) delivered > some meaningful floor that reflects the drop-tolerant contract, OR restructure so producers send tagged sequence numbers on a drained channel and assert completeness. Read the pump's drop policy (`internal/app/app_msgpump.go:52-76`) before choosing the assertion — the test must assert the *contract* (no starvation of the critical queue while non-critical floods), not "everything arrives".

## Commands you will need

| Purpose    | Command                                        | Expected on success |
|------------|------------------------------------------------|---------------------|
| Tmux tests | `go test ./internal/tmux -run 'FindRunSessions' -count=1 -v` | pass |
| App tests  | `go test ./internal/app -run 'MsgPump|ExternalMsgPump' -count=1 -v` | pass |
| Race       | `go test ./internal/app -race -run 'MsgPump|ExternalMsgPump' -count=1` | pass |
| Full gate  | `make devcheck`                                | exit 0              |

## Scope

**In scope**:
- `internal/tmux/detached_session_test.go` — sort before asserting.
- `internal/app/app_msgpump_race_test.go` — strengthen the delivery assertion per the pump's contract.

**Out of scope**:
- `internal/app/app_msgpump.go` — the two-queue drop design is deliberate; the test must assert it, not change it.
- Other msgpump tests.

## Git workflow

- Branch: `advisor/014-tighten-test-assertions` off `main`.
- Commit style: `test: sort unsorted session assert; tighten msgpump delivery check`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Sort the `FindRunSessions` assert

Add `sort.Strings(got)` before the positional compare (import `"sort"` if absent) — matching `tmux_sessions_test.go`'s convention.

**Verify**: `go test ./internal/tmux -run 'FindRunSessions' -v` → pass (or documented tmux skip).

### Step 2: Strengthen the msgpump assertion

Read `internal/app/app_msgpump.go` drop policy first, then choose the tightest honest assertion:

- Preferred: make critical producers send N ≤ cap messages and assert **all N critical messages delivered** (critical queue must not starve while 16k non-critical flood). Keep non-critical flood for contention; assert delivered-total ≥ N (the critical count) or a derived floor — document the drop-tolerant contract in a comment.
- Alternative: tagged sequence numbers — critical messages carry `j`; collect delivered IDs into a map under the pump consumer; assert the set equals sent. More code, stronger check — pick this if it's clean in the existing test shape.

Also keep `WaitForAtomic` for the wait (it's the right primitive; the predicate is what changes).

**Verify**: `go test ./internal/app -run 'ExternalMsgPumpConcurrent' -count=1 -v` → pass; `go test ./internal/app -race -run 'ExternalMsgPumpConcurrent' -count=1` → pass.

### Step 3: Full gate

**Verify**: `make devcheck` → exit 0.

## Test plan

- The two modified tests are the deliverable. For the msgpump test, the regression signal: it must now FAIL if the critical queue starves — sanity-check by reasoning (or temporarily widening the flood) that a starved pump would trip the assertion, then revert any experiment.
- Verification: the focused `go test` runs above + `make devcheck`.

## Done criteria

- [ ] `FindRunSessions` test sorts before comparing.
- [ ] Msgpump test asserts more than floor-of-one — specifically the critical-queue non-starvation contract.
- [ ] `go test ./internal/tmux ./internal/app -count=1` exits 0 (or tmux documented skip).
- [ ] `-race` run on the msgpump test passes.
- [ ] `make devcheck` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The drop policy was redesigned (drift) — re-derive the contract before writing the assertion.
- `FindRunSessions` now sorts internally — keep the sort in the test anyway (defensive) or drop to match convention; minor either way.
- The critical-producer volume already exceeds queue capacity in the test — then "all critical delivered" is unattainable; assert the non-starvation invariant differently (e.g. all critical enqueued-before-close delivered once drained) or report.

## Maintenance notes

- The msgpump test's *real* value is `-race` channel-safety coverage; the strengthened assertion adds functional coverage — keep both comments accurate so future edits don't weaken it back.
- If `perf.Count("external_msg_drop"...)` counters were ever assertable in tests, they're the ideal non-starvation signal — check `internal/perf` for a test hook before settling on the predicate.
