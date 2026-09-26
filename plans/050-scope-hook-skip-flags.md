# Plan 050: Scope hook skip flags to their advertised checks

> **Executor instructions:** Follow the steps and their verification gates; update this plan's row in `plans/README.md` when done. Do not commit, push, invoke a real push, or install hooks. Preserve existing user changes.
>
> **Drift check:** Run `git diff --stat 7c530ee..HEAD -- .githooks internal/devtools CONTRIBUTING.md README.md docs/CONFIG.md docs/ORCHESTRATION.md`, `git diff HEAD -- .githooks internal/devtools CONTRIBUTING.md README.md docs/CONFIG.md docs/ORCHESTRATION.md`, and `git ls-files --others --exclude-standard -- internal/devtools`. Compare the excerpts below; the audit included a dirty README. New unrelated edits must not be overwritten.

## Status

- **Priority:** P2
- **Effort:** S
- **Risk:** LOW
- **Depends on:** none; isolate e2e verification as described below until plan 059 lands
- **Category:** dx / tests / docs
- **Planned at:** commit `7c530ee` plus existing working-tree changes, 2026-09-26

## Why this matters

The documented escape hatches skip more than their names promise. `AMUX_SKIP_LINT` exits pre-commit before formatting and config checks, while `AMUX_SKIP_HARNESS` exits pre-push before lint and e2e tests. Keep each flag local to its named expensive check so developers can bypass one tool without unknowingly bypassing other gates.

## Current state

`.githooks/pre-commit:4` exits the entire script:

```bash
if [[ -n "${AMUX_SKIP_LINT:-}" ]]; then
  echo "amux pre-commit: skipping lint (AMUX_SKIP_LINT set)"
  exit 0
fi
```

`.githooks/pre-push:4` has the same shape for `AMUX_SKIP_HARNESS`. Its lines 12–18 already show the desired scoped convention for strict lint:

```bash
if [[ -n "${AMUX_SKIP_LINT:-}" ]]; then
  echo "amux pre-push: skipping strict lint (AMUX_SKIP_LINT set)"
else
  base_ref="${AMUX_LINT_BASE_REF:-origin/main}"
  echo "amux pre-push: running CI-parity strict lint (base_ref=${base_ref})..."
  BASE_REF="$base_ref" make lint-ci-parity
fi
```

`CONTRIBUTING.md:81` says skip-lint is a scoped escape hatch that preserves formatting, file-length, and harness checks; skip-harness is advertised as skipping the harness run. Preserve the existing nonempty-value semantics (even `0` means set), error propagation from `set -euo pipefail`, NUL-delimited staged filename handling, and missing-tmux warning after e2e.

Use the repository-contract test style in `internal/update/install_parity_test.go:18`: locate the repo with `filepath.Join("..", "..")`, read its actual tooling files, and fail with actionable diagnostics. For this change execute the real hook scripts with stub external commands rather than asserting their source spelling. Put those tests in a new test-only `internal/devtools` package so `go test ./...` discovers them without adding a Makefile/CI test runner.

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Shell syntax | `bash -n .githooks/pre-commit .githooks/pre-push` | exit 0 |
| Hook contracts | `go test ./internal/devtools -run TestHookSkipFlags -count=1 -v` | every matrix case passes |
| Tooling tests | `go test ./internal/devtools -count=1` | PASS |
| Full gate | `env -u AMUX_WORKSPACES_ROOT make devcheck` | exit 0; report baseline failures separately |
| Changed-code gate | `make lint-strict-new` | exit 0 |
| Whitespace | `git diff --check` | exit 0 |

The audit's unsanitized devcheck failed in e2e. Plan 059 covers the inherited workspace-root defect; do not run a broad e2e gate against an ambient user workspace root. The command-local `env -u` workaround does not edit the user's shell environment.

## Scope

**In scope:** `.githooks/pre-commit`, `.githooks/pre-push`, new `internal/devtools/hooks_test.go` (split a helper into `hooks_helpers_test.go` if the 500-line cap requires it), `CONTRIBUTING.md`, `README.md`, `docs/CONFIG.md`, `docs/ORCHESTRATION.md`, this plan and its index row. The three user-contract documents must reflect any changed env-flag description; identify these as contributor-hook flags, not runtime app configuration.

**Out of scope:** new skip flags, `--no-verify` behavior, CI policy, Makefile target semantics, hook installation, release commands, actual Git commits/pushes, application input behavior.

## Git workflow

