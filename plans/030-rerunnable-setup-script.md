# Plan 030: Make `setup` re-runnable on demand from the scripts surface

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/app/workspacesvc/workspace_service_scripts.go internal/app/app_workspace_scripts.go internal/app/app_operations.go internal/messages/messages_workspace_ops.go internal/ui/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: direction
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`setup` is the one lifecycle script users can't re-trigger: it fires automatically on create/restore/trust-approval, but a transient failure (flaky `npm install`) or a `setup-workspace` edited after creation leaves a half-provisioned workspace — recovery means hand-running commands in a sidebar terminal or shelve+restore churn. `Service.RunSetupAsync` already exists; the `O` viewer already exists to diagnose the failure; the only missing piece is a user trigger. The messages doc even records the asymmetry: "Only `run` is user-triggerable" was a stated simplification, not a design verdict.

## Current state

- `internal/app/workspacesvc/workspace_service_scripts.go:20-30` — `RunSetupAsync(ws)` exists, returns a `tea.Cmd` producing `WorkspaceSetupComplete` — the exact service call create/restore already use.
- `internal/app/app_operations.go:226` — wired to create.
- `internal/app/app_workspace_scripts.go:124-171, 411-447` — the run-script surface: `R` viewer, `a` attach, script editor flow — the natural home for a re-run key.
- `internal/messages/messages_workspace_ops.go:139-141` — the comment recording "only `run` is user-triggerable" — update it when this lands.
- Trust gating: `RunSetup` already runs the trust check internally (the `ErrScriptsNotTrusted` path) — a re-run on an untrusted/edited config re-prompts through the same flow; nothing new to build.
- The `O` viewer (setup output) is the diagnosis surface — re-run key placement should sit next to it or in the scripts editor; check `app_workspace_scripts.go` for where `O`/`R` bindings live (`internal/app/app_prefix.go:33-52` `prefixCommandTable` for the key map).

## Commands you will need

| Purpose    | Command                                   | Expected on success |
|------------|-------------------------------------------|---------------------|
| Build      | `go build ./internal/app`                 | exit 0              |
| Unit tests | `go test ./internal/app -count=1`         | all pass            |
| E2E        | `go test ./internal/e2e -count=1` (if a scenario is added) | pass or documented skip |
| Full gate  | `make devcheck`                           | exit 0              |

## Scope

**In scope**:
- The key binding + message + handler wiring for re-running setup on the active workspace.
- `internal/messages/messages_workspace_ops.go` — update the "only run is user-triggerable" comment.
- Tests for the new handler path.
- `README`/`docs/CONFIG.md` — one line in the controls table for the new key.

**Out of scope**:
- `RunSetupAsync`/`RunSetup` internals — correct as-is.
- Making `archive` user-triggerable — different risk profile (destructive hook); explicitly out.
- Toast/message redesign — reuse the existing `WorkspaceSetupComplete` handling; the only new wrinkle is messaging "re-run" vs "initial" (see Step 3).

## Git workflow

- Branch: `advisor/030-rerunnable-setup` off `main`.
- Commit style: `feat: allow re-running workspace setup on demand`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Choose the trigger surface

Read `app_workspace_scripts.go` + `app_prefix.go`'s `prefixCommandTable` and pick the binding: a key in the scripts editor/`O` viewer context (e.g. `s` for "re-run setup" or `r` for re-run inside the `O` view) — match surrounding conventions (single keys inside modal viewers; prefix chords at top level). Document the choice in the PR. Check for conflicts: `grep -rn "case 'r'\|case 's'\|\"r\"\|\"s\"" internal/ui/center internal/app/app_prefix.go` for collisions in the chosen context.

**Verify**: you can name the exact file + key + dispatch case.

### Step 2: Wire the request → service call

Follow the `run`-script trigger's exact shape: input → handler → `workspaceService.RunSetupAsync(ws)` → `WorkspaceSetupComplete` message → existing completion handling (toast + script-running indicator update). Find the run-trigger's full path (`grep -rn 'RunScriptAsync\|ToggleScriptAsync' internal/app | grep -v _test`) and mirror it for setup.

**Verify**: `go build ./internal/app` → exit 0.

### Step 3: Re-run semantics in the completion path

`WorkspaceSetupComplete` today means "initial setup done" — check what its handler does (unlock workspace? toast text? `grep -rn 'WorkspaceSetupComplete' internal/app`). A re-run landing in the same handler must not replay create-only side effects (e.g. unlocking, first-run messaging). If the handler is already side-effect-free beyond status display, nothing needed; if it isn't, tag the request (`Rerun: bool` on the message or a distinct `WorkspaceSetupRerunComplete`) — pick the smaller change.

**Verify**: `go test ./internal/app -count=1` → pass.

### Step 4: Tests + docs

- Unit test: binding → `RunSetupAsync` called on the service fake (`grep -rn 'RunSetupAsyncFunc\|runSetupAsync' internal/testutil internal/app --include='*_test.go'` for the fake shape); completion → status reflects success/failure.
- Update the messages comment (`:139-141`) and the README controls table row for the new key.

**Verify**: `go test ./internal/app -count=1` → pass; `make devcheck` → exit 0.

## Test plan

- New: trigger-path test (key → service call), completion-message semantics for rerun (if tagged), untrusted-config re-gate (the trust path already tested — pin that re-run also hits it if trivially testable).
- Structural pattern: the `run`-trigger's handler tests in `internal/app`.

## Done criteria

- [ ] A user-facing trigger re-runs `setup` on the active workspace through `RunSetupAsync`.
- [ ] Re-run honors the trust gate (edited config re-prompts).
- [ ] Re-run completion doesn't replay create-only side effects.
- [ ] `messages_workspace_ops.go` comment + README updated.
- [ ] `go test ./internal/app -count=1` exits 0; `make devcheck` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `RunSetupAsync` isn't safe to re-run (e.g. assumes create-time state) — check its impl; if re-running is unsafe, STOP and report rather than exposing it.
- The completion handler does create-only work that can't be cleanly gated — report the minimal tagging approach needed.
- No free key exists in the chosen surface without conflicts — pick the alternative surface or report for a product call.

## Maintenance notes

- This is the lifecycle-script UX asymmetry fix; `archive` stays non-triggerable deliberately — if that ever changes, it needs a confirmation gate, not this pattern.
- The trust gate makes re-run safe by construction: content-edited configs re-prompt — reviewers should verify the re-run path doesn't accidentally bypass `IsTrusted` (it can't — `RunSetup` checks internally — but say it in the PR).
