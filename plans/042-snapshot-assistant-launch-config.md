# Plan 042: Snapshot assistant settings before dispatching launch commands

> **Executor instructions:** Follow this plan in order, including real-input and race verification. Do not commit or push. Stop on the conditions below. Update this plan's status row unless the dispatcher maintains the index.
>
> **Drift check first:** `git diff --stat 7c530ee..HEAD -- internal/pty/agent.go internal/pty/agent_config_test.go internal/ui/center/model_tabs.go internal/ui/center/model_tabs_restore.go internal/ui/center/model_tabs_session_reattach.go internal/ui/center/model_tabs_session_test_helpers_test.go internal/ui/center/model_tabs_session_reattach_snapshot_error_test.go internal/ui/center/model_tabs_session_reattach_safety_test.go internal/ui/center/model_tabs_session_snapshot_race_test.go internal/ui/center/model_tabs_session_reattach_order_test.go internal/ui/center/model_tabs_session_reattach_history_size_test.go internal/ui/center/model_tabs_session_reattach_dead_test.go internal/ui/center/model_tabs_launch_config_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md`; run `git diff HEAD --` with the same paths and `git status --short`. Compare live excerpts, including working-tree changes. Only the explicitly scoped files below may be edited.

## Status

- **Priority:** P1
- **Effort:** M
- **Risk:** MED
- **Depends on:** none
- **Category:** bug / concurrency
- **Planned at:** commit `7c530ee`, 2026-09-26, including the known dirty working tree

## Why this matters

An asynchronous agent launch reads the same ordinary Go map that Settings mutates on the UI goroutine. That violates the single-writer ownership model and can cause a data race or fatal concurrent-map failure. Each dispatched operation should use the assistant settings captured when the operation was requested; a subsequent operation should see subsequently saved settings.

## Current state

`internal/pty/agent.go:109` performs the unsynchronized lookup:

```go
assistantCfg, ok := m.config.Assistants[string(agentType)]
if !ok {
    return nil, fmt.Errorf("unknown agent type: %s", agentType)
}
```

`internal/ui/center/model_tabs.go:113` returns an asynchronous command that later reaches that lookup at line 130:

```go
agent, err := m.agentManager.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, ptyRows, ptyCols, tags)
```

Settings writes the map synchronously at `internal/app/app_dialog_settings.go:142`:

```go
cfg.Command = cmd
a.config.Assistants[name] = cfg
```

Manual reattach/restart use `createAgentWithTagsFn` in `model_tabs_session_reattach.go:24`; restored placeholders call the same seam in `model_tabs_restore.go:174`. Manual reattach/restart check membership before dispatch, but the manager repeats the unsafe lookup later; placeholder restoration reaches the manager lookup without a prior capture. `AssistantConfig` contains only one string and two integers, so copying the value provides an immutable snapshot without locks.

Conventions: capture dimensions/options before `return func() tea.Msg`, as `model_tabs_restore.go:126` already does. Preserve the center pane's existing Epoch validation and live-session ownership checks. Reattach must not restart a dead session: `ptyio.SessionAttachable` requires existence and a live pane. `restoreReattachSeams` at `model_tabs_session_test_helpers_test.go:19` restores package seams with `t.Cleanup`; tests overriding these must not use `t.Parallel`.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Scoped tests | `go test ./internal/pty ./internal/ui/center -count=1` | both pass |
| Snapshot regressions | `go test ./internal/ui/center -run 'Test.*LaunchConfig' -count=1` | PASS |
| Race coverage | `go test -race ./internal/pty ./internal/ui/center -count=1` | pass, no races |
| Repository race gate | `env -u AMUX_WORKSPACES_ROOT make test-race` | exit 0, no races |
| Real tmux race gate | `env -u AMUX_WORKSPACES_ROOT make test-race-tmux` | exit 0, no races; inspect skips |
| Settings callers | `go test ./internal/app -run 'Test.*(Settings|Assistants)' -count=1` | PASS |
| Real tmux/e2e | `env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e -count=1 -v` | pass; inspect skips |
| Input gate | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | both required input tests explicitly PASS |
| UI harness smoke | `make harness-presets` | all presets exit 0 |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | exit 0 |
| Changed-code lint | `make lint-strict-new` | zero new issues, clean formatter diff |

The audit's devcheck failed real-e2e scenarios; ambient AMUX_WORKSPACES_ROOT was later confirmed to escape the test HOME and cause a workspace-name collision in the actual user root. Until plan 059 isolates that environment, every broad/e2e invocation must use the sanitizing prefix above. Verify-loop passed during the audit. Reproduce failures under isolation before attributing them; do not fix unrelated failures here.

## Scope

**In scope:** `internal/pty/agent.go`; new `internal/pty/agent_config_test.go`; `internal/ui/center/model_tabs.go`; `internal/ui/center/model_tabs_restore.go`; `internal/ui/center/model_tabs_session_reattach.go`; new `internal/ui/center/model_tabs_launch_config_test.go`; the seven existing seam test files named in the drift command (including `model_tabs_session_test_helpers_test.go`); `README.md`, `docs/CONFIG.md`, `docs/ORCHESTRATION.md`; this plan's index status.

**Out of scope:** making all Config fields concurrent, changing Settings' map representation, workspace snapshots/identity, view/run-only tabs, session naming/tags, command quoting, environment precedence, agent lifecycle cleanup, and PTY input encoding. Existing config convenience APIs used synchronously in tests may remain; no production asynchronous caller may use the live-map lookup.

## Git workflow

