# Plan 016: Add lint-config drift checks to the git hooks

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- .githooks/ Makefile .golangci.yml .golangci.strict.yml`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: dx
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`lint-config-drift` and `check-fmt-config` are dedicated CI gates (`ci.yml:51-58`) because strict-config drift used to merge green — but neither git hook runs them. Editing `.golangci.yml`'s `local-prefixes` or dropping a baseline line from `.golangci.strict.yml` passes both hooks and fails only in CI. These are sub-second checks — ideal hook material — and the hooks already run the heavier `make lint`/`fmt-check` gates.

## Current state

`.githooks/pre-commit:12-38` runs: `make fmt-check`, `make lint` (which internally runs check-golangci-version + file-length), staged file-length guard. `make lint` (Makefile:288-291) does NOT include `lint-config-drift` or `check-fmt-config` — they're separate devcheck steps (Makefile:128-129) and separate CI steps.

`.githooks/pre-push:20-26` runs `lint-ci-parity` + center harness + `go test ./internal/e2e`. The unit-test sweep (`scripts/test_pkgs.sh`) is in no hook — deliberately deferred here (hook-speed is a design value; adding it would push people to `--no-verify`).

Hook escape valves already exist: `AMUX_SKIP_LINT` honored by both hooks — the drift checks should ride the same guard.

## Commands you will need

| Purpose       | Command                                    | Expected on success |
|---------------|--------------------------------------------|---------------------|
| Drift checks  | `make lint-config-drift check-fmt-config`  | exit 0              |
| Hook run      | `bash .githooks/pre-commit` (in a dirty-index state, or `git commit --dry-run`-adjacent manual invoke) | exits 0 on clean tree |
| Shell syntax  | `bash -n .githooks/pre-commit`             | exit 0              |
| Full gate     | `make devcheck`                            | exit 0              |

## Scope

**In scope**:
- `.githooks/pre-commit` — add the two drift checks.

**Out of scope**:
- `.githooks/pre-push` unit-test sweep — deliberately NOT added (see Why; the e2e package test is already there).
- `Makefile` targets — they exist; only hook wiring changes.
- CONTRIBUTING.md escape-hatch docs — covered by plans/025.

## Git workflow

- Branch: `advisor/016-hooks-drift-checks` off `main`.
- Commit style: `chore: run lint-config drift checks in pre-commit hook`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add the checks to pre-commit

In `.githooks/pre-commit`, after the `make lint` block and before the file-length guard, add:

```bash
echo "amux pre-commit: checking lint config drift..."
if ! make lint-config-drift check-fmt-config; then
  echo "(lint config must stay in sync — see LINTING.md)"
  exit 1
fi
```

Placed after `make lint` because it's config-level (cheaper than lint but only meaningful once lint itself is invoked); it inherits `AMUX_SKIP_LINT`'s early-exit at the top — no extra guard needed.

**Verify**: `bash -n .githooks/pre-commit` → exit 0.

### Step 2: Exercise the hook both ways

1. Clean state: `bash .githooks/pre-commit` → exits 0 (all checks pass).
2. Negative: temporarily break the drift condition (e.g. remove a line from `.golangci.strict.yml` or change `local-prefixes` in `.golangci.yml` — whichever `check-fmt-config`/`lint-config-drift` actually polices; confirm which file each target checks by reading the Makefile recipes) → hook must FAIL → `git checkout -- <file>` to revert.

**Verify**: the negative run exits non-zero with the drift message; the clean run exits 0.

### Step 3: Full gate

**Verify**: `make devcheck` → exit 0.

## Test plan

- Shell-hook changes aren't unit-tested; the two-way exercise (Step 2) is the verification.
- `bash -n` syntax check + clean/negative runs are the gate.

## Done criteria

- [ ] `pre-commit` runs `lint-config-drift` and `check-fmt-config` and fails the commit on drift.
- [ ] `AMUX_SKIP_LINT` still bypasses everything.
- [ ] `bash -n .githooks/pre-commit` exits 0; clean-state run exits 0; drifted-config run fails.
- [ ] `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The drift targets were merged into `make lint` already (drift) — then the hook needs nothing; report as moot.
- The checks are not sub-second on this repo (measure once) — report if they'd meaningfully slow the hook (unlikely; they're grep/diff checks).
- The hooks were rewritten (e.g. husky-style manager introduced) — adapt placement, same intent.

## Maintenance notes

- Hook mirrors CI: when CI adds a new config-drift gate, the hook should gain it in the same PR — add that to `.githooks/` header comments? No — keep the file's existing comment style; LINTING.md is the policy doc if a reminder belongs anywhere (don't add docs in this plan beyond the code change).
- `pre-push`'s deliberate omission of the unit sweep stays — revisit only if push-time red CI becomes the dominant failure mode.
