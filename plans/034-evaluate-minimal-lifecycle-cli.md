# Plan 034: Evaluate a minimal read-only lifecycle surface for external orchestrators (spike)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- docs/ORCHESTRATION.md cmd/amux/main.go internal/app/workspacesvc/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: direction
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters — and why it might not

`docs/ORCHESTRATION.md` deliberately removed the CLI (PR #204): an external orchestrator drives agents through tmux seams. But the tmux seam only reaches sessions that *already exist* — an orchestrator can't create a workspace (worktree + metadata + `AMUX_PORT` + setup hooks), shelve/restore one, or enumerate workspaces with lifecycle state. The doc itself records a revisit trigger: "Revisit this only when a specific orchestrator requirement is demonstrably unmet by tmux — name that requirement." This plan is a **spike, not a build**: answer whether the trigger is met, and if so, land the smallest honest read-only surface (`ls --json`-style). Do NOT build a write surface — provisioning is the heavier half and needs product sign-off first.

## Current state

- `docs/ORCHESTRATION.md:11-16` — CLI removal rationale; `:266-282` — "Option B: a minimal CLI (recorded, not recommended)… Revisit this only when a specific orchestrator requirement is demonstrably unmet by tmux."
- `cmd/amux/main.go:40-43` — rejects all args today.
- `internal/app/workspacesvc` — concentrates lifecycle behind `Deps`-injected seams, but methods return `tea.Cmd` — a non-TUI caller needs an adapter (the service layer is the honest seam, but its cmd-shaped API means the spike must either (a) extract the non-cmd core, or (b) read the `data` stores directly for read-only listing — (b) is dramatically smaller).
- The tmux seam's shape for reference: `ORCHESTRATION.md` documents session names/tags — an orchestrator today does `tmux send-keys` to existing sessions only.

**Spike questions to answer before ANY code:**

1. What concrete requirement is unmet? (Enumerate: create-workspace? list-with-state? per-workspace script status?) Without a named consumer, the doc says don't build.
2. Is `~/.amux` state readable without the app running — i.e. is the ground truth in the metadata dir + tmux tags sufficient for a read-only `ls`? (`internal/data` store files + `FindRunSessions`-style tmux reads suggest yes.)
3. If a create surface is eventually wanted: does `workspacesvc` expose enough non-cmd machinery, or does it need a `tea.Cmd`-free core extracted? (Assess, don't implement.)

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Read    | `cat docs/ORCHESTRATION.md` | the revisit-trigger text |
| Probe   | `go build ./internal/data` + a scratch `go run` reading `NewWorkspaceStore(<home>)` `.ListAll()` | store is readable standalone |
| Test    | `go test ./internal/data -count=1` | all pass |

## Scope

**In scope** (spike + minimal landing):
- A short findings writeup (in the PR description or `docs/orchestration-cli-proposal.md` only if reviewers prefer a doc — ask first; do NOT add a doc file unprompted).
- IF the read-only case is justified: `cmd/amux` gains an `ls`/`status` arg path producing JSON from `data` stores + tmux tags — implemented in a NEW file (e.g. `cmd/amux/ls.go`), NOT by mutating the TUI entry path.
- A test for the JSON surface.

**Out of scope**:
- Any create/shelve/write command — trigger-condition not met by a listing; that's a separate product decision.
- Changing `workspacesvc`'s `tea.Cmd` shape for a hypothetical future CLI — assessment only.
- Any behavior change to the TUI's startup path (`main.go` arg rejection stays for TUI-mode invocation).

## Git workflow

- Branch: `advisor/034-lifecycle-cli-spike` off `main`.
- Commit style: `feat: read-only workspace listing for external orchestrators` (only if landing) — otherwise no commit, findings only.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Answer the spike questions

Read `docs/ORCHESTRATION.md` fully + the store APIs (`internal/data/workspace_store.go` `ListAll`, `ByRepo`). Confirm a standalone reader sees: workspaces, roots, archived/shelved state, and live/dead tmux status per `FindRunSessions`/`SessionStateFor`-style probes.

**Verify**: you can produce the JSON schema on paper that an orchestrator actually needs — write it in the PR/workup. If "just tmux tags" suffice → the honest answer may be "no amux change needed — document the tmux read pattern instead". That's a valid spike outcome.

### Step 2: If landing read-only, implement `ls`

`cmd/amux/main.go` arg handling: today rejects all args — add a guarded branch for exactly `ls`/`status` (explicit list, everything else still rejected) that loads `config.DefaultPaths` → `data.NewWorkspaceStore(home)` → `ListAll` → joins tmux state → prints JSON, exits. No TUI init, no supervisor, no tmux writes (read probes only).

**Verify**: `amux ls` prints valid JSON; `amux` (no args) still launches TUI; `amux bogus` still errors.

### Step 3: Test

`cmd/amux` test or an e2e-ish exec test: fixture metadata dir + `ls` → parseable JSON with expected fields. Check how `cmd/amux` is tested today (`ls cmd/amux/*_test.go`).

**Verify**: `go test ./cmd/amux -count=1` → pass; `make devcheck` → exit 0.

### Step 4: Document the boundary

One paragraph in `docs/ORCHESTRATION.md` under the Option-B section: what the read-only surface is, what it deliberately doesn't do, and the trigger condition that remains for write paths.

**Verify**: doc updated; `make devcheck` → exit 0.

## Test plan

- New: `ls` JSON test (fixture store → expected fields).
- Existing: nothing broken — the TUI arg-rejection for all other args must still hold (test or manual `amux foo`).

## Done criteria

- [ ] The spike questions are answered in the PR/writeup with the named unmet requirement (or the recommendation is "don't build").
- [ ] If implemented: `amux ls` emits JSON, touches nothing but reads; all other args still rejected.
- [ ] `go test ./cmd/amux`, `make devcheck` pass.
- [ ] `ORCHESTRATION.md` documents the surface's boundary.
- [ ] `plans/README.md` status row updated (or REJECTED with the spike's verdict recorded).

## STOP conditions

- No named orchestrator requirement emerges — the doc's own gate says don't build; close the spike with the writeup and mark the plan REJECTED-by-verdict.
- Store reading requires app-level secrets/locks that make a sidecar reader unsafe (flock contention with a running amux — check `internal/data` lock discipline: stores use per-file flocks; a read-only `ListAll` may contend — verify before shipping `ls` alongside a running app).
- Any write capability creeps in — that's a product decision requiring explicit maintainer sign-off; STOP.

## Maintenance notes

- The revisit trigger stays in force: `ls` answers enumeration; `create`/`shelve` remain gated on a named requirement.
- `tea.Cmd`-shaped `workspacesvc` methods stay as-is — extracting a cmd-free core is a refactor justified only by the write path landing.
- If `ls` ships: its JSON schema is a compat surface — version it (`"version": 1` field) from day one.

## Spike verdict — 2026-09-26 (executed on `advisor/all-plans`)

**Verdict: don't build.** The doc's revisit gate — "a specific orchestrator
requirement demonstrably unmet by tmux; name that requirement" — is not met:
no orchestrator consumer exists in the project or the request that spawned
this plan. Convenience is explicitly insufficient.

Spike answers:

1. **Candidate unmet requirement**: "enumerate workspaces including ones with
   no live tmux session." It is the only real gap — shelved/archived/dead
   workspaces mint no sessions, so `@amux_workspace` sweeps never see them.
   But it is not *demonstrably* unmet: `~/.amux/projects.json` and
   `~/.amux/workspaces-metadata/<id>.json` are plain JSON on disk, and
   `data.NewWorkspaceStore(home).ListAll()` reads them without the app, with
   no write lock (per-workspace flocks guard only mutation paths). Live-state
   joins are `tmux list-sessions -F` on the documented tags.
2. **Standalone readability**: yes — verified `WorkspaceStore.ListAll`,
   `Registry.Projects`, and record fields (`Archived`, `Shelved`,
   `ArchivedAt`, `Repo`, `Root`, script config) are all public API + plain
   JSON. No secrets or app-held locks gate reads.
3. **Write-path shape**: `workspacesvc` is `tea.Cmd`-shaped end to end — a
   future create/shelve CLI needs a cmd-free core extracted first. That
   refactor stays gated on a named requirement, unchanged.

**When a real consumer names the requirement**: either (a) document the
projects.json + tmux-tags read pattern as a compat surface, or (b) if the
on-disk schema isn't acceptable as a contract, land `amux ls --json`
(versioned `"version": 1`) reading `data.NewWorkspaceStore` +
`tmux.FindRunSessions` — read-only, no TUI init, in a new `cmd/amux/ls.go`.
Either way the trigger record lives here, not in a shipped surface.
