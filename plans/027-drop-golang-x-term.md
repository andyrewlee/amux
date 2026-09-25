# Plan 027: Switch `fakeagent` from `golang.org/x/term` to `charmbracelet/x/term` and drop the duplicate dep

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/e2e/fakeagent/main.go go.mod go.sum`
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

Two terminal-utility modules do the same job: production uses `github.com/charmbracelet/x/term` (`cmd/amux/main.go:17`) while the e2e fake agent uses `golang.org/x/term` (`internal/e2e/fakeagent/main.go:32`). The charmbracelet module exports a superset (`IsTerminal`, `MakeRaw`, `GetState`, `Restore` — verified in its source) and is already required transitively by bubbletea v2.0.9, so switching the one file removes a module from the tree entirely.

## Current state

- `internal/e2e/fakeagent/main.go:32` — `import "golang.org/x/term"`; uses `term.IsTerminal`, `term.MakeRaw`, `term.Restore`.
- `cmd/amux/main.go:17` — `import "github.com/charmbracelet/x/term"`.
- `github.com/charmbracelet/x/term` v0.2.2 (already in go.mod): exports `IsTerminal(fd uintptr) bool`, `MakeRaw(fd uintptr) (*State, error)`, `GetState(fd uintptr)`, `Restore(fd uintptr, state *State) error` — check signatures: charm's take `uintptr` fds and return `*State`, vs x/term's `int` fds + `State`/`*State` — the call sites need the fd cast (`os.Stdin.Fd()` returns `uintptr` already — `golang.org/x/term` APIs take `int`! Check the actual calls; the conversion is likely `int(os.Stdin.Fd())` → just drop the cast, or pass `os.Stdin.Fd()` directly).

## Commands you will need

| Purpose    | Command                                          | Expected on success |
|------------|--------------------------------------------------|---------------------|
| Build      | `go build ./internal/e2e/fakeagent`              | exit 0              |
| Tidy       | `go mod tidy`                                    | `golang.org/x/term` gone from go.mod direct requires |
| Tidy check | `make tidy-check` (or whatever the CI gate is called — grep Makefile) | exit 0 |
| E2E        | `go test ./internal/e2e -count=1` (fakeagent is the e2e binary) | pass or documented skip |
| Verify-loop| `make verify-loop` (input path uses the fake agent) | exit 0 — the honest end-to-end check |

## Scope

**In scope**:
- `internal/e2e/fakeagent/main.go` — import + signature adjustments.
- `go.mod`/`go.sum` — via `go mod tidy`.

**Out of scope**:
- `cmd/amux/main.go` — already on the right module.
- Any other dep — `atotto/clipboard` is plans/028.

## Git workflow

- Branch: `advisor/027-drop-x-term` off `main`.
- Commit style: `chore: switch fakeagent to charmbracelet/x/term`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Swap the import and fix signatures

Change the import to `charm.land/x/term`... — WAIT, get the import path right first: `grep 'charmbracelet/x/term' go.mod` for the module path (it's `github.com/charmbracelet/x/term` per cmd/amux/main.go:17). Then adjust calls:

- `term.IsTerminal(int(os.Stdin.Fd()))` → `term.IsTerminal(os.Stdin.Fd())` (or per actual call shape — read the file's three call sites).
- `oldState, _ := term.MakeRaw(int(fd))` → `state, _ := term.MakeRaw(fd)` noting charm returns `*State`.
- `term.Restore(int(fd), oldState)` → `term.Restore(fd, state)`.

Verify each signature against the module source (`go doc github.com/charmbracelet/x/term.IsTerminal` etc. or read the module cache file the auditor verified at `term.go:11-35`).

**Verify**: `go build ./internal/e2e/fakeagent` → exit 0.

### Step 2: Tidy

`go mod tidy` → `golang.org/x/term` should leave the direct require block (it may remain as an indirect dep of something else — check `go mod why golang.org/x/term`; if nothing imports it, it's gone entirely).

**Verify**: `grep -n 'golang.org/x/term' go.mod` → absent or `// indirect` with a real consumer (`go mod why -m golang.org/x/term`).

### Step 3: End-to-end

**Verify**: `go test ./internal/e2e -count=1` → pass/skip-documented; `make verify-loop` → exit 0 (this exercises the fake agent's raw-mode path — the actual behavioral check).

### Step 4: Gates

**Verify**: `make devcheck` → exit 0; `make tidy-check` if it's a separate target (`grep -n 'tidy' Makefile .github/workflows/ci.yml`).

## Test plan

- No new tests — the fake agent's behavior is exercised by `internal/e2e` + `make verify-loop` end-to-end. A raw-mode regression surfaces there.

## Done criteria

- [ ] `fakeagent` imports `charmbracelet/x/term`; zero `golang.org/x/term` imports remain repo-wide.
- [ ] `go mod tidy` removes the module (or leaves it only as a genuinely-needed indirect).
- [ ] `go test ./internal/e2e`, `make verify-loop`, `make devcheck` all pass.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- charm's `x/term` lacks a needed API (e.g. `GetState` shape differs) — fall back to keeping `golang.org/x/term` and report; the dep isn't worth a behavioral shim.
- `go mod tidy` leaves `golang.org/x/term` as a *direct* require via another file — find and assess that importer (`grep -rn 'golang.org/x/term' --include='*.go' .`).
- `make verify-loop` fails on the raw-mode path — the signatures didn't map cleanly; report rather than papering over it.

## Maintenance notes

- New terminal-utility needs: use `github.com/charmbracelet/x/term` — the charm stack pins it; don't reintroduce `golang.org/x/term`.
- `go mod tidy` in the same commit keeps go.sum minimal — reviewers should see only the fakeagent diff + mod/sum churn.
