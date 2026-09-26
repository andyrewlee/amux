# Plan 053: Keep the installed executable present throughout self-update

> **Executor instructions:** Follow the scoped install repair and verification. Never update the actual running amux during tests. Do not commit or push. Stop on the conditions below and update this plan's status row only when instructed.
>
> **Drift check first:** `git diff --stat 7c530ee..HEAD -- internal/update/install.go internal/update/install_sync.go internal/update/install_test.go internal/update/install_copy_test.go internal/update/install_atomic_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md`; then `git diff HEAD --` with those paths and `git status --short`. Compare the excerpts against the live dirty tree, including user edits to README. Preserve all unrelated work.

## Status

- **Priority:** P2
- **Effort:** M
- **Risk:** MED
- **Depends on:** none
- **Category:** bug / update
- **Planned at:** commit `7c530ee`, 2026-09-26, including the known dirty tree

## Why this matters

The updater moves the installed executable out of the way before moving its replacement in. A kill or interruption in that gap leaves the normal amux pathname missing, even though returned errors have rollback handling. On supported Darwin/Linux targets, staging and a direct rename over the live pathname can provide continuous availability.

## Current state

`internal/update/install.go:202` and line 207 perform two separate renames:

```go
if err := renameFile(currentBinaryPath, backupPath); err != nil {
    return fmt.Errorf("backing up current binary: %w", err)
}

// Atomically replace with staged binary (same filesystem, so rename works)
if err := renameFile(stagedPath, currentBinaryPath); err != nil {
```

The second failure triggers another rename to restore the backup. At line 191 a staging copy failure returns before staging cleanup is deferred. `copyFile` already creates destinations with `O_EXCL` and mode 0755, copies, fsyncs, and checks Close; preserve those boundaries and the executable-mode policy. The updater does not promise to preserve arbitrary prior file modes.

`internal/fsatomic/fsatomic.go:79` is the project's POSIX replacement exemplar:

```go
} else if err := renameFile(tmpPath, path); err != nil {
    return err
}
if err := syncParentDirForGOOS(goos, dir); err != nil {
```

