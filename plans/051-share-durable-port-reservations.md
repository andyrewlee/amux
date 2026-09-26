# Plan 051: Share durable port reservations across app instances

> **Executor instructions:** Follow each step and its verification. This is a self-contained implementation handoff; no audit-session context is assumed. Honor the STOP conditions. No commits or pushes are authorized.
>
> **Drift check (run first):**
> ~~~sh
> git diff --stat 7c530ee..HEAD -- internal/data/workspace.go internal/data/workspace_identity_test.go internal/data/workspace_clone_test.go internal/data/port_reservations.go internal/data/port_reservations_test.go internal/data/port_reservations_subprocess_test.go internal/process/ports.go internal/process/ports_test.go internal/process/env.go internal/process/env_test.go internal/process/scripts.go internal/process/scripts_stop.go internal/process/scripts_release_test.go internal/process/session_env_test.go internal/process/run_session_test.go internal/app/app_init.go internal/app/app_workspace_status.go internal/app/app_workspace_status_test.go internal/app/workspacesvc/workspace_service_app_ops.go internal/app/workspacesvc/workspace_service_app_ops_test.go internal/app/workspacesvc/workspace_service_scripts.go internal/app/workspacesvc/workspace_service_scripts_test.go internal/app/port_reservations.go internal/app/port_reservations_test.go internal/app/port_reservations_integration_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git diff HEAD -- internal/data/workspace.go internal/data/workspace_identity_test.go internal/data/workspace_clone_test.go internal/data/port_reservations.go internal/data/port_reservations_test.go internal/data/port_reservations_subprocess_test.go internal/process/ports.go internal/process/ports_test.go internal/process/env.go internal/process/env_test.go internal/process/scripts.go internal/process/scripts_stop.go internal/process/scripts_release_test.go internal/process/session_env_test.go internal/process/run_session_test.go internal/app/app_init.go internal/app/app_workspace_status.go internal/app/app_workspace_status_test.go internal/app/workspacesvc/workspace_service_app_ops.go internal/app/workspacesvc/workspace_service_app_ops_test.go internal/app/workspacesvc/workspace_service_scripts.go internal/app/workspacesvc/workspace_service_scripts_test.go internal/app/port_reservations.go internal/app/port_reservations_test.go internal/app/port_reservations_integration_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md
> git status --short
> ~~~
> Compare BOTH commit changes and the current working tree against the Current state excerpts below. The audited baseline intentionally includes dirty user work on 2026-09-26. A known dirty file is not automatically a mismatch: preserve the audited behavior and the user's additional changes. On unexplained semantic drift, stop and report. New files will appear only in status, so inspect them before creating a same-named file.

## Status

- **Priority:** P2
- **Effort:** L
- **Risk:** MED
- **Depends on:** plans/039-transactional-workspace-field-updates.md; plans/041-stop-workspace-lifecycle-processes.md. Run plan 059 before broad e2e validation; plan 036 also affects the picker baseline.
- **Category:** bug
- **Planned at:** commit 7c530ee, 2026-09-26, INCLUDING the known dirty working tree (run-session picker/transcript-browser changes).
- **Status:** TODO

## Why this matters

A new app process starts allocating at the configured first port even while its previous tmux sessions remain alive. Two concurrent instances also maintain separate maps. Persisting reservations before launch makes a workspace's injected range stable across restarts and prevents a different workspace in the same amux state home from receiving that range.

## Current state

