# Plan 056: Give the shared fake-agent build directory a cleanup owner

> **Executor instructions:** Repair test-fixture ownership only. Run each gate. Do not commit or push. Stop on conditions below. Update this plan's index status unless maintained by the dispatcher.
>
> **Drift check first:** `git diff --stat 7c530ee..HEAD -- internal/e2e/fakeagent_test.go internal/e2e/main_test.go internal/e2e/fakeagent_cleanup_test.go`; run `git diff HEAD --` with those paths and `git status --short`, then compare the excerpts. The plan was written against the known dirty tree of 2026-09-26; preserve pre-existing edits, including any plan-059 changes to TestMain or fixture environment handling.

## Status

- **Priority:** P3
- **Effort:** S
- **Risk:** LOW
- **Depends on:** none; use isolated e2e commands until plan 059 lands
- **Category:** dx / tests
- **Planned at:** commit `7c530ee`, 2026-09-26, including the known dirty working tree

## Why this matters

Each fresh e2e test process builds one shared fake-agent binary under the system temporary directory and leaves it behind. Failed builds leave their directory as well. Ownership should match the shared amux test binary: clean failed builds immediately, and successful builds after every test, including parallel tests, has finished.

## Current state

`internal/e2e/fakeagent_test.go:32` allocates the directory:

```go
dir, err := os.MkdirTemp("", "amux-fakeagent-*")
```

The failure branch at line 40 records an error but does not clean that directory:

```go
if combined, err := cmd.CombinedOutput(); err != nil {
    fakeAgentErr = fmt.Errorf("build fakeagent: %w\n%s", err, combined)
    return
}
fakeAgentPath = out
```

`fakeAgentOnce` makes the binary process-shared; an individual test's `t.Cleanup` must not remove it while other tests still use it. `internal/e2e/main_test.go:19` is the lifecycle exemplar:

```go
code := m.Run()
if err := cleanupBuiltAmuxBinary(); err != nil {
    fmt.Fprintf(os.Stderr, "e2e binary cleanup: %v\n", err)
}
os.Exit(code)
```

Keep that exit-code policy and add the fakeagent counterpart after m.Run. `cleanupBuiltAmuxBinary` in `internal/e2e/pty.go:352` guards its directory prefix before removing it; match the ownership guard rather than introducing broad temp-directory sweeps. `TestFakeAgentRecordsRawCarriageReturn` is parallel and depends on the shared binary remaining live.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Cleanup tests | `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run 'Test.*FakeAgent.*(Build|Cleanup)' -count=1` | new tests PASS |
| Fixture tests | `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e ./internal/e2e/fakeagent -run 'Test(FakeAgent|RecordStream|ReadyBanner|OpenLogFile)' -count=1` | PASS |
| Cleanup race | `env -u AMUX_WORKSPACES_ROOT go test -race ./internal/e2e -run 'Test.*FakeAgent' -count=1` | PASS, no races |
| Real tmux race gate | `env -u AMUX_WORKSPACES_ROOT make test-race-tmux` | exit 0, no races; inspect skips |
| Input gate | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | both required raw-agent tests explicitly PASS |
| Real suite | `env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e -count=1 -v` | pass; inspect skips |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | exit 0 |
| Changed-code lint | `make lint-strict-new` | no new issues/formatter diff |

Audit devcheck failed real-e2e; inherited AMUX_WORKSPACES_ROOT was confirmed to escape temporary HOME and collide with a workspace in the user's actual root. Never run unsanitized e2e commands until plan 059 fixes isolation. Audit verify-loop passed. Do not label future failures unrelated without sanitized reproduction.

## Scope

**In scope:** `internal/e2e/fakeagent_test.go`, `internal/e2e/main_test.go`, new `internal/e2e/fakeagent_cleanup_test.go`, this plan's index status.

**Out of scope:** fakeagent recorder semantics, shared amux-binary cleanup, stale-directory janitors, build caching/flags, PTY input, e2e timing assertions, environment isolation implementation (plan 059), and product docs. No lifecycle/key/config/env/tag product contract changes are intended.

## Git workflow

