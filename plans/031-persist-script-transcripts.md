# Plan 031: Persist lifecycle-script transcripts to the workspace metadata dir

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/process/script_output.go internal/data/ internal/fsatomic/ internal/app/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: direction
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`O` (the setup/archive/on-done output viewer) reads from `r.lastOutput` — an in-memory map that dies with the process. A restart erases every recorded transcript, so "why did setup fail yesterday" and "what did archive emit before that shelve" are unanswerable exactly when they matter (post-incident). Per-workspace durable storage already exists (`~/.amux/workspaces-metadata/<id>/workspace.json`) and `internal/fsatomic` provides crash-safe writes — persisting the same bounded tail makes `O` truthful across restarts.

## Current state

`internal/process/script_output.go`:

```go
// recordScriptOutput stores the latest transcript for the workspace+type.
// Successful runs with no output are not recorded — an empty transcript
// would only render as a blank section in the viewer.
func (r *ScriptRunner) recordScriptOutput(ws *data.Workspace, scriptType ScriptType, text string, runErr error) {
	if text == "" && runErr == nil {
		return
	}
	entry := ScriptOutput{Text: text, FinishedAt: time.Now()}
	...
	r.lastOutput[scriptOutputKey(ws, scriptType)] = entry    // :74 — memory only
}
```