- internal/process/ports.go:22–27 initializes an empty in-memory allocator:
~~~go
func NewPortAllocator(start, rangeSize int) *PortAllocator {
    return &PortAllocator{
        portStart: start,
        rangeSize: rangeSize,
        allocated: make(map[string]int),
        nextPort:  start,
~~~
- internal/app/app_init.go:121–125 constructs a fresh runner on every app start:
~~~go
registry := data.NewRegistry(cfg.Paths.RegistryPath)
workspaces := data.NewWorkspaceStore(cfg.Paths.MetadataRoot)
scripts := process.NewScriptRunner(cfg.PortStart, cfg.PortRangeSize)
scripts.SetTranscriptMetadataRoot(cfg.Paths.MetadataRoot)
workspaceService := workspacesvc.New(registry, workspaces, scripts, cfg.Paths.WorkspacesRoot)
~~~
- internal/app/app_workspace_status.go:140–144 currently reconstructs the displayed range from current config:
~~~go
if base, ok := a.workspaceService.WorkspaceScriptPort(ws); ok {
    st.portBase, st.portAllocated = base, true
    if a.config != nil {
        st.portEnd = base + a.config.PortRangeSize - 1
    }
}
~~~
The service delegates to scripts.PortAllocated at workspace_service_app_ops.go:66–72. A persisted interval can have a different width from the current config, so this display must use the actual stored end.
- Every script, agent, viewer, and sidebar session receives the same runner's environment provider (app_init.go:200–215). internal/process/env.go:55–64 currently keys its allocation by a raw path:
~~~go
if b != nil && b.portAllocator != nil {
    port, rangeEnd, err := b.portAllocator.PortRange(ws.Root)
    if err != nil {
        return nil, err
    }
    env = append(env,
        fmt.Sprintf("AMUX_PORT=%d", port),
        fmt.Sprintf("AMUX_PORT_RANGE=%d-%d", port, rangeEnd),
    )
}
~~~
- internal/process/scripts_stop.go:122–125 releases a hosted workspace's range after its run-specific liveness query:
~~~go
if r.portAllocator != nil {
    r.portAllocator.ReleasePort(ws.Root)
}
return
~~~
That query does not prove every agent/terminal using the same environment has exited. StopAll at :158–173 intentionally stops local processes while hosted sessions persist. Plan 041 adds local lifecycle admission/draining and must be preserved.
- Workspace.MetadataID and ID both fall back to ComputedID when the private storeID is empty (internal/data/workspace.go:121–145). Add an explicit read-only stored-ID accessor rather than treating this fallback as proof of persistence. Successful Save sets storeID at workspace_store.go:247 and Load sets it at :169. The stored identity is authoritative; do not recompute durable identity from Root. Plan 039 establishes field-specific workspace writes; this reservation registry is separate and must never be included in an old full-workspace snapshot.
- Use the private file/OS lock conventions in internal/data/registry_lock_unix.go and registry_lock_open.go and atomic JSON writes in internal/fsatomic. internal/data/project_env.go:31–34 exemplifies a state-home store path, but its permissive corrupt-file recovery and only-process-local mutex are NOT suitable for reservations.
- process cannot import tmux because tmux already imports process (internal/tmux/tmux.go:20; also explained in internal/app/run_session_host.go:8–12). Keep tmux migration checks in app. Existing pane environments are embedded in launch commands, so show-environment is not an authoritative source of a legacy session's allocated range. Never parse or log those command strings; they may contain secrets.

## Behavior decisions

1. Production owns one durable registry at cfg.Paths.Home/port-reservations.json, with a sibling lock file and schema version 1. All upgraded app instances sharing that state home use it, independent of their tmux server choice. This does not promise exclusion against unrelated programs or other OS users/state homes; a reservation is not a bound TCP socket.
2. A reservation contains only the stable workspace metadata ID and an inclusive valid port interval. No environment values, commands, tokens, or paths are needed. Read, validate, select, and write under one cross-process exclusive lock using the existing data-package lock helpers. Commit the reservation BEFORE returning the environment to a spawn.
3. Existing IDs always receive their persisted interval, including after a configuration width/start change, relocation, restart, or restore. New IDs choose the first valid configured-size interval starting at the configured base that does not overlap ANY persisted interval, even intervals allocated under older settings. Validate 1–65535, positive width, and overflow before arithmetic. Exhaustion returns the existing typed error through spawn error handling.
4. Reservations are deliberately retained indefinitely in this first implementation. ReleaseWorkspace, old completion callbacks, quit, crash, failed spawn, delete, shelve, and a stale release from another instance NEVER make a durable interval reusable. This solves stale-release ownership without guessing that a run-only liveness query proves all consumers are dead. Reusing the same stored ID reuses its interval; a different ID never does. Keep the transient in-memory allocator's existing release behavior for isolated tests/nonproduction callers.
5. No automatic reclamation, TTL, socket probing, owner PID lease, new session tag, new public environment variable, or user config key is added. Finite-range exhaustion is the explicit retention tradeoff. A future reclaim operation needs proof across agents, terminals, runs, local lifecycle processes, and all app instances; it is outside this plan.
6. Corrupt, unreadable, null, overlapping, out-of-range, or newer-schema registry data causes a typed allocation/startup error and remains byte-for-byte untouched. Never silently use an empty map or switch production to the memory allocator.
7. First adoption requires a quiescent legacy installation: stop all old amux app processes and their amux sessions before launching the upgraded version. During initial registry creation, the app layer checks the configured tmux server for existing tagged amux sessions sharing its state-home namespace; any such session, ambiguous legacy amux session, or discovery error refuses initialization with an actionable message. Do not kill sessions or invent ranges. The operator must also stop legacy sessions on other custom tmux servers; no current API can prove their absence. Mixed old/new binaries during migration are unsupported and this limitation must be explicit in docs. After initialization, surviving upgraded sessions and concurrent upgraded apps require no migration check because the registry is authoritative.
8. A missing registry is handled with the same guarded initialization every time; it is never a license to ignore live sessions. A successful first initialization writes a valid empty envelope while holding the registry lock, then allocations use normal transactions. A failed guard creates no registry. The guard has the existing bounded tmux command timeout and may not run under an App/model or lifecycle mutex.

## Steps

### Step 1: Implement a locked, versioned reservation store

Create the three data files in Scope. Follow the existing registry lock/open and private atomic-write helpers instead of adding a second flock implementation. Provide initialization with an injected bounded guard, and a Reserve(workspaceID, start, size) operation returning the persisted interval. Initialization and reservation share the same lock ordering. Validate the entire current envelope before considering a write. Missing data may initialize only through the guard; schema/corruption failures preserve original bytes.

Write temporary-directory tests for two independent store objects, same-ID idempotence, different-ID exclusion, older intervals with a changed width/base, range exhaustion, invalid data, future schema, write failure, guard failure, and stable state after a fresh store instance. Add helper-subprocess contention coverage that exercises OS locking rather than only goroutines. The tests must demonstrate nonoverlapping committed intervals; do not only count lock calls.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/data -run 'Test.*(PortReservation|StoredID|Clone)' -count=1 → every new case passes, including independent subprocess allocation and preservation of refused bytes.

### Step 2: Wire durable identity into the shared environment allocator

Add Workspace.StoredID() (WorkspaceID, bool), returning the private storeID and whether it is nonempty without recomputing any path identity. Test unsaved false, successful Save true, Load true even under a drifted key, failed Save false for a new record, and Clone preservation. Do not change existing ID/MetadataID fallback semantics.

Extend PortAllocator with a clearly separate durable backend, preserving the existing transient constructor for tests. Introduce an identity-aware workspace reservation method used by BOTH BuildEnvLayers and BuildEnvMap; in durable mode require a nonempty valid stored metadata ID, rather than falling back to a path hash. In production app_init.go, construct the durable store using cfg.Paths.Home. Resolve tmux options and the state-home instance namespace before the first-initialization guard; finish guarded initialization and inject the durable allocator into ScriptRunner before any environment provider is exposed or any spawn is scheduled. Keep one allocator shared by setup, run, archive/on-done, agent/viewer, and sidebar environments.

Confirm production launch inputs are persisted: CreateWorkspace saves before returning WorkspaceCreated (workspace_service.go:171–183), ordinary loading starts with stored records (workspace_service_load.go:90–109), and primary checkout synthesis attempts LoadMetadataFor/Save (workspace_service_load.go:190–200). Primary metadata load/save failures can currently leave an unsaved visible workspace; the durable environment provider must refuse its spawn with an actionable metadata-persistence error, never mint a fallback ID or perform a hidden full Save. Cover this error path and the successful primary/create/restore paths in app tests.

Make durable release paths retain their reservation, including pending releases/completion callbacks left by plan 041. Do not disturb local lifecycle cancellation or hosted session persistence. Inject store errors through the existing environment/spawn error returns; never launch with a guessed or omitted AMUX_PORT.

Add a non-allocating interval lookup returning base, end, found, and error through data store → process runner → workspace_service_app_ops.go. Keep existing synchronous cached getters memory-only; they must not gain disk I/O. In app_workspace_status.go, perform the authoritative lookup in the existing handleShowWorkspaceStatus tea.Cmd alongside RunScriptStatus, capturing the stored ID/safe workspace snapshot before return. Carry the actual interval in workspaceStatusReadyMsg and apply it only after the existing dialog-token fence; remove current-config end reconstruction. A failed lookup returns the existing user-visible error path rather than displaying a guessed range. No interval is allocated merely to view status.

Add status/service tests for changed config width with an old persisted interval, restart before local allocation, another instance having reserved the workspace, absent reservation, read failure, no allocation on read, and a stale status result after another dialog opens. The status render structure is unchanged; only its supplied interval becomes authoritative.

Add tests with two runners using the same temporary state home: same stored ID produces identical AMUX_PORT and AMUX_PORT_RANGE; distinct IDs get disjoint ranges; a new runner after release/shutdown preserves the old range; delayed release from runner A cannot free runner B's current range. Assert ordinary user/repo env precedence and reserved-key filtering remain unchanged.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/process ./internal/app/workspacesvc ./internal/app -run 'Test.*(Port|Env|Release|Session|WorkspaceStatus)' -count=1 → transient release, durable ownership, and actual-interval status cases pass.

### Step 3: Make first adoption fail safely around legacy sessions

In app/port_reservations.go implement the initialization guard using existing tmux discovery, instance namespace matching, and no-server classification; do not add a process-to-tmux import. Run it only while creating a missing registry, before the production runner can spawn. The check must be conservative about recognizable amux sessions lacking sufficient tags. A non-amux session alone is not a blocker. Discovery failure is not an empty result.

Return an actionable startup error explaining that old app instances and existing amux sessions must be stopped once before retrying; never automatically kill them, delete metadata, or print session environments. Document the cross-custom-server and mixed-version migration prerequisite. Test absent server, clean server, unrelated sessions, same-home legacy sessions, ambiguous old amux sessions, discovery failure, and a second upgraded instance observing a valid registry. For first-initializer contention, demonstrate that the losing initializer sees the committed envelope instead of attempting a conflicting initialization.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app -run 'Test.*PortReservation' -count=1 → blocked adoption leaves sessions/bytes unchanged, clean adoption succeeds, and a valid existing registry permits restart.

### Step 4: Prove persistence with a real surviving tmux session and document retention

Use a test-owned state directory and isolated tmux server. Launch a workspace's run session using the durable provider, discard the first runner without killing that session, build a second runner, then allocate for another workspace. Assert that the surviving session retains its first range and the second workspace receives a disjoint range; do not use listening-port availability as the ownership oracle. Exercise same-ID reattachment/allocation and delayed release while a different session consumer remains alive. Scope cleanup to the server/sessions created by the test.

Update README.md, docs/CONFIG.md, and docs/ORCHESTRATION.md together: shared-state-home guarantees, port-setting changes affecting only new IDs, retained reservations, exhaustion, fail-closed errors, first-upgrade quiescence and custom-server limitation. State plainly that there is no automatic range reclamation in this release and that users must not delete the registry while sessions exist. Do not add an undocumented reset command or instruct users to bulk-delete their actual state.

**Verify:** env -u AMUX_WORKSPACES_ROOT go test ./internal/app -run 'Test.*PortReservation' -count=1 -v → real-tmux restart case PASS without a skip; then run every final command below.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Focused package verification | env -u AMUX_WORKSPACES_ROOT go test ./internal/data ./internal/process ./internal/app/workspacesvc ./internal/app | All selected tests pass; no new skips |
| Race verification | env -u AMUX_WORKSPACES_ROOT go test -race ./internal/data ./internal/process ./internal/app/workspacesvc ./internal/app | Exit 0, no race reports |
| Standard repository gate | env -u AMUX_WORKSPACES_ROOT make devcheck | Exit 0; investigate and record baseline exceptions as described below |
| Changed-code strict gate | make lint-strict-new | Exit 0, zero new issues and clean formatter diff |
| Full concurrency gate | env -u AMUX_WORKSPACES_ROOT make test-race | Exit 0, no race reports |
| Real tmux concurrency gate | env -u AMUX_WORKSPACES_ROOT make test-race-tmux | Exit 0, no race reports; report skips |
| Real input path | env -u AMUX_WORKSPACES_ROOT make verify-loop | Both real-agent keystroke tests pass without skips |
| Real tmux lifecycle suite | env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e | Exit 0; report skips/baseline blockers |
| Portability | make windows-build | Exit 0 |
| Diff integrity | git diff --check | Exit 0 |

No dependency installation or module upgrade is needed. Format changed Go files with the repository's gofumpt-compatible tooling; make fmt is the repository formatting command, but do not accept unrelated formatting changes in this dirty checkout.

**Verification baseline:** Audit verification is not wholly green: the broad devcheck run failed in real e2e tests. An ambient AMUX_WORKSPACES_ROOT escaped the test HOME and caused a collision with the user's real workspace root; plan 059 owns that isolation defect. Until 059 lands, prefix every command below that can reach e2e with env -u AMUX_WORKSPACES_ROOT. The filtered-picker regression is separately owned by plan 036 and may affect e2e results. The real input verify-loop passed during the audit. Record exact remaining failures; do not fix unrelated failures, weaken checks, kill user sessions, or claim skipped real-tmux tests provide end-to-end validation.

## Scope

**In scope — only these paths may be changed for this implementation:**

- internal/data/workspace.go
- internal/data/workspace_identity_test.go
- internal/data/workspace_clone_test.go
- internal/data/port_reservations.go
- internal/data/port_reservations_test.go
- internal/data/port_reservations_subprocess_test.go
- internal/process/ports.go
- internal/process/ports_test.go
- internal/process/env.go
- internal/process/env_test.go
- internal/process/scripts.go
- internal/process/scripts_stop.go
- internal/process/scripts_release_test.go
- internal/process/session_env_test.go
- internal/process/run_session_test.go
- internal/app/app_init.go
- internal/app/app_workspace_status.go
- internal/app/app_workspace_status_test.go
- internal/app/workspacesvc/workspace_service_app_ops.go
- internal/app/workspacesvc/workspace_service_app_ops_test.go
- internal/app/workspacesvc/workspace_service_scripts.go
- internal/app/workspacesvc/workspace_service_scripts_test.go
- internal/app/port_reservations.go
- internal/app/port_reservations_test.go
- internal/app/port_reservations_integration_test.go
- README.md
- docs/CONFIG.md
- docs/ORCHESTRATION.md
- plans/README.md — only this plan's status row, unless the reviewer maintains it.

**Out of scope:** all other source files; unrelated refactoring; dependency/toolchain upgrades; changes to the trust model or automatic Git push/checkout behavior; other plans' implementation. Do not expand wildcard test scope into production edits.

## Git workflow

Work on advisor/051-share-durable-port-reservations only if the operator has selected that branch; otherwise use their current checkout. Do not commit, push, open a PR, stash, clean, reset, or discard user edits without explicit instruction. Before editing, record git status --short and the existing diff. Existing dirty changes are input to this plan, not cleanup targets. Only this plan's implementation delta must fit Scope; pre-existing unrelated edits may remain. Update only this plan's row in plans/README.md at completion unless the reviewer maintains that index.

## Test plan

Use the regression cases and existing test exemplars in the steps. Tests must assert the observable outcome, not merely that a new helper was called. Use temporary repositories/metadata and isolated tmux servers, never the user's actual state. Channel/barrier-based scheduling is preferred over arbitrary pacing sleeps.

## Done criteria

- [ ] Every regression case in Steps passes with env -u AMUX_WORKSPACES_ROOT go test ./internal/data ./internal/process ./internal/app/workspacesvc ./internal/app.
- [ ] env -u AMUX_WORKSPACES_ROOT go test -race ./internal/data ./internal/process ./internal/app/workspacesvc ./internal/app exits 0 with no races.
- [ ] env -u AMUX_WORKSPACES_ROOT make devcheck and make lint-strict-new were run; both pass, or the exact independently established baseline blocker is recorded and this plan remains BLOCKED rather than DONE.
- [ ] env -u AMUX_WORKSPACES_ROOT make test-race: Exit 0, no race reports.
- [ ] env -u AMUX_WORKSPACES_ROOT make test-race-tmux: Exit 0, no race reports; report skips.
- [ ] env -u AMUX_WORKSPACES_ROOT make verify-loop: Both real-agent keystroke tests pass without skips.
- [ ] env -u AMUX_WORKSPACES_ROOT go test ./internal/tmux ./internal/e2e: Exit 0; report skips/baseline blockers.
- [ ] make windows-build: Exit 0.
- [ ] git diff --check exits 0.
- [ ] git diff --name-only and git status --short, compared to the captured initial state, show no implementation edits outside Scope.
- [ ] User work is preserved; no commit/push occurred; this plan's status row is updated only when all required work is complete.

## STOP conditions

- The relevant live semantics differ from the excerpts and steps for reasons not explained by the declared dependencies or audited dirty baseline.
- A verification fails twice after a focused, reasonable fix attempt.
- The change requires production files outside Scope, alters a public behavior this plan explicitly preserves, or cannot be made without discarding user edits.
- Plans 039 and 041 must be complete or their equivalent behavior independently confirmed; do not implement an old field-write or stop contract.
- Stop if a durable production allocation can occur before registry injection, or if any launch route bypasses the shared environment provider.
- After adding StoredID, stop if any production launch legitimately requires an unsaved workspace and cannot return the planned persistence error; do not silently switch durable keys to Root or hide a metadata Save inside the environment builder.
- Do not claim automatic migration of legacy live sessions. If the requested product contract requires uninterrupted first-upgrade adoption, stop for a revised plan; current source cannot recover their allocated ranges reliably.
- Do not add automatic reclamation to avoid a failing exhaustion test, reuse on a run-only liveness result, or silently accept corruption.
- Do not promise collisions are impossible across separate state homes, unrelated applications, or legacy processes running contrary to the migration prerequisite.
- A broad baseline check fails outside scope: report the exact test/error and retain BLOCKED status; do not silently waive the gate or repair unrelated code.

## Maintenance notes

- The durable registry is the range owner; app processes and tmux sessions are consumers. Its lifetime intentionally exceeds all consumers.
- Retaining allocations makes stale release a no-op and avoids a distributed lease protocol. Reviewers should scrutinize first-adoption wording, stable identity, cross-process locking, and full-envelope validation.
- Deletion/reclamation is a future product decision with an explicit drain-and-proof contract, not background GC. High workspace churn can exhaust the configured port space under the conservative first version.
- No public environment/tag redesign is required. Preserve existing AMUX_PORT/AMUX_PORT_RANGE names and layer precedence.
- Tests must never inspect or reproduce user environment values; only test-created port keys and synthetic IDs are needed.

