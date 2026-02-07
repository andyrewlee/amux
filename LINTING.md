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
- lint policy files (`.golangci.yml`, `.golangci.strict.yml`, `LINTING.md`, `Makefile`, `.github/workflows/ci.yml`):
  - call out intent in PR summary

## Agent Workflow

For any non-trivial code change, agents should run:

```bash
make devcheck
```

For formatting-only maintenance or before large refactors:

```bash
make fmt
```

For pull requests, agents should include validation commands run in the PR summary.
