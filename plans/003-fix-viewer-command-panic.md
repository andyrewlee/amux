# Plan 003: Fall back to the default viewer when `viewer_command` is whitespace-only

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/ui/center/model_tabs_viewer.go internal/config/user_settings.go`
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

`viewerLaunch` checks `viewer == ""` but not whitespace-only values. A `viewer_command: " "` (or `"\t"`) in the user's `config.json` makes `strings.Fields(viewer)` return an empty slice, and `[0]` panics inside the `tea.Cmd` closure — Bubble Tea does not recover panics in commands, so the whole TUI crashes the next time a file-viewer tab opens. It's a local-config footgun, trivially fixed, and exactly the kind of crash that erodes trust in a tool meant to manage agents.

## Current state

`internal/ui/center/model_tabs_viewer.go:180-192`:

```go
// viewerLaunch builds the viewer shell line `<command> -- '<file>'` and the
// tab label. The configured command is a user-provided shell fragment (e.g.
// "vim", "nvim", "less -R") and is deliberately NOT quoted — only the file
// path is. The label is the fragment's first token so "less -R" labels "less".
func (m *Model) viewerLaunch(filePath string) (cmd, label string) {
	viewer := m.viewerCommand
	if viewer == "" {
		viewer = "vim"
	}
	escaped := "'" + strings.ReplaceAll(filePath, "'", "'\\''") + "'"
	return viewer + " -- " + escaped, strings.Fields(viewer)[0]
}
```

- `m.viewerCommand` comes from `config.json`'s `viewer_command` field, loaded via `internal/config/user_settings.go` (~lines 26, 50) — passed straight through, unvalidated.
- The `viewer == ""` check does not catch `" "`, `"\t"`, `"\n"` etc.

Repo conventions: test files live next to sources; look for an existing `model_tabs_viewer_test.go` or a `viewerLaunch` test in `internal/ui/center` (`grep -rn 'viewerLaunch\|viewer_command' internal/ui/center --include='*_test.go'`).

## Commands you will need

| Purpose   | Command                              | Expected on success |
|-----------|--------------------------------------|---------------------|
| Build     | `go build ./internal/ui/center`      | exit 0              |
| Unit test | `go test ./internal/ui/center -count=1` | all pass         |
| Lint      | `make lint`                          | exit 0              |
| Full gate | `make devcheck`                      | exit 0              |

## Scope

**In scope**:
- `internal/ui/center/model_tabs_viewer.go`
- `internal/ui/center/model_tabs_viewer_test.go` (create if absent) or wherever `viewerLaunch` tests live.

**Out of scope**:
- `internal/config/user_settings.go` — do not add config-level validation there; the fix belongs at the use site (config validation is a separate, larger surface).
- The deliberate non-quoting of the viewer fragment — that's documented design ("deliberately NOT quoted"); a whitespace value is a degenerate case, not a reason to re-engineer quoting.
- Any other `strings.Fields(...)[0]` sites — if you find them during review, note them in your report but don't expand scope.

## Git workflow

- Branch: `advisor/003-viewer-command-whitespace` off `main`.
- Commit style: `fix: fall back to default viewer on whitespace-only viewer_command`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Fix the degenerate check

In `viewerLaunch`, replace the empty check with a whitespace-insensitive one and make the label computation defensive:

```go
	viewer := strings.TrimSpace(m.viewerCommand)
	if viewer == "" {
		viewer = "vim"
	}
```

This single change covers both the command and the label: `strings.Fields("vim")[0]` is always safe. (Alternative, equally acceptable: keep `viewer` untrimmed for the command but compute the label as `if f := strings.Fields(viewer); len(f) > 0 { label = f[0] } else { label = "vim" }`. Prefer the trim — it also means `"  nvim  "` produces `nvim -- 'file'` rather than `  nvim   -- 'file'`.)

**Verify**: `go build ./internal/ui/center` → exit 0.

### Step 2: Tests

Add table cases for `viewerLaunch` (find or create the test file):

- `viewerCommand = ""` → cmd starts `vim -- '...'`, label `"vim"` (existing behavior, pin it).
- `viewerCommand = " "` → same as empty (the bug fix).
- `viewerCommand = "\t\n"` → same.
- `viewerCommand = "less -R"` → cmd starts `less -R -- '...'`, label `"less"` (existing multi-token behavior, pin it).
- `viewerCommand = "  nvim "` → label `"nvim"`.

For the file path, include one with a single quote (`/tmp/it's/a.txt`) to pin the existing escaping — label should still be the first field.

**Verify**: `go test ./internal/ui/center -run 'ViewerLaunch|Viewer' -count=1 -v` → all pass.

### Step 3: Full gate

**Verify**: `make devcheck` → exit 0.

## Test plan

- New table cases in the `viewerLaunch` test (Step 2).
- The panic case (`" "`) is the regression test — assert it returns rather than panics (the test itself is the assertion; a panic fails it).
- Verification: `go test ./internal/ui/center -count=1` → all pass.

## Done criteria

- [ ] `viewerLaunch` treats whitespace-only `viewer_command` as unset (falls back to `vim`).
- [ ] Label is never taken from an empty `strings.Fields` result.
- [ ] `go test ./internal/ui/center -count=1` exits 0 with the new cases.
- [ ] `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `viewerLaunch` has been refactored (e.g. moved to a dialog/config package) — apply the same fix at its new home or report.
- The codebase now validates `viewer_command` at config load — confirm the use site is still defensive or report the finding as already-fixed.
- `strings.Fields(viewer)[0]` is gone entirely — drift; verify no equivalent panic path remains and report.

## Maintenance notes

- `viewer_command` is a documented user config key (see `docs/CONFIG.md`); if validation is ever added at load time, keep this use-site guard anyway — defense in depth on a panic path costs nothing.
- Reviewer: check that the trim doesn't change the command for already-valid values with interior spacing (`"less -R"` must still yield `less -R -- 'file'`).
