# Implementation Plans

Latest batch: **25 handoffs (035–059)** from the 2026-09-26 deep audit at
`7c530ee`, including the existing dirty working tree. The user selected all 22
confirmed findings and both product directions; verification found one additional
test-isolation defect, covered by 059. Only plans were written; no implementation
or commit/push is included. [Full audit and evidence](2026-09-26-deep-audit.md).

Start with **059 + 036**, then continue in the table order. 059 isolates e2e
worktrees; 036 repairs a reproduced picker regression blocking the baseline.
Neither should be marked DONE until the combined baseline is verified. Until
059 lands, prefix any command reaching e2e with `env -u AMUX_WORKSPACES_ROOT`.
Each new plan has its own `7c530ee` drift check covering committed changes and
the working tree. Preserve user edits; never stash/reset/clean them to obtain a
clean baseline. All rows below mean plans ready for an executor, not fixes shipped.

## 2026-09-26 execution order and status

| Plan | Title | Priority | Effort | Depends on | Status |
|---|---|---|---|---|---|
| [059](059-isolate-e2e-workspace-root.md) | Isolate e2e workspace roots | P1 | S | — | DONE |
| [036](036-restore-filtered-picker-navigation.md) | Restore filtered-picker navigation | P1 | S | —; validate with 059 | DONE |
| [035](035-recheck-merge-destination.md) | Recheck the approved merge destination | P1 | S | — | DONE |
| [037](037-capture-diff-tab-index.md) | Capture the created diff-tab index | P1 | S | — | DONE |
| [038](038-merge-persisted-script-output.md) | Merge persisted script output safely | P1 | M | — | TODO |
| [039](039-transactional-workspace-field-updates.md) | Apply ordered field-scoped workspace writes | P1 | M | — | TODO |
| [040](040-drain-pty-output-before-stop.md) | Drain PTY output before stopping | P1 | M | — | TODO |
| [041](041-stop-workspace-lifecycle-processes.md) | Cancel and drain lifecycle subprocesses | P1 | L | — | TODO |
| [042](042-snapshot-assistant-launch-config.md) | Snapshot assistant launch configuration | P1 | M | — | TODO |
| [043](043-preserve-config-on-invalid-reads.md) | Preserve config on invalid reads | P1 | S | — | TODO |
| [044](044-support-paste-in-custom-editors.md) | Support paste in custom editors | P2 | S | — | TODO |
| [045](045-fence-sidebar-reattach-results.md) | Fence sidebar reattachment outcomes | P2 | M | — | TODO |
| [046](046-reuse-shelved-workspace-snapshot.md) | Reuse metadata snapshots for shelves | P2 | S | — | TODO |
| [047](047-prefer-explicit-filepicker-paths.md) | Honor explicit file-picker paths | P2 | S | — | TODO |
| [048](048-bound-git-cancellation.md) | Bound Git cancellation and draining | P2 | M | — | TODO |
| [049](049-preserve-zero-interrupt-delay.md) | Preserve zero interrupt delays | P2 | S | 043 | TODO |
| [050](050-scope-hook-skip-flags.md) | Scope hook skip flags | P2 | S | — | TODO |
| [051](051-share-durable-port-reservations.md) | Share durable port reservations | P2 | L | 039, 041 | TODO |
| [052](052-publish-semantic-activity-transitions.md) | Publish time-driven activity transitions | P2 | M | — | TODO |
| [053](053-replace-executable-without-path-gap.md) | Replace executable without a pathname gap | P2 | M | — | TODO |
| [054](054-scroll-wrapped-diff-rows.md) | Scroll wrapped diff display rows | P2 | M | — | TODO |
| [055](055-load-project-tree-asynchronously.md) | Load the project tree asynchronously | P2 | M | — | TODO |
| [056](056-clean-up-fakeagent-build-directory.md) | Clean up the shared fakeagent fixture | P3 | S | — | TODO |
| [057](057-spike-script-output-search.md) | Spike: search retained script output | P3 | S | 038 | TODO |
| [058](058-spike-workspace-recovery-diagnostics.md) | Spike: workspace recovery diagnostics | P3 | S | 039, 041, 048 | TODO |
| [060](060-prefix-palette-after-restore-flake.md) | Prefix palette intermittently fails after restore | P1 | M | — | DONE — identity-drift mutation guard + stale-duplicate restore skip |

