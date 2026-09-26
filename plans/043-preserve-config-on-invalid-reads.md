# Plan 043: Preserve existing configuration when a save cannot read it safely

> **Executor instructions:** Execute only this scoped fix and its tests. Run each verification. Stop on the conditions below. Do not commit or push. Update this plan's index row when complete unless the dispatcher maintains it.
>
> **Drift check first:** `git diff --stat 7c530ee..HEAD -- internal/config/config.go internal/config/user_settings.go internal/config/config_save.go internal/config/config_save_test.go internal/config/assistants_save_test.go internal/config/user_settings_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md`; then run `git diff HEAD --` with that same path list and `git status --short`. Compare the excerpts below, not just HEAD. The 2026-09-26 working tree already contained user edits, including README; preserve them.

## Status

- **Priority:** P1
- **Effort:** S
- **Risk:** LOW
- **Depends on:** none
- **Category:** bug / persistence
- **Planned at:** commit `7c530ee`, 2026-09-26, including the known dirty tree

## Why this matters

Both configuration save paths ignore read failures and can overwrite an existing configuration with a partial document. A top-level JSON `null` instead causes a nil-map assignment panic. App.Update recovers this panic, but the save aborts after settings may already have changed in memory. A save must reject inputs it cannot preserve and leave the original file unchanged.

## Current state

`internal/config/user_settings.go:84` starts the UI read-modify-write operation with:

```go
payload := map[string]any{}
if existing, err := readConfigPath(path); err == nil && len(bytes.TrimSpace(existing)) > 0 {
```

`internal/config/config.go:301` repeats that pattern for assistants. Both unmarshal into `payload`; `null` yields nil without error. Assignments at `user_settings.go:106` and `config.go:322` then panic. Non-ENOENT read failures skip unmarshalling and proceed to `fsatomic.WriteJSON` with only the section being saved.

`readConfigPath` in `config.go:122` confines reads through `os.OpenRoot` and reports close errors. Keep it. `readConfigFile` is deliberately tolerant on load and isolates section decode errors; this plan changes save validation only. The existing `TestSaveAssistantsRefusesMalformedExistingFile` at `assistants_save_test.go:160` asserts an error and compares the original bytes after rejection; use that pattern. `fsatomic.WriteJSON` supplies crash-safe writes and private temporary files; do not replace it with direct writes.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Save regressions | `go test ./internal/config -run 'Test.*Save' -count=1` | all pass after fix |
| Config suite | `go test ./internal/config -count=1` | PASS |
| Config race | `go test -race ./internal/config -count=1` | PASS, no races |
| Settings callers | `go test ./internal/app -run 'Test.*(Settings|Assistants|Theme)' -count=1` | PASS |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | exit 0 |
| Changed-code lint | `make lint-strict-new` | no new issues or formatter diff |
| Patch hygiene | `git diff --check` | exit 0 |

Audit devcheck failed in real-e2e scenarios; verify-loop passed. Ambient AMUX_WORKSPACES_ROOT was confirmed to escape temporary HOME and collide with a workspace in the actual user root. Until plan 059 fixes isolation, sanitize every broad/e2e invocation as above. Reproduce failures under isolation before attributing them; failed gates remain blocking, and unrelated fixes stay out of scope. No new input/tmux behavior is introduced here.

## Scope

**In scope:** `internal/config/config.go`, `internal/config/user_settings.go`, new `internal/config/config_save.go`, new `internal/config/config_save_test.go`, `internal/config/assistants_save_test.go`, `internal/config/user_settings_test.go`, `README.md`, `docs/CONFIG.md`, `docs/ORCHESTRATION.md`, this plan's index status.

**Out of scope:** assistant serialization fidelity (plan 049), tolerant startup loading, config locking/concurrent-instance merge semantics, settings rollback UX, adding schemas/migrations, symlink-policy changes, and `internal/fsatomic` behavior.

## Git workflow

Use the operator checkout or an authorized isolated `advisor/043-preserve-config-on-invalid-reads` branch. Record initial status/diff. Never stash, reset, clean, commit, or push. Preserve the user's existing README edits and other audit implementation work.

