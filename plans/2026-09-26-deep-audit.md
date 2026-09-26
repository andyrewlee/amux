# Deep audit — 2026-09-26

Audited commit: `7c530ee`, **including the existing uncommitted working tree**. Finding 02 is introduced by those uncommitted changes; the other confirmed findings are present in committed code. This is an advisory audit, not an implementation plan. No source files were modified.

The previous audit's plans 001–033 are marked DONE and 034 is REJECTED. Those statuses and the user's existing edits to `plans/README.md` were preserved. Findings below describe remaining or newly introduced defects, not requests to repeat completed plans.

## Prioritized findings

Effort includes implementation and tests: S = hours, M = roughly a day, L = multiple days. Risk describes the **fix**, not the severity of the defect. All 22 original findings have HIGH source confidence; the narrower on-done-hook consequence in 18 is MED and is not necessary to establish the tag defect.

| # | Finding | Category | Impact | Effort | Fix risk | Main evidence |
|---|---|---|---|---|---|---|
| 01 | Recheck merge destination after confirmation | Correctness | Merge can modify a branch the user did not approve | S | MED | `internal/app/app_workspace_merge.go:171` |
| 02 | Restore filtered-picker arrow/Tab navigation | Correctness / tests | Cannot navigate the agent list by keyboard | S | LOW | `internal/ui/common/dialog_update.go:89` |
| 03 | Capture diff-tab index before asynchronous dispatch | Correctness / concurrency | Wrong index and concurrent Go map access | S | LOW | `internal/ui/center/model_tabs_viewer.go:178` |
| 04 | Merge persisted transcripts before new writes | Correctness / persistence | Normal post-restart runs erase older script diagnostics | S–M | LOW | `internal/process/script_output_store.go:80` |
| 05 | Make workspace writes field-scoped transactions | Architecture / correctness | Pending tab saves revert settings, scripts, or names | M | MED | `internal/app/app_persistence.go:125` |
| 06 | Drain all PTY output before termination | Correctness / tests | Terminal exit loses final output | M | MED | `internal/ui/ptyio/pty_reader.go:144` |
| 07 | Stop lifecycle subprocesses during workspace teardown | Correctness / lifecycle | Setup survives deletion/shelving; reruns lose tracking | M | MED | `internal/process/scripts_stop.go:16` |
| 08 | Snapshot assistant configuration for async launches | Correctness / concurrency | Settings updates race launch-time map reads | M | MED | `internal/pty/agent.go:109` |
| 09 | Preserve config on read failures and reject null roots | Correctness / persistence | Partial overwrite or panic during Settings save | S | LOW | `internal/config/user_settings.go:85` |
| 10 | Accept bracketed paste in custom editors | Correctness / usability | Pasted env values and commands silently disappear | S | LOW–MED | `internal/ui/common/env_dialog.go:147` |
| 11 | Fence sidebar reattachment results by generation | Correctness / lifecycle | Late results undo detach or replace/leak newer PTYs | M | MED | `internal/ui/sidebar/terminal_update_session.go:132` |
| 12 | Reuse the metadata snapshot for shelved rows | Performance | 1 + project-count complete metadata scans per reload | S | LOW | `internal/app/workspacesvc/workspace_service_load.go:131` |
| 13 | Prefer explicit typed paths in the file picker | Correctness / usability | Enter can select a different directory/file | S | LOW–MED | `internal/ui/common/filepicker_navigation.go:269` |
| 14 | Bound Git cancellation and descendant pipe draining | Correctness / process lifecycle | Operations and locks can outlive their timeout indefinitely | M | MED | `internal/git/operations.go:310` |
| 15 | Preserve explicit zero interrupt delays | Correctness / config | Saving assistant commands changes later Ctrl-C timing | S | LOW | `internal/config/config.go:317` |
| 16 | Scope hook skip flags to the advertised checks | DX / docs | Skip-lint/skip-harness silently disable unrelated gates | S | LOW | `.githooks/pre-commit:4`, `.githooks/pre-push:4` |
| 17 | Share port reservations across persistent sessions | Architecture / correctness | Restarted/concurrent instances allocate occupied ranges | M–L | MED | `internal/process/ports.go:22` |
| 18 | Compare activity with last published semantic state | Correctness / orchestration | Clock-only transitions leave tmux state tags stale | M | MED | `internal/app/app_tmux_activity_result.go:192` |
| 19 | Keep the executable pathname present during update | Correctness / update | Interruption between renames leaves amux missing | M | MED | `internal/update/install.go:202` |
| 20 | Scroll wrapped diffs by visual rows | Correctness / rendering | Long wrapped lines have unreachable tails | M | MED | `internal/ui/diff/model.go:255` |
| 21 | Move project-tree directory reads off Update | Performance / architecture | Slow directory reads freeze the whole TUI | M | MED | `internal/ui/sidebar/project_tree.go:206` |
| 22 | Remove the process-scoped fake-agent build artifact | DX / tests | Every fresh e2e process leaves a compiled temp fixture | S | LOW | `internal/e2e/fakeagent_test.go:32` |