Its directory sync implementation is the convention to follow, but do not edit that package. [Go's os.Rename contract](https://pkg.go.dev/os#Rename) describes replacement and the platform caveat; amux's interactive release targets are Darwin/Linux. Update signature/checksum/archive confinement is a separate verified boundary and must remain unchanged. Existing install tests at `install_test.go:174`, `:224`, and `:274` encode the old backup-move/restore mechanics; replace those mechanical assumptions with preservation assertions.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Install tests | `go test ./internal/update -run 'Test(InstallBinary|CopyFile)' -count=1` | PASS after fix |
| Full updater suite | `go test ./internal/update -count=1` | PASS |
| Race coverage | `go test -race ./internal/update -count=1` | PASS, no races |
| Compile portability | `GOOS=windows GOARCH=amd64 go build ./...` | exit 0; no output binary emitted |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | exit 0 |
| Changed-code lint | `make lint-strict-new` | no new issues/formatter diff |
| Patch hygiene | `git diff --check` | exit 0 |

Audit devcheck failed in real-e2e, and inherited AMUX_WORKSPACES_ROOT was confirmed to direct tests outside their temporary HOME. Until plan 059 lands, sanitize every broad/e2e invocation as above. Verify-loop passed in the audit, but this plan does not change input/tmux. Do not attribute a new failure to baseline without an isolated reproduction.

## Scope

**In scope:** `internal/update/install.go`, new `internal/update/install_sync.go`, `internal/update/install_test.go`, `internal/update/install_copy_test.go`, new `internal/update/install_atomic_test.go`, `README.md`, `docs/CONFIG.md`, `docs/ORCHESTRATION.md`, this plan's index status.

**Out of scope:** signatures/checksums, download/extraction, release selection/version guards, Homebrew/go-install policy, install.sh, GitHub workflows, cross-process update serialization, Windows support, or changing `internal/fsatomic`.

## Git workflow

Use the operator checkout or an authorized isolated `advisor/053-replace-executable-without-path-gap` branch. Record dirty baseline and preserve it. No stash/reset/clean/commit/push. All test binaries and targets must be under t.TempDir, never `os.Executable()` or a real install path.

## Steps

### Step 1: Test continuity at the replacement boundary

Add a test wrapping `renameFile`: immediately before the staged-to-current rename, read the live current pathname and assert it still contains the complete old binary. After success require complete new bytes and the existing executable-mode policy (0755 subject to the process umask, not arbitrary old-mode preservation). Existing code must fail the pre-replacement assertion. Add precommit failure assertions: failed staging copy or failed final rename leaves old bytes at the live pathname, and no partial staging file remains.

Use the existing package seam/t.Cleanup style; do not run tests that override seams in parallel. Keep existing tests for cross-directory sources, private/random staging names, and precreated destination/symlink refusal.

**Verify:** `go test ./internal/update -run 'TestInstallBinary.*(Continuous|Atomic|Preserve)' -count=1` runs at least one new named test and fails specifically because the old pathname is absent before replacement.

### Step 2: Prepare a backup without moving the live file

Keep the staged new binary in the target directory. Make `copyFile` remove its partial destination on write/sync/close failure, registering cleanup only after its `O_EXCL` open succeeds. Once copying succeeds, ownership transfers to `InstallBinary`, which defers staged cleanup as today. A generated but uncreated pathname is not owned: an exclusive-create failure must leave any pre-existing file or symlink untouched. Extend the existing refusal tests to assert that the destination itself remains present, as well as unchanged target bytes.

Create a random backup path and copy the current executable to it with that same exclusive-create/fsync copy discipline. Register caller cleanup only for the successfully copied backup, with a flag to retain it after a postcommit durability failure in Step 3. Copying the backup rather than renaming current preserves the live pathname and avoids hard-link/platform capability assumptions.

After both copies complete, replace current using the single same-directory `renameFile(stagedPath, currentBinaryPath)`. On rename failure, return a contextual error and clean temporary files; the old target never moved, so do not attempt a restore rename. Rewrite the old restore-specific tests to assert old-target preservation and a preserved wrapped error cause. Do not maintain a synthetic restore failure branch that is no longer reachable.

**Verify:** `go test ./internal/update -run 'Test(InstallBinary|CopyFile)' -count=1` → PASS, including the continuity test and copy-failure cleanup.

### Step 3: Sync the directory and test interruption/failure ownership

Add a small private directory-sync helper/seam matching the existing fsatomic pattern. Sync the target directory after preparing the backup before replacement, and after the final rename. A precommit sync failure leaves the old target intact and removes temporary files. A postcommit sync failure must return an explicit error saying replacement occurred but durability could not be confirmed, retain the complete backup, and include its path with an accurately quoted manual recovery hint. Do not automatically roll the new executable back after replacement. After successful postcommit sync, remove the backup; if cleanup fails, return a contextual error explicitly stating the new executable is installed and identifying the retained backup. Cleanup failure must never remove/roll back the live new target.

Add helper-process interruption tests using only temporary paths: pause in the pre-final-rename seam after the backup is prepared, signal the parent via a pipe/file, then let the parent kill the helper and assert the live file is still old. Repeat a pause immediately after the real replacement and assert the live file is new. A subprocess killed after completed replacement is not a power-loss test; report that limit. Assert sync invocation/failure phases with seams and verify the backup contains old bytes after postcommit failure.

**Verify:** `go test ./internal/update -count=1` and `go test -race ./internal/update -count=1` → PASS. Interruption tests must prove the helper reached the intended seam before killing it, not race a timer.

### Step 4: Update the recovery contract and run gates

Update README's self-update guidance, CONFIG's settings/update guidance, and ORCHESTRATION's operational guidance: old or new executable remains reachable at the installed pathname; post-replacement sync failure may require manual recovery using the reported backup. Do not claim multi-process transactional updates or universal power-loss immunity.

**Verify:** `rg -n 'replacement|backup|self-update' README.md docs/CONFIG.md docs/ORCHESTRATION.md` finds the updated contract in all three. Run `GOOS=windows GOARCH=amd64 go build ./...`, isolated devcheck, lint-strict-new, and diff-check → exit 0; report reproducible isolated failures without broader edits. Compile portability is required even though Windows runtime installation remains out of scope. A failed gate blocks marking DONE.

## Test plan

New tests cover live-name continuity, staged-copy write/sync/close failure cleanup, unchanged pre-existing destinations after exclusive-create refusal, backup-copy failure, final-rename failure, pre/post replacement directory-sync errors, exact old/new bytes, executable permission, and controlled helper termination on both sides of rename. Preserve hostile-extraction/signature tests through the full updater suite. Use real files for pathname assertions and narrow seams for failures, following `install_copy_test.go`.

## Done criteria

- [ ] No production install path renames `currentBinaryPath` away before replacement.
- [ ] Before the sole replacement rename, live bytes are old; afterward they are new.
- [ ] Precommit errors preserve old target; postcommit sync errors preserve new target plus recoverable old backup.
- [ ] Staging/backup partial-copy failures do not leave owned partial files.
- [ ] Updater/race tests, Windows compile portability, isolated devcheck, lint-strict-new, and diff-check pass.
- [ ] Three contract docs updated; no scoped test touches the actual installed executable.
- [ ] Scope and index requirements met.

## STOP conditions

Stop if a supported deployment requires non-POSIX replacement semantics, copy/rename now operates on a different trust boundary, a helper test would target the actual binary, directory syncing is unsupported on the current target without a documented policy, external concurrent updates are needed to explain a failure, or two targeted verification attempts fail. Do not weaken signature verification or add sudo behavior.

## Maintenance notes

Atomic rename-over-existing protects pathname continuity, not arbitrary multi-process update ordering. Directory sync errors after rename mean the installation may already be new; callers and logs must not claim the old executable was restored. Any future backup refactor must keep the current pathname present until final replacement and maintain explicit ownership of partial files.
