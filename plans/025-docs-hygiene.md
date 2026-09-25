# Plan 025: Fix three documentation gaps — LINTING.md devcheck drift, undocumented hook escape valves, `make help` omissions

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- LINTING.md CONTRIBUTING.md Makefile`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: docs
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

Three small doc gaps, each a real contributor trap: (1) `LINTING.md` — labeled "Lint policy source of truth" by AGENTS.md — describes a devcheck that's four steps stale (misses the `internal/pty` test exclusion, `tmux-skip-check`, `lint-config-drift`, `check-fmt-config`, and `golangci-lint fmt --diff` inside `make lint`), so a contributor editing lint config gets unexplained gate failures. (2) The hook escape valves (`AMUX_SKIP_LINT`, `AMUX_SKIP_HARNESS`, `AMUX_LINT_BASE_REF`, `AMUX_HARNESS_CENTER_ARGS`) exist but are documented nowhere a contributor would look — the discoverable bypass becomes `--no-verify`, which disables everything. (3) `make help` omits `windows-build`, `install`, and `check-fmt-config`, and the `ci` help line omits `windows-build` — the cross-compile gate is invisible until it fails in CI.

## Current state

- `LINTING.md:15-20` — stale bullet list describing devcheck as vet + `go test` "on all packages except `internal/tmux`, `internal/e2e`, and `internal/app`" + golangci + file-length. Actual `devcheck` (Makefile:121-130): same exclusions **plus `internal/pty`** (:124), plus `tmux-skip-check` (:127), `lint-config-drift` (:128), `check-fmt-config` (:129); and `make lint` (:290) additionally runs `golangci-lint fmt --diff`.
- `.githooks/pre-commit:4`, `pre-push:4,12,15,20` — the four env vars; CONTRIBUTING.md's "Dev-side environment variables" section (~lines 72-80) documents none of them (verify the section exists and its format — match its bullet style).
- `Makefile:411-448` (`help`) — no `windows-build` (defined ~:107), `install` (:45), or `check-fmt-config` (:357); the `ci` help line (~:420) omits `windows-build` though the target (:119) runs it.

## Commands you will need

| Purpose      | Command                    | Expected on success |
|--------------|----------------------------|---------------------|
| Help output  | `make help`                | lists the new targets |
| Doctor/doc   | `make devcheck` (dry read) | doc now matches the printed steps |
| Format       | `gofumpt -l .` — n/a for docs; `make devcheck` as smoke | exit 0 |

## Scope

**In scope**:
- `LINTING.md` — devcheck section.
- `CONTRIBUTING.md` — env-var section.
- `Makefile` — help text lines only.

**Out of scope**:
- `.githooks/*` — documenting, not changing.
- AGENTS.md/README — they point at LINTING.md/CONTRIBUTING; no edits needed unless you find an outright contradiction.
- plans/016's hook changes — land independently; if 016 lands first, the doc text stays accurate (the env vars don't change).

## Git workflow

- Branch: `advisor/025-docs-hygiene` off `main`.
- Commit style: `docs: sync lint docs and make help with actual gates`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Rewrite the devcheck description in LINTING.md

Enumerate the actual `devcheck` recipe (Makefile:121-130) step for step: package test sweep with the four-package exclusion (`tmux`, `e2e`, `app`, `pty`), `tmux-skip-check`, `lint-config-drift`, `check-fmt-config`, `make lint` contents (golangci + `fmt --diff` + file-length). Keep the doc's existing voice — terse bullets, command names verbatim.

**Verify**: `make devcheck` output matches the new description step-for-step (run it, read output against the bullets).

### Step 2: Document the hook escape valves in CONTRIBUTING.md

In the "Dev-side environment variables" section, add the four vars with one line each: what it skips, where honored (pre-commit/pre-push), and the honest guidance (scoped escape > `--no-verify`).

**Verify**: `grep -n 'AMUX_' CONTRIBUTING.md` → the four new lines present.

### Step 3: Complete `make help`

Add help lines for `windows-build`, `install`, `check-fmt-config` (match the existing `## comment` convention the help target parses — check how help is generated: usually `##` comments above targets); append `+ windows-build` to the `ci` line.

**Verify**: `make help` → the three targets appear; the `ci` line mentions windows-build.

### Step 4: Smoke

**Verify**: `make devcheck` → exit 0 (docs change can't break it, but run as the repo convention requires).

## Test plan

- Docs-only: verification is `make help` output + devcheck-vs-doc parity (Step 1 check) + `grep` for the new lines.
- No tests needed.

## Done criteria

- [ ] LINTING.md's devcheck description matches `Makefile:121-130` step-for-step.
- [ ] All four hook escape vars documented in CONTRIBUTING.md.
- [ ] `make help` lists `windows-build`, `install`, `check-fmt-config`; `ci` line is accurate.
- [ ] `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `devcheck` changed since planning (drift) — re-enumerate the recipe, update to match.
- The env-var section doesn't exist in CONTRIBUTING.md (drift) — add it under the closest section or report.
- `make help` is generated differently than assumed — follow the actual mechanism.

## Maintenance notes

- LINTING.md drifts whenever devcheck gains a step — the durable fix would be generating the doc from the Makefile, but that's out of scope; when devcheck next changes, this section must be edited in the same PR (say so in LINTING.md? Only if the file already carries such maintenance notes — match its style).
- If a fifth escape valve is added to the hooks, document it in the same PR.