## Evidence, remediation boundaries, and missing tests

### 01 — Merge destination changes while confirmation is open

The preflight checks HEAD at `internal/app/app_workspace_merge.go:81–95`, then opens a dialog. The later command at `:171–186` passes only repository and source branch to `git.MergeWorkspaceBranch`; `internal/git/merge.go:126` merges into whichever branch is checked out then. The success toast uses the earlier approved base at `app_workspace_merge.go:212`.

Carry the approved destination into the mutation boundary and recheck immediately before executing. Preserve the product rule that amux never checks out a different branch automatically. Serialize amux-originated repository writes where appropriate; do not claim this eliminates all external Git races.

Test by opening confirmation on main, switching the primary checkout's branch, then executing confirmation. Assert refusal and unchanged branch tips.

### 02 — Filtered picker navigation is disabled

The current working-tree change excludes every filtered select from both navigation branches at `internal/ui/common/dialog_update.go:89–114`. That excludes Up/Down/Tab/Shift+Tab along with printable j/k. `internal/ui/common/agent_picker.go:41` enables filtering for the agent picker.

Separate structural navigation keys from printable filter text. Test actual key messages for all four navigation keys and j/k text entry. Existing rendering/filter tests and the new unfiltered run-picker tests do not exercise this regression. Fix risk is low because the desired behavior is already clear.

### 03 — Diff-tab command reads an event-loop-owned map

The closure at `internal/ui/center/model_tabs_viewer.go:178` reads `m.tabs.ActiveByWorkspace` when its asynchronous command runs. Selection mutates the same ordinary map at `internal/ui/center/model_tab.go:381`. The command can report a later selection's index, and overlapping access can produce a Go map race.

Capture the created index before constructing the command. A deterministic test should create the diff tab, change selection, execute the retained command, and assert the original index. Match the existing immutable command-result style.

### 04 — Transcript persistence loses other types after restart

`internal/process/script_output_store.go:80–96` writes the full envelope from this process's memory. Only `LastScriptOutputs` hydrates older disk entries (`script_output.go:103–110`). A restarted process recording on-done before opening the viewer therefore overwrites the preceding process's setup/archive entries. The reader's future-version guard at `script_output_store.go:116` has no writer counterpart.

Read, validate, merge by script type/freshness, and write under a per-workspace transaction. Preserve newer-schema files and keep failures best effort. Disk-to-memory hydration must not overwrite a newer in-memory result during a concurrent record.

Test restart → record a different type → read, plus newer-schema write refusal and competing runner writers. This is a follow-up defect in plan 031's shipped behavior, not a repeat of its implementation.

### 05 — Atomic files still suffer lost updates

`internal/app/app_persistence.go:125–147` snapshots the whole workspace and saves it later. `internal/data/workspace_store.go:234–235` replaces the complete record. `SetEnv` (`workspace_store_env.go:20–31`), `SetScripts`, and `Rename` (`workspace_store.go:257–272`) independently load, modify, and save outside a transaction spanning the read.

Use a locked fresh-load/mutate/write primitive and a narrow tab-state operation. Preserve workspace identity, discovery ownership, mutation guards, and shutdown handling. Order competing tab saves as well; field-scoped writes alone do not prevent an older tab snapshot from winning.

Retain a pending tab-save command, perform a settings change, then execute the old command. Assert both settings and tabs survive. Add two-field-setter interleavings. This is architectural debt with a demonstrated data-loss consequence.

### 06 — EOF can overtake buffered PTY bytes

The reader checks error before processing `n > 0` at `internal/ui/ptyio/pty_reader.go:87–96`. It also sends errors on a separate channel; after receiving one at `:144`, the exits at `:189` and `:204` can report Stopped before queued data chunks are consumed.

