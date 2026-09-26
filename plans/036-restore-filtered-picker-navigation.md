# Plan 036: Restore structural navigation in the filtered agent picker

> **Executor instructions:** Execute the steps in order and record every gate. This is a source implementation handoff, not permission to commit or push. Update this plan's status row in `plans/README.md` only if the coordinating reviewer has not reserved the index.
>
> **Drift check first:** Run `git status --short`, `git diff --stat 7c530ee..HEAD -- internal/ui/common/dialog_update.go internal/ui/common/dialog_navigation_test.go internal/ui/common/run_session_picker_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md`, and `git diff HEAD -- internal/ui/common/dialog_update.go internal/ui/common/run_session_picker_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md`. Also read any untracked in-scope files. This plan includes the dirty tree audited on 2026-09-26; the faulty condition itself is uncommitted. Preserve the run-session picker and every unrelated user edit. Compare the excerpts below; stop if behavior has already changed materially.

## Status

- **Priority:** P1
- **Effort:** S
- **Risk:** LOW
- **Depends on:** no implementation dependency; [plan 059](059-isolate-e2e-workspace-root.md) before unrestricted e2e validation
- **Category:** bug / tests
- **Planned at:** commit `7c530ee`, plus audited uncommitted working tree, 2026-09-26

## Why this matters

The agent picker supports fuzzy filtering and a selectable list. The pending run-session-picker changes correctly make j/k navigate an unfiltered list, but also disable arrows and Tab in the filtered agent picker. Restore those structural keys while keeping j/k available as filter text; opening an agent must not require a mouse or an exact filter.

## Current state

amux is a Go Bubble Tea v2 TUI. Model mutation belongs on the Update goroutine; commands return messages. Existing common widgets use `key.Matches` and `textinput.Model`, with package-local tests constructing actual `tea.KeyPressMsg` values.

`internal/ui/common/dialog_update.go:89–98` currently contains:

```go
case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "down", "j"))):
    // j is a navigation key only for unfiltered selects — on filtered
    // ones it must reach the filter input as typed text.
    if d.dtype != DialogInput && !(d.dtype == DialogSelect && d.filterEnabled) {
        maxLen := len(d.options)
        if d.filterEnabled {
            maxLen = len(d.filteredIndices)
        }
```

The equivalent Up/Shift+Tab/k branch at `:102–114` has the same exclusion. `agent_picker.go:NewAgentPicker` sets `filterEnabled: true`; the selected position is an index into `filteredIndices`, and Enter translates it to an original option index. The bottom of `Dialog.Update` forwards messages to `filterInput.Update` and reapplies filtering after text changes.

Match `internal/ui/common/run_session_picker_test.go:TestRunSessionPicker_NavigationAndSelect`, which drives `tea.KeyPressMsg{Code: 'j', Text: "j"}` and executes the resulting `DialogResult` command. Preserve that uncommitted feature and its tests. Architecture requires UI-owned state and command-returned effects; this fix needs no goroutines, no widget replacement, and no render redesign.

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Baseline | `go test ./internal/ui/common -count=1` | Existing tests pass |
| Focused | `go test ./internal/ui/common -run 'TestDialogNavigation|TestRunSessionPicker' -count=1 -v` | Named new and existing tests PASS |
| UI package | `go test ./internal/ui/common -count=1` | All pass |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | Exit 0, or record an independently established baseline failure without claiming success |
| Changed-code lint | `make lint-strict-new` | Exit 0, no new diagnostics |
| Real input | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | Both real-agent keystroke tests PASS, not SKIP |
| Real picker regression | `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^TestWorkspaceCreateAgentsHaveDistinctSessions$' -count=1 -v` | PASS with distinct Claude and Codex sessions, not SKIP |
| Patch hygiene | `git diff --check` | Exit 0 |

The audit's original devcheck encountered real-e2e failures; verify-loop passed. Later isolation found inherited `AMUX_WORKSPACES_ROOT` could redirect tests into the user's workspace directory, so all e2e-reaching commands here remove it until plan 059 lands. A sanitized replay made the overlay and shelve cases pass, but `TestWorkspaceCreateAgentsHaveDistinctSessions` still failed at `internal/e2e/agent_dupe_test.go:42`, creating two Claude sessions and no Codex session. That is direct evidence for this plan's Tab-navigation regression, not an unrelated baseline failure. Require that exact real test to pass after the fix. Do not repair other e2e defects under this scope.

## Scope

**In scope:** `internal/ui/common/dialog_update.go`; new `internal/ui/common/dialog_navigation_test.go`; `internal/ui/common/run_session_picker_test.go` only for preservation/regression coverage; `README.md`, `docs/CONFIG.md`, `docs/ORCHESTRATION.md` for the picker keyboard contract; the plan index status if delegated.

**Out of scope:** Dialog layout, fuzzy-match algorithm, agent configuration, session enumeration, overlay routing, PTY input encoding, persistence, and other user edits.

