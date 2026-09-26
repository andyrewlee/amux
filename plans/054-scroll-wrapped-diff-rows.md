# Plan 054: Make every wrapped diff row reachable within a bounded viewport

> **Executor instructions:** Follow the visual-row model and gates below. This is a scoped rendering correction; do not commit or push. Performance baseline changes are outside this repair plan; investigate any regression and report the measured tradeoff before expanding scope. The reviewer owns the index unless delegated.
>
> **Drift check first:** Run `git status --short`, `git diff --stat 7c530ee..HEAD -- internal/ui/diff/model.go internal/ui/diff/view.go internal/ui/diff/wheel.go internal/ui/diff/visual_rows.go internal/ui/diff/visual_rows_test.go internal/ui/diff/model_test.go internal/ui/diff/model_state_test.go internal/ui/diff/view_test.go internal/ui/center/model_diff_wrap_view_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md`, and `git diff HEAD --` with those paths. Inspect untracked files and compare Current state. Baseline is `7c530ee` plus the dirty tree audited 2026-09-26. Preserve all existing changes; do not stash/clean/reset.

## Status

- **Priority:** P2
- **Effort:** M
- **Risk:** MED
- **Depends on:** [plan 059](059-isolate-e2e-workspace-root.md) before unrestricted e2e validation; use sanitized commands meanwhile
- **Category:** bug / rendering
- **Planned at:** commit `7c530ee`, plus audited uncommitted working tree, 2026-09-26

## Why this matters

The diff viewer slices and scrolls logical source lines, then expands those lines into arbitrarily many wrapped display rows. A one-line diff can fill several screens while reporting zero possible scroll. Use one visual-row model for rendering and navigation so the final segment is reachable, the footer stays visible, and hunk jumps remain meaningful after wrapping or resizing.

## Current state

amux uses Go, Bubble Tea v2, Lip Gloss, and `charmbracelet/x/ansi` for grapheme/display-width operations. The diff widget is an internal center tab. Loaded results are immutable and routed by tab identity; preserve that async design. Render caches are deliberate: repeated View calls must reuse unchanged work.

`internal/ui/diff/model.go:255–264` computes the scroll limit from source lines:

```go
total := len(m.diff.Lines)
visible := m.visibleHeight()
if total <= visible {
    return 0
}
return total - visible
```

`view.go:234–244` then emits every wrapped row of each selected source line:

```go
actualRows := 0
for i := start; i < end; i++ {
    line := lines[i]
    rendered := m.renderLine(i, line, lineNumWidth, contentWidth)
    actualRows += strings.Count(rendered, "\n") + 1
    b.WriteString(rendered)
    if i < end-1 {
        b.WriteString("\n")
    }
}
```

`wrapLine` at `view.go:314–315` uses `ansi.Hardwrap` and indents continuation rows. `wheel.go:CanConsumeWheel` delegates to maxScroll. `nextHunk`/`prevHunk` compare Hunk.StartLine with the same source-line scroll value. `view_test.go:TestViewMemoized` pins reuse of a View string; `TestViewMultibyteWrap` currently checks only no panic/nonempty output. `internal/ui/center/model_render.go:41–45` embeds the diff View in center output.

Architecture says full-canvas composition is deliberate and Ultraviolet owns downstream cell diffing. Fix the leaf diff model; do not redesign compositor or vterm.

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Baseline | `go test ./internal/ui/diff -count=1` | PASS |
| New cases | `go test ./internal/ui/diff -run TestVisualRows -count=1 -v` | New visual-row tests PASS after fix |
| Integration | `go test ./internal/ui/diff ./internal/ui/center -count=1` | PASS |
| Render/perf unit checks | `go test ./internal/ui/diff -run TestVisualRowsCache -count=1 -v` | Cache build counts remain stable across scrolling/repeated View |
| Harness presets | `make harness-presets` | All presets complete successfully |
| Perf gate | `PERF_STRICT=1 make perf-check` | Every host preset stays within its checked-in p95 baseline |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | Exit 0 or accurately recorded unresolved failure |
| Lint | `make lint-strict-new` | Exit 0 |
| Real input | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | Required tests PASS, not SKIP |
| Hygiene | `git diff --check` | Exit 0 |

