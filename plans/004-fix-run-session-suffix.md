# Plan 004: Compute run-session suffixes from the existing set, not `len(names)+1`

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/process/run_session.go internal/process/scripts_stop.go internal/tmux/detached_session.go`
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

Under `script_mode: concurrent`, `runScriptHosted` names the next session `run-<len(names)+1>`, assuming the live set is a dense sequence `run`, `run-2`, …, `run-N`. When a mid-sequence session dies (killed manually, GC'd `remain-on-exit` corpse) the set has a gap — e.g. `run` + `run-3` alive → computes `run-3` → `Ensure`'s create-unless-present script sees it exists and no-ops → `RunScript` returns `nil`. The UI reports the run started; nothing ever runs; no error anywhere. Dead sessions also inflate `len(names)`, so suffixes grow past the live count. Naming must come from the actual suffix set, not the count.

## Current state

`internal/process/run_session.go:120-140` (`runScriptHosted`):

```go
func (r *ScriptRunner) runScriptHosted(ws *data.Workspace, cmdStr string) error {
	names, err := r.findRunSessions(ws)
	if err != nil {
		return fmt.Errorf("list run sessions: %w", err)
	}
	name := runSessionBaseName(ws)
	if len(names) > 0 {
		name = fmt.Sprintf("%s-%d", name, len(names)+1)
	}
	env, err := r.buildScriptEnv(ws)
	...
	err = r.runHost.Ensure(name, ws.Root, cmdStr, env, RunSessionMeta{...})
```

Supporting facts:

- `internal/tmux/detached_session.go:23-54` + `internal/tmux/command.go:125-130` — `Ensure`/`ensureSessionScript` is `has-session -t =name || new-session || has-session`: when the name exists, the script succeeds **without starting anything**. That's the silent no-start.
- `internal/process/scripts_stop.go:21-30` — `Stop` kills all found sessions, but individual sessions can also die selectively (tmux `kill-session`, GC of dead `remain-on-exit` sessions) leaving gaps.
- `runSessionBaseName(ws)` produces the base name; concurrent runs historically get `-N` suffixes.
- `internal/process/run_session.go:214-216` (`RunScriptOutput`) and `:241-258` (`RunScriptAttachTarget`) assume `names[len(names)-1]` is the newest — tmux `list-sessions` ordering is lexical, so `run-10` can sort before `run-2`. Secondary symptom of the same naming assumption; fix the naming first (see Scope).

Repo conventions: `internal/process` tests exercise `ScriptRunner` with real subprocesses/tmux where needed; look at existing `run_session*_test.go` / `scripts*_test.go` for fixtures (real-tmux tests are gated by `skipIfNoTmux`-style helpers — check `grep -rn 'runScriptHosted\|RunScript' internal/process --include='*_test.go'`).

## Commands you will need

| Purpose      | Command                                       | Expected on success |
|--------------|-----------------------------------------------|---------------------|
| Build        | `go build ./internal/process`                  | exit 0              |
| Unit test    | `go test ./internal/process -count=1`          | all pass            |
| Real tmux    | `go test ./internal/tmux ./internal/e2e -count=1` | all pass (or skips with the documented tmux-missing reason) |
| Lint         | `make lint`                                    | exit 0              |
| Full gate    | `make devcheck`                                | exit 0              |

## Scope

**In scope**:
- `internal/process/run_session.go` — the suffix computation (and, if trivially in reach, the newest-selection helpers that share the dense-sequence assumption — see below).
- `internal/process/run_session_test.go` or the test file covering `runScriptHosted` naming.

**Out of scope**:
- `internal/tmux/detached_session.go` / `command.go` — `Ensure`'s create-unless-present contract is correct and load-bearing (it closes the concurrent-create race); do not change it.
- The `RunScriptOutput`/`RunScriptAttachTarget` newest-selection ordering fix — only fold it in if the suffix fix makes it a one-line consequence (e.g. sorting by parsed suffix becomes free). A run-session *picker* UI is a separate plan (plans/032).
- `Stop`/`StopAll` kill sweeps — correct.
- `script_mode` documentation.

## Git workflow

- Branch: `advisor/004-run-session-suffix` off `main`.
- Commit style: `fix: pick unused run-session suffix instead of len+1`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Parse existing suffixes and pick a free one

In `runScriptHosted`, replace `len(names)+1` with suffix analysis:

```go
	name := runSessionBaseName(ws)
	if len(names) > 0 {
		name = fmt.Sprintf("%s-%d", name, nextRunSuffix(name, names))
	}
```

Add `nextRunSuffix(base string, names []string) int`: for each name equal to `base` treat it as suffix 1 occupied; for names matching `base-N` (parse with `strings.TrimPrefix` + `strconv.Atoi`, reject `N <= 0`), mark occupied. Return the smallest unoccupied positive integer ≥ 2 (base itself is "slot 1" — a fresh sequence starts at `-2`, matching existing `-2`, `-3` history). Choosing smallest-free rather than max+1 also reuses dead slots, keeping names dense and the lexical-tail selection (see Current state) closer to creation order.

Edge cases that MUST hold: `names = [run]` → returns 2; `[run, run-2]` → 3; `[run, run-3]` → 2 (reuses the gap — this is the bug fix); `[run-2]` (base dead, suffix alive) → 2 is occupied → returns 3.

**Verify**: `go build ./internal/process` → exit 0.

### Step 2: Fail loudly if Ensure still no-ops

After `r.runHost.Ensure(...)`, verify the session actually started rather than pre-existed. The cheapest honest check: `Ensure` already ends with `has-session` — add a post-call `Status`/`Exists` probe via the session host (`r.runHost.Status(name)` — check `RunSessionHost` interface at run_session.go:18-33 for the exact method) and, if the session exists but was *already* dead/pre-existing, surface an error like `run session %q already exists` rather than nil. If the interface lacks a probe that distinguishes "created now" from "pre-existing", do NOT extend it — instead keep the smallest-free naming (which makes collision unreachable in practice) and note the residual in the PR description. STOP if the only way to detect it requires changing `Ensure`'s contract.

**Verify**: `go test ./internal/process -count=1` → all pass.

### Step 3: Tests

Unit-test `nextRunSuffix` exhaustively (pure function — easy table): empty, base-only, dense, gap, suffix-only, double-digit (`[run, ..., run-10]` → 11), non-matching names ignored, `base-0`/`base--1`/`base-abc` ignored.

For `runScriptHosted` end-to-end: if an existing test exercises hosted runs with a fake `RunSessionHost`, add a fixture where `findRunSessions` returns `[run, run-3]` and assert `Ensure` is called with `run-2`. Find the fake via `grep -rn 'EnsureFunc\|runHost' internal/process --include='*_test.go'`.

**Verify**: `go test ./internal/process -count=1 -run 'RunSuffix|RunScript' -v` → pass.

### Step 4: Full gate

**Verify**: `make devcheck` → exit 0. Then `go test ./internal/tmux ./internal/e2e -count=1` → pass or documented skip.

## Test plan

- New unit tests: `nextRunSuffix` table (Step 3), hosted-run naming with gapped set.
- Structural pattern: existing `internal/process` runner tests using fake `RunSessionHost`/`runHost`.
- Edge: 10+ concurrent runs (double-digit suffixes), killed middle session, base dead but suffixes alive.
- Verification: `go test ./internal/process -count=1` → all pass.

## Done criteria

- [ ] Suffix comes from parsing existing session names, not `len(names)+1`.
- [ ] A gapped set (`run`, `run-3`) produces `run-2` and a real new session — no silent no-start.
- [ ] `go test ./internal/process -count=1` exits 0 with new tests.
- [ ] `make devcheck` exits 0; `go test ./internal/tmux ./internal/e2e -count=1` passes or skips with documented reason.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `runScriptHosted` already computes suffixes differently (drift).
- `Ensure`'s contract changed (e.g. now errors on existing) — re-evaluate whether the naming fix is still needed.
- `RunSessionHost` interface has no Status/Exists probe AND the smallest-free naming alone doesn't satisfy reviewers — report rather than expanding the interface silently.
- Concurrent-mode session naming moved elsewhere (e.g. into `internal/tmux`).

## Maintenance notes

- The `names[len(names)-1]` "newest" assumption in `RunScriptOutput`/`RunScriptAttachTarget` remains approximately-right with dense suffixes but is still lexical-order dependent at ≥10 sessions; the run-session picker plan (plans/032) should sort by parsed suffix or `@amux_created_at` when it lands.
- A reviewer should confirm the smallest-free policy vs max+1: smallest-free keeps names dense (good for the lexical tail) but can recycle a slot whose dead session still has scrollback someone might reattach — acceptable because `Ensure` would have no-oped on it anyway before this change (recycling only applies to *absent* names).
- `remain-on-exit` dead sessions keep names alive in `findRunSessions`; GC reaping (`scripts_stop.go`/`app_tmux_gc.go`) is unchanged and unaffected.
