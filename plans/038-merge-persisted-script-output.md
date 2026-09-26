# Plan 038: Merge durable script output without losing other runs

> **Executor instructions:** Follow each step and its verification. This is a self-contained implementation handoff; no audit-session context is assumed. Honor the STOP conditions. No commits or pushes are authorized.
>
> **Drift check (run first):**
> ~~~sh
> git diff --stat 7c530ee..HEAD -- internal/process/script_output.go internal/process/script_output_store.go internal/process/script_output_store_test.go internal/process/scripts.go internal/data/script_transcript_store.go internal/data/script_transcript_store_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git diff HEAD -- internal/process/script_output.go internal/process/script_output_store.go internal/process/script_output_store_test.go internal/process/scripts.go internal/data/script_transcript_store.go internal/data/script_transcript_store_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git status --short
> ~~~
> Compare BOTH commit changes and the current working tree against the Current state excerpts below. The audited baseline intentionally includes dirty user work on 2026-09-26. A known dirty file is not automatically a mismatch: preserve the audited behavior and the user's additional changes. On unexplained semantic drift, stop and report. New files will appear only in status, so inspect them before creating a same-named file.

## Status

- **Priority:** P1
- **Effort:** M
- **Risk:** LOW
- **Depends on:** none
- **Category:** bug
- **Planned at:** commit 7c530ee, 2026-09-26, INCLUDING the known dirty working tree (run-session picker/transcript-browser changes).
- **Status:** TODO

## Why this matters

A new runner begins with no in-memory transcripts. Recording an on-done hook after restart currently overwrites the durable envelope with that single type, erasing the previous setup/archive diagnosis before the viewer has hydrated it. The write path also overwrites newer schema versions, and viewer hydration can replace a newer in-memory result with a stale disk result.

## Current state

- internal/process/script_output_store.go:80–96:
~~~go
r.transcriptMu.Lock()
defer r.transcriptMu.Unlock()
r.mu.Lock()
outputs := r.outputsForLocked(ws)
r.mu.Unlock()
// ... directory creation and marshal ...
if err := fsatomic.WriteFile(filepath.Join(dir, scriptTranscriptsFilename), raw, 0o600); err != nil {
    logging.Warn("could not persist script transcripts workspace_root=%s error=%v", ws.Root, err)
}
~~~
The mutex is per runner; it does not serialize independent amux processes.
- internal/process/script_output.go:103–111:
~~~go
if missing := missingScriptTypes(out); len(missing) > 0 {
    for st, entry := range r.loadScriptOutputs(ws) {
        if _, have := out[st]; have {
            continue
        }
        out[st] = entry
        r.mu.Lock()
        r.lastOutput[scriptOutputKey(ws, st)] = entry
        r.mu.Unlock()
    }
}
~~~
The have check uses an old copy; a concurrent record can already have installed a newer entry.
- The v1 envelope at script_output_store.go stores latest output per script type and the reader rejects Version > 1. ScriptOutput fields are Text, Err, FinishedAt; transcripts retain a bounded 64 KiB tail.
- Follow data.WorkspaceStore's lockWorkspaceIDs and saveWorkspaceLocked pattern (internal/data/workspace_store_locking.go:13; workspace_store.go:281) and fsatomic.WriteJSON. The data package already owns cross-process flock/inode handling; do not duplicate raw flock code in process.
- Stable identity is ws.MetadataID(); WorkspaceIdentitySet supplies legacy lookup aliases. Transcript persistence is best effort: log a warning and keep the script outcome unchanged. Private state is 0700 directories / 0600 files.

## Behavior decisions

Use one cross-process transaction under the existing workspace lock set, loading the primary plus identity-alias envelopes and merging latest FinishedAt per script type. On equal timestamp preserve the already-persisted value unless it is byte-for-byte equal; do not make map iteration choose a winner. Missing files are empty. Corrupt, unreadable, or newer-version existing files refuse the write and remain byte-identical; this plan intentionally makes diagnostic persistence fail conservatively rather than replace unknown data. No transcript history or new schema version is added.

