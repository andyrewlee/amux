# Plan 002: Refuse writes against newer-schema store files instead of overwriting them

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/data/project_env.go internal/data/project_scripts.go internal/data/registry.go internal/process/script_trust.go internal/data/store_version_test.go internal/data/project_scripts_test.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

Four versioned JSON stores treat *any* load failure — including "file has a newer schema version than this binary understands" — identically to corruption, and their write paths then overwrite the file at v1. A user who downgrades amux (brew downgrade, `go install @old`, binary swap) silently loses the entire file on the first `Set`/`Trust` call: every project's env vars (the documented home for secrets), script defaults, trust approvals, and the project registry itself. Reads fail closed; writes must too — a newer-version file is intact data from a newer binary, not corruption, and must be preserved.

## Current state

Three stores in `internal/data`, one in `internal/process`, all sharing the versioned-envelope shape:

1. `internal/data/project_env.go` — per-project env vars (documented secrets home, lines 16-25). `load()` returns `fmt.Errorf("unsupported project-env.json schema version %d ...")` at lines 116-117; `Set()` maps **any** load error to `all = map[string]map[string]string{}` at lines 67-73, then `fsatomic.WriteJSON` at v1 (lines 81-84).
2. `internal/data/project_scripts.go` — per-project script defaults. Same shape: version rejection inside `load()` (~lines 108-109) feeding `Set()`'s wholesale replace (~lines 66-71).
3. `internal/process/script_trust.go` — the trust registry (`trusted-scripts.json`) gating repo-supplied script execution. `load()` swallows the version error: `logging.Warn(...unsupported schema version...)` + `return map[string]string{}` at lines 126-131, then `Trust()` writes `scriptTrustFile{Version: scriptTrustFileVersion, Trusted: entries}` at lines 180-189 — clobbering every newer-format approval.
4. `internal/data/registry.go` — the project registry (`projects.json`). `parseRegistryData` rejects `Version > registryFileVersion` (lines 242-243); `loadUnlocked` falls back to `.bak` on *any* parse error (lines ~94-108), after which `AddProject`/`RemoveProject` call `saveUnlocked` → rewrites the primary at v1. The newer-version primary file is preserved only if the `.bak` fallback itself fails (POSIX: `.bak` rarely exists — see `internal/fsatomic/fsatomic.go` ~130-154 for when it does).

The version-rejection excerpt (project_env.go:111-118):

```go
	if _, isEnvelope := probe["version"]; isEnvelope {
		var file projectEnvFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, err
		}
		if file.Version > projectEnvFileVersion {
			return nil, fmt.Errorf("unsupported project-env.json schema version %d (newest known: %d)", file.Version, projectEnvFileVersion)
		}
```

And the destructive write path (project_env.go:67-73):

```go
	all, err := s.load()
	if err != nil {
		// A corrupt file is replaced wholesale — Set is the authoritative
		// write and holding onto unparseable bytes would wedge every future
		// edit.
		all = map[string]map[string]string{}
	}
```

The contrast that proves the codebase already knows the right pattern — `internal/config/user_settings.go` (~lines 84-106) **refuses** to write `config.json` when the existing file can't be parsed, preserving unknown keys via a `map[string]any` merge. Match that intent: newer-schema = preserve + error; corrupt = the existing documented wholesale-replace behavior is *deliberate* (see the `project_env.go:69-71` comment) and stays.

**Critical distinction the fix must honor**: genuinely corrupt files (unparseable JSON) keep the current replace behavior — that's a designed recovery path. Only the *version-too-new* case must refuse. Corrupt-file replacement is intentional because holding unparseable bytes wedges every future edit; a newer-version file is parseable and valid — it's just from the future.

## Commands you will need

| Purpose   | Command                                   | Expected on success |
|-----------|-------------------------------------------|---------------------|
| Build     | `go build ./internal/data ./internal/process` | exit 0           |
| Unit test | `go test ./internal/data ./internal/process -count=1` | all pass |
| Lint      | `make lint`                               | exit 0              |
| Full gate | `make devcheck`                           | exit 0              |

