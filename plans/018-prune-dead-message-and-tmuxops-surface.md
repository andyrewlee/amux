# Plan 018: Delete dead exported message types and unreachable `TmuxOps` methods

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/messages/messages.go internal/app/services.go internal/app/service_tmux.go internal/tmux/clients.go internal/tmux/activity.go internal/testutil/tmuxops/`
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

Post-refactor drift left dead API surface: four message types (`ProjectAdded`, `FocusPane`, `CreateAgentTab`, `SwitchTab`) with zero construction or handling sites invite readers to assume live flows that nothing implements — and `ProjectRemoved` IS live (`app_input_dispatch.go:264`), so the pair reads as intentional symmetry it isn't. On the interface side, `TmuxOps` declares `SessionStateFor`, `SessionHasClients`, `SessionCreatedAt`, `ContentHash` with no production caller — every fake/stub must implement them, and three package-level exports (`tmux.SessionCreatedAt`, `tmux.SessionActiveWithin`, `tmux.SessionLatestActivity`) are dead weight misleading readers about live call paths.

## Current state

- `internal/messages/messages.go:78` — `ProjectAdded`; `:113` — `FocusPane`; `:118` — `CreateAgentTab`; `:182` — `SwitchTab`. Repo-wide grep finds only the type declarations (the `TestCreateAgentTab*` names in `internal/ui/center/model_tabs_test.go` exercise center functions, not the message).
- `internal/app/services.go:25-54` — `TmuxOps` interface; dead slots at `:38-40` (`SessionStateFor`, `SessionHasClients`, `SessionCreatedAt`) and `:53` (`ContentHash`).
- `internal/app/service_tmux.go:41-49, 88-89` — forwarding impls for the dead methods.
- `internal/testutil/tmuxops/fakes.go:232` — fakes implementing the dead methods.
- `internal/tmux/clients.go:38` — `SessionCreatedAt` reachable only via the dead interface method.
- `internal/tmux/activity.go:60` — `SessionActiveWithin`: no production callers at all. `:79` — `SessionLatestActivity`: kept deliberately as a test cross-check per its own comment — KEEP unless the comment is stale (re-verify).
- NOTE: `tmux.SessionStateFor`/`tmux.SessionHasClients` (package functions) DO have direct callers (e2e/sidebar paths) — only the *interface methods* die, not the package functions. Verify with `grep -rn 'tmux\.SessionStateFor\|tmux\.SessionHasClients' internal/ cmd/`.

Verification commands before cutting:
- `grep -rn 'messages\.ProjectAdded\|messages\.FocusPane\|messages\.CreateAgentTab\|messages\.SwitchTab\|\bFocusPane{\|\bSwitchTab{\|\bProjectAdded{\|\bCreateAgentTab{' internal/ cmd/ --include='*.go'`
- `grep -rn '\.SessionStateFor(\|\.SessionHasClients(\|\.SessionCreatedAt(\|\.ContentHash(' internal/ --include='*.go' | grep -v 'func '` — distinguish interface-method calls from package-function calls by receiver.

## Commands you will need

| Purpose    | Command                             | Expected on success |
|------------|-------------------------------------|---------------------|
| Build      | `go build ./...`                    | exit 0              |
| Tests      | `go test ./internal/messages ./internal/app ./internal/tmux -count=1` | all pass |
| Vet        | `go vet ./...`                      | clean               |
| Lint       | `make lint && make lint-strict-new` | exit 0              |
| Full gate  | `make devcheck`                     | exit 0              |

## Scope

**In scope**:
- `internal/messages/messages.go` — delete the four dead types.
- `internal/app/services.go` — delete the four dead interface methods.
- `internal/app/service_tmux.go` — delete their forwarding impls.
- `internal/testutil/tmuxops/fakes.go` — delete fake impls.
- `internal/tmux/clients.go` — delete `SessionCreatedAt` only if truly unreachable after the interface trim.
- `internal/tmux/activity.go` — delete `SessionActiveWithin` only if zero callers remain; KEEP `SessionLatestActivity` per its documented cross-check role.

**Out of scope**:
- `tmux.SessionStateFor`/`tmux.SessionHasClients` package functions — they have direct callers; keep.
- Any other interface cleanup — don't expand.
- `ProjectRemoved` — live.

## Git workflow

- Branch: `advisor/018-dead-surface-prune` off `main`.
- Commit style: `chore: remove dead message types and TmuxOps methods`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Delete the dead message types

Remove `ProjectAdded`, `FocusPane`, `CreateAgentTab`, `SwitchTab` from `internal/messages/messages.go` (and any per-type helpers/comments exclusively attached to them).

**Verify**: `go build ./...` → exit 0 (any surprise caller fails to compile — that's the check; if compile fails, the type wasn't dead → put it back and note it).

### Step 2: Delete the dead `TmuxOps` methods

Remove the four methods from `internal/app/services.go`, their impls in `service_tmux.go`, and fake impls in `internal/testutil/tmuxops/fakes.go`.

**Verify**: `go build ./...` → exit 0; `go test ./internal/app -count=1` → pass (fakes compile-clean).

### Step 3: Delete newly-unreachable package exports

After step 2, re-grep for callers of `tmux.SessionCreatedAt` and `tmux.SessionActiveWithin`. Delete only those with zero remaining callers. KEEP `SessionLatestActivity` (documented cross-check — verify its comment is still accurate; if the cross-check test was deleted, it's dead too → delete and note).

**Verify**: `go build ./... && go vet ./...` → clean.

### Step 4: Gates

**Verify**: `go test ./internal/messages ./internal/app ./internal/tmux -count=1` → pass; `make devcheck` → exit 0; `make lint-strict-new` → exit 0.

## Test plan

- No new tests — deletion verified by compilation + existing suite. The suite itself is the regression net: if a test referenced a deleted type it fails to compile and you reconsider.
- Verification: `go test ./internal/messages ./internal/app ./internal/tmux -count=1` → all pass.

## Done criteria

- [ ] The four message types, four interface methods, and the unreachable exports are deleted.
- [ ] `go build ./...`, `go vet ./...`, `go test ./internal/messages ./internal/app ./internal/tmux` all clean.
- [ ] `make devcheck`, `make lint-strict-new` exit 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- Any "dead" type/method turns out to have a caller (compile failure or grep hit missed at audit) — restore it and record which caller was found.
- `SessionLatestActivity`'s documented cross-check no longer exists — it becomes dead too; deleting it is in-spirit but note it in the PR.
- The interface is consumed by an out-of-tree plugin — impossible for `internal/` packages in Go, but if a `//go:linkname`-style hack exists, report.

## Maintenance notes

- Dead-surface audits age: the message catalog and `TmuxOps` are the two places drift accumulates post-refactor — a periodic `grep` for zero-caller types is cheap; don't institutionalize it as CI unless it recurs.
- Reviewer focus: confirm `ProjectRemoved` retained (asymmetry is the tell that `ProjectAdded` was genuinely dead, not just unwired).
