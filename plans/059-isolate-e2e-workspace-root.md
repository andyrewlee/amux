# Plan 059: Keep e2e worktrees inside their test-owned workspace root

> **Executor instructions:** This additional finding was confirmed while validating the deep audit. Implement it before relying on broad real-e2e gates. Follow the steps and update this row in `plans/README.md`. Preserve user changes; do not commit or push, and do not clean up anything under a real user's `.amux` directory.
>
> **Drift check:** Run `git diff --stat 7c530ee..HEAD -- internal/e2e/pty.go internal/e2e/util.go internal/e2e/pty_env.go internal/e2e/pty_env_test.go internal/e2e/workspace_root_isolation_test.go CONTRIBUTING.md` and `git diff HEAD --` with those paths; inspect `git ls-files --others --exclude-standard -- internal/e2e`. Compare current excerpts before proceeding. The audit included a dirty application tree, but these existing helper files were unchanged.

## Status

- **Priority:** P1 — validation prerequisite, numbered last because discovered during baseline verification
- **Effort:** S
- **Risk:** LOW
- **Depends on:** none; plan 036 may be needed for the entire e2e suite to pass
- **Category:** tests / correctness / dx
- **Planned at:** commit `7c530ee` plus existing working-tree changes, 2026-09-26

## Why this matters

`StartPTYSession` changes HOME but inherits `AMUX_WORKSPACES_ROOT`. When tests run inside amux, the parent has exported that variable, so fixture worktrees can be created under the actual user's workspace directory. This both escapes test isolation and makes repeated tests collide. During this audit `TestOverlayQueueDefersAsyncOpenUntilFirstResolves` failed because `~/.amux/workspaces/001/qtest` already existed. Fix the child environment without removing that directory or changing the production relocation feature.

## Current state

`internal/e2e/pty.go:101` builds the child environment this way:

```go
cmd.Env = append(stripGitEnv(os.Environ()),
	"HOME="+home,
	"TERM=xterm-256color",
	"AMUX_PROFILE=0",
	"AMUX_PROFILE_INTERVAL_MS=0",
)
if len(opts.Env) > 0 {
	cmd.Env = append(cmd.Env, opts.Env...)
}
```

`internal/e2e/util.go:14` filters Git-specific environment variables only. The production setting is intentional: `internal/config/paths.go:34` prefers `AMUX_WORKSPACES_ROOT` over the default under HOME:

```go
if override := strings.TrimSpace(os.Getenv(WorkspacesRootEnvVar)); override != "" {
	workspacesRoot = override
}
```

`internal/app/app_init.go:371` re-exports the resolved root for subprocesses. The correct fix belongs in the test launcher, not those production files.

Match the existing fixture convention from `internal/e2e/lifecycle_shelve_test.go:53`:

```go
const wsName = "shelveme"
wsRoot := filepath.Join(home, ".amux", "workspaces", filepath.Base(repo), wsName)
```

`workspace_agent_test.go`/`agent_dupe_test.go` provide `writeRegistry`, `writeConfig`, `writeStubAssistant`, `sessionEnv`, a unique named tmux server with deferred cleanup, and `createWorkspaceWithAgent`. Reuse observable helpers; do not add pacing sleeps. Only use `t.TempDir()` roots in the new tests.

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Environment unit tests | `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^TestPTYSessionEnv' -count=1 -v` | PASS |
| Isolation integration | `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^TestPTYSessionIgnoresAmbientWorkspaceRoot$' -count=1 -v` | PASS, no skip on a working tmux host |
| Focused lifecycle check | `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^(TestOverlayQueueDefersAsyncOpenUntilFirstResolves|TestShelveRestorePurgeLifecycle)$' -count=1 -v` | PASS without touching the ambient root |
| Full gate | `env -u AMUX_WORKSPACES_ROOT make devcheck` | exit 0, or separately recorded known product failure |
| Strict lint | `make lint-strict-new` | exit 0 |
| Real input gate | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | literal CR delivery tests PASS |
| Path-relevant tests | `env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e` | PASS; inspect skips |
| Whitespace | `git diff --check` | exit 0 |

Keep command-local `env -u` during implementation; regression tests inject an ambient value pointing to a second temporary directory themselves. Do not run an intentionally vulnerable reproduction with a real user's path. Once the fix and its isolation assertions pass, downstream plans can rely on launcher isolation, but sanitized commands remain valid.

## Scope

**In scope:** `internal/e2e/pty.go`, new `internal/e2e/pty_env.go`, `internal/e2e/pty_env_test.go`, `internal/e2e/workspace_root_isolation_test.go`, `CONTRIBUTING.md` for the test-isolation note, this plan and index row. `internal/e2e/util.go` is read-only unless the extracted env builder needs to relocate an existing helper without changing Git-filter behavior.

