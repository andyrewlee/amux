# Linting Strategy

This repository is lint-driven by design. The goal is to keep code quality high and keep code shape consistent across humans and coding agents.

## Current Gate (CI + Local)

The required gate is:

```bash
make devcheck
```

This runs:

- `go vet ./...`
- `go test ./...`
- `golangci-lint run`
- file length guard (`*.go` files must be <= 500 lines)

CI enforces the same lint checks in `.github/workflows/ci.yml`.

## One-Time Setup: Pinned golangci-lint

`make lint` / `make devcheck` need the golangci-lint version pinned in
`.golangci-version`. A system golangci-lint is frequently the wrong version and
fails confusingly: a v2.x binary cannot read this repo's v1 `.golangci.yml`
(`unsupported version of the configuration`), and even a prebuilt v1.64.8 can be
rejected because it was built with an older Go than the `go.mod` target
(`build Go < target Go`).

Run this once (and again only if `.golangci-version` changes):

```bash
make lint-tools
```

It builds the pinned version **from source** with your local Go toolchain into
the gitignored `./.cache/bin/`. It is idempotent (a no-op when the local binary
already reports the pinned version) and never deletes an existing good binary.

`make lint`, `make lint-strict`, and `make lint-strict-new` then prefer
`./.cache/bin/golangci-lint` when it matches `.golangci-version`, otherwise they
fall back to a `golangci-lint` on `PATH`. CI is unaffected: it has no
`./.cache/bin` binary (it is gitignored) and uses `golangci-lint-action`, so the
Makefile resolves to the action-installed `PATH` binary there.

## Phase 2: Strict Ratchet

Phase 2 is enabled as a stricter profile in `.golangci.strict.yml`.

Use it locally on changed code:

```bash
make lint-strict-new
```

Or against a specific base revision:

```bash
make lint-strict-new BASE=origin/main
```

CI runs this strict profile for pull requests only, ratcheted to changed code via `--new-from-rev=<base-sha>`.

### Complexity ratchet

The changed-code strict ratchet also gates function-level complexity. The
500-line file guard does nothing about oversized or deeply-nested *functions*
inside sub-500-line files, so `make lint-strict-new` and the PR-only
`lint-strict-pr` job enable:

- `funlen` — `lines: 120`, `statements: 60` (excluded on `_test.go`, where
  table-driven tests legitimately run long)
- `gocyclo` — `min-complexity: 30` (high-branching functions)
- `nestif` — `min-complexity: 5` (deeply nested `if` blocks; a different shape
  than `gocyclo` — a linear nested-`if` chain trips `nestif` but not `gocyclo`)

Because these complexity linters only run with `--new` or `--new-from-rev`,
every existing offender is grandfathered: they never fail on legacy code and
only prevent *new* functions from exceeding the budget (and gently nudge
ongoing decompositions). They are intentionally absent from baseline
`.golangci.yml` and from full-tree `make lint-strict`.

## Phase 3: Baseline Promotion + Escalation

Phase 3 promotes additional low-noise rules into baseline `.golangci.yml`:

- `depguard` (blocks `github.com/pkg/errors` and `io/ioutil`)
- `forbidigo` (blocks direct print/log side-effect APIs in internal packages)
- `thelper` (helper function hygiene in tests)
- `usestdlibvars` (prefer stdlib constants, for example `http.MethodGet`)
- `whitespace` (no unnecessary leading/trailing blank lines)
- `gofumpt` (stricter canonical formatting)

Phase 3 keeps CI fully automated (no PR-body parsing). The gate is enforced by required CI jobs:

- baseline lint/test/harness checks in `.github/workflows/ci.yml`
- strict changed-code lint in `lint-strict-pr`

## Baseline Lint Rules

The enforced baseline is in `.golangci.yml`.

Focus areas:

- correctness and safety (`errcheck`, `govet`, `staticcheck`, `unused`, `errorlint`)
- mechanical consistency (`gofumpt`, `gofmt`, `goimports`, `copyloopvar`)
- dependency and output discipline (`depguard`, `forbidigo`)
- hygiene (`nolintlint`, `misspell`, `unconvert`, `ineffassign`, `gosimple`, `whitespace`)
- test helper quality (`thelper`)

## `nolint` Policy

- `nolint` must be specific (for example `//nolint:unused`).
- `nolint` must include a short explanation.
- If a suppression becomes unnecessary, remove it.

## Rule Changes

When adding or tightening lint rules:

1. Prefer low-noise, high-signal rules first.
2. Run lint and tests locally before opening a PR.
3. If a rule causes widespread churn, land it in a follow-up phase with explicit migration notes.

The strict profile is where new rules should be introduced first (ratcheted on changed code), before considering promotion into the baseline gate.

## Ownership And Escalation Rules

Escalation is path-based and automated by CI jobs. For local confidence, use:

- `internal/ui/`, `internal/vterm/`, `cmd/amux-harness/`:
  - run `make harness-presets`
- `internal/tmux/`, `internal/e2e/`, `internal/pty/`:
  - run `go test ./internal/tmux ./internal/e2e`
- agent input/send path (`internal/tmux/send.go`, `internal/pty/`, keystroke forwarding):
  - run `make verify-loop` — drives a real keystroke through amux into a real raw-mode agent and asserts it arrives intact (incl. a literal CR). `make devcheck` alone is insufficient: the real-tmux tests skip there, so it gives a false green for send/Enter behavior.
- lint policy files (`.golangci.yml`, `.golangci.strict.yml`, `LINTING.md`, `Makefile`, `.github/workflows/ci.yml`):
  - call out intent in PR summary

## Agent Workflow

For any non-trivial code change, agents should run:

```bash
make lint-tools   # one-time; idempotent, builds the pinned linter into ./.cache/bin
make devcheck
```

For formatting-only maintenance or before large refactors:

```bash
make fmt
```

For pull requests, agents should include validation commands run in the PR summary.
