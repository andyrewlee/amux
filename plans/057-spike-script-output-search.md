# Plan 057: Specify search for retained script output

> **Executor instructions:** This is a design spike. Write only the artifacts listed in Scope; do not implement the feature. Produce a concrete GO / DEFER / REJECT decision with an executable build handoff if GO, then update this plan's index row. Do not commit or push.
>
> **Drift check:** Run `git diff --stat 7c530ee..HEAD -- internal/ui/common/output_dialog.go internal/app/app_workspace_scripts.go internal/app/app_run_output.go internal/app/app_overlays.go internal/process/script_output.go internal/process/script_output_store.go README.md docs/CONFIG.md docs/ORCHESTRATION.md` and `git diff HEAD --` with those same paths; inspect `git ls-files --others --exclude-standard -- internal/app`. The audit includes the user's uncommitted run-output/picker changes. Reconcile plan 038's intended transcript changes rather than treating those as accidental drift.

## Status

- **Priority:** P3
- **Effort:** S for spike; M estimated for a subsequent feature
- **Risk:** LOW for design; MED if input routing changes in implementation
- **Depends on:** `plans/038-merge-persisted-script-output.md` before finalizing the build handoff
- **Category:** direction
- **Planned at:** commit `7c530ee` plus existing working-tree changes, 2026-09-26

## Why this matters

The in-app script-output viewer retains useful diagnostics, but users must scroll manually to locate an error. A bounded text search could help with long setup/run output. This is an optional product feature, so first resolve the interactions with follow mode, modal keys, output replacement, and truncation; do not assume a global terminal search system is warranted.

## Current state

`internal/ui/common/output_dialog.go:98` handles scroll/follow/close. Its close and follow cases are:

```go
case key.Matches(keyMsg, key.NewBinding(key.WithKeys("esc", "enter"))):
	d.visible = false
	return d, func() tea.Msg { return OutputDialogResult{} }
case key.Matches(keyMsg, key.NewBinding(key.WithKeys("f"))):
	d.following = !d.following
	if d.following {
		d.offset = len(d.lines)
	}
```

The same file's `SetContent` sanitizes output and pins the offset to the bottom while following. Search must operate on sanitized visible text, never raw ANSI. `internal/process/script_output.go:15` bounds each setup/archive/on-done transcript:

```go
const scriptOutputTailBytes = 64 << 10
```

`internal/app/app_workspace_scripts.go:221` composes `LastScriptOutputs` into the `O` script viewer. `internal/app/app_run_output.go:127` opens live `R` output, sets an attach hint, and its overlay owns the `a` action. Workspace status also reuses `OutputDialog`; do not automatically enable search on every use. README documents `O` as latest lifecycle transcripts and `R` as live run output. Saved full terminal transcripts already open in the configured external viewer; keep them outside this feature.

Existing behavioral test convention, `internal/ui/common/output_dialog_test.go:59`, drives real key messages and then inspects follow/offset state. Its key setup includes:

```go
d := NewOutputDialog("out", content.String())
d.SetSize(80, 12) // viewCap < len(lines) so there's somewhere to scroll
d.Show()
```

Use that direct state-machine style in the proposed regression matrix, plus app-level routing tests based on `internal/app/app_run_output_test.go` (attach, refresh tokens, nonattachable status).

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Inspect baseline behavior | `go test ./internal/ui/common -run '^TestOutputDialog_' -count=1` | PASS |
| Inventory call sites | `rg -n 'NewOutputDialog|handleRunOutputInput|SetContent|SetAttachHint' internal/app internal/ui/common` | callers accounted for in design |
| Artifact checks | Python validation in Step 3 | exit 0 |
| Whitespace | `git diff --check` | exit 0 |

This spike changes documents only. Do not run formatters, install dependencies, or modify source to prototype. The future build plan must require `env -u AMUX_WORKSPACES_ROOT make devcheck` until plan 059 lands, `make lint-strict-new`, `env -u AMUX_WORKSPACES_ROOT make verify-loop`, `make harness-presets`, and `PERF_STRICT=1 make perf-check` on a quiescent host. Harness output alone does not validate actual input delivery.

## Scope

**In scope for writes:** `plans/spikes/057/decision.md`, `plans/spikes/057/interaction-cases.json`, `plans/spikes/057/implementation.md`, this plan and its index row.

**Read-only source:** the files in Drift check, `internal/ui/common/output_dialog_test.go`, `internal/app/app_run_output_test.go`, `internal/app/app_input_dialogs.go`, `internal/app/app_overlays.go`, and transcript/viewer call sites found by the inventory command.

**Out of scope:** production changes, global terminal search, full-history indexing, fuzzy/regex queries, new transcript persistence or retention policies, CLI/API additions, telemetry, external services, unrelated dialog redesign.

## Git workflow

Suggested branch: `advisor/057-script-output-search-spike`. Record the initial dirty paths and preserve them. No commit, push, stash, or reset. Plans-only output is reviewable without changing the application.