**Dependencies and coordination:**

- 049 requires 043's strict config-save boundary.
- 051 requires 039's storage/locking foundation and must preserve 041's lifecycle ownership; implement 041 before 051.
- 057 is a plans-only search design spike; finalize its handoff after 038.
- 058 is a plans-only recovery design spike; finalize after 039, 041, and 048.
- 040 and 045 address separate reader-ordering and attachment-generation defects; both need their own regressions.
- Coordinate 038/039/041/051 in process/data/service files; coordinate 036/044/047 in common UI and 037/042 in center launch code. Shared documentation edits should be merged, not overwritten.
- Substantive implementations require devcheck and strict-new lint. Concurrency changes need race coverage; input/tmux changes need verify-loop and real-tmux checks; render changes need harness presets and a quiescent strict perf check. Each plan lists its exact gates.
- A failed or skipped required gate is not DONE. Record the exact blocker rather than weakening tests or folding unrelated repairs into a plan.

**Verification baseline:** govulncheck, strict-new lint, ordinary lint/config-drift/formatter checks, and verify-loop passed. Full devcheck failed in e2e. An isolated rerun passed overlay queuing and shelve/restore but reproduced `TestWorkspaceCreateAgentsHaveDistinctSessions` creating two Claude sessions instead of Claude + Codex (036). The unsanitized launcher inherited the actual workspace root (059). No full isolated devcheck, race sweep, or performance benchmark is claimed.

**Coverage:** the new audit swept all Go packages and all nine advisory categories, with deep reads concentrated on lifecycle/storage/process, UI async boundaries, PTY/tmux/update/config, and e2e tooling. It was not a line-by-line read of every file. Live real-agent workflows, prolonged fuzzing, power-loss injection, Windows runtime, other tmux/OS versions, upstream dependency implementations, full Git-history secret scanning, and fleet performance were not experimentally audited. See the audit for rejections and limits.

## 2026-09-25 batch (historical)

Generated by a deep `/improve` audit on 2026-09-25 against commit `af432f7`
(post-merge of the 5-PR refactor stack + the `containsASCIIFold` fix). Six
read-only auditors covered the ~195K-LOC Go codebase; every finding below was
verified against source before planning.

Execute in the order below unless dependencies say otherwise. Each executor:
read the plan fully before starting, honor its STOP conditions, run its drift
check first (`git diff --stat af432f7..HEAD -- <in-scope paths>`), and update
your row when done.

## Execution order & status