Suggested branch: `advisor/050-scope-hook-skip-flags`. Inspect `git status --short` first; retain all pre-existing modifications. Do not stash, reset, commit, or push. A branch is optional if the operator already supplied an isolated checkout.

## Steps

### Step 1: Add an executable hook contract matrix

In `internal/devtools/hooks_test.go`, run each actual hook via `bash` from an unrelated temporary working directory. Prepend a temporary stub directory to the child's PATH; stub `git`, `make`, `go`, and `tmux` and record argv as one unambiguous record per invocation. Stub `git rev-parse --show-toplevel` to a temporary repository fixture, and staged diff to no files initially. Never invoke real `make`, tmux, Git mutation, or go-run from these tests. Construct child env explicitly for both flags so inherited values do not influence a case.

Cover flags unset, lint only, harness only, and both. Pre-commit must always run `fmt-check`, `lint-config-drift check-fmt-config`, and the staged-file guard; only `make lint` may disappear. Pre-push must always run `go test ./internal/e2e -count=1` and tmux availability checking; only strict lint and the harness command vary with their respective flags. Capture existing base-ref/argument forwarding as well.

**Verify:** `go test ./internal/devtools -run TestHookSkipFlags -count=1 -v` → expected RED specifically because the current early exits omit required invocations. Fix fixture errors before proceeding.

### Step 2: Narrow the two conditional blocks

Remove the top-level early exits. In pre-commit wrap only the golangci announcement and `make lint` in the lint flag branch. In pre-push wrap only `center_args`, the harness announcement, and `go run ./cmd/amux-harness` in the harness flag branch. Keep lint, e2e, and the tmux warning code in their current order outside it. Keep skip messages truthful.

**Verify:** `bash -n .githooks/pre-commit .githooks/pre-push` → exit 0; `go test ./internal/devtools -run TestHookSkipFlags -count=1 -v` → all matrix cases PASS.

### Step 3: Prove retained gates fail visibly

Add table cases where each retained stub fails. With lint skipped, failing formatting/config checks must still produce nonzero exit. With harness skipped, strict lint or e2e failure must still fail. Stub unusable tmux after successful e2e and assert the existing warning remains. Feed staged filenames containing spaces through the real `wc`/`awk` guard using a 501-line temporary Go file and assert failure even with lint skipped; do not rewrite the NUL-safe pipeline as a shortcut.

**Verify:** `go test ./internal/devtools -count=1` → PASS, including negative cases that assert nonzero child exit. Nothing is committed, pushed, or launched in tmux.

### Step 4: Reconcile documentation and run gates

Update the flag table in CONTRIBUTING and the three contract documents concisely with exactly which gates remain. Keep LINTING.md's existing lint policy intact; this plan changes hook dispatch, not lint configuration. Run the full and changed-code commands above. Record any reproducible baseline e2e failure by test name; do not silently bypass it or broaden this patch.

**Verify:** `git diff --check` → exit 0; `env -u AMUX_WORKSPACES_ROOT make devcheck` and `make lint-strict-new` → exit 0, or report a blocked final gate with its exact existing failure; `git diff --name-only` → only authorized delta beyond the recorded baseline.

## Test plan

`TestHookSkipFlags` is the dispatch matrix. Add `TestHookRetainedGatesFail`, `TestHookStagedFilenameWithSpaces`, and `TestHookMissingTmuxWarning` in the same test-only package. Use subprocess env isolation and temporary fixtures; no timing sleeps or external network. Match `internal/update/install_parity_test.go` for repo-root lookup and actionable failures, but validate actual behavior rather than text matching.

## Done criteria

- [ ] Shell syntax and all `internal/devtools` tests pass.
- [ ] Both skip flags remove only their named gate in the invocation matrix.
- [ ] All final gates pass; otherwise the row is BLOCKED with the exact gate failure.
- [ ] CONTRIBUTING and the three contract docs agree with tested behavior.
- [ ] No new changes outside Scope; index row updated; no commit/push.

## STOP conditions

Stop if hook semantics have intentionally changed since this snapshot, a platform cannot run the Bash hooks at all (skip with an explicit reason rather than changing the product platform contract), stubs invoke a real mutation, scope must expand into CI/Makefile policy, or a verification fails twice after a targeted correction. Do not fix the picker/e2e product regressions under this plan.

## Maintenance notes

When adding a new hook gate, add it to the expected invocation matrix and make an explicit decision about which existing flag, if any, skips it. Keep staged paths NUL-delimited and hook diagnostics independent of success exit status.