## Steps

### Step 1: Map the input and refresh states

Read the actual overlay route before writing a proposed key map. In `decision.md`, add `## Evidence` and `## State model` with source references, each OutputDialog caller, which caller opts in, and the transitions for closed / browsing / editing query / browsing matches. Define whether both `O` and `R` opt in; recommended default is both script surfaces, with status excluded. Explain how a query containing `a`, `f`, `j`, or `k` avoids invoking existing actions, and how pasted text is routed. Resolve refresh behavior before choosing a widget implementation.

**Verify:** `go test ./internal/ui/common -run '^TestOutputDialog_' -count=1` → PASS; `rg -n '^## (Evidence|State model)$' plans/spikes/057/decision.md` → both headings present.

### Step 2: Specify a small complete interaction contract

In `decision.md`, add `## Interaction contract`, `## Alternatives`, and `## Decision`. Start from `/` to edit, Enter to accept the query without closing the dialog, Escape to leave query edit first and close on a subsequent browse-mode Escape, `n` / `N` for next / previous, literal case-insensitive substring search, and explicit no-match feedback. These are proposals to validate against existing keys, not permission to silently override them. Record the chosen behavior for every collision.

Specify wrapping and its indicator, empty queries, Unicode index/display-cell conversion, narrow layouts, sanitized content, duplicate lines, truncation markers, and refreshed output that removes the selected match. Recommended follow rule: searching pauses follow; `f` in browse mode explicitly resumes it, with query text retained. Decide whether refresh recomputes matches from the latest bounded snapshot or freezes a snapshot and label it; justify the result. Do not present matches in dropped history as searchable. Include terminal-text mockups for query editing, found/no-match, and refreshed/evicted match states, using synthetic output only.

Create `interaction-cases.json` as an object with `decision` (`GO`, `DEFER`, or `REJECT`) and `cases` (array of objects with unique `id`, `given`, `input`, `expected`). Include at least 12 cases covering all listed edge states and attach-key isolation. A DEFER/REJECT decision still supplies cases to explain the cost and a precise revisit trigger.

**Verify:** `python3 -c 'import json; from pathlib import Path; d=json.loads(Path("plans/spikes/057/interaction-cases.json").read_text()); assert d["decision"] in ("GO","DEFER","REJECT"); c=d["cases"]; assert len(c)>=12; assert len({x["id"] for x in c})==len(c); assert all(all(x.get(k) for k in ("id","given","input","expected")) for x in c)'` → exit 0.

### Step 3: Write the implementation handoff or explicit deferral

If GO, make `implementation.md` a self-contained build plan: selected semantics, exact scoped files, minimum query state, sanitation/index rules, overlay routing order, tests named for the JSON cases, render/input verification commands with expected results, STOP conditions, and the three required user-contract doc updates (`README.md`, `docs/CONFIG.md`, `docs/ORCHESTRATION.md`). The executor should be able to build from that file alone. If DEFER/REJECT, write the rationale, concrete unmet criterion, and the evidence needed to revisit it; do not fabricate build steps. Cross-check plan 038's final transcript behavior first.

**Verify:** `python3 -c 'from pathlib import Path; p=Path("plans/spikes/057"); assert all((p/n).is_file() and (p/n).stat().st_size>0 for n in ("decision.md","interaction-cases.json","implementation.md")); s=(p/"decision.md").read_text(); assert all("## "+h in s for h in ("Evidence","State model","Interaction contract","Alternatives","Decision"))'` → exit 0; rerun Step 2 JSON check and `git diff --check` → exit 0.

## Test plan

No new application tests in this spike. The handoff must map each interaction case to a future common-package or app routing test. Existing exemplars are `TestOutputDialog_ShowScrollClose`, `TestOutputDialog_FollowPinsToBottomOnRefresh`, `TestOutputDialog_SanitizesContent`, and app run-output attach/refresh tests. Explicitly include typing/pasting `a` while editing a query and Enter not accidentally closing the modal.

## Done criteria

- [ ] Three artifacts exist and both Python validations pass.
- [ ] Decision and JSON agree; all keyboard/refresh/truncation choices are resolved.
- [ ] GO includes a standalone build plan; DEFER/REJECT includes a concrete revisit trigger.
- [ ] No production files changed; `git diff --check` passes; index records spike completion and verdict, not feature completion.

## STOP conditions

Stop if reliable search requires unbounded transcript retention, raw terminal-stream indexing, changing external viewer behavior, or an unresolved key-routing collision. If plan 038 has not stabilized the data available to the viewer, record the dependency and finish only the independent interaction research. Do not implement to settle a design uncertainty.

## Maintenance notes

Future changes to `OutputDialog` callers must explicitly opt in or out of search. Keep match offsets tied to sanitized text and distinguish byte indexes from display cells. Revisit snapshot refresh semantics whenever transcript truncation or follow behavior changes.
