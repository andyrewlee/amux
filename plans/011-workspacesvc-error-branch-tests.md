# Plan 011: Add direct tests for workspacesvc script-lifecycle error branches

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/app/workspacesvc/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: tests
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`workspace_service_scripts.go` carries error-mapping branches that no test exercises: `RunSetupAsync`'s error propagation into `WorkspaceSetupComplete.Err` (a regression surfaces as *silent setup success*), `ToggleScriptAsync`'s documented truthful-state invariant (a failed stop must report `Running:true` + `Err` — a regression makes the sidebar's run indicator lie), and the nil-service guards on `IsScriptRunning`/`RunScriptStatus`/`RunScriptOutput`/`ReleaseWorkspacePort`/`RunScriptAttachTarget`. The happy paths are covered transitively by app-level integration tests; the failure semantics are not — and they're exactly the branches that encode the UI's truthfulness contract.

## Current state

`internal/app/workspacesvc/workspace_service_scripts.go` (read the whole file — it's ~190 lines):

- `:20-30` `RunSetupAsync` — maps `RunSetup` error into the completion message; no `svc.RunSetupAsync` call exists in any `*_test.go` (grep-verified at audit time — re-verify).
- `:153-168` `ToggleScriptAsync` — the stop-failure invariant at `:152-153` (`Running:true` + `Err`); happy path covered via `workspace_run_script_integration_test.go:71-106`, failed stop uncovered.
- `:107-189` — `IsScriptRunning`/`RunScriptStatus`/`RunScriptOutput`/`RunScriptOutputAndStatus`/`ReleaseWorkspacePort`/`RunScriptAttachTarget` nil-guard branches; only `RunOnDoneScript` and `StopAll` have direct nil-service tests today.

Existing test patterns to follow:

- `internal/app/workspacesvc/workspace_service_delete_kill_order_test.go` — demonstrates a real `*process.ScriptRunner` fixture with `RunSetup` on a goroutine.
- `internal/app/workspacesvc/workspace_service_test.go:418-443` — `StopAll` nil-service test (the nil-guard test shape).
- `internal/app/workspacesvc/workspace_service_app_ops_test.go:92` — `RunOnDoneScript` nil-service test.
- `internal/app/workspacesvc/services.go` — `Deps`-injected seams (`GitOps`, `GitPathWaitTimeout`, `Configure`) used by the fixtures.
- `internal/testutil` — `FakeGitOps`, `FakeProjectRegistry`.

A failed-stop fixture: a workspace whose tracked script process is already dead (or a runner whose session was killed out-of-band) makes `Stop` fail while `Running` must stay true — the kill-order test's fixture shape is the model.

## Commands you will need

| Purpose    | Command                                            | Expected on success |
|------------|----------------------------------------------------|---------------------|
| Build      | `go build ./internal/app/workspacesvc`             | exit 0              |
| Unit tests | `go test ./internal/app/workspacesvc -count=1 -v`  | all pass            |
| Lint       | `make lint`                                        | exit 0              |
| Full gate  | `make devcheck`                                    | exit 0              |

## Scope

**In scope**:
- `internal/app/workspacesvc/` test files only — extend the existing file per area or add `workspace_service_scripts_test.go` if none covers this file today (`ls internal/app/workspacesvc/*_test.go`).

**Out of scope**:
- `workspace_service_scripts.go` production code — this plan only adds tests. If a test reveals the documented invariant is *wrong*, STOP and report rather than "fixing" production in a test plan.
- `TrustRepoScriptsAndRunSetupAsync` — already directly covered (`app_input_workspace_test.go:181-216`, `:150-179`); exclude it.
- `internal/process` runner internals.

## Git workflow

- Branch: `advisor/011-workspacesvc-error-tests` off `main`.
- Commit style: `test: cover workspacesvc script error branches`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: `RunSetupAsync` error-mapping test

Build a service with a `Deps.GitOps` fake whose `CreateWorkspace`/worktree setup succeeds but whose `ScriptRunner` setup fails — check what `RunSetup` needs to fail deterministically (untrusted repo scripts → `ErrScriptsNotTrusted` is the cleanest deterministic failure; `skipIfNoGit`/real-runner fixture per the kill-order test). Assert the emitted message is `messages.WorkspaceSetupComplete` with non-nil `Err` — and specifically assert it's the propagated error (`errors.Is` where a sentinel exists).

**Verify**: `go test ./internal/app/workspacesvc -run 'RunSetupAsync' -v` → pass.

### Step 2: `ToggleScriptAsync` failed-stop invariant

Fixture: workspace with a running script (real `ScriptRunner` + a `sleep`-ish command, per the integration test's shape), then break the stop path (kill the underlying session/process out-of-band, or inject a failing host — pick whichever the existing fakes support). Call `ToggleScriptAsync`, collect the message, assert: `Running == true` AND `Err != nil` — the documented truthful-state invariant.

**Verify**: `go test ./internal/app/workspacesvc -run 'ToggleScript' -v` → pass.

### Step 3: Nil-guard sweep

Table-drive the nil-service calls: construct `Service` with `scripts == nil` (however `New`/`Configure` expresses that — check `services.go`), call each of `IsScriptRunning`, `RunScriptStatus`, `RunScriptOutput`, `RunScriptOutputAndStatus`, `ReleaseWorkspacePort`, `RunScriptAttachTarget`, assert the documented zero-values (no panic, false/-1/empty as documented per method godoc).

**Verify**: `go test ./internal/app/workspacesvc -count=1` → all pass.

### Step 4: Full gate

**Verify**: `make devcheck` → exit 0.

## Test plan

- New tests: the three groups above (~6-9 cases total).
- Structural patterns: `workspace_service_test.go` nil-guard shape; `workspace_run_script_integration_test.go` real-runner fixture; `workspace_service_delete_kill_order_test.go` goroutine+poll fixture.
- Edge: `RunSetupAsync` on nil service must produce a failed/complete message, not panic (check godoc for the documented behavior, then pin it).

## Done criteria

- [ ] `RunSetupAsync` error propagation is asserted.
- [ ] `ToggleScriptAsync` failed-stop asserts `Running:true` + `Err`.
- [ ] Every nil-guard branch on the listed methods has a test.
- [ ] `go test ./internal/app/workspacesvc -count=1` exits 0; `make devcheck` exits 0.
- [ ] No production files modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The branches have been refactored away or already covered (drift).
- A deterministic failure fixture for `ToggleScriptAsync` isn't constructible without changing production code — report the seam gap.
- `RunSetupAsync`'s nil-service behavior is undocumented and panics — do not pin a panic; report the inconsistency.

## Maintenance notes

- These pin *truthfulness invariants* — future changes to `ToggleScriptAsync`/`RunSetupAsync` must preserve "reported state == real state" semantics; if semantics change, the tests must change with them, not be weakened.
- If `RunScriptHost`/runner interfaces gain new failure modes, extend the table rather than adding bespoke tests.