## Scope

**In scope** (the only files you should modify):
- `internal/data/project_env.go`
- `internal/data/project_scripts.go`
- `internal/data/registry.go`
- `internal/process/script_trust.go`
- Their test files: `internal/data/store_version_test.go`, `internal/data/project_scripts_test.go`, `internal/data/registry_test.go` (or wherever the per-store tests live — find via `grep -rn "unsupported schema\|FutureVersion\|UnknownVersion" internal/data internal/process --include='*_test.go'`), `internal/process/script_trust_test.go` (or the file holding trust tests).

**Out of scope**:
- `internal/data/workspace_store*.go` — workspace store already reject-returns versions on read AND does not collapse errors to wholesale-rewrite on save the same way; it has separate drift handling. Do not refactor it here.
- Any forward-migration scaffolding (per-version decode dispatch, unknown-key preservation) — explicitly deferred; see Maintenance notes. This plan only changes the *write* path's response to a newer-version read failure.
- `internal/fsatomic` — the atomic-write primitive is correct.
- The corrupt-file → replace branch itself — that behavior is deliberate; only the newer-version path changes.

## Git workflow

- Branch: `advisor/002-schema-version-write-guard` off `main`.
- Commit style: `fix: refuse to overwrite newer-schema store files on write`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add a sentinel error for "newer schema version"

In `internal/data` (e.g. a small addition to `project_env.go` or a shared `errors.go` if one exists — check `ls internal/data/errors*.go` first), define:

```go
// errUnsupportedVersion marks a store file whose schema version is newer than
// this binary understands. Write paths must refuse rather than replace the
// file — its data is valid for a newer binary.
var errUnsupportedVersion = errors.New("unsupported store schema version")
```