Preserve byte ordering through EOF, handle bytes returned with an error, and send exactly one terminal result after draining. Keep cancellation responsive and queues bounded.

Existing `pty_reader_test.go:198` exercises many chunks but asserts the final error rather than complete output. Add byte-completeness tests across size flushes, timer flushes/backpressure, and a read returning data plus EOF.

### 07 — Hosted runs bypass lifecycle-process teardown

Production installs a run host (`internal/app/app_init.go:199`). In hosted mode, `ScriptRunner.Stop` returns after handling tmux run sessions (`internal/process/scripts_stop.go:16–30`), never checking `r.running`. Setup still uses that subprocess map (`scripts.go:249–256`). Shelve calls Stop before archive/removal (`workspace_service_shelve.go:113–126`).

Repeated rerun requests are also accepted at `internal/app/app_workspace_scripts.go:34–48`, while `setRunningEntry` replaces the one tracked slot (`scripts.go:158–164`).

Separate hosted-run tracking from lifecycle work. Teardown must prevent new setup steps, stop/drain current lifecycle processes, and reject or serialize overlapping setup. Preserve persistent-session behavior on ordinary quit.

Combine a hosted runner with blocked setup in tests, then exercise delete/shelve and repeated reruns. Existing tests cover these modes separately.

### 08 — Assistant settings race asynchronous launch

`internal/pty/agent.go:109` reads `m.config.Assistants` from commands dispatched at `internal/ui/center/model_tabs.go:113–130`. Settings writes the same map at `internal/app/app_dialog_settings.go:143`.

Resolve immutable assistant settings before dispatch, or make every read and write use shared synchronization. The manager's mutex cannot protect direct external writes to Config. Include create, restart, and reattach readers in scope; future launches must still see newly saved settings.

Test settings updates concurrent with launch configuration lookup under the race detector. Existing tests cover tmux-option synchronization, not this map.

### 09 — Saving cannot safely preserve an unreadable/null config

`internal/config/user_settings.go:85` and `config.go:302` ignore non-ENOENT read errors, then write partial payloads. JSON null makes the payload map nil without an unmarshal error; assignment panics at `user_settings.go:106` / `config.go:322`. App.Update recovers that panic (`internal/app/app_input.go:25`), so this is an aborted save/internal error, not a proven process crash.

Share read-for-update validation: allow absence, propagate other read failures, and require a nonnil object. Preserve original bytes on rejection. Test null and an unreadable existing file in a writable directory, not only malformed JSON.

### 10 — Custom editors discard paste messages

`internal/ui/common/env_dialog.go:147–150` and `scripts_dialog.go:74–77` accept only KeyPressMsg. `settings.go:190–243` handles mouse/key events but no PasteMsg. The app overlay route passes and consumes paste (`internal/app/app_input_dialogs.go:42–50`) without converting it into typing.

Handle paste as text through the focused field's validation. Cover add modes and specify single-line newline handling; pasted characters must never execute shortcuts or toggle mode rows.

Add paste tests for environment values, setup/run commands, assistant commands, tmux text fields, noneditable rows, cancellation, and save.

### 11 — Late sidebar attachment outcomes overwrite newer state

Automatic attachment has a guard (`terminal_sessions.go:27–44`); manual reattach/restart at `terminal_pty_attach.go:153–198` does not acquire it. `terminal_reattach_stall.go:25–44` releases stalled attempts without cancelling them. Result structs at `terminal_pty_config.go:76–99` have no generation.

`terminal_update_session.go:132–136` applies every success and overwrites the terminal without closing a previous one; failures at `:159–169` can stop newer successful state. A success can also clear an explicit user detach.

Follow the center pane's epoch/stale-detach discipline. Invalidate on detach/teardown, guard all dispatches, and close rejected successful PTYs. Test reordered success/failure, detach during attach, and retry after stall.

### 12 — Shelf loading reintroduces repeated full scans

`workspace_service_load.go:80` already gathers one record set. Its per-project loop calls `listShelvedWorkspaces` at `:131`; that calls `ListByRepoIncludingArchived` (`workspace_service_shelve.go:253`), which calls `ListAll` (`internal/data/workspace_store.go:437`).

Use the same snapshot for live and shelved rows, preserving archived/shelved filtering and fallback behavior. A counting-store test should assert one snapshot across several projects. The repeated work is source-proven; its wall-clock cost was not benchmarked.

### 13 — Explicit file-picker paths lose to the current selection

