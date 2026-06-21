# amux Feature Audit — Report

A four-phase audit of every user-facing feature in amux: catalogue → test → fix → re-test.

## Deliverables (this directory)

| File | What it is |
|------|------------|
| `FEATURES.csv` | **The canonical spreadsheet.** 144 user stories with code-derived expected behaviour, source/test refs, and per-story status (test method, result, errors, fix, re-test). |
| `FEATURES.md` | Human-readable mirror of the spreadsheet, grouped by area. |
| `SUMMARY.md` | Status roll-up by status and by area. |
| `FEATURES.json` | Machine-readable form of the rows (consumed by tooling/agents). |
| `build_audit.py` | Single source of truth. Holds the rows + generates all outputs. `results.json` overlays per-phase status so the spreadsheet stays one regenerable artifact. |
| `results.json` | Per-story overlay (status/result/errors/fix/re-test) merged on top of the rows at generate time. |
| `phase2_findings.json` | Raw evidence from the Phase 2 multi-agent verification (one finding per story, with file:line evidence). |
| `merge_findings.py` | Merges verification findings into `results.json`. |

Regenerate anytime: `python3 feature-audit/build_audit.py`

## Method

### Phase 1 — Catalogue (144 user stories, 17 areas)
Fanned out 7 parallel exploration agents across the whole app (dashboard, center, sidebar, workspace/git lifecycle, dialogs/palette/settings/mouse, agents/activity/persistence, trust/update/tmux/ops/config). Their output was curated into a single canonical set of **144 user stories**, each with a code-derived "expected behaviour", source-file and test-file references.

Baseline: `go test ./...` green (30/30 packages with tests pass).

### Phase 2 — Test / verify (multi-agent)
Ran the `amux-feature-verify` workflow: **17 verification agents** (in rate-limit-safe waves of 4), one per area. Each adversarially cross-checked every story's expected behaviour against the real source and classified it Pass / Error, with file:line evidence.

Result: **143 Pass, 1 Error.** The verifiers also flagged ~15 places where the *catalogue wording* (not the code) was imprecise; those expected-behaviour strings were corrected so the spreadsheet is accurate. The code in all of those cases is correct and good UX.

### Phase 3 — Fix
Two changes (see Findings). Each fix: minimal blast radius, preserves existing side effects, covered by a new/updated test.

### Phase 4 — Re-test
- New regression tests pass.
- Full `go test ./...` green.
- `make verify-loop` green — drives a real keystroke through amux's actual input path into a real raw-mode agent and asserts the bytes arrive (the only check that proves end-to-end send behaviour).
- `make devcheck` green (fmt, lint with the pinned golangci-lint v2.12.2, tests, harness-golden, file-length).
- Independent adversarial re-verification of the fix by a fresh agent: **AGT-07 and OPS-05 both CONFIRMED-FIXED, no regressions.** It verified the interrupt runs off the Bubble Tea update loop (so the inter-press sleep can't block rendering), that diff-viewer and sidebar-terminal Ctrl+C paths are untouched, that the literal `0x03` and echo-window bookkeeping are preserved, that `SendInterrupt` has a single production caller and the floor-to-1 restores (not regresses) viewer behaviour, and that the close-during-interrupt race is safe (`Terminal.Write` guards `closed`/`nil` under its mutex; `go test -race` clean on center+pty).

## Findings & fixes

### AGT-07 (Error, medium) — Ctrl+C did not honour per-agent interrupt count — FIXED
**Defect:** The live Ctrl+C path in the center pane (`handleTerminalCtrlKey`) had no `ctrl+c` case, so it fell through and sent a single `0x03` byte, ignoring each agent's configured `InterruptCount`/`InterruptDelayMs`. Claude's TUI requires two Ctrl-C presses within a short window to interrupt (which is exactly why the registry sets `InterruptCount: 2, InterruptDelayMs: 200`), so a single user Ctrl-C could not interrupt Claude. The canonical `AgentManager.SendInterrupt` (which does the multi-press) existed but had **no production caller** — the behaviour had been written and never wired.

**Fix:**
- `internal/ui/center/model_input_keys.go`: added a `ctrl+c` case that routes through `AgentManager.SendInterrupt` via a `tea.Cmd` (off the Bubble Tea update loop, since it sleeps between presses), preserving the raw path's snap-to-bottom, `0x03` echo window, and activity tag.
- `internal/pty/agent.go`: floored `SendInterrupt` to at least one interrupt, so viewers (zero-value config) still receive exactly one Ctrl-C instead of silently swallowing it.
- Tests: `TestCenterCtrlCRoutesToAgentInterrupt`, `TestInterruptActiveAgentCmdNilSafe`, and an updated `TestAgentManager_SendInterrupt_ZeroCount` contract.

### OPS-05 (low) — config.json save was not crash-safe — HARDENED
**Defect:** `saveUISettings` wrote `config.json` with plain `os.WriteFile` (no temp+rename), inconsistent with the rest of amux's persistence (workspace.json, trusted-scripts.json use atomic writes). A crash mid-save could leave a torn config.

**Fix:** `internal/config/user_settings.go` now writes via `internal/fsatomic.WriteFile` (temp + fsync + atomic rename).

## Shipped

Both fixes landed on `main` via reviewed PRs:
- **AGT-07** — Ctrl-C per-agent interrupt: PR #537.
- **OPS-05** — atomic `config.json` save: PR #538.

This audit (catalogue + spreadsheet + tooling) lands as its own docs PR.

## Status

142 Pass + 2 Verified = **144/144 stories tested**, the 1 defect fixed and verified, and the spreadsheet corrected for accuracy. See `SUMMARY.md` for the live roll-up.