Use the operator checkout or authorized isolated `advisor/056-clean-up-fakeagent-build-directory` branch. Record dirty baseline. No stash, reset, clean, commit, or push. Coordinate TestMain edits with 059 rather than replacing the file wholesale.

## Steps

### Step 1: Extract explicit build-directory ownership

Keep the sync.Once/public helper behavior, but isolate the owned build operation into a private helper accepting a destination root and an injectable build runner for tests. Production supplies os.TempDir and the current go-build command. Allocate an `amux-fakeagent-*` directory, defer removal while build success is false, and transfer ownership only after the binary build succeeds. The helper returns the binary path; it must never create or clean arbitrary caller-owned directories.

Add unit tests with a fake runner that writes a small sentinel binary, plus a runner returning an error after writing a partial file. Success leaves the owned directory for process-level cleanup; failure removes it immediately. Use per-test local helpers so tests do not reset or race the global sync.Once.

**Verify:** the Cleanup tests command runs the new build tests and they PASS. No actual go build is needed for the error-path test.

### Step 2: Clean successful ownership after all tests finish

Add `cleanupBuiltFakeAgent` and call it after `m.Run`, next to existing amux cleanup. It is a no-op when no binary was built, checks the expected directory basename prefix before removal, and is safe on an already-removed owned directory. It must not delete an arbitrary path or a directory supplied by an environment variable. Preserve the original test exit code; print any cleanup error to stderr as TestMain already does for amux.

Test empty ownership, successful cleanup, repeated cleanup, and unexpected-path refusal while preserving a sentinel in the unrelated directory. Add a subprocess test of the test binary restricted to a tiny helper test: set a private TMPDIR, build fakeagent, have the helper expose its path, allow normal TestMain exit, and assert the recorded directory is gone. Guard helper mode with a test-only env key and an exact test-name filter to prevent recursion. Register subprocess cleanup on failure. Do not add per-test cleanup of the globally shared binary.

**Verify:** the Cleanup tests command and Cleanup race command → PASS. The helper-process integration must assert the directory existed before exit and was removed after exit.

### Step 3: Verify real fixture users and broad gates under isolation

Run fixture tests, the real-tmux race gate, verify-loop, real tmux/e2e, isolated devcheck, lint-strict-new, and `git diff --check`. The direct fixture and close-loop tests share the build; they must both pass before process-level cleanup removes it. Inspect skips and preserve any isolated failure diagnostics rather than changing recorder behavior or weakening tests.

**Verify:** all commands exit 0; verify-loop names both required tests as PASS. `git diff --check` passes, and added changes are confined to the three scoped test files plus instructed index status. A failed gate blocks marking DONE.

## Test plan

Use local temporary roots and injectable build runners for ownership cases. Test partial-build failure, successful deferred ownership, no-op without a build, idempotence, unexpected path refusal, and actual cleanup after TestMain normal exit. The subprocess test is necessary to distinguish process-lifetime cleanup from an unsafe per-test cleanup. Existing raw-carriage-return tests remain the behavioral guard.

## Done criteria

- [ ] Failed fakeagent builds remove only their owned directory immediately.
- [ ] Successful shared binary remains available through all tests and is removed after m.Run.
- [ ] Cleanup refuses unrelated paths and is idempotent.
- [ ] Unit, subprocess-lifecycle, fixture/race, real-tmux race, isolated tmux/e2e, verify-loop, devcheck, lint-strict-new, and diff-check gates pass.
- [ ] No product or environment-isolation source changes were bundled; index handled as instructed.

## STOP conditions

Stop if the fixture now supports externally supplied binaries without a clear ownership flag, process-global test state would need resetting while parallel tests run, the cleanup helper would sweep unrelated temp dirs, TestMain has conflicting plan-059 edits, or verification fails twice after targeted corrections. Do not delete pre-existing leaked directories as part of implementing this plan.

## Maintenance notes

Shared fixtures belong to the test process, not the first test that requested them. Future build overrides must make ownership explicit before cleanup. Crash-time stale-directory sweeping is deliberately deferred; this plan handles ordinary exit and build failure only.