- `:91` — `lastOutput` iteration; entries are bounded (`64KiB`? — check the cap constant in the file) and keyed `workspace|scriptType`.
- `ScriptRunner` gets its metadata dir context from `data` — find how `ws` maps to `~/.amux/workspaces-metadata/<id>/` (`data.WorkspaceStore`'s metadata root + `ws.MetadataID()`/`WorkspaceIdentitySet` — the store knows the dir per record; check what `ScriptRunner` is constructed with — `NewScriptRunner` signature and where the metadata root could be injected, likely via `Deps` or a `store` ref already present).
- `internal/fsatomic` — `WriteJSON` for crash-safe writes; transcripts are plain text — `fsatomic` may have a `Write`/`WriteFile` for non-JSON, or write via temp+rename manually matching its semantics (`grep -rn 'func ' internal/fsatomic/*.go | grep -v _test`).
- Where `O` reads: find `LastOutput`/`ScriptOutputFor` accessor (`grep -rn 'lastOutput\|ScriptOutput' internal/process internal/app --include='*.go' | grep -v _test | grep 'func'`) — the read path needs a disk fallback.

Design notes:

- Retention: mirror the in-memory bound — last run per (workspace, scriptType), same size cap. A N-run ring is a possible upgrade; keep v1 to "latest per type" for parity (the viewer's current semantic).
- Layout: `<metadataDir>/scripts/<type>.log` or a single `script-output.json` envelope — prefer the envelope (one atomic write, versionable) matching the store's JSON conventions.
- Identity: write under the durable identity dir — `ws.MetadataID()` (the persisted key used for the metadata dir — verify which ID form `WorkspaceStore` uses for its dir naming; using `WorkspaceIdentitySet` for the *read* fallback covers drifted IDs).
- The `O` view must prefer memory (fresher) then disk, or just disk — simplest correct: memory-first, disk-fallback (memory is written first anyway).

## Commands you will need

| Purpose    | Command                                        | Expected on success |
|------------|------------------------------------------------|---------------------|
| Build      | `go build ./internal/process ./internal/app`   | exit 0              |
| Unit tests | `go test ./internal/process ./internal/app -count=1` | all pass        |
| Lint       | `make lint && make lint-strict-new`            | exit 0              |
| Full gate  | `make devcheck`                                | exit 0              |

## Scope

**In scope**:
- `internal/process/script_output.go` — write-through + read-fallback.
- `internal/process/` — a small `script_output_store.go` (new file if the persistence deserves one; the package splits by concern).
- Constructor wiring — wherever `ScriptRunner` gets its deps, add the metadata-root resolution (probably already has `data.WorkspaceStore` or the home dir — check `NewScriptRunner`'s signature).
- Tests.

**Out of scope**:
- `O` viewer UI changes — it reads via the accessor; the disk fallback is inside the accessor.
- Historical/multi-run rings — v1 is latest-per-type parity.
- `run`-session output (lives in tmux `remain-on-exit` panes — different mechanism; not this plan).
- Transcript viewer for `~/.amux/transcripts` — plans/033.

## Git workflow

- Branch: `advisor/031-persist-script-transcripts` off `main`.
- Commit style: `feat: persist lifecycle script transcripts across restarts`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Locate the metadata-root injection point

Read `NewScriptRunner`'s signature and construction site(s) (`grep -rn 'NewScriptRunner' internal/ cmd/`). Determine how to reach the per-workspace metadata dir: `data.WorkspaceStore` exposes metadata paths? (`grep -rn 'metadataDir\|MetadataDir\|metadataRoot' internal/data --include='*.go' | head`). If `ScriptRunner` doesn't have a store/home reference, add the minimal dep (a `dirResolver func(ws) string` func field or the store itself — match how other deps are injected).

**Verify**: you can write `metadataPathFor(ws, scriptType)` as a pure function.

### Step 2: Write-through on record

In `recordScriptOutput`, after updating `lastOutput`, persist the same `{type -> {text, finishedAt}}` map for the workspace — an envelope `script-output.json` in the workspace's metadata dir via `fsatomic.WriteJSON`. Errors → `logging.Warn` (transcript loss must not fail the script path). Keep the 64KiB (or whatever) text cap identical to memory.

**Verify**: `go build ./internal/process` → exit 0; a unit test asserting the file appears with correct content after `recordScriptOutput`.

### Step 3: Read fallback on the accessor

The `O`-serving accessor (find it — likely `LastScriptOutput`/`ScriptOutputs(ws)`): on memory miss, load the envelope (identity-set lookup for drifted dir names — check how `findStoredWorkspace`/metadata discovery resolves dirs to reuse the pattern), populate memory, return. Corrupt/missing file → empty, no error (same posture as other stores' read path — fail closed on diagnostics).

**Verify**: `go test ./internal/process -run 'ScriptOutput' -v` → new cases pass (memory-hit, disk-fallback, corrupt-file, drifted-ID dir).

### Step 4: Version the envelope

`Version int` field in the file envelope, reject `> 1` on read — matching the repo's store convention (see `project_env.go`'s envelope pattern — and plans/002's guard applies to writes if the store is ever versioned later).

**Verify**: `go test ./internal/process ./internal/app -count=1` → pass; `make devcheck` → exit 0.

## Test plan

- New: write-through content, restart-equivalent read (fresh runner, pre-seeded file), corrupt file → empty, wrong-ID dir → fallback resolves via identity set.
- Structural pattern: `internal/data/store_version_test.go` for version fixtures; existing `script_output` tests if any.
- Edge: workspace deleted → metadata dir may be pruned — reading should degrade silently (already the posture).

## Done criteria

- [ ] `recordScriptOutput` persists transcripts under the workspace metadata dir atomically.
- [ ] The `O` read path serves transcripts across process restarts.
- [ ] In-memory map remains the fast path; disk is fallback.
- [ ] `go test ./internal/process ./internal/app -count=1` exits 0; `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `ScriptRunner` can't reach a metadata-root dep without a signature change rippling through many callers — report the minimal wiring needed.
- The metadata-dir naming isn't stable per `MetadataID` (e.g. uses `ComputedID` and drifts) — resolve via `WorkspaceIdentitySet` or report.
- `fsatomic` lacks a suitable write primitive and adding one is more than a few lines — write temp+rename inline matching its semantics, or report.
- Persisting transcripts conflicts with the prune logic (`PruneStale` may delete the metadata dir) — that's intended (transcripts die with their workspace); if tests reveal otherwise, report.

## Maintenance notes

- Envelope versioning future-proofs a multi-run ring — when that lands, version bump + read branch per the repo convention.
- Transcripts can contain anything the script printed (incl. accidental secrets) — file perms must match the metadata dir's (check what `WorkspaceStore` sets, likely 0700/0600 — match it).
- Reviewer focus: the identity-set dir resolution (drifted IDs) and that script failures never block on transcript writes.
