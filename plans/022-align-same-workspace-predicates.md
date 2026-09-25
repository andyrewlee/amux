# Plan 022: Align the two "same workspace" comparisons onto one canonical predicate

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/app/app_input_messages_workspace_lookup.go internal/ui/sidebar/workspace_identity.go internal/data/workspace.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: MED
- **Depends on**: none
- **Category**: tech-debt
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

Two helpers answer "are these the same workspace" with different semantics: `rootsReferToSameWorkspace` compares canonical roots only; `sameWorkspaceByCanonicalPaths` compares ID, then canonical root, then canonical repo (root equal + repos differ → *not* same). The app helper drives git-status routing (a status result addressed to workspace A could be applied to workspace B's sidebar if roots alias); the sidebar helper drives workspace rebinding. Practically rare — two live workspaces sharing a canonical root — but the pair invites drift as each gains callers, and one canonical predicate belongs at the `data` layer where identity semantics already live.

## Current state

`internal/app/app_input_messages_workspace_lookup.go:9-19` — `rootsReferToSameWorkspace(a, b string) bool`: canonicalizes both roots and compares. Callers: `internal/app/app_input_pty.go:60` (git-status routing) and the gitstatus handler path.

`internal/ui/sidebar/workspace_identity.go:7-27` — `sameWorkspaceByCanonicalPaths(a, b *data.Workspace) bool`: `ID()` fast path → canonical root compare → canonical repo compare. Drives sidebar workspace rebinding.

`internal/data/workspace.go` — the identity vocabulary already lives here: `ID()` (persisted store identity), `ComputedID()` (path-derived), `WorkspaceIdentitySet(ws)` (deduplicated forms). A canonical "same workspace" predicate fits naturally here (e.g. `data.SameWorkspace(a, b)` or `data.RootsReferToSameWorkspace`) — but the two current helpers take different input types (roots-as-strings vs workspaces); the canonical shape should accept `*data.Workspace` or `(root, repo)` — decide by looking at what both call sites actually have in hand (the app site has two root strings + an active workspace; the sidebar has two workspaces — a `data.SameWorkspaceIdentity(wsA, wsB)` plus a thin app-side adapter for its string inputs covers both without mangling signatures).

Key semantic question to resolve BEFORE writing code: is root-only matching ever *correct* for the app path? The git-status result carries `Root`; if two workspaces could legitimately share a canonical root (e.g. a workspace re-created at the same path under a different repo), root-only routing misroutes the status. If roots are guaranteed unique per live workspace, root-only is merely underspecified — the canonical predicate is still the better expression. Check how `msg.Root` is produced (`requestGitStatus*` sites) to answer.

## Commands you will need

| Purpose    | Command                                            | Expected on success |
|------------|----------------------------------------------------|---------------------|
| Build      | `go build ./internal/data ./internal/app ./internal/ui/sidebar` | exit 0 |
| Unit tests | `go test ./internal/data ./internal/app ./internal/ui/sidebar -count=1` | all pass |
| Lint       | `make lint && make lint-strict-new`                | exit 0              |
| Full gate  | `make devcheck`                                    | exit 0              |

## Scope

**In scope**:
- `internal/data/workspace.go` or `internal/data/workspace_identity*.go` — the canonical predicate (new small function).
- `internal/app/app_input_messages_workspace_lookup.go` — delegate `rootsReferToSameWorkspace` to it.
- `internal/ui/sidebar/workspace_identity.go` — delegate `sameWorkspaceByCanonicalPaths` to it.
- Tests for the canonical predicate (`internal/data/workspace_test.go` or identity test file).

**Out of scope**:
- Changing what counts as "same" — the predicate encodes the sidebar's stricter semantics (ID + root + repo); the app caller opts into it. If root-only turns out to be *deliberately* looser for git-status routing (Step 1 answers this), the alternative resolution is: keep both helpers but document WHY they differ in each file's comment — still resolves the "silent divergence" problem.
- `WorkspaceIdentitySet` internals.
- Any other same-X comparisons — don't expand.

## Git workflow

- Branch: `advisor/022-same-workspace-predicate` off `main`.
- Commit style: `refactor: unify same-workspace comparison on one data predicate`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Answer the semantic question

Trace `msg.Root` in `GitStatusResult` — `grep -rn 'GitStatusResult{' internal/app internal/git --include='*.go' | head` and find what fills `Root` (workspace root? repo root?). Then answer: can two live workspaces ever share that canonical value? Write the answer in the predicate's godoc — it's the design fact this whole plan hinges on.

**Verify**: you can state in one sentence which semantic the app path actually needs.

### Step 2: Add the canonical predicate in `internal/data`

Based on Step 1: either `func SameWorkspaceIdentity(a, b *Workspace) bool` (ID set intersection OR canonical-root+repo equality — mirror `sameWorkspaceByCanonicalPaths`'s logic, which is the stricter and correct-er of the two) and/or a root-level variant matching what the app caller needs. Keep it small — compose from the existing identity helpers (`WorkspaceIdentitySet`, canonical path helpers already in `data`).

**Verify**: `go build ./internal/data` → exit 0; unit tests for the predicate: same persisted ID → true; same root+repo → true; same root different repo → false; different everything → false. `go test ./internal/data -run 'SameWorkspace' -v` → pass.

### Step 3: Delegate both callers

- `rootsReferToSameWorkspace` becomes a thin adapter: canonicalize strings as today (it only has strings — if the predicate needs workspaces, either the app site constructs the canonical comparison from its strings via the same shared helper, or the predicate gets a `(rootA, repoA, rootB, repoB)` shape — pick what reads cleanest; document the choice).
- `sameWorkspaceByCanonicalPaths` delegates to the predicate outright (keep the function name if callers prefer it, body becomes `return data.SameWorkspaceIdentity(a, b)`).

**Verify**: `go build ./...` → exit 0; `go test ./internal/app ./internal/ui/sidebar -count=1` → pass.

### Step 4: Gates

**Verify**: `make devcheck` → exit 0; `make lint-strict-new` → exit 0.

## Test plan

- New: predicate unit tests (Step 2 table) in `internal/data`.
- Existing: sidebar identity tests + app gitstatus routing tests must pass unchanged — they pin the semantics; if one now fails, the semantic you chose diverged from a caller's assumption → STOP.

## Done criteria

- [ ] One canonical same-workspace predicate exists in `internal/data` with godoc stating the semantic contract.
- [ ] Both helpers delegate to it (or, if root-only proved deliberate, both carry a comment naming why they differ — the divergence is then explicit, not silent).
- [ ] `go test ./internal/data ./internal/app ./internal/ui/sidebar -count=1` exits 0.
- [ ] `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `msg.Root` in `GitStatusResult` is a repo root, not workspace root (Step 1 flips the assumption) — the whole comparison basis changes; report before proceeding.
- A test fails because a caller relies on root-only looseness — document the intentional divergence per the alternative resolution and do NOT unify.
- The predicate needs identity forms that require filesystem work (`ComputedID` stats paths) — the predicate must take whatever's already resolved at call sites; if it forces disk I/O on the Update goroutine, redesign (or report).

## Maintenance notes

- Future "same workspace" questions must go through `data.SameWorkspaceIdentity` (or its documented root-level sibling) — the two-helper pattern is what rotted.
- The semantic contract in the godoc is the artifact that matters — reviewers should check it against the `msg.Root` producer.
