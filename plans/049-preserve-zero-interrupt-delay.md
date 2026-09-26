# Plan 049: Preserve an explicit zero assistant interrupt delay across saves

> **Executor instructions:** Implement only serialization fidelity and its tests/docs. Run every gate; stop on conditions below. Do not commit or push. Update this plan's index status unless the dispatcher owns it.
>
> **Drift check first:** `git diff --stat 7c530ee..HEAD -- internal/config/config.go internal/config/assistants_save_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md`; run `git diff HEAD --` with the same path list and `git status --short`, then compare the excerpts. The plan was written against the dirty working tree of 2026-09-26. Changes from dependency 043 to the save-read helper are expected; verify that helper's preservation tests pass and retain it. Other semantic drift is a STOP condition.

## Status

- **Priority:** P2
- **Effort:** S
- **Risk:** LOW
- **Depends on:** `plans/043-preserve-config-on-invalid-reads.md`
- **Category:** bug / config
- **Planned at:** commit `7c530ee`, 2026-09-26, including the known dirty tree

## Why this matters

Users can explicitly set Claude's interrupt delay to zero. Editing any assistant command in Settings writes the entire assistants map, omits zero delay fields, and causes the next startup to restore Claude's built-in 200 ms delay. Saving should preserve the effective settings the user already chose.

## Current state

`internal/config/config.go:311` builds assistant entries. The problematic serialization at line 317 is:

```go
if cfg.InterruptDelayMs > 0 {
    entry["interrupt_delay_ms"] = cfg.InterruptDelayMs
}
```

Loading overlays only fields that are present (`config.go:217`):

```go
if override.InterruptDelayMs != nil {
    cfg.InterruptDelayMs = *override.InterruptDelayMs
}
```

The default registry at `internal/config/agents.go:19` includes:

```go
{Name: "claude", DefaultCommand: "claude", InterruptCount: 2, InterruptDelayMs: 200},
```

`internal/config/assistants_save_test.go:69` currently expects a custom assistant's zero delay to be omitted. That serialization assertion should change; the custom assistant's effective behavior should not. The test's load/save round-trip assertions currently check commands rather than complete `AssistantConfig` equality. Follow its temp-file/readConfigFile/defaultAssistants/applyAssistantOverrides structure.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Regression | `go test ./internal/config -run 'TestSaveAssistants' -count=1` | PASS after fix |
| Config/interrupt tests | `go test ./internal/config ./internal/pty -count=1` | both pass |
| Race coverage | `go test -race ./internal/config ./internal/pty -count=1` | pass, no races |
| Real tmux/e2e | `env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e -count=1 -v` | pass; inspect skips |
| Input gate | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | both required input tests explicitly PASS |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | exit 0 |
| Changed-code lint | `make lint-strict-new` | zero new issues and clean formatter diff |

The audit's verify-loop passed; devcheck failed real-e2e scenarios. Ambient AMUX_WORKSPACES_ROOT was confirmed to escape temporary HOME and collide with a workspace in the actual user root. Until plan 059 fixes isolation, sanitize every broad/e2e command as above. Reproduce failures under isolation before attribution; do not fix unrelated failures or weaken real-agent assertions. Because this changes persisted input timing, run the real input gate even though the implementation is small.

## Scope

**In scope:** `internal/config/config.go`, `internal/config/assistants_save_test.go`, `README.md`, `docs/CONFIG.md`, `docs/ORCHESTRATION.md`, this plan's index status.

**Out of scope:** default registry values, `interrupt_count` semantics, delay/count clamping in `internal/pty`, send pacing, user interfaces for interrupt tuning, schema migrations, and config read-for-update behavior repaired by 043.

## Git workflow

Use the operator checkout or an authorized isolated `advisor/049-preserve-zero-interrupt-delay` branch containing 043. Record initial diffs. Do not stash, clean, reset, commit, or push. Preserve concurrent documentation edits and merge only the narrow contract wording.

## Steps

### Step 1: Characterize full configuration round trips

Add `TestSaveAssistantsPreservesExplicitZeroDelay`: load a config overriding Claude's delay to zero, change its command, save, reload using the normal defaults-plus-overrides path, and compare all fields. Add positive-delay and custom-zero cases. Confirm a missing delay in a hand-authored built-in override still inherits the built-in value; missing and explicit zero are distinct.

**Verify:** `go test ./internal/config -run 'TestSaveAssistantsPreservesExplicitZeroDelay' -count=1` fails before the serialization repair with delay 200 instead of 0. Run 043's save tests to establish that read-error preservation remains working.

### Step 2: Serialize the effective delay, including zero

Always put `interrupt_delay_ms` in each saved assistant entry. In-memory loader-produced values are already nonnegative; do not introduce new validation or change the runtime clamp. Keep existing positive-only count handling unchanged. Update the old custom-zero omission assertion to require an explicit numeric zero. Compare round-tripped `AssistantConfig` values, not only command strings.

**Verify:** `go test ./internal/config -run 'TestSaveAssistants' -count=1` → all pass. `go test ./internal/config ./internal/pty -count=1` → both packages pass, including current interrupt timing/clamping tests.

### Step 3: Document omission versus explicit zero and validate input

Update all three user-contract documents. CONFIG should say omitted built-in fields inherit built-in defaults, explicit zero delay disables spacing, and Settings preserves that zero. README may state the preservation guarantee briefly near assistant configuration. ORCHESTRATION should distinguish configured Ctrl-C spacing from external text/submit pacing; do not revise its raw carriage-return input contract.

**Verify:** `rg -n 'zero|interrupt_delay_ms' README.md docs/CONFIG.md docs/ORCHESTRATION.md` shows the clarified behavior in each. Run race coverage, isolated real tmux/e2e, `env -u AMUX_WORKSPACES_ROOT make verify-loop`, `env -u AMUX_WORKSPACES_ROOT make devcheck`, `make lint-strict-new`, and `git diff --check` → pass. A skipped verify-loop or any failed gate prevents marking this plan DONE.

## Test plan

Exercise Claude zero → save command edit → reload zero, Claude positive delay, custom zero delay, and omitted built-in delay inheriting its default. Assert the on-disk field is present with zero and all effective fields match. Preserve dependency 043's malformed/null/read-error rejection coverage. No new timing-based sleep test is needed; existing PTY interrupt tests validate runtime interpretation.

## Done criteria

- [ ] Explicit zero survives full load/save/load; absent built-in delay still inherits the default.
- [ ] Custom assistants keep the same effective behavior, with explicit zero now serialized.
- [ ] Dependency 043's save-preservation tests pass.
- [ ] Config/PTY/race tests, real-tmux/e2e, verify-loop, devcheck, lint-strict-new, and diff-check pass.
- [ ] All three contract documents updated and only scoped changes added.
- [ ] Index status updated or returned to the index-owning dispatcher.

## STOP conditions

Stop if 043 is absent or broken, the serialization block has changed beyond 043's helper extraction, a fix appears to require changing interrupt delivery or registry defaults, a twice-failed verification needs unrelated edits, or required tmux coverage cannot run.

## Maintenance notes

Whenever a built-in assistant gains a nonzero default, omitting its effective zero changes behavior. Round-trip tests must compare complete values and retain the missing-versus-present distinction in pointer-backed raw fields. Input pacing and Ctrl-C spacing are separate contracts.
