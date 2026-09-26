# Plan 037: Capture the created diff-tab index before asynchronous dispatch

> **Executor instructions:** Follow the steps and verification gates. Do not commit or push. The reviewer may retain ownership of `plans/README.md`; otherwise update only this plan's status row when complete.
>
> **Drift check first:** Run `git status --short`, `git diff --stat 7c530ee..HEAD -- internal/ui/center/model_tabs_viewer.go internal/ui/center/model_tabs_diff_dispatch_test.go`, and `git diff HEAD -- internal/ui/center/model_tabs_viewer.go internal/ui/center/model_tabs_diff_dispatch_test.go`; read untracked in-scope files too. Compare the excerpts below against the live implementation. The baseline is commit `7c530ee` plus the dirty tree audited on 2026-09-26. Preserve every unrelated modification; do not stash/clean/reset.

## Status

- **Priority:** P1
- **Effort:** S
- **Risk:** LOW
- **Depends on:** none
- **Category:** bug / concurrency
- **Planned at:** commit `7c530ee`, plus audited uncommitted working tree, 2026-09-26

## Why this matters

Creating a native diff tab returns a Bubble Tea command that later reads the active-tab map. The UI may have selected another tab by then, producing the wrong `TabCreated.Index`, and overlapping map reads/writes are unsafe in Go. The creation command should carry an immutable snapshot of the index created during Update.

## Current state

`internal/ui/center/model_tabs_viewer.go:172–178`:

```go
m.tabs.ByWorkspace[wsID] = append(m.tabs.ByWorkspace[wsID], tab)
m.setActiveTabIdxForWorkspace(wsID, len(m.tabs.ByWorkspace[wsID])-1)
m.noteTabsChanged()

return common.SafeBatch(
    dv.Init(),
    func() tea.Msg { return messages.TabCreated{Index: m.tabs.ActiveByWorkspace[wsID], Name: displayName} },
)
```

`internal/ui/center/model_tab.go:377–381` mutates that map during selection:

```go
func (m *Model) setActiveTabIdxForWorkspace(wsID string, idx int) {
    if wsID == "" {
        return
    }
    m.tabs.ActiveByWorkspace[wsID] = idx
```

The app is Go/Bubble Tea v2. Its documented single-writer convention means mutable model maps stay on Update; background commands capture values and return messages. `model_tabs.go:handlePtyTabCreated` already captures `createdIdx` before its `TabCreated` command; match this small local-value pattern. Native diff loading remains asynchronous and is separately routed by `diffResultMsg` carrying workspace/tab identity. Do not alter that routing or the shared TabSet design.

Tests in `model_tabs_diff_reuse_test.go` demonstrate constructing a model/workspace, invoking the creation path, and unpacking `tea.BatchMsg`. Use that structure without running unrelated git loads merely to inspect the creation notification.

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Baseline package | `go test ./internal/ui/center -count=1` | PASS |
| Regression | `go test ./internal/ui/center -run TestCreateDiffTabDispatch -count=1 -v` | New tests executed and PASS after fix |
| Race gate | `go test -race ./internal/ui/center -count=1` | PASS without race reports |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | Exit 0 or documented independent baseline failure |
| Changed-code lint | `make lint-strict-new` | Exit 0 |
| Hygiene | `git diff --check` | Exit 0 |

The audit observed real-e2e failures in devcheck and a passing verify-loop. Later investigation found inherited `AMUX_WORKSPACES_ROOT` defeated HOME isolation; [plan 059](059-isolate-e2e-workspace-root.md) repairs that test setup. Sanitize every e2e-reaching command until it lands. The filtered-picker failure is owned by plan 036; do not assume every remaining failure is unrelated without evidence. This map-capture change does not change input encoding/rendering and needs no performance rebaseline. Require green final gates or report BLOCKED rather than expanding scope.

## Scope

**In scope:** `internal/ui/center/model_tabs_viewer.go`; new `internal/ui/center/model_tabs_diff_dispatch_test.go`; the index status if delegated.

**Out of scope:** Assistant configuration race, TabSet synchronization, diff loading implementation, tab identity/persistence schema, viewer launch commands, PTY handling, docs/contracts (no public behavior surface is being changed), and other plans' source files.