Run performance checks on a quiescent host after other tests/builds finish. Do not infer performance from runs competing with this audit or other agents. The harness is render-only and does not prove tmux/input behavior. Audit devcheck failed some real-e2e cases; verify-loop passed. Inherited AMUX_WORKSPACES_ROOT later proved to break test isolation; remove it from all e2e-reaching commands until plan 059 lands. Attribute remaining failures with isolated evidence, not assumption.

## Scope

**In scope:** `internal/ui/diff/model.go`, `view.go`, `wheel.go`; new `visual_rows.go` and `visual_rows_test.go`; existing `model_test.go`, `model_state_test.go`, `view_test.go` for changed scroll semantics; new `internal/ui/center/model_diff_wrap_view_test.go`; README, CONFIG, ORCHESTRATION; index status if delegated.

**Out of scope:** Git diff parsing/loading/size caps, syntax-highlighting redesign, center source, vterm, compositor, harness production source, performance baseline files, configurable wrap defaults, horizontal scrolling, persistence, and new keybindings.

## Git workflow

Use the operator's checkout; optional isolated branch `advisor/054-diff-visual-rows`. Preserve all staged/unstaged/untracked baseline work and compare only the added implementation delta. No stash, clean, reset, commit, or push. Keep performance baseline files unchanged within this plan; an intentional change in performance needs a revised scope and the three-quiescent-run, per-preset-median procedure required by AGENTS.md.

## Steps

### Step 1: Add a behavioral regression for hidden wrapped tails

Create `visual_rows_test.go` using `newSizedModel` from view_test. Give the viewer a single long synthetic line ending in an unmistakable marker, set a small positive viewport, enable wrap using the actual `w` key, then send End/Down/PgDown and wheel input. Assert the final marker can be visible, the footer remains within the viewport, `CanConsumeWheel` is true when physical rows overflow, and every emitted row fits the display width. Count ANSI-stripped display rows, not bytes. Include a multi-hunk fixture with long lines before each hunk.

**Verify:** `go test ./internal/ui/diff -run TestVisualRows -count=1 -v` → audited code fails tail reachability or bounded-height assertions. If it does not, stop to inspect the fixture rather than weakening the expected behavior.

### Step 2: Build one cached visual-row representation

In `visual_rows.go`, define rows containing at least source-line index, segment index, and rendered row content, plus a source-line-to-first-visual-row lookup. Build from sanitized diff text using existing ANSI/grapheme helpers. Empty logical lines still produce one visual row. Without wrap, produce one truncated visual row per logical line. With wrap, wrap content to available display cells and emit a gutter only on the first segment; continuation gutters are spaces of the same width. Compute content width from actual gutter width, not the current forced minimum of 20 cells. For a viewport too narrow for a wide grapheme, render a width-safe placeholder rather than a broken half glyph; preserve source mapping.

Cache this representation by immutable DiffResult pointer, width, wrap, and styles revision when rows include style. Rebuild only when those inputs change; scrolling/focus/footer changes must not rewrap the entire diff. Include path/mode in the outer View memo if the refactored render reads them; clearing/resetting the source must invalidate both caches. Compute added/deleted stats once per diff-cache rebuild or retain existing behavior if already cheap; do not broaden into Git changes.

**Verify:** `go test ./internal/ui/diff -run 'TestVisualRowsBuild|TestVisualRowsUnicode|TestVisualRowsCache' -count=1 -v` → PASS for empty lines, ANSI sanitization, narrow widths, CJK/combining text, and cache reuse.

### Step 3: Use visual rows consistently for navigation and anchoring

Make `scroll` the first visible visual-row index. `maxScroll` uses visual-row count minus content capacity; arrows, page keys, wheel, home/end, and CanConsumeWheel use that same quantity. Hunk jumps map each source Hunk.StartLine through the first-row lookup, then clamp to the scrollable range. Preserve existing cyclic n/p navigation semantics; track the selected hunk explicitly so a last hunk near the bottom does not get stuck because its top cannot be aligned. Footer position should explicitly show visual row position/total when wrapped; source line numbers remain source based.

For wrap toggle and width changes, capture the top row's source-line and segment before invalidating the cache, rebuild, and anchor to the same source line with the segment clamped to the new segment count. Height-only resize preserves the current visual offset then clamps; new diff data/ResetSource resets to top as today. No scroll value may exceed the new maximum after load, toggle, or resize.