| Plan | Title | Pri | Effort | Depends on | Status |
|------|-------|-----|--------|------------|--------|
| 001 | Clamp the CSI ICH count in `insertChars` | P1 | S | — | DONE |
| 002 | Refuse writes against newer-schema stores | P1 | S | — | DONE |
| 003 | Fall back to default viewer on whitespace `viewer_command` | P1 | S | — | DONE |
| 004 | Run-session suffix from existing set, not len+1 | P1 | S | — | DONE |
| 005 | Replace fixed pacing sleeps in shared e2e helpers | P2 | M | — | DONE |
| 006 | Collapse tmux discovery list-sessions fan-out + lazy AllSessionMeta | P2 | M | — | DONE |
| 007 | Skip dashboard/sidebar work on unchanged GitStatusResult | P2 | M | — | DONE |
| 008 | Stop republishing dashboard state per PTY message | P2 | S | — | DONE |
| 009 | Panic-log stack traces + message/workspace context | P2 | S | — | DONE |
| 010 | Replace remaining fixed-deadline test loops | P2 | S | — | DONE |
| 011 | workspacesvc script-lifecycle error-branch tests | P2 | S | — | DONE |
| 012 | PruneStale secondary retention-branch tests | P3 | M | — | DONE |
| 013 | e2e coverage: crash reattach, rename, update, dialog queue | P3 | L | 005 | DONE |
| 014 | Tighten weak test assertions (unsorted compare, msgpump floor-1) | P3 | S | — | DONE |
| 015 | install.sh ↔ release parity check in CI | P2 | S | — | DONE |
| 016 | Add lint-config drift checks to pre-commit hook | P3 | S | — | DONE |
| 017 | POSIX-safe version compare in `make doctor` | P3 | S | — | DONE |
| 018 | Delete dead message types + TmuxOps methods | P3 | S | — | DONE |
| 019 | Move FilePicker directory I/O off the Update goroutine | P3 | M | — | DONE |
| 020 | Cheaper change signal for `visibleScreenDigest` | P3 | M | — | DONE |
| 021 | Rename `sidebar.Model` → `ChangesModel` | P3 | S | — | DONE |
| 022 | Align the two same-workspace predicates | P3 | S | — | DONE |
| 023 | Dedupe Makefile recipes + tmux-package exclusion lists | P3 | S | — | DONE |
| 024 | Log-level fast path before the logger mutex | P3 | S | — | DONE |
| 025 | Docs hygiene: LINTING.md devcheck, hook env vars, make help | P3 | S | — | DONE |
| 026 | Warn-vs-Error log-level convention + re-level real failures | P3 | M | — | DONE |
| 027 | Switch fakeagent to charmbracelet/x/term, drop golang.org/x/term | P3 | S | — | DONE |
| 028 | Replace unmaintained atotto/clipboard fallback | P3 | M | — | DONE |
| 029 | Route agent BEL into the attention surface (Option A: count-based) | P2 | M | — | DONE |
| 030 | Re-runnable `setup` on demand | P2 | S | — | DONE |
| 031 | Persist lifecycle-script transcripts across restarts | P3 | M | — | DONE |
| 032 | Run-session picker for concurrent `run` sessions | P3 | M | 004 | DONE |
| 033 | In-app browser for saved transcripts | P3 | M | — | DONE |
| 034 | Spike: minimal read-only lifecycle CLI per ORCHESTRATION.md trigger | P3 | M | — | REJECTED — spike verdict recorded in plan file: no named orchestrator requirement; on-disk stores + tmux tags already cover enumeration |

Status values: TODO | IN PROGRESS | DONE | BLOCKED (with one-line reason) | REJECTED (with one-line rationale)

## Dependency notes

- **013 depends on 005**: the new e2e scenarios reuse the observable-wait helpers; building them on fixed sleeps recreates the flake class.
- **032 depends on 004**: the picker's suffix-ordered enumeration assumes the corrected naming; it also wants 004's suffix-parsing helper shape.
- **008 and 007 touch adjacent dirty-gate code** — either order works; review both diffs for the same `handleGitStatusResult`/`dashboard.Update` region if landed together.
- **026 and 024 share `internal/logging/logger.go`** — land 024 first (smaller), then 026.
- **018 vs everything**: dead-surface deletion trivially conflicts with any plan adding uses of the pruned methods — land 018 late or re-verify its "dead" list if other plans merged meanwhile.
- Plans 001-004 are independent P1s — any order; 001 first only because it's the security finding.

## Coverage notes (what was NOT audited)

- Line-depth of `internal/ui/{sidebar,diff,common}` beyond sink-scanning, `internal/e2e` test bodies, `cmd/amux-harness`, `internal/pty`, `internal/update`, `internal/config`.
- No `govulncheck`/`go vet` run by the audit itself (CI gates exist); no benchmarks run — perf findings are structural estimates, `make perf-check` is the arbiter.
- CI workflow YAML ↔ Makefile parity was asserted from comments, not diffed (plan 023 discovers any drift there).