## Git workflow

Use the operator's checkout; optional isolated branch name is `advisor/037-diff-tab-index`. Record initial status/diff before edits. Preserve all staged, unstaged, and untracked work. No stash, reset, clean, commit, push, or PR without authorization.

## Steps

### Step 1: Prove that delayed notification reports the wrong tab

Add a test creating a diff tab after a preexisting tab. Retain the returned command; move active selection to the preexisting tab before executing the creation-notification leaf command. Unpack SafeBatch's `tea.BatchMsg` and identify `messages.TabCreated` without depending on order; use a temp workspace if executing the independent loader leaf. Assert that the message retains the index of the created diff and its name. Add a second case deleting the active-index map entry before the retained notification executes; it must still report the created index. Do not modify unrelated package-wide seams or use `t.Parallel` in this package.

**Verify:** `go test ./internal/ui/center -run TestCreateDiffTabDispatch -count=1 -v` → the audited implementation fails the immutable-index assertions. If it passes, stop and reconcile code drift before editing.

### Step 2: Capture the index on Update

Immediately after appending the new tab, compute a local `createdIdx := len(m.tabs.ByWorkspace[wsID]) - 1`. Pass it to `setActiveTabIdxForWorkspace` and use it in the `TabCreated` closure. Keep `displayName`, diff initialization, visibility updates, and source-reuse behavior unchanged. Do not solve a single captured-map read by adding a mutex around all tab state.

**Verify:** `go test ./internal/ui/center -run 'TestCreateDiffTabDispatch|TestCreateDiffTab_Reuses|TestReuseDiffTab' -count=1 -v` → all PASS. `go test -race ./internal/ui/center -count=1` → PASS with no race reports.

### Step 3: Run repository gates and review the minimal patch

Run the required lint and devcheck gates. Compare against the starting dirty state, not a clean-repository assumption. Record baseline failures with exact test names; do not modify tests to mask them.

**Verify:** `make lint-strict-new` → exit 0; `env -u AMUX_WORKSPACES_ROOT make devcheck` → exit 0 or explicitly documented unrelated baseline failure; `git diff --check` → exit 0; `git diff -- internal/ui/center/model_tabs_viewer.go` → the change is limited to capturing/reusing the created index.

## Test plan

Add `TestCreateDiffTabDispatchRetainsCreatedIndexAfterSelection` and `TestCreateDiffTabDispatchRetainsCreatedIndexAfterMapRemoval`. Both should assert immutable command payload, not merely absence of panic. Follow the BatchMsg-unpacking pattern in `model_tabs_diff_reuse_test.go`. Run the entire center package under `-race`; no flaky timing loops are needed to prove this defect because delayed execution gives a deterministic behavioral failure.

## Done criteria

All required final gates must pass before this plan is marked DONE. A known or newly discovered gate failure leaves the plan BLOCKED with the exact command/test and evidence; recording a failure is not a substitute for passing. Do not expand implementation scope to repair other findings. Until [plan 059](059-isolate-e2e-workspace-root.md) lands, remove `AMUX_WORKSPACES_ROOT` from every command reaching e2e, including devcheck, verify-loop, and test-race-tmux.

- [ ] Both named delayed-command tests execute and pass.
- [ ] `go test -race ./internal/ui/center -count=1` and `make lint-strict-new` pass.
- [ ] The `TabCreated` command in `createDiffTab` no longer reads any model map.
- [ ] `env -u AMUX_WORKSPACES_ROOT make devcheck` result is recorded without masking baseline failures.
- [ ] `git diff --check` passes; only the two allowed source/test files gain implementation changes beyond the starting dirty state.
- [ ] The designated index owner records completion and gate results.

## STOP conditions

Stop if tab creation moved to a different ownership model, if the asynchronous creation message no longer exists, if reproducing/fixing requires another source file, or if a focused gate fails twice after reasonable correction. Do not bundle the separate assistant-map/configuration race into this patch.

## Maintenance notes

Future command closures must capture values before leaving Update. A Go map does not become safe because most commands execute quickly. Preserve identity-wrapped diff results; they solve a different asynchronous routing concern.
