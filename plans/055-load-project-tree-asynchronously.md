# Plan 055: Load the project tree asynchronously with stale-result protection

> **Executor instructions:** Implement the bounded command/result design and tests below. Do not commit or push. Update only this plan's index row if the reviewer delegates index ownership.
>
> **Drift check first:** Run `git status --short`, `git diff --stat 7c530ee..HEAD -- internal/ui/sidebar/project_tree.go internal/ui/sidebar/project_tree_load.go internal/ui/sidebar/project_tree_view.go internal/ui/sidebar/project_tree_rebase.go internal/ui/sidebar/project_tree_async_test.go internal/ui/sidebar/project_tree_test.go internal/ui/sidebar/project_tree_reload_test.go internal/ui/sidebar/project_tree_mouse_test.go internal/ui/sidebar/project_tree_view_test.go internal/ui/sidebar/workspace_rebind_test.go internal/ui/sidebar/tabs.go internal/ui/sidebar/tabs_test.go internal/app/app_input.go internal/app/app_project_tree_async_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md ARCHITECTURE.md`, then `git diff HEAD --` with the same paths. Read untracked files and compare Current state. This baseline includes the dirty tree audited on 2026-09-26; preserve all user changes and do not stash/clean/reset.

## Status

- **Priority:** P2
- **Effort:** M
- **Risk:** MED
- **Depends on:** [plan 059](059-isolate-e2e-workspace-root.md) before unrestricted e2e validation; use sanitized commands below until it lands
- **Category:** perf / architecture / bug
- **Planned at:** commit `7c530ee`, plus audited uncommitted working tree, 2026-09-26

## Why this matters

Project-tree expansion, workspace switching, and refresh call ReadDir on the single Bubble Tea Update goroutine. A slow mount blocks all rendering, input, and PTY message handling; refresh repeats the delay for every expanded directory. Commands should load immutable directory snapshots while Update retains sole ownership of tree nodes and applies only current results.

## Current state

`internal/ui/sidebar/project_tree.go:201–208`:

```go
func (m *ProjectTree) expandNode(node *projectTreeNode) {
    if !node.IsDir || node.Expanded {
        return
    }
    entries, err := os.ReadDir(node.Path)
    if err != nil {
        return
    }
```

`reloadTree` at `:305–307` synchronously expands root, restores prior expansion recursively, then rebuilds the visible list. `restoreExpansion` at `:340–345` reads every previously expanded descendant again. `SetWorkspace` returns no command and calls reload immediately.

`internal/ui/sidebar/tabs.go:378–382` already has an app-facing command return:

```go
func (m *TabbedSidebar) SetWorkspace(ws *data.Workspace) tea.Cmd {
    m.workspace = ws
    cmd := m.changes.SetWorkspace(ws)
    m.projectTree.SetWorkspace(ws)
    return cmd
}
```

`TabbedSidebar.Update` routes branch/ahead-behind results to the Changes model even while its tab is inactive (`tabs.go:112–120`). `internal/app/app_input.go:136–145` similarly routes those results to the sidebar regardless of focus. Add the project result to both routing boundaries; default active-tab dispatch would lose results.

Repo conventions: Go/Bubble Tea v2, UI model mutation on Update, I/O in Cmd, `common.SafeBatch` for command composition, `ContentVersion` dirty marks for every visible tree mutation. `ARCHITECTURE.md` permits leaf widgets to load their own data inside async commands. `internal/ui/common/filepicker_navigation.go:resolvePathCmd`/`loadDirectoryCmd` are small immutable-result exemplars; project-tree identity requires stronger generation checks than path alone.

Existing `project_tree_test.go:newSeededProjectTree` assumes synchronous SetWorkspace. `project_tree_reload_test.go:TestProjectTreeReloadPreservesExpansionAndCursor` assumes synchronous expand/reload. Update these fixtures to pump commands, preserving their assertions rather than deleting coverage.

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Baseline | `go test ./internal/ui/sidebar -count=1` | PASS |
| Async regressions | `go test ./internal/ui/sidebar -run TestProjectTreeAsync -count=1 -v` | New tests PASS |
| Tree/wrapper | `go test ./internal/ui/sidebar -run 'TestProjectTree|TestTabbedSidebar|Test.*Rebind' -count=1 -v` | Existing/new tests PASS |
| App routing | `go test ./internal/app -run TestProjectTreeAsyncRouting -count=1 -v` | PASS |
| Race | `go test -race ./internal/ui/sidebar -count=1` | PASS, no races |
| Repository race | `env -u AMUX_WORKSPACES_ROOT make test-race` | Exit 0 with no race reports |
| Real-tmux race | `env -u AMUX_WORKSPACES_ROOT make test-race-tmux` | Exit 0 with no races; actual tmux coverage recorded |
| Render gate | `make harness-presets` | All presets complete |
| Perf gate | `PERF_STRICT=1 make perf-check` | Every host p95 baseline passes on quiescent host |
| Input | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | Required tests PASS, not SKIP |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | Exit 0 or honest unresolved-failure report |
| Lint/hygiene | `make lint-strict-new` and `git diff --check` | Both exit 0 |