Keep lifecycle output types usable by process without an import cycle: define a small data.ScriptTranscript record with the existing JSON fields and alias process.ScriptOutput to it, converting map keys between ScriptType and string at the boundary. A data.ScriptTranscriptStore owns the envelope I/O; it may use the package-private workspace lock helpers. It must not manufacture workspace metadata, and writes require an existing valid workspace.json under the primary identity. Memory-only runners still work when transcriptRoot is unset.

## Steps

### Step 1: Pin restart overwrite with realistic metadata

Read script_output_store_test.go and its RestartRead/DriftedIdentityDir fixtures. Change persistent fixtures to save an actual temporary workspace record before recording output; memory-only fixtures remain unchanged. Add restart → record another type → read, preserving both values, and a future-schema overwrite refusal case. Add corrupt/read-failure byte-preservation assertions without including any real transcript contents in diagnostics.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/process -run 'Test.*(ScriptOutput|ScriptTranscripts)' -count=1 → existing fixtures pass; the newly isolated restart-write/newer-write regressions fail only at the expected lost-entry/overwritten-file assertions until Step 2.

### Step 2: Introduce the data-owned transcript transaction and migrate writes

Create script_transcript_store.go with v1 decode/validate, read/merge/write, and primary/alias identity handling. Acquire all needed workspace ID locks in their existing sorted order; reload envelopes after locking. Verify workspace.json still exists and no delete tombstone is present before writing, so a late hook cannot recreate a removed record. Do not call WorkspaceStore.Save or a nested locking method while these locks are held.

Move only durable storage concerns into the data store; retain the runner's output capture/listener behavior. Write via fsatomic and propagate a wrapped error to the existing warning boundary. Two independent store/runner instances must merge distinct types correctly. Newer schemas in any candidate envelope cause a warning/no write, not fallback overwrite. Preserve the on-disk v1 JSON field names.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/data ./internal/process -run 'Test.*(Transcript|ScriptOutput)' -count=1 → all preservation, restart, deletion, identity-alias, and private-permission assertions pass.

### Step 3: Make hydration freshness-safe

After reading disk, reacquire r.mu and compare each candidate against the current map entry, not the pre-read copy. Keep a newer in-memory entry and return the merged fresh result. Never hold r.mu over filesystem I/O. Use a deterministic barrier test around the storage accessor (a small private test seam is allowed) to schedule record between read and hydration; assert the fresh memory value survives.

Document that latest tails survive restarts and remain best effort; the corrupt/newer-schema refusal preserves prior diagnostics. Update README.md, docs/CONFIG.md, and docs/ORCHESTRATION.md consistently without advertising a new config or CLI.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test -race ./internal/data ./internal/process -run 'Test.*(Transcript|ScriptOutput)' -count=1 → no races, latest memory/disk content remains correct. Then run all final commands.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Focused package verification | env -u AMUX_WORKSPACES_ROOT go test ./internal/data ./internal/process ./internal/app/workspacesvc | All selected tests pass; no new skips |
| Race verification | env -u AMUX_WORKSPACES_ROOT go test -race ./internal/data ./internal/process | Exit 0, no race reports |
| Standard repository gate | env -u AMUX_WORKSPACES_ROOT make devcheck | Exit 0; investigate and record baseline exceptions as described below |
| Changed-code strict gate | make lint-strict-new | Exit 0, zero new issues and clean formatter diff |
| Full concurrency gate | env -u AMUX_WORKSPACES_ROOT make test-race | Exit 0, no race reports |
| Real tmux concurrency gate | env -u AMUX_WORKSPACES_ROOT make test-race-tmux | Exit 0, no race reports; report skips |
| Real input path | env -u AMUX_WORKSPACES_ROOT make verify-loop | Both real-agent keystroke tests pass without skips |
| Real tmux lifecycle suite | env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e | Exit 0; report skips/baseline blockers |
| Diff integrity | git diff --check | Exit 0 |

No dependency installation or module upgrade is needed. Format changed Go files with the repository's gofumpt-compatible tooling; make fmt is the repository formatting command, but do not accept unrelated formatting changes in this dirty checkout.