## Git workflow

Work in the operator-provided branch/check-out. An isolated branch may be named `advisor/036-filtered-picker-navigation` if requested; do not switch or reconstruct the dirty tree blindly. Record the starting status and diff so existing modifications are not attributed to this fix. Do not stash, clean, reset, commit, push, or open a PR without explicit authorization.

## Steps

### Step 1: Establish the regression at the public Update boundary

Create table-driven tests in `dialog_navigation_test.go`. Use `NewAgentPicker` with at least three names, call Show, and send Down, Up, Tab, and Shift+Tab (use `tea.ModShift` with Tab) through Update. Assert cursor movement/wrap and Enter's original index after a nonempty filter. Add j/k text-entry cases with names that retain matches, plus zero-match and one-match cases. Do not mutate private cursor state to simulate the movement being tested.

**Verify:** `go test ./internal/ui/common -run TestDialogNavigation -count=1 -v` → the new structural-key cases fail against the audited condition; text-entry cases establish the behavior to preserve. If structural navigation already passes, stop to reconcile drift.

### Step 2: Separate structural navigation from filter text

In `Dialog.Update`, arrows/Tab/Shift+Tab navigate any select/confirm that previously supported them. j/k navigate unfiltered selects only and reach the filter input for filtered selects. Input dialogs continue receiving j/k as text. Consume structural navigation without forwarding it into the filter input, return no selection command, and retain modulo/wrap safeguards when the candidate count is zero. Do not alter Enter/Esc or mouse selection semantics.

**Verify:** `go test ./internal/ui/common -run 'TestDialogNavigation|TestRunSessionPicker' -count=1 -v` → all cases PASS, including the existing unfiltered run picker. `go test ./internal/ui/common -count=1` → PASS.

### Step 3: Document the key contract and run gates

Update the picker help descriptions in all three user-contract documents: Up/Down/Tab/Shift+Tab navigate, printable text filters the agent picker, and the unfiltered run-session picker retains j/k. Keep prose short and avoid documenting implementation conditions. Run the command table's required checks and record any established baseline failure separately.

**Verify:** `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^TestWorkspaceCreateAgentsHaveDistinctSessions$' -count=1 -v` → PASS with distinct assistant sessions; `env -u AMUX_WORKSPACES_ROOT make verify-loop` → both required tests PASS; `make lint-strict-new` → exit 0; `env -u AMUX_WORKSPACES_ROOT make devcheck` → exit 0 (otherwise record BLOCKED with the exact failure); `git diff --check` → exit 0. `git diff --name-only` and `git status --short` → this implementation adds changes only in scope beyond the recorded starting dirty tree.

## Test plan

Use actual messages, following `run_session_picker_test.go`. Cover all four structural keys, wrapping at both ends, no matches, one match, filtered-index-to-original-index translation, j/k typed into a filter, j/k in an input dialog, and unfiltered run-picker j/k. Do not assert only that Update returns nonnil: assert the selected value/index and filter text.

## Done criteria

All required final gates must pass before this plan is marked DONE. A known or newly discovered gate failure leaves the plan BLOCKED with the exact command/test and evidence; recording a failure is not a substitute for passing. Do not expand implementation scope to repair other findings. Until [plan 059](059-isolate-e2e-workspace-root.md) lands, remove `AMUX_WORKSPACES_ROOT` from every command reaching e2e, including devcheck, verify-loop, and test-race-tmux.

- [ ] `go test ./internal/ui/common -run 'TestDialogNavigation|TestRunSessionPicker' -count=1 -v` passes with structural-key tests executed.
- [ ] `go test ./internal/ui/common -count=1`, `make lint-strict-new`, and `env -u AMUX_WORKSPACES_ROOT make verify-loop` pass.
- [ ] Sanitized `TestWorkspaceCreateAgentsHaveDistinctSessions` executes and passes, proving Tab selects a different assistant end to end.
- [ ] `env -u AMUX_WORKSPACES_ROOT make devcheck` result is recorded honestly; unrelated baseline failures are not weakened or repaired here.
- [ ] All three contract docs describe the resulting key behavior.
- [ ] `git diff --check` is clean; only authorized in-scope edits were added; existing run-picker changes remain.
- [ ] Index status is updated by its designated owner, and verification details accompany the handoff.

## STOP conditions

Stop and report if the picker no longer uses `Dialog`, if the conditions in Current state have already changed materially, if fixing movement requires PTY/overlay changes, or if two reasonable attempts cannot pass the focused tests. Do not remove the dirty run-picker implementation to make tests pass. A baseline e2e failure is a reporting blocker, not authorization to widen scope.

## Maintenance notes

Keep navigation keys and printable filter text separate whenever adding select-dialog variants. Reviewers should require input tests when expanding a shared key branch; run-picker-only tests did not protect the filtered sibling. No new keybinding or widget abstraction is needed.