The audit saw failing real-e2e devcheck tests and a passing verify-loop. Later investigation found inherited AMUX_WORKSPACES_ROOT defeating test HOME isolation; plan 059 repairs this prerequisite. Until then remove that variable from all e2e-reaching commands. Do not call remaining failures unrelated without fresh isolated evidence. The harness is render-only; use unit barriers to prove nonblocking Update, and verify-loop for real input.

## Scope

**In scope:** `project_tree.go`, new `project_tree_load.go`, `project_tree_view.go`, `project_tree_rebase.go`, new `project_tree_async_test.go`, existing `project_tree_test.go`, `project_tree_reload_test.go`, `project_tree_mouse_test.go`, `project_tree_view_test.go`, `workspace_rebind_test.go`, `tabs.go`, `tabs_test.go` under `internal/ui/sidebar`; `internal/app/app_input.go` and new `app_project_tree_async_test.go`; README, CONFIG, ORCHESTRATION, ARCHITECTURE; index status if delegated.

**Out of scope:** Filesystem watchers, new ignore/symlink policies, full recursive repository indexing, background services/shared pools, Changes-model git loading, file-picker code, terminal/PTy logic, app workspace selection lifecycle, harness production source, and performance baseline files.

## Git workflow

Use the operator's checkout; optional isolated branch `advisor/055-async-project-tree`. Record the initial status/diff and preserve unrelated user work. No stash, clean, reset, commit, push, PR, or automatic perf rebaseline. Tests operate only on t.TempDir fixtures.

## Steps

### Step 1: Define immutable directory results and bounded request state

In `project_tree_load.go`, define exported `ProjectTreeDirectoryLoaded` so App can route it, carrying tree generation, node path, request ID, a slice of immutable entry records `{Name, IsDir}`, and error. Capture the path and IDs when issuing a command; never capture mutable `projectTreeNode` pointers or read model fields inside Cmd. `os.ReadDir` and conversion to entry records run inside Cmd; retain directory-first case-insensitive sorting and current hidden-file rules when applying the result.

Add per-tree generation and monotonic request IDs, per-node pending request identity, and a bounded queue with at most 4 executing ReadDir commands per ProjectTree. Deduplicate repeated requests for the same current node. Removing stale pending requests must not forget executing jobs: retain their job IDs until results arrive, even after workspace/refresh changes, to enforce the cap. Superseded queued jobs are discarded. A stale completion releases its executing slot but cannot mutate nodes; then issue eligible current queued work. A hung filesystem read may occupy a bounded slot until the OS returns; do not claim to cancel os.ReadDir or spawn an unbounded goroutine to time it out.

Use a per-model readDir function initialized to os.ReadDir for deterministic tests; snapshot that function into Cmd at issue time. Do not add package-global mutable production seams.

**Verify:** `go test ./internal/ui/sidebar -run 'TestProjectTreeAsyncRequest|TestProjectTreeAsyncBound' -count=1 -v` → new request identity, dedup, four-job cap, and stale-slot-release tests PASS.

### Step 2: Return commands from workspace/expand/refresh paths

Change ProjectTree.SetWorkspace, expandNode, and reloadTree to return commands; Update propagates/batches them for keyboard and mouse operations. SetWorkspace(nil) invalidates generation and clears the visible tree plus queued work. A changed workspace/root or refresh increments generation before dispatch so old results cannot apply. A metadata-only same-path rebind keeps current loads; a root spelling/path rebase invalidates outstanding requests and reloads against the new root without allowing old-path results to enter the rebased tree.

Update `TabbedSidebar.SetWorkspace` to SafeBatch the Changes command and project-tree command without changing its public signature. Change test fixture helpers to pump returned messages/commands until quiescent, following FilePicker's test pump pattern. No synchronous fallback is allowed in tests or runtime; it would mask missing command propagation.

**Verify:** `go test ./internal/ui/sidebar -run 'TestProjectTreeAsyncNonblocking|TestProjectTree|Test.*Rebind' -count=1 -v` → PASS. Nonblocking test uses a readDir barrier: SetWorkspace/Update returns its command before that command is executed, and a separately executing blocked command does not prevent Update from handling navigation. No timing-only sleep assertions.

### Step 3: Apply current results and preserve interaction state

Handle ProjectTreeDirectoryLoaded before the existing focus guard in ProjectTree.Update. Validate tree generation, node path, and node request ID; also verify the node still wants expansion. Collapse invalidates that node's pending request and queued descendant requests, so delayed results cannot reopen it. A subsequent expansion obtains a new request ID. Current success installs children on Update, rebuilds the flat list, marks content dirty, and queues remembered expanded descendants using the bounded scheduler.

For refresh, snapshot expanded paths and selected path; retain the old visible tree while root reload is pending, then replace it with current results. Restore expansions incrementally and restore the selected path when available. Track a navigation version: if the user navigates while refresh is pending, do not later yank selection back to the old path. If the target disappears, clamp to a valid nearby row. Root-workspace changes clear the old tree immediately so another workspace's files are never shown as current.

