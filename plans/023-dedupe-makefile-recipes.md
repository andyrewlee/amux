# Plan 023: Collapse duplicated Makefile recipe blocks and the three-way tmux-package exclusion lists

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- Makefile scripts/test_pkgs.sh`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: tech-debt
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

Two duplication hazards: (1) `test` (Makefile:60-65) and `devcheck` (:122-127) carry identical four-line go-list/filter/test/skip-check recipes — a flag added to one silently misses the other; `lint-ci-parity` duplicates a ~13-line mktemp/golangci-invocation block verbatim at :317-329 and :332-343 differing only in `--new-from-rev` vs `--new`. (2) The real-tmux package exclusion list lives in three places (`Makefile:62`, `Makefile:124`, `scripts/test_pkgs.sh:18`) with an explicit "keep both lists in sync" warning — adding a tmux-dependent package and updating some lists silently changes coverage.

## Current state

- `Makefile:60-65` (`test`) vs `:122-127` (inside `devcheck`) — same recipe modulo indentation.
- `Makefile:317-329` vs `:332-343` (`lint-ci-parity` two golangci branches) — verbatim block, one flag differs.
- Exclusion lists: `Makefile:62` and `:124` use `grep -v -E '/internal/(tmux|e2e|app|pty)$'`; `scripts/test_pkgs.sh:18` uses `(tmux|e2e|pty)` — the `app` delta between local sweep and CI sweep is DELIBERATE (test_pkgs.sh:15's comment documents it) and must be preserved.

Design constraints: Makefile recipes must stay POSIX-sh; `scripts/` is where shared shell already lives; `test_pkgs.sh` is already the "package list" authority for CI.

## Commands you will need

| Purpose    | Command                                              | Expected on success |
|------------|------------------------------------------------------|---------------------|
| Test sweep | `make test` (or the devcheck equivalent)             | exit 0              |
| Parity     | `BASE_REF=origin/main make lint-ci-parity`           | exit 0              |
| Pkg list   | `bash scripts/test_pkgs.sh`                          | prints same list as before (diff against pre-change output) |
| Full gate  | `make devcheck`                                      | exit 0              |

## Scope

**In scope**:
- `Makefile` — dedupe the two recipe blocks.
- `scripts/test_pkgs.sh` or a new `scripts/tmux_pkgs.list` — single source for the exclusion set.

**Out of scope**:
- `.github/workflows/*.yml` — only touch if they grep the exclusion list directly (verify; they should call the same make/script entry points).
- `.githooks/*` — plans/016's domain; do not touch hooks here.
- The `app` exclusion delta — deliberate, must survive.

## Git workflow

- Branch: `advisor/023-makefile-dedup` off `main`.
- Commit style: `chore: deduplicate Makefile recipes and tmux package lists`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Single-source the exclusion list

Create `scripts/real_tmux_pkgs.txt` (or extend `test_pkgs.sh` to emit both variants): one file containing the tmux-dependent package basenames (`tmux`, `e2e`, `pty` — and document that `app` joins them only for the local sweep). Makefile reads it via `$(shell cat scripts/real_tmux_pkgs.txt)` composed into the grep -E pattern (or pipe join); `test_pkgs.sh` reads the same file for its variant and appends `app` per its documented delta.

Alternative honest shape: make `test_pkgs.sh` the single authority — Makefile calls `./scripts/test_pkgs.sh --local` / `--ci` to get the package list directly, eliminating the grep filters entirely. Prefer this if the script's output format is already `go test`-ready package paths — read the script first.

**Verify**: `bash scripts/test_pkgs.sh` output is identical to pre-change (capture `git stash`-free before/after via `diff`).

### Step 2: Dedupe `test`/`devcheck` recipe

Make `devcheck` depend on `test` (if ordering within devcheck allows — check what else devcheck runs before/after; if ordering matters, extract the shared block into a `define`/`call` block or a `scripts/run_unit_tests.sh`).

**Verify**: `make devcheck` → exit 0; the test sweep step runs identically (watch output).

### Step 3: Dedupe the `lint-ci-parity` golangci block

Extract the ~13-line block into a parameterized private target or `define` — e.g. `lint-strict-run` taking the diff-arg (`--new` or `--new-from-rev $(BASE)`) — preserving mktemp cleanup + version probe + error handling exactly.

**Verify**: `BASE_REF=origin/main make lint-ci-parity` → exit 0; run both branches if feasible (with and without BASE_REF).

### Step 4: Full gate

**Verify**: `make devcheck` → exit 0; `make lint` → exit 0.

## Test plan

- Behavioral: identical package list output (Step 1 diff) + identical devcheck output ordering (Step 2) + both lint-ci-parity branches (Step 3).
- No new unit tests — Makefile refactor verified by running the targets.

## Done criteria

- [ ] One source of truth for the real-tmux package set (shared by Makefile + test_pkgs.sh), with the `app` delta preserved explicitly.
- [ ] `test`/`devcheck` share one recipe path.
- [ ] `lint-ci-parity` runs one parameterized golangci block.
- [ ] `make devcheck`, `make test`, `make lint-ci-parity` all pass.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `test_pkgs.sh` has grown options incompatible with Makefile consumption — fall back to the `.txt` shared-list approach.
- The `app` exclusion delta turns out NOT to be deliberate (CI runs app tests and local doesn't — if the intent is different, preserve what exists and flag it).
- A workflow YAML greps the exclusion list inline — then the list isn't single-sourced yet; wire it to the same file or report.

## Maintenance notes

- New real-tmux packages get added to the ONE list file — update `test_pkgs.sh:15`'s warning comment to point at it.
- Future recipe shared with CI should prefer "script in scripts/ + make wrapper" over inline recipe blocks — the two-copy hazard is inherent to duplicated Make recipes.
