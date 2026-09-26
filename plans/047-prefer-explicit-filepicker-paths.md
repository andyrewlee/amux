# Plan 047: Prefer explicit typed paths over file-picker row selection

> **Executor instructions:** Follow the steps and gates. Do not commit or push. Update the index status only if delegated by the reviewer.
>
> **Drift check first:** Run `git status --short`, `git diff --stat 7c530ee..HEAD -- internal/ui/common/filepicker_navigation.go internal/ui/common/filepicker_explicit_path_test.go internal/ui/common/filepicker_navigation_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md`, and `git diff HEAD --` for those same paths; inspect untracked files. Compare Current state against live code. Baseline is `7c530ee` plus the dirty tree audited 2026-09-26. Preserve unrelated edits and do not stash/clean/reset.

## Status

- **Priority:** P2
- **Effort:** S
- **Risk:** LOW–MED
- **Depends on:** [plan 059](059-isolate-e2e-workspace-root.md) before unrestricted e2e validation; sanitized gates below work meanwhile
- **Category:** bug / usability
- **Planned at:** commit `7c530ee`, plus audited uncommitted working tree, 2026-09-26

## Why this matters

Typing or pasting an external absolute path leaves the current directory's entries selectable. Enter chooses the highlighted current entry before it ever resolves the typed path, so it opens or confirms the wrong location. Give explicit path input its own precedence while preserving fuzzy selection for ordinary names and the existing async filesystem boundary.

## Current state

`internal/ui/common/filepicker_navigation.go:137–143`:

```go
if rawQuery != "" && (strings.HasPrefix(rawQuery, "/") || strings.HasPrefix(rawQuery, "~") || strings.HasPrefix(rawQuery, ".")) && !withinCurrent {
    fp.filteredIdx = make([]int, len(fp.entries))
    for i := range fp.entries {
        fp.filteredIdx[i] = i
    }
    return
}
```

`handleEnter` at `:269–278` consumes that list first:

```go
if len(fp.filteredIdx) > 0 && fp.cursor >= 0 && fp.cursor < len(fp.filteredIdx) {
    entry := fp.entries[fp.filteredIdx[fp.cursor]]
    if entry.IsDir() {
        newPath := filepath.Join(fp.currentPath, entry.Name())
        fp.currentPath = newPath
        fp.input.SetValue(fp.inputBasePath())
        fp.input.CursorEnd()
        fp.markNeedsLoad()
        return fp, nil
```

Typed-path resolution occurs only later at `:293–306`. `handleAutocomplete` similarly chooses the selected entry before `handleOpenFromInput`. The `resolvePathCmd`/`pathResolvedMsg` helpers call Stat off Update and drop results if input changed; keep that pattern. `applyResolvedPath` differentiates Enter (confirm directories in directories-only mode, otherwise navigate directories/select files) from autocomplete (navigate only directories).

Go Bubble Tea v2 conventions: Update mutates widget state, tea.Cmd owns I/O, and common tests pump returned commands. `internal/ui/common/filepicker_test.go:13–55` provides `pumpMsgs`/`pumpPicker`; use them. Existing fallback tests in `filepicker_navigation_test.go` clear `filteredIdx`, which bypasses the real populated-list defect. Plan 019's asynchronous FilePicker design is already implemented; do not reintroduce sync Stat/ReadDir.

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Baseline | `go test ./internal/ui/common -count=1` | PASS |
| Regression | `go test ./internal/ui/common -run TestFilePickerExplicitPath -count=1 -v` | Named tests PASS after fix |
| Existing paths | `go test ./internal/ui/common -run TestFilePicker -count=1 -v` | PASS |
| Race | `go test -race ./internal/ui/common -count=1` | PASS |
| Real input | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | Both required tests PASS, not SKIP |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | Exit 0 or accurately reported unresolved blocker |
| Lint | `make lint-strict-new` | Exit 0 |
| Hygiene | `git diff --check` | Exit 0 |

The audit saw failing devcheck real-e2e tests and passing verify-loop. Inherited AMUX_WORKSPACES_ROOT was later found to defeat test HOME isolation; plan 059 repairs that test setup. Until it lands, sanitize every e2e-reaching command as shown, and do not assume remaining failures are unrelated without evidence.

## Scope

**In scope:** `internal/ui/common/filepicker_navigation.go`; new `filepicker_explicit_path_test.go`; existing `filepicker_navigation_test.go` only if prior fallback expectations need explicit intent clarification; README, CONFIG, ORCHESTRATION; index status if delegated.

**Out of scope:** FilePicker rendering/size, async request-generation redesign, project-tree loading, repository opening/persistence, symlink policy, hidden-file default, clipboard handling, new keybindings, and ~other-user expansion.

## Git workflow

Use the operator's checkout, optionally `advisor/047-filepicker-path-precedence` when an isolated branch is requested. Record and preserve the initial dirty tree. No stash, clean, reset, commit, push, or PR without authorization. Use t.TempDir fixtures, never navigate tests into user workspaces.

## Steps

### Step 1: Pin input intent with public message tests