**Verification baseline:** Audit verification is not wholly green: the broad devcheck run failed in real e2e tests. An ambient AMUX_WORKSPACES_ROOT escaped the test HOME and caused a collision with the user's real workspace root; plan 059 owns that isolation defect. Until 059 lands, prefix every command below that can reach e2e with env -u AMUX_WORKSPACES_ROOT. The filtered-picker regression is separately owned by plan 036 and may affect e2e results. The real input verify-loop passed during the audit. Record exact remaining failures; do not fix unrelated failures, weaken checks, kill user sessions, or claim skipped real-tmux tests provide end-to-end validation.

## Scope

**In scope — only these paths may be changed for this implementation:**

- internal/process/script_output.go
- internal/process/script_output_store.go
- internal/process/script_output_store_test.go
- internal/process/scripts.go
- internal/data/script_transcript_store.go
- internal/data/script_transcript_store_test.go
- README.md
- docs/CONFIG.md
- docs/ORCHESTRATION.md
- plans/README.md — only this plan's status row, unless the reviewer maintains it.

**Out of scope:** all other source files; unrelated refactoring; dependency/toolchain upgrades; changes to the trust model or automatic Git push/checkout behavior; other plans' implementation. Do not expand wildcard test scope into production edits.

## Git workflow

Work on advisor/038-merge-persisted-script-output only if the operator has selected that branch; otherwise use their current checkout. Do not commit, push, open a PR, stash, clean, reset, or discard user edits without explicit instruction. Before editing, record git status --short and the existing diff. Existing dirty changes are input to this plan, not cleanup targets. Only this plan's implementation delta must fit Scope; pre-existing unrelated edits may remain. Update only this plan's row in plans/README.md at completion unless the reviewer maintains that index.

## Test plan

Use the regression cases and existing test exemplars in the steps. Tests must assert the observable outcome, not merely that a new helper was called. Use temporary repositories/metadata and isolated tmux servers, never the user's actual state. Channel/barrier-based scheduling is preferred over arbitrary pacing sleeps.

## Done criteria

- [ ] Every regression case in Steps passes with env -u AMUX_WORKSPACES_ROOT go test ./internal/data ./internal/process ./internal/app/workspacesvc.
- [ ] env -u AMUX_WORKSPACES_ROOT go test -race ./internal/data ./internal/process exits 0 with no races.
- [ ] env -u AMUX_WORKSPACES_ROOT make devcheck and make lint-strict-new were run; both pass, or the exact independently established baseline blocker is recorded and this plan remains BLOCKED rather than DONE.

- [ ] env -u AMUX_WORKSPACES_ROOT make test-race: Exit 0, no race reports.
- [ ] env -u AMUX_WORKSPACES_ROOT make test-race-tmux: Exit 0, no race reports; report skips.
- [ ] env -u AMUX_WORKSPACES_ROOT make verify-loop: Both real-agent keystroke tests pass without skips.
- [ ] env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e: Exit 0; report skips/baseline blockers.
- [ ] git diff --check exits 0.
- [ ] git diff --name-only and git status --short, compared to the captured initial state, show no implementation edits outside Scope.
- [ ] User work is preserved; no commit/push occurred; this plan's status row is updated only when all required work is complete.

## STOP conditions

- The relevant live semantics differ from the excerpts and steps for reasons not explained by the declared dependencies or audited dirty baseline.
- A verification fails twice after a focused, reasonable fix attempt.
- The change requires production files outside Scope, alters a public behavior this plan explicitly preserves, or cannot be made without discarding user edits.
- Do not introduce a process→data→process import cycle or a second lock protocol for the same workspace files.
- If production legitimately persists lifecycle transcripts without workspace.json, stop and identify the caller before relaxing the no-resurrection boundary.
- Do not store an unbounded run history, change the 64 KiB tail budget, or make transcript persistence failure fail a script.
- Identity alias files may contain separate useful transcripts; never delete them as an incidental cleanup.
- A broad baseline check fails outside scope: report the exact test/error and retain BLOCKED status; do not silently waive the gate or repair unrelated code.

## Maintenance notes

- Any future transcript schema must update both reader and writer refusal tests.
- Keep read-modify-write serialization cross-process; a runner-local mutex alone is insufficient.
- The new data store is storage-specific, not a general arbitrary-file transaction API.