Use the operator checkout or an authorized isolated `advisor/042-snapshot-assistant-launch-config` branch. Record initial diffs; preserve dirty README and unrelated center/app changes. Never stash, reset, clean, commit, or push. Do not modify the audit report or other plans.

## Steps

### Step 1: Add an explicit-config spawn path

Extract the spawn portion of `CreateAgentWithTags` into `CreateAgentWithConfig`, taking the same workspace/type/session/size/tags plus an `AssistantConfig` value. That method must validate a nonnil workspace and never access `m.config.Assistants`; use the passed value for shell command construction and `Agent.Config`. Keep the existing synchronous convenience method's early workspace validation, lookup, unknown-type errors, and delegation. Document that its caller owns synchronization of the shared config; production commands will use the explicit-config path instead.

Add a PTY test using the existing isolated-server pattern from `agent_create_test.go` that passes a command/config differing from the manager's map. Assert the returned Agent.Config and launched command use the supplied snapshot. Test nil workspace without launching a process. Keep existing env/reset/registration behavior intact.

**Verify:** `go test ./internal/pty -count=1` → PASS; inspect tmux skips, and do not treat skipped real-spawn coverage as proof.

### Step 2: Capture values for all four center launch paths

For fresh create, placeholder restore, manual reattach, and restart, resolve membership and copy the `AssistantConfig` before constructing the asynchronous command. On unknown/nil configuration, preserve each path's current error/toast and reattach-guard release semantics; do not leave an acquired guard latched. Capture the lookup result/error before dispatch rather than re-reading the map in an error closure.

Extend the existing create-agent seam to accept the value and delegate to the new method, updating its listed test stubs. Route fresh creation through the same seam so it can be characterized without a real agent. Every command must close over the copied config and pass it by value. Do not substitute a pointer into the map or defer lookup until inside the closure. Leave workspace pointers, tags, and unrelated closure captures unchanged.

**Verify:** `go test ./internal/pty ./internal/ui/center -count=1` → PASS. `rg -n 'CreateAgentWithTags|createAgentWithTagsFn|CreateAgentWithConfig' internal/ui/center --glob '*.go' --glob '!**/*_test.go'` must show production spawn routes invoking the explicit-config path through the seam, with no direct asynchronous call to the lookup-based method.

### Step 3: Pin dispatch-time and future-launch behavior

Add table-driven `Test...LaunchConfig` cases covering all four paths. Retain the returned command, replace or edit the assistant entry, then execute the command through controlled tmux/spawn seams. Assert the command observes the original command/count/delay; construct a second operation and assert it sees the edited values. Cover an unknown assistant and a custom assistant added between operations.

Add a race variant: construct the command before the mutation goroutine starts, repeatedly mutate the UI-owned map while the retained command executes, and synchronize completion with channels. No lookup from the command may touch that map. Tests must not themselves concurrently call the synchronous convenience method or construct UI commands while mutating UI state. Complement the seam test with the actual explicit-config manager test so a hidden manager re-lookup cannot pass unnoticed.

**Verify:** `go test ./internal/ui/center -run 'Test.*LaunchConfig' -count=1` and `go test -race ./internal/pty ./internal/ui/center -count=1` → PASS, no races. Run the settings caller command from the table → PASS.

### Step 4: Document and run isolated real gates

Update README, CONFIG, and ORCHESTRATION: an already requested launch uses its captured assistant settings; later launches use current saved settings; attaching an existing tmux session does not rerun its pane command. Avoid promising live reconfiguration of running agents. Run both repository race gates, isolated tmux/e2e, verify-loop, harness-presets, devcheck, lint-strict-new, and `git diff --check` from the table. The broader race gates satisfy CONTRIBUTING.md:63 and :68; harness-presets satisfies :66. This changes launch ownership rather than rendering, so no performance rebaseline is authorized.

**Verify:** each gate exits 0, verify-loop names both required passing tests, and `rg -n 'captur|subsequent|later launch' README.md docs/CONFIG.md docs/ORCHESTRATION.md` finds the documented timing contract. Report any reproducible isolated failure; a failed gate blocks marking DONE.

## Test plan

Follow `agent_create_test.go` for private tmux servers and cleanup, and `model_tabs_session_test_helpers_test.go` for nonparallel seam restoration. Verify complete config values for fresh/restore/reattach/restart, later edits, custom/unknown assistants, and a race overlap after dispatch. Existing ownership, dead-session, epoch, rollback, environment, and registration tests must stay green.

## Done criteria

- [ ] Every production async agent spawn uses an immutable AssistantConfig captured before dispatch.
- [ ] Unknown/nil config paths release any acquired reattach guard and preserve existing result types.
- [ ] Four-path snapshot tests and actual manager-config tests pass under the race detector.
- [ ] Scoped/settings tests, both repository race gates, isolated real-tmux/e2e tests, verify-loop, harness-presets, devcheck, lint-strict-new, and diff-check pass.
- [ ] All three user-contract documents updated, with existing-session semantics preserved.
- [ ] Only scoped changes added; index handled as instructed.

## STOP conditions

Stop if the map is already synchronized by new code, a production caller outside the listed paths still needs async live-map access, signatures require unrelated test/API migration, the fix changes existing-session command execution, a test needs unsafe shared seam mutation, or verification fails twice after a targeted correction. Do not solve a broader Config architecture problem in this plan.

## Maintenance notes

New assistant launch entrypoints must capture settings on the event-loop owner before dispatch. A copied map pointer is not a snapshot; copy the scalar config value. If AssistantConfig later gains maps/slices/pointers, review copy depth. The convenience lookup method remains synchronous-only by contract.
