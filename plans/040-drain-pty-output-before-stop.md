# Plan 040: Deliver every queued PTY byte before Stopped

> **Executor instructions:** Follow the steps and verification gates. This is an implementation handoff, not permission to commit or push. Stop on the conditions below. Update only this plan's status row in `plans/README.md` when complete, unless the dispatcher owns the index.
>
> **Drift check first:** Run `git diff --stat 7c530ee..HEAD -- internal/ui/ptyio/pty_reader.go internal/ui/ptyio/pty_reader_test.go internal/ui/ptyio/pty_reader_order_test.go` and `git diff HEAD -- internal/ui/ptyio/pty_reader.go internal/ui/ptyio/pty_reader_test.go internal/ui/ptyio/pty_reader_order_test.go`. Compare the excerpts below to the live files, including untracked files reported by `git status --short`. This plan includes the known dirty working tree of 2026-09-26; HEAD alone is not the baseline. Preserve all pre-existing edits.

## Status

- **Priority:** P1
- **Effort:** M
- **Risk:** MED
- **Depends on:** none
- **Category:** bug / tests
- **Planned at:** commit `7c530ee`, 2026-09-26, including the existing uncommitted working tree

## Why this matters

The shared terminal reader can report termination while earlier output is still queued. It also drops bytes returned in the same Read call as an error. Final diagnostics can consequently disappear from either the center agent tab or sidebar terminal. The repaired contract is all accepted bytes in order, followed by exactly one Stopped event, except when explicit cancellation abandons the stream.

## Current state

`internal/ui/ptyio/pty_reader.go` owns the inner blocking reader and outer frame-coalescing loop. At line 87 it tests the error before using the bytes:

```go
n, err := r.Read(buf)
if err != nil {
    if isReadTimeout(err) {
        continue
    }
```

The inner loop sends the error separately on `errCh` and closes buffered `dataCh`. The outer receive at line 144 sets `stoppedErr`; lines 189 and 204 can then terminate without waiting for the remaining data channel entries:

```go
if stoppedErr != nil && len(pending) == 0 {
    SendPTYMsg(msgCh, cancel, factory.Stopped(stoppedErr))
    return
}
```

`internal/ui/ptyio/pty_reader_test.go:198` already produces 64 chunks but checks only the error identity. Its `runReaderAndForward` helper collects outputs, verifies both goroutines finish, and provides `outputBytes()`; reuse that style. Tests that exercise coalescing must compare complete byte sequences, not the number of chunks. `RunPTYReader` is the sole owner of `msgCh`, and its deferred close must remain exactly once. Keep `safego.Go`, deadline polling, bounded queue sizes, idle heartbeat, and ownership transfer of byte slices. This is shared I/O plumbing; do not move the fix into either pane.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Focused tests | `go test ./internal/ui/ptyio -run 'TestRunPTYReader' -count=1` | PASS |
| Ordering stress/race | `go test -race ./internal/ui/ptyio -run 'TestRunPTYReader' -count=50` | PASS, no races |
| Consumer tests | `go test ./internal/ui/ptyio ./internal/ui/center ./internal/ui/sidebar` | all packages pass |
| Repository race gate | `env -u AMUX_WORKSPACES_ROOT make test-race` | exit 0, no races |
| Real tmux race gate | `env -u AMUX_WORKSPACES_ROOT make test-race-tmux` | exit 0, no races; inspect skips |
| Real tmux/input | `env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e -count=1 -v` | pass; inspect all skips |
| Real input gate | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | both required raw-agent tests explicitly PASS |
| UI harness smoke | `make harness-presets` | all presets exit 0 |
| PTY ingest soak | `env -u AMUX_WORKSPACES_ROOT AMUX_SOAK_DURATION=5m make soak` | TestSoakHarnessPTY passes; record the 5m duration |
| Main checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | exit 0 |
| Changed-code lint | `make lint-strict-new` | zero issues, clean formatter diff |

Audit verify-loop passed; devcheck failed in real-e2e scenarios. Ambient AMUX_WORKSPACES_ROOT was confirmed to escape the temporary HOME and collide with a workspace in the actual user root. Until plan 059 isolates the environment, sanitize every broad/e2e invocation as above. Reproduce any failure under isolation before attributing it; failures remain blocking. CONTRIBUTING.md:63–69 requires repository race coverage and a pre-landing soak for this PTY-ingest change. This does not change the render algorithm, so no performance rebaseline or harness source change is authorized.

## Scope

**In scope:** `internal/ui/ptyio/pty_reader.go`, `internal/ui/ptyio/pty_reader_test.go`, new `internal/ui/ptyio/pty_reader_order_test.go`, and this plan's index status only.