`internal/ui/common/filepicker_navigation.go:137–143` retains the current listing when input names a different absolute/home-relative path. Enter first selects from that listing at `:269–291`, reaching explicit path resolution only afterward at `:293–306`.

Recognize explicit-path intent before current-row selection while preserving fuzzy selection and asynchronous Stat. Test public Update paste/type + Enter with a populated current directory and an external target; existing helper-level tests bypass the problematic precedence. Review autocomplete ordering in the same change.

### 14 — Git cancellation waits for descendant-held pipes

After cancellation, `internal/git/operations.go:310–316` kills Git but waits unboundedly for Cmd.Wait. Captured output uses non-file writers, and no WaitDelay/process-group teardown bounds inherited pipes.

The existing 50 ms cancellation test (`operations_coverage_test.go:228–231`) passed in 1.30 s with a one-second child; it checks error classification, not elapsed termination. The underlying pipe behavior is documented in [Go's Cmd.WaitDelay contract](https://pkg.go.dev/os/exec#Cmd).

Bound process-tree termination and pipe draining using existing platform helpers where feasible. Test descendants holding output pipes, elapsed bounds, child cleanup, and error classification. Fix before relying on Git timeout guarantees in recovery UX.

### 15 — An explicit zero interrupt delay does not round-trip

`internal/config/config.go:317` omits zero. Loading starts with built-in defaults and only overrides present pointer fields (`:186–217`); Claude's delay is 200 ms (`agents.go:19`). Editing/saving an assistant command can therefore reset a deliberately configured zero on restart.

Serialize the effective zero, preferably with a direct load/save/load fidelity test. Current assistant-save round-trip assertions focus on commands and nonzero delays.

### 16 — Hook flags disable more than their documented scope

`.githooks/pre-commit:4–6` exits the entire hook for AMUX_SKIP_LINT, bypassing format, file-length, and drift checks too. `.githooks/pre-push:4–6` exits before strict lint and e2e tests when AMUX_SKIP_HARNESS is set. `CONTRIBUTING.md:81–84` describes narrower escapes.

Conditionally skip the named stage rather than exiting the whole hook, preserving the documented contract. Test script control flow with stubbed commands so checks can be asserted without committing/pushing or running an expensive suite. Review the user-facing env documentation with the behavior change.

### 17 — Port reservations have shorter lifetimes than their sessions

Every new allocator starts empty at the same configured base (`internal/process/ports.go:22–27`); every app creates one (`app_init.go:123`). Hosted runs and agent sessions persist, while `StopAll` kills only local tracked subprocesses (`scripts_stop.go:158–173`). README promises parallel workspace collision avoidance at `:190`.

Define durable shared reservation ownership, or rebuild/claim reservations from persistent session state before new allocation. Include multiple amux instances, stale cleanup, workspace identity, and existing-session migration. This needs an explicit design decision before coding.

Tests currently share one allocator. Add independent runners, restart with a surviving server, and allocation for a different workspace. Avoid presenting port probing alone as a durable ownership solution.

### 18 — Time-only semantic transitions are never published

`internal/app/app_tmux_activity_result.go:195–196` reclassifies both prior and next snapshots at the current time. `internal/app/activity/agent_state.go:70–79` depends on elapsed hold/done windows. After a done window expires, both snapshots classify idle, producing no tag write even though the last published tag was done.

Compare against last applied/published semantic state, evaluating retained live sessions each scan. Define reset/owner-handoff behavior so hooks are not duplicated. Test unchanged-content scans with a controlled clock through working/done/idle. The stale done tag is HIGH confidence; related missed on-done fallback edges need targeted characterization.

### 19 — Self-update has a missing-executable interval

`internal/update/install.go:202` moves the existing executable away, then `:207` installs the new one. Returned errors trigger rollback; process interruption between the calls does not. This contradicts the atomic-replacement claim at `:173`.

Prepare backup without removing the live name, then use platform-appropriate replacement and directory syncing. Go documents replace-existing behavior and its platform caveat in [os.Rename](https://pkg.go.dev/os#Rename); amux's release targets are Darwin and Linux.

Test pathname continuity and interruption/failure handling, preserving permissions and recoverability. This revises the previous audit's broad atomic-update rejection; signature/checksum/archive confinement were not found broken.

### 20 — Wrapped diff content cannot all be reached

`internal/ui/diff/view.go:212–244` slices logical lines and emits all their wrapped rows. `model.go:255–264` also bounds scrolling by logical line count. A short diff with one line that wraps across several screens has zero maximum scroll.

Use a visual-row representation with source-line/hunk mapping, and apply it consistently to viewport limits, scrolling, jumps, wrap toggles, and resizing. Test bounded rendered height and reachability of the final wrapped segment, beyond current Unicode/nonempty wrapping assertions.

### 21 — Project-tree I/O blocks the single writer

`internal/ui/sidebar/project_tree.go:206` calls ReadDir synchronously on expansion. Workspace changes and recursive refresh reach it through reloadTree, including every previously expanded directory.

Move reads to commands with immutable results and workspace/node request generations. Preserve expansion and cursor state. Test delayed/out-of-order loads, collapse/re-expand, and workspace replacement. Plan 019's FilePicker work is a useful pattern; it did not cover this separate tree path.

### 22 — E2E fake-agent binaries have no cleanup owner

`internal/e2e/fakeagent_test.go:32–44` creates a shared temporary build directory and never removes it, including after a failed build. `main_test.go:20` cleans only the separate amux binary.

Give the fixture process-scoped cleanup after parallel tests finish, with immediate failed-build cleanup. Avoid per-test cleanup of a shared binary. This is low priority relative to runtime correctness.

## Direction options

These are product choices, not defects ranked against the table.

1. **Search retained script output.** The output dialog already supports scrolling and following (`internal/ui/common/output_dialog.go:98–136`), and persisted transcripts give it historical material. A small search/next/previous feature would make finding the cause of a failed run easier. Coarse effort M, grounding HIGH; decide how search interacts with follow mode and refreshed output. Complete 04 first. Saved full terminal transcripts currently open in the configured viewer, which may already offer search, so keep the scope on the in-app script-output surface.

2. **Expose recovery diagnostics in workspace status.** The status dialog aggregates operational state (`internal/app/app_workspace_status.go:48–65`), while unfinished deletion stages are mostly warning logs and resurfaced rows (`internal/app/workspacesvc/workspace_delete_tombstone.go:129–139`). A design spike could add pending stage, latest error, and an existing safe retry action. Coarse effort M, grounding HIGH; the cost is maintaining a stable user-facing interpretation of cleanup markers. Stabilize 05, 07, and 14 first.

## Additional finding from baseline verification

**23 — E2E inherits the real workspace root despite temporary HOME.** HIGH confidence, correctness/tests, P1, S effort, LOW fix risk. `internal/e2e/pty.go:101–108` copies the parent environment, replaces HOME, and then applies explicit fixture overrides. `internal/e2e/util.go:14` strips only Git-specific overrides. `internal/config/paths.go:34` intentionally prefers `AMUX_WORKSPACES_ROOT`; `internal/app/app_init.go:371` exports that variable in normal amux sessions. Consequently tests launched inside amux can create fixture worktrees in the user's actual workspace root.

The diagnostic run encountered an existing `~/.amux/workspaces/001/qtest` and failed before opening the expected trust dialog. Removing that env key only for the child test command made the overlay and shelf tests pass. Plan 059 sets the fixture root explicitly while preserving deliberate test-owned overrides, with synthetic-env and real-binary sentinel tests. No actual user collision directory was removed, and no claim is made that the diagnostic run left the external filesystem unchanged. Further e2e commands were sanitized after diagnosis.

## Selection and dependency guidance

The user selected **all 22 findings and both direction options**. Plans 035–056 cover the findings; 057–058 are design spikes. Baseline verification subsequently confirmed the additional isolation defect below, planned as 059. Start execution with 059 and 036 to establish a usable test baseline, then follow [the index](README.md). Plan selection authorizes these handoffs; this audit did not implement source changes.

- 01–04 are independent.
- 05 should establish store transaction conventions before expanding durable state for 17.
- 06 and 11 are distinct PTY reader and attach-lifecycle repairs; characterize each separately.
- 07 should land before adding richer lifecycle control/recovery features.
- 09 and 15 share config persistence files; implement the validation/helper boundary first, then serialization fidelity.
- 10 should cover all custom editors together, with shared field semantics rather than three divergent paste implementations.
- Every substantive implementation plan should require `make devcheck` and `make lint-strict-new`; PTY/input/tmux changes also require `make verify-loop`, relevant real-tmux tests, and race tests.
- Render changes require `make harness-presets` and `PERF_STRICT=1 make perf-check` on this Darwin/arm64 host.
- Changes to lifecycle, keys, config/env, or tags must update README.md, docs/CONFIG.md, and docs/ORCHESTRATION.md in the same implementation.

## Verification

- `make govulncheck`: PASS, “No vulnerabilities found.”
- `make lint-strict-new`: PASS, zero issues and clean formatter diff.
- `make devcheck`: FAIL in the real-e2e portion after vet and the ordinary package sweep passed. The complete suite was not rerun under a sanitized environment. A diagnostic e2e rerun was stopped after the inherited workspace-root problem was identified; do not treat that interrupted run as a completed baseline.
- `make verify-loop`: PASS, including `TestCloseLoopKeystrokeDeliveryToRawAgent` and `TestFakeAgentRecordsRawCarriageReturn`.
- `make lint lint-config-drift check-fmt-config`: PASS.
- Isolated focused e2e command: `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^(TestOverlayQueueDefersAsyncOpenUntilFirstResolves|TestShelveRestorePurgeLifecycle|TestWorkspaceCreateAgentsHaveDistinctSessions)$' -count=1 -v`. Overlay queuing PASS (4.78 s), shelve/restore PASS (10.50 s), distinct-agent selection FAIL (46.06 s): two Claude sessions, no Codex. This is direct runtime evidence for finding 02 / plan 036.
- Focused Git timeout test: PASS, 1.30 s despite its 50 ms deadline and one-second child; this supports finding 14 and exposes the missing timing assertion.
- Existing short-lived run creation/status tests: 15 repetitions each passed; no skips. The hypothesized retention-setup race remains unconfirmed.

A clean vulnerability scan is bounded to the tool's known-vulnerability analysis, not proof that application code is secure. [Go vulnerability tooling documentation](https://go.dev/doc/security/vuln/) describes that scope.

## Coverage and limits

Recon covered README, AGENTS/CLAUDE, CONTRIBUTING, architecture/message-flow/scrolling documents, config/orchestration contracts, Makefile, Go module/dependency pins, lint configuration, hooks, install/release/perf scripts, CI workflows, recent Git history, the prior plans index, and the point-in-time feature audit. No separate ADR/PRD/PRODUCT/DESIGN/CONTEXT collection was present in the discovered repo files.

All Go packages were included in the package sweep. Detailed reading concentrated on lifecycle/storage/Git/process, UI common/sidebar/diff and async boundaries, PTY/tmux/update/config, e2e helpers/scenarios, and the small shared packages. Correctness, security, performance, tests, architecture, dependencies, tooling, docs, and direction were considered.

This was **not** a line-by-line read of every source/test line. Not audited experimentally: prolonged fuzzing, live real-agent workflows, crash/fault injection, Windows runtime behavior, other OS/tmux versions, external Homebrew repository or GitHub settings, upstream dependency implementations, complete Git-history secret scanning, every center cursor heuristic, and real-fleet performance. No performance regression is claimed from timing measured under this concurrent audit; finding 12 is structural. No production sessions were intentionally targeted.

## Considered and rejected / deferred

- Prior completed findings were not repeated merely because their original code surfaces still exist.
- Same-server tmux tag forgery, approved configured shell execution, and viewer command fragments are documented trust/product choices.
- Script-trust hash pinning, update signatures/checksums/archive confinement, opt-in OSC52, and reviewed display sanitization showed no new confirmed bypass.
- Full-canvas rendering, bounded frame caches, shared actor machinery, and intentional tmux orchestration are not architecture findings.
- A possible very-fast-run exit before remain-on-exit configuration was not reproduced in 30 focused executions. Do not commission a fix without a deterministic failing case.
- Synchronous clipboard/PTY-write blocking was not promoted without a demonstrated trigger.
- Emulator malformed-UTF8/complex-grapheme concerns need tmux-mediated characterization before being called user-visible bugs or vulnerabilities.
- The Go module explains that lipgloss currently supplies the winning ultraviolet version, while ARCHITECTURE.md still attributes it to Bubble Tea. Correct this small documentation drift with the next Charm-stack maintenance change; it does not justify a standalone migration plan.
- No speculative framework/dependency upgrade is recommended solely for version freshness; the pinned vulnerability gate passed.
- The previously rejected generic lifecycle CLI remains rejected: this audit found no new orchestrator requirement overturning that decision.
- No secret values were copied into this report. No malicious prompt-injection content was identified in the reviewed material.