Show a bounded root Loading indicator and a node-local loading marker using existing theme styles. A load error clears loading, preserves prior children for refresh where possible, and exposes a short sanitized error/retry hint; it must not masquerade as an empty successful directory. Retry remains existing expand/refresh input. Error state must be generation scoped. Keep control-text sanitation from project_tree_view intact.

**Verify:** `go test ./internal/ui/sidebar -run TestProjectTreeAsync -count=1 -v` → PASS for out-of-order workspace results, refresh generations, collapse/re-expand, navigation during refresh, missing nodes, and errors; `go test -race ./internal/ui/sidebar -count=1` → PASS without races.

### Step 4: Route completions while hidden/unfocused and test the app boundary

Add ProjectTreeDirectoryLoaded to TabbedSidebar.Update's explicit background-result routing, targeting projectTree regardless of active tab. Add it to App.update's sidebar-results case in `app_input.go`. Returned follow-up commands must propagate through both layers. In `app_project_tree_async_test.go`, obtain a real project load command, execute its result after switching to Changes/another focus/overlay, and assert it still reaches the current tree. Also assert old-workspace results cannot change the new tree through App routing.

**Verify:** `go test ./internal/app -run TestProjectTreeAsyncRouting -count=1 -v` → tests execute and PASS; `go test ./internal/ui/sidebar -count=1` → PASS with existing tab/focus tests preserved.

### Step 5: Document loading behavior and run rendering/final gates

Update ARCHITECTURE's leaf-widget boundary to describe async project-tree reads. Update README, CONFIG, and ORCHESTRATION together for workspace/refresh loading, retry behavior, and unchanged keyboard actions. Do not introduce a config knob for queue size. Run harness presets and perf on a quiescent host because loading/error rendering changed; do not rebaseline intentional regressions automatically. Finish sanitized input/devcheck and strict-new lint.

**Verify:** `make harness-presets` → presets succeed; `PERF_STRICT=1 make perf-check` → all host baselines pass; `env -u AMUX_WORKSPACES_ROOT make verify-loop` → required PASS lines; `env -u AMUX_WORKSPACES_ROOT make test-race` and `env -u AMUX_WORKSPACES_ROOT make test-race-tmux` → exit 0 with no races; `make lint-strict-new` → exit 0; `env -u AMUX_WORKSPACES_ROOT make devcheck` → exit 0, otherwise record BLOCKED; `git diff --check` → clean.

## Test plan

Keep expansion/cursor/sorting/hidden-file/mouse/rebase tests and adapt their command pumps. New tests must cover delayed root/child loads, old workspace root returning last, repeated refresh, collapse/re-expand, nested expansion restoration, user navigation overriding restoration, deleted target, ReadDir error with retry, hidden-tab/unfocused routing, follow-up command routing, bounded queue across generation churn, and content-version changes after results. Use per-model injected readDir and channels; do not depend on network mounts or sleeps. Test queued obsolete jobs are dropped and running stale jobs release slots without publishing content.

## Done criteria

All required final gates must pass before this plan is marked DONE. A known or newly discovered gate failure leaves the plan BLOCKED with the exact command/test and evidence; recording a failure is not a substitute for passing. Do not expand implementation scope to repair other findings. Until [plan 059](059-isolate-e2e-workspace-root.md) lands, remove `AMUX_WORKSPACES_ROOT` from every command reaching e2e, including devcheck, verify-loop, and test-race-tmux.

- [ ] No ProjectTree constructor/SetWorkspace/Update/View path calls ReadDir except inside a returned command.
- [ ] `TestProjectTreeAsync...` and `TestProjectTreeAsyncRouting...` execute and PASS, including concurrency bounds and stale-result cases.
- [ ] `go test -race ./internal/ui/sidebar -count=1` and all existing sidebar tests pass.
- [ ] Sanitized repository `make test-race` and `make test-race-tmux` pass without races; real tmux execution is recorded.
- [ ] Harness presets, quiescent strict perf gate, lint-strict-new, and sanitized verify-loop pass; devcheck outcome is honestly recorded.
- [ ] Docs agree on loading/retry and architecture; no new config/session surface or perf baseline change was introduced.
- [ ] `git diff --check` passes; only in-scope implementation changes were added beyond the recorded dirty baseline.
- [ ] Index owner receives completion and validation/blocker details.

## STOP conditions

Stop if another app caller requires a new public SetWorkspace API beyond the existing wrapper, if generation identity cannot survive canonical path rebinding without new data-layer changes, if the queued-command architecture requires a global goroutine service, or after two failed focused correction attempts. Do not add synchronous fallback or unbounded per-node goroutines. Investigate perf failure instead of rebaseline, and do not run e2e with ambient workspace-root overrides before plan 059.

## Maintenance notes

Async results need routing independent of focus at both App and TabbedSidebar. Path equality alone is not freshness: generation and node request identity handle refresh/collapse races. Keep read concurrency bounded across workspace churn, and preserve the limitation that kernel-blocked ReadDir cannot be cancelled by a Go context.