Create a populated current directory containing an alphabetically first unrelated child and an external target in another temp directory. Show and pump the picker, then replace its input using actual Ctrl+A/paste or the same textinput replacement sequence supported by current tests; do not clear `filteredIdx`. Send Enter and assert that the resolved/confirmed path is the external target, not the highlighted child. Repeat with a file-capable picker and Tab. Use synthetic paths only.

**Verify:** `go test ./internal/ui/common -run TestFilePickerExplicitPath -count=1 -v` → external-path cases fail against the audited selection-first code. If not, stop to reconcile behavior or the test sequence.

### Step 2: Implement explicit-path precedence with unchanged fuzzy behavior

Introduce one package-local intent helper used by Enter and autocomplete. Apply these rules in order: (1) an unchanged base-path input is not explicit; (2) for input beginning with the current base-path prefix, a simple one-component suffix remains a fuzzy row query, while a suffix containing another separator is explicit; (3) otherwise an absolute path, exactly `~`, prefix `~/`, exactly `.`/`..`, prefix `./`/`../`, or a string containing a separator is explicit; (4) other simple names remain fuzzy. Thus typing a partial name after the prefilled base retains filtering, while a path to another directory takes precedence over its unrelated rows. Use `filepath` helpers for separators and preserve current trim/home expansion rules; do not add ~username support. Cover root-directory base input as well: a multi-component absolute target still qualifies under rule 2.

Check that intent before row selection. Enter on explicit input must return the async resolve command and never fall back to a highlighted row if the explicit target fails or is disallowed. Preserve existing resolve semantics: directories-only Enter confirms an existing directory, file-capable Enter navigates an existing directory or confirms a file, and Tab only navigates a directory. An invalid explicit path leaves input, current path, and visibility unchanged. Shared path preparation should avoid divergent resolution in Enter/Tab without changing normal fuzzy matches.

After navigating/confirming from the result, retain existing stale-input rejection. If the existing input-only identity is demonstrably insufficient for this fix, stop and propose a separately scoped generation change; do not silently broaden into the full picker async lifecycle.

**Verify:** `go test ./internal/ui/common -run 'TestFilePickerExplicitPath|TestFilePickerHandleAutocomplete|TestFilePickerHandleOpenFromInput' -count=1 -v` → PASS. `go test ./internal/ui/common -run TestFilePicker -count=1 -v` → all existing tests PASS.

### Step 3: Add edges, document semantics, and run gates

Cover home-relative and parent-relative paths using controlled temporary HOME where the platform permits; Unicode/spaces; explicit file rejected by directories-only mode; nonexistent explicit target; base input; bare fuzzy names; nested relative target; zero current entries; and a resolved result arriving after input changes. Do not treat every leading dot in a simple filename as relative navigation. Update all three contract docs to explain explicit-path Enter/Tab versus name filtering without adding bindings.

**Verify:** `go test -race ./internal/ui/common -count=1` → PASS; `env -u AMUX_WORKSPACES_ROOT make verify-loop` → required PASS lines; `make lint-strict-new` → exit 0; `env -u AMUX_WORKSPACES_ROOT make devcheck` → exit 0 or recorded unresolved failure; `git diff --check` → clean.

## Test plan

Use actual Update and command round-trips following `pumpPicker`. Assert exact DialogResult.Value, visibility, currentPath, and unchanged selected-row state for invalid targets. Add separate Enter and Tab cases with a populated listing; the selected child must intentionally differ from the target. Verify fuzzy queries such as a bare filename and a simple suffix after the displayed base still select their matching row. Avoid platform sleeps and filesystem permissions assumptions.

## Done criteria

All required final gates must pass before this plan is marked DONE. A known or newly discovered gate failure leaves the plan BLOCKED with the exact command/test and evidence; recording a failure is not a substitute for passing. Do not expand implementation scope to repair other findings. Until [plan 059](059-isolate-e2e-workspace-root.md) lands, remove `AMUX_WORKSPACES_ROOT` from every command reaching e2e, including devcheck, verify-loop, and test-race-tmux.

- [ ] New `TestFilePickerExplicitPath...` tests execute and PASS with populated-list fixtures.
- [ ] All FilePicker tests and `go test -race ./internal/ui/common -count=1` pass.
- [ ] Stat/ReadDir continue to run only within commands; invalid explicit paths cannot select unrelated rows.
- [ ] `make lint-strict-new` and sanitized verify-loop pass; sanitized devcheck outcome is recorded honestly.
- [ ] All three contract docs agree, `git diff --check` passes, and only in-scope implementation edits were added beyond initial dirty work.
- [ ] Designated index owner receives final status and verification results.

## STOP conditions

Stop if textinput semantics prevent a faithful public-message regression, if path intent needs a new user preference, if platform/path behavior requires ~username or symlink-policy changes, if fixing it requires extra production files, or after two failed focused correction attempts. Do not run e2e with ambient AMUX_WORKSPACES_ROOT before plan 059. Do not replace async resolution with synchronous I/O.

## Maintenance notes

Keep explicit intent classification shared between Enter and Tab. A populated-list test is essential: helper tests that clear rows can remain green while user navigation is broken. New path syntaxes must specify whether they are fuzzy queries or explicit navigation before changing precedence.