**Out of scope:** terminal parser/rendering, flush policy in `flush.go`, buffer tuning constants, reader restart/backoff, attachment generations, tmux commands, input encoding, and public lifecycle/key/config/env/tag surfaces. No user-facing contract change is intended, so README/config/orchestration documentation does not need new behavior claims.

## Git workflow

Use an operator-provided checkout/branch, or an isolated branch named `advisor/040-drain-pty-output-before-stop` if authorized. Record initial status/diffs. Never stash, reset, clean, commit, or push to work around the dirty tree. No source change outside Scope is allowed.

## Steps

### Step 1: Add regression cases for partial reads and EOF ordering

Add a reader returning `(len(payload), io.EOF)` in one call and another returning data plus a sentinel non-timeout error. Assert exact bytes, one Stopped event, and the original error. Add multiple uniquely numbered chunks that exceed a small MaxPendingBytes, with a queue large enough to contain the tail when the producer reaches EOF. Use channels to pause the first outgoing flush while the producer fills the queue; release it after the terminal read is observed. Cover EOF and EIO, size-triggered flushing, and an elapsed flush tick with a backpressured output sink. Do not rely on arbitrary sleeps to establish ordering; repeated schedules supplement, rather than replace, exact-byte assertions.

**Verify:** `go test ./internal/ui/ptyio -run 'TestRunPTYReader' -count=1` must fail on the new same-call data/error regression before the fix. Existing failures unrelated to that regression are a STOP condition for this step.

### Step 2: Put data and terminal errors on one ordered bounded stream

Replace the independent data/error channels with one channel of private read events containing a byte slice and an error. Keep its capacity equal to `cfg.ReadQueueSize`. The inner goroutine copies and publishes `n > 0` bytes even when an error accompanies them; a terminal event may carry both data and error. Treat timeout errors as retry signals after forwarding any accompanying data. Publication must select on cancellation. Defer closing this one producer channel and clearing the read deadline as today.

The outer loop appends event data before handling its error. Because the event channel is FIFO and a terminal event is last, flush pending output and send Stopped exactly once when that event arrives. A closed channel without a terminal event uses EOF after flushing remaining pending output; this preserves panic-unwind behavior. Timer/size flushes alone must never imply termination. Cancellation continues to return without sending Stopped and may discard pending bytes, matching existing tests. Keep all external function signatures and output-merger behavior unchanged.

**Verify:** `go test ./internal/ui/ptyio -run 'TestRunPTYReader' -count=1` → all pass. `go test -race ./internal/ui/ptyio -run 'TestRunPTYReader' -count=50` → all pass without races or hangs.

### Step 3: Verify shared consumers and actual input

Run every command in the table, including both repository race gates, harness smoke, and the 5m soak. Keep the environment-sanitizing prefix on every broad/e2e command. Inspect skip output; skipped real input coverage is not success. Check `git diff --check` and compare the final changed-file list with the initial dirty-tree inventory. Use the repository's gofumpt formatting conventions; do not format unrelated pre-existing edits.

**Verify:** all listed commands exit 0, the two required verify-loop tests explicitly PASS, and the 5m soak passes. If an isolated gate fails, stop finalization and report the exact tests and diagnostics; do not claim the full gate passed or mark this plan DONE.

## Test plan

Use `runReaderAndForward`, `testOutputMsg`, and `testStoppedMsg` from `pty_reader_test.go`. New cases cover bytes+EOF, bytes+EIO, data+timeout followed by more data, queued EOF at a size boundary, timer flush/backpressure, empty EOF, and cancellation. Preserve existing exactly-once close, heartbeat/deadline, and error-identity tests. Do not require a specific output chunk count; require concatenated bytes and terminal-event ordering.

## Done criteria

- [ ] Focused tests and the 50-run race command pass.
- [ ] Data/error regression cases compare all bytes and assert one final Stopped.
- [ ] Consumer tests, isolated real tmux/e2e tests, and `env -u AMUX_WORKSPACES_ROOT make verify-loop` pass without vacuous skips.
- [ ] Both repository race gates, harness smoke, and the 5m soak pass; soak duration is recorded.
- [ ] `env -u AMUX_WORKSPACES_ROOT make devcheck`, `make lint-strict-new`, and `git diff --check` pass.
- [ ] Only scoped source/test files were changed beyond the initial dirty tree; index updated only as instructed.

## STOP conditions

Stop if the live excerpts differ substantively, the reader now has multiple producers, a fix requires changing pane message semantics or tuning, a new test needs production sleeps, a verification fails twice after a reasonable targeted correction, or a required real-tmux/soak gate cannot run. Report isolated gate failures separately; do not expand scope or mark DONE with a failed gate.

## Maintenance notes

Future buffering changes must preserve the ordering of bytes and terminal events. A buffered channel being closed does not mean it is drained. Readers may legally return data and an error together; review timeout handling with the same rule. Keep cancellation as the one intentional output-abandonment path.