**Verify:** `go test ./internal/ui/diff -run 'TestVisualRows|TestDiffScroll|Test.*Hunk|TestSetSize|TestResetSource' -count=1 -v` → expected navigation/anchor tests execute and PASS; `go test ./internal/ui/diff -count=1` → all existing tests PASS after updating only source-vs-visual assumptions.

### Step 4: Enforce viewport dimensions and integration behavior

Render exactly the selected visual-row slice, pad only to the available content height, and reserve header/stats/footer rows explicitly. Decision: positive height at least 3 reserves those three chrome rows; smaller heights drop stats first, show header plus footer at height 2 and only header at height 1; no content rows are possible there. Width/height at or below zero returns an empty View. Clip all chrome to the provided width with ANSI-aware truncation. A zero content capacity has no effective scroll/scroll-consuming wheel behavior, even if data exists. Add center integration coverage that builds a diff tab fixture and verifies a wrapped tail remains reachable within the allocated content rectangle.

**Verify:** `go test ./internal/ui/diff ./internal/ui/center -count=1` → PASS, including height 0/1/2/3, widths 1/small/normal, footer visibility, and integration tail reachability. `make harness-presets` → all presets complete.

### Step 5: Update contracts and run final gates

Update all three user-contract docs: `w` toggles wrapping; scrolling in wrapped mode moves display rows; n/p still navigate hunks; resizing/toggling preserves the top source location where possible. Run the perf gate once on a quiescent host. A real regression requires investigation; do not change baselines to force a pass. Finally run sanitized input/devcheck gates and strict-new lint.

**Verify:** `PERF_STRICT=1 make perf-check` → all host baselines pass; `env -u AMUX_WORKSPACES_ROOT make verify-loop` → required PASS lines; `make lint-strict-new` → exit 0; `env -u AMUX_WORKSPACES_ROOT make devcheck` → actual result recorded; `git diff --check` → clean.

## Test plan

Add named `TestVisualRows...` groups for the long-single-line failure, wrapped/unwrapped mixed lines, row widths/heights, wheel gating, page/home/end movement, cyclic hunk navigation near the bottom, resize/toggle source anchoring, load/reset invalidation, empty/binary/error/loading states, escape stripping, valid UTF-8, wide/combining glyphs, and cache reuse. Cache tests should assert wrapping is not rebuilt during scrolling or repeated View; they must not pin incidental allocations. Keep original async load-generation tests unchanged. Center integration tests may construct leaf state but must drive actual diff Update and rendered output.

## Done criteria

All required final gates must pass before this plan is marked DONE. A known or newly discovered gate failure leaves the plan BLOCKED with the exact command/test and evidence; recording a failure is not a substitute for passing. Do not expand implementation scope to repair other findings. Until [plan 059](059-isolate-e2e-workspace-root.md) lands, remove `AMUX_WORKSPACES_ROOT` from every command reaching e2e, including devcheck, verify-loop, and test-race-tmux.

- [ ] All new visual-row and center integration tests execute and PASS.
- [ ] Every wrapped segment is reachable, output dimensions are bounded, and n/p cycles through hunks.
- [ ] Repeated View/scrolling does not rebuild wrapped row layout, demonstrated by `TestVisualRowsCache...`.
- [ ] `make harness-presets`, quiescent `PERF_STRICT=1 make perf-check`, `make lint-strict-new`, and sanitized verify-loop pass.
- [ ] Sanitized devcheck result is reported honestly, all three docs agree, and no baseline files changed.
- [ ] `git diff --check` passes; added implementation edits stay in scope beyond the recorded starting dirty tree.
- [ ] Index owner receives final results and any blockers.

## STOP conditions

Stop if the diff parser's lines are mutated in place without an invalidation signal, if a source line cannot be mapped reliably to a hunk, if implementing the viewport requires center/compositor production changes, or if two reasonable focused fixes fail. Stop and investigate a perf regression rather than rebaseline. Do not run e2e unsanitized before plan 059. Do not expand into horizontal scrolling or a replacement viewport framework.

## Maintenance notes

Visual rows are derived data, source lines remain the identity for gutters/hunks. Width, wrap, data, and style changes invalidate layout; ordinary scrolling does not. Keep tests that prove tail reachability and dimension bounds: nonempty/valid-Unicode assertions alone cannot protect this behavior.