**Out of scope:** production config/path behavior, changing the public env variable, ambient `.amux` directories or worktrees, generic environment sanitization, tmux server selection policy, deleting failed-test residue, picker navigation, teardown or fakeagent cleanup (plan 056). No runtime contract change is intended, so README/CONFIG/ORCHESTRATION edits are not required for this test-only repair.

## Git workflow

Suggested branch: `advisor/059-isolate-e2e-workspace-root`. Record `git status --short` first; preserve dirty files and untracked user work. No commit/push/stash/reset. Do not run `git worktree prune` against a real user's repository to make a test pass.

## Steps

### Step 1: Extract and characterize child environment assembly

Move the assembly into a small helper in `pty_env.go`, taking parent env, fixture home, and explicit `PTYOptions.Env` as inputs; call it from `StartPTYSession`. Keep the current Git stripping, TERM, profiling flags, and explicit-option precedence. The helper must not read global environment itself, so unit tests use synthetic input. Add table tests that resolve the effective value using the same last-entry-wins semantics as exec, including duplicate ambient keys and an explicit override. First characterize unchanged semantics, then add the root-isolation assertion.

**Verify:** `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^TestPTYSessionEnv' -count=1 -v` → only the newly added ambient-root isolation case should fail; existing env precedence assertions pass. Do not start a real PTY yet.

### Step 2: Override the inherited root with the fixture root

Append `config.WorkspacesRootEnvVar + "=" + filepath.Join(home, ".amux", "workspaces")` with the fixture defaults before explicit `opts.Env`. This makes the default deterministic and still lets a test deliberately exercise a relocated root by passing an explicit test-owned override. Do not globally unset it or mutate the parent environment. Preserve unrelated variables and similarly named keys. Document that explicit test root overrides must be temporary/test-owned.

Table cases must cover inherited absolute root, absent key, whitespace-only inherited value, duplicate inherited entries, home path containing spaces, explicit temporary custom root, explicit blank override (production fallback to fixture HOME), and unchanged Git filtering.

**Verify:** `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^TestPTYSessionEnv' -count=1 -v` → every case PASS; `go test ./internal/config -run '^TestDefaultPathsWorkspacesRootEnvOverride$' -count=1` → PASS, production relocation unchanged.

### Step 3: Prove the actual subprocess honors isolation

In `workspace_root_isolation_test.go`, set an ambient root with `t.Setenv` to a second `t.TempDir()` containing a sentinel file. Do not use `t.Parallel` because the environment is process-global. Start the normal fixture with its own home and unique tmux server, create one workspace with the first assistant option, and assert the worktree is under fixture home while the ambient directory still contains only the unchanged sentinel. Defer the normal PTY/server cleanup. Add a second case using an explicit temporary override and assert that worktree goes there instead. Verify both through the real binary and filesystem, not only the helper's output.

**Verify:** `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^TestPTYSession(IgnoresAmbientWorkspaceRoot|HonorsExplicitWorkspaceRoot)$' -count=1 -v` → both PASS with no unexpected skips. Inspect the captured failure screen if creation fails; do not weaken the assertion or switch to the render-only harness.

### Step 4: Document the fixture contract and run gates

Add a short CONTRIBUTING note that e2e defaults to its temporary home/root and explicit relocation fixtures must own their target. Run the commands table. If `TestWorkspaceCreateAgentsHaveDistinctSessions` still fails after isolation, record it under plan 036 rather than changing the picker here. Gate failures must be recorded honestly; do not label an unsanitized previous failure a regression of this patch.

**Verify:** all applicable command-table gates → exit 0. If a known remaining product defect blocks broad tests, report its exact test and leave the plan status BLOCKED pending verification; unit and integration isolation checks must still pass. `git diff --check` → exit 0; compare changed paths to the initial baseline.

## Test plan

Add `TestPTYSessionEnv` table cases and the two real subprocess isolation cases above. Follow existing `workspace_agent_test.go` setup/cleanup and observable waits. Unit tests must run without tmux; integration tests use existing skip helpers and report skips. Never test with the developer's actual workspace root or assert cleanup by deleting it.

## Done criteria

- [ ] Synthetic env matrix passes, including explicit override precedence.
- [ ] Real subprocess default and explicit-override tests pass; ambient sentinel unchanged.
- [ ] Required gates pass and real-tmux skips are accounted for.
- [ ] Production config override test still passes; no production files changed.
- [ ] No cleanup of ambient user data; only scoped delta; index updated.

## STOP conditions

Stop if a reproduction would write outside test-owned directories, a fixture deliberately needs a non-temporary user worktree, or the product paths need changing to achieve test isolation. Stop on an unexplained root mismatch or after two failed targeted corrections. Never remove collision directories to unstick the suite; ownership was not established by this audit.

## Maintenance notes

When adding another path-bearing runtime environment variable, explicitly decide how the PTY fixture overrides it. A temporary HOME alone is insufficient when the application supports absolute-path env overrides. Keep env assembly testable without launching the app.