## Steps

### Step 1: Add save-boundary regression tests

Table-drive both `saveUISettings` and `saveAssistants`. For top-level `null`, arrays, strings, numbers, malformed JSON, and a read error, assert a nonnil error, no panic, and unchanged existing content/path. Keep missing-file and whitespace-only-file behavior as successful empty configuration, matching the current save path. Test valid objects retain unknown top-level keys and the unrelated section.

For this regression step, use a real-path boundary case (e.g. an outside-root symlink rejected by the existing reader) proving both public save paths preserve the symlink and target. Do not weaken `os.OpenRoot` to make that test work. Add deterministic injected permission/I/O-error cases with the helper introduced in Step 2, rather than depending solely on chmod under root.

**Verify:** `go test ./internal/config -run 'Test.*Save' -count=1` should fail on new null/read-failure regressions before repair; existing valid-object cases remain green.

### Step 2: Share strict read-for-update validation

Add `config_save.go` with one private helper used by both save functions. Its production caller uses `readConfigPath`; an internal function parameter can provide the reader in unit tests without package-global mutation. If reading returns an error, allow only `errors.Is(err, os.ErrNotExist)` as an empty object and propagate all other errors with path context. A blank successful read remains an empty object. Unmarshal a nonblank read into `map[string]any`; reject malformed JSON and a nil result (JSON null) with an error before callers mutate anything. Add helper tests using injected permission and other I/O failures, asserting no payload is returned and the wrapped error retains its identity.

Keep parent creation, UI key-by-key merge, full assistants-section replacement, and final `fsatomic.WriteJSON` calls unchanged. Remove now-unused imports only from scoped files. Do not turn the tolerant loader into the strict save helper. Preserve current nil receiver/nil Paths no-op behavior.

**Verify:** `go test ./internal/config -count=1` and `go test -race ./internal/config -count=1` → PASS. New read-failure tests must assert `errors.Is` retains the injected cause.

### Step 3: Document the save contract and run gates

Update README's configuration guidance, CONFIG's config-file behavior, and ORCHESTRATION's state/config guidance in the same change: startup can fall back to defaults, while saving refuses an unreadable or non-object existing document so unrelated content survives. Explicitly distinguish a missing file from a rejected existing file; do not promise concurrent-process merging or automatic repairs.

**Verify:** `rg -n 'unreadable|non-object|preserv' README.md docs/CONFIG.md docs/ORCHESTRATION.md` → each document contains the new contract. Run settings caller tests, `env -u AMUX_WORKSPACES_ROOT make devcheck`, `make lint-strict-new`, and `git diff --check` → all exit 0, or STOP with the exact isolated failures; do not mark DONE with a failed gate.

## Test plan

Model preservation checks after `TestSaveAssistantsRefusesMalformedExistingFile`. Cover both save entrypoints for null and I/O rejection, plus helper-level ENOENT, permission error, other I/O error, empty bytes, whitespace, valid object, and non-object roots. Assert byte equality/path identity after failure and unrelated sections after success. Tests using temporary files must never touch the actual user config.

## Done criteria

- [ ] Both save paths use one strict read-for-update boundary.
- [ ] Null/non-object/read-failure cases return errors and leave originals intact.
- [ ] Missing/blank files and valid-object merge behavior remain supported.
- [ ] Config, race, settings caller, devcheck, lint-strict-new, and diff-check gates pass.
- [ ] README, CONFIG, and ORCHESTRATION describe the distinction without changing startup load policy.
- [ ] Only scoped changes were added beyond the initial dirty tree; index handled as instructed.

## STOP conditions

Stop on substantive excerpt drift, unexpected parent-directory behavior requiring a storage-policy change, a need to modify app rollback semantics, inability to preserve read-error identity, two failed targeted repair attempts, or out-of-scope changes. Do not silently treat permission errors as absence. An isolated devcheck failure must be reported, not hidden with skips or treated as DONE.

## Maintenance notes

Every future save operation must use this read-for-update boundary before rewriting the shared document. Save strictness and load fallback are intentionally different. Plan 049 subsequently changes delay serialization; preserve this helper when applying it.