Wrap it where each `load()`/`parseRegistryData` produces the version error: `fmt.Errorf("%w: project-env.json schema %d (newest known: %d)", errUnsupportedVersion, ...)` — or return it wrapped via `errors.Join`/a typed error; the key requirement is `errors.Is(err, errUnsupportedVersion)` is true. `internal/process/script_trust.go` can't import `data`'s unexported sentinel — give it its own equivalent exported-free sentinel OR export one from `data` (check whether `data` already exports error sentinels — `ErrScriptsNotTrusted`-style in `script_trust.go` shows the codebase's sentinel style).

**Verify**: `go build ./internal/data ./internal/process` → exit 0.

### Step 2: Refuse the write on newer-version, keep corrupt-file replace

- `project_env.go` `Set()`: `if err != nil && !errors.Is(err, errUnsupportedVersion) { all = map... } else if err != nil { return err }` — i.e. propagate the version error instead of wiping.
- `project_scripts.go` `Set()`: same change.
- `script_trust.go`: `load()` currently swallows everything into an empty map — split it so the version-too-new case is *distinguishable* to `Trust()`. Minimal honest fix: keep `load()` returning the map for read callers (fail-closed trust = empty = not trusted, correct), but have `Trust()` re-check the file's version explicitly before writing (e.g. a `checkVersionReadable()` helper that reads + probes the envelope version and returns the version error if too new) — refuse `Trust` with that error. Do NOT make `load()` return an error — that changes every read caller's signature.
- `registry.go`: in `loadUnlocked`'s backup-fallback path, when `parseErr` is a version error, return it *as* the load error (do NOT fall back to `.bak` — a newer-version primary is not corruption; falling back then rewriting is exactly the clobber). Concretely: before `readRegistryFile(backupPath)`, `if errors.Is(parseErr, errUnsupportedVersion) { return nil, false, parseErr }`. The backup fallback stays for genuine parse failures.

**Verify**: `go build ./internal/data ./internal/process` → exit 0. `go vet ./internal/data ./internal/process` → clean.

### Step 3: Regression tests — one per store

For each of the four stores, add a test: write a fixture file with `Version` bumped one past the known max (the existing `TestProjectEnv_UnknownVersionDegradesToEmpty` / `TestProjectScriptStore_FutureVersionRejected` tests in `internal/data/store_version_test.go` and `project_scripts_test.go:97-101` show the fixture style — extend that file), then call the write method (`Set`/`Trust`/`AddProject`) and assert:

1. The call returns an error (for `Trust`: returns error; for `Set`: returns error; for `AddProject`/`RemoveProject`: returns error).
2. **The file on disk is byte-identical to the fixture** — `os.ReadFile` before vs after, `bytes.Equal` or string compare. This is the machine-checkable property; a passing error return that still rewrote the file is a failure.
3. `errors.Is(err, errUnsupportedVersion)` where the sentinel is used.

Also keep/assert the counterpart: a genuinely corrupt file (invalid JSON bytes) still takes the replace path for `Set` (writes succeed, file is replaced) — one test per env/scripts store so nobody "fixes" the deliberate recovery path away.

For `script_trust.go`: a v2 fixture → `Trust()` returns error AND file unchanged; `IsTrusted` still returns false for all entries (fail-closed, unchanged).

For `registry.go`: a v2 primary + valid `.bak` → `AddProject` must return the version error and NOT write from the backup (this pins "newer version is not corruption" precisely).

**Verify**: `go test ./internal/data ./internal/process -count=1` → all pass including new tests.

### Step 4: Full gate

**Verify**: `make devcheck` → exit 0.

## Test plan

- New tests in `internal/data/store_version_test.go` (env, scripts, registry write-refusal + corrupt-file-replace-still-works) and the trust test file (trust write-refusal).
- Structural pattern: `TestProjectEnv_UnknownVersionDegradesToEmpty`, `TestProjectScriptStore_FutureVersionRejected` (which already asserts "the file is not destroyed by the failed read" — extend to "not destroyed by the *write*").
- Edge cases: v0 bare-map file (no "version" key) still loads and upgrades on write — must NOT be refused. Missing file still writes fine.
- Verification: `go test ./internal/data ./internal/process -count=1 -run 'Version|Trust|Registry'` → pass.

## Done criteria

- [ ] `Set`/`Trust`/`AddProject`/`RemoveProject` return an error (never silently rewrite) when the backing file's version is newer than the binary's.
- [ ] The newer-version file's bytes are preserved in every refusal path (asserted by test).
- [ ] Genuinely corrupt files still take the documented replace/recovery paths (asserted by test).
- [ ] `go test ./internal/data ./internal/process -count=1` exits 0.
- [ ] `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- Any store's version handling has already changed to refuse writes (drift).
- `load()` in `script_trust.go` has been refactored to return errors — reassess rather than bolting on the version-check helper.
- The sentinel approach conflicts with an existing versioned-error type — reuse the existing type instead.
- Tests reveal a caller *depends* on the clobber-on-version behavior (e.g. a migration path that writes v1 over v2 on purpose) — report; that changes the fix shape.

## Maintenance notes

- When schema v2 is eventually introduced, each store will need a per-version read dispatch (the `project_env.go:129` comment already anticipates "adding a read branch for the older shapes"). At that point revisit: (a) unknown-key preservation on rewrite (`workspaceJSON` drops unknown fields — acknowledged in `workspace_serial_parity_test.go:40-44`), (b) `findStoredWorkspace`'s fallback scan skipping unparseable records (`workspace_store.go:410-418`). This plan deliberately does NOT build that scaffolding — it only stops the destructive write.
- A reviewer should check the registry.go change extra carefully: the `.bak` fallback exists for crash-recovery, and ordering "version error → return" before "backup read" is the whole fix — wrong ordering reintroduces the bug.
- `internal/update`'s `ForceDowngrade` flag (updater.go:48-53) is test-only today; if downgrade ever ships, these guards become user-visible rather than edge-case — keep them.