## Findings considered and rejected

- **`waitForGitPath` untested (reported as COVERAGE-02)** — refuted: `internal/app/app_operations_rescan_test.go` exercises the missing-`.git` timeout + rollback path and `app_input_messages_workspace_create_test.go` sets `GitPathWaitTimeout`; the branch is covered.
- **`containsASCIIFold` index panic** — already fixed at `af432f7` (PR #645).
- **Two known apt-lane flakes** (`TestRunSessionStatusReportsNonzeroExit`, `TestDragSelectUpAutoScrollsWhileRepainting`) — not reported individually; plans 005 + 010 are the systemic fix.
- **Forward-migration scaffolding for store schemas** (per-version decode dispatch / unknown-key preservation) — deferred by design: no v2 schema is planned; plan 002 delivers the needed guard without building speculative machinery. Revisit at first schema bump (the plan's maintenance notes list the three sub-issues).
- **`appendScrollbackDeltaMatchStart` quadratic path** — unreachable under default `history-limit`, bounded when reached.
- **tmux command injection via session names/paths/env** — verified clean: every interpolated value is `shellutil.ShellQuote`d or argv-passed; `paneLaunchCommand` keeps values in positional params; amux doesn't use `send-keys` internally.
- **Script-trust bypass** — SHA-256 content pinning, fail-closed load, `TrustRepoScriptsIfHash` closes the prompt race.
- **Update/install chain** — signature, checksum, download bounds, and archive confinement remain sound in the reviewed code. The earlier blanket atomic-replace conclusion is revised by plan 053: two renames leave a pathname gap.
- **PTY reader lifecycle / reattach pinning / overflow** — the earlier blanket rejection was too broad. Plans 040 and 045 address queued-output loss and missing sidebar generations; center generation guards and bounded overflow remain distinct existing defenses.
- **OSC52 / PTY-trace exfiltration** — env-gated, size-capped, sanitized filenames, 0600.
- **Workspace/registry traversal or torn writes** — `validateWorkspaceID`, per-ID flocks, `os.OpenRoot`, `fsatomic`.
- **Chrome filter data loss** — tightly bounded heuristic.
- **`RunArchive`/`p.Start()` deadlock hypothesis** — `reapAfterTimeout` deliberately abandons; no deadlock at cited lines.
- **`Stop` first-error partial kill** — exit-1 treated as success; callers see real errors.
- **Compositor full-canvas draw** — deliberate; Ultraviolet diffs cells downstream.
- **Duplicated `formatScrollPos` (6 lines), `workspaceErrContext` vs `errorContext`** — cosmetic / import-cycle-justified.
- **`IsInSelection` re-normalization** — fallback path only, never hot.
- **Multiple `tea.Tick` chains, `dashboard.View` build cost, `visibleFrameVersion` triple-lock, PTY reader per-chunk alloc** — designed machinery, each bounded.
- **Runtime docker/cloud-sandbox constants, `internal/update` hand-rolled sig/semver** — documented forward-compat / deliberate stdlib-only design.
- **Push/PR creation feature** — "amux never pushes" is a stated product boundary.
- **Bulk shelve/restore/purge** — already shipped (`app_bulk_shelve.go`).
- **Dashboard `/` filter** — real asymmetry, weak demand signal; viable later if fleet size justifies it.
- **install.sh tmux precheck** — covered in-app at `app_tmux_activity.go:332`.
- **`make dev` suggesting `air@latest`** — advisory-only install suggestion.
- **Dependabot pinned tool installs, ultraviolet ignore, charm-grouping** — designed supply-chain posture.
- **Transitive deps on stale upstreams** (`xo/terminfo`, `go-colorful`) — charm-stack pull-ins, dependabot carries them.
- **No prompt-injection content found**; no secrets were exposed or reproduced anywhere in these plans.
