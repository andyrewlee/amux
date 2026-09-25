# Plan 019: Move FilePicker directory I/O off the Update goroutine

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/ui/common/filepicker_navigation.go internal/ui/common/filepicker*.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: perf
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`loadDirectory` runs `os.ReadDir` synchronously inside `Update` on every Enter/backspace-to-parent/autocomplete keystroke, and `handleBackspace`/`handleEnter`/`handleOpenFromInput` each `os.Stat` the input path inline. A keystroke into a large or slow directory (network mount, FUSE, huge dir) stalls the entire TUI until the syscall returns — this is the only in-Update file I/O found in the repo. The codebase's convention is async `tea.Cmd` work; the picker should match it.

## Current state

`internal/ui/common/filepicker_navigation.go`:

```go
// loadDirectory loads entries from the current path
func (fp *FilePicker) loadDirectory() {
	fp.entries = nil
	...
	entries, err := os.ReadDir(fp.currentPath)     // :20 — synchronous, in Update
	...
}
```

Call sites reaching it inside `Update`: Enter (:212), backspace-to-parent (:145,:174), autocomplete (:310). Plus `os.Stat` at :162, :239, :291 on the input path.

The model: `FilePicker` is a bubbletea sub-model used modally; check `internal/ui/common/` for how sibling components do async work (`grep -rn 'tea.Cmd\|func.*Msg\|loading' internal/ui/common/filepicker*.go internal/ui/common/*.go | head -30`) and whether a `filePickerMsg`-style message type exists or one must be added. The codebase's async convention: return `tea.Cmd` from Update, deliver results via a typed `tea.Msg` — e.g. `messages.*` types in `internal/messages/` or local msg types for component-internal flows. Since FilePicker is inside `internal/ui/common` (which cannot import `internal/messages` — check the package's imports; `ui` packages shouldn't import `app`), the result message should be a package-local type (e.g. `type directoryLoadedMsg struct { entries []os.DirEntry; err error }`).

Safety notes to honor:

- Out-of-order loads: user may navigate again while a load is in flight → tag the result with the path it belongs to (`directoryLoadedMsg{path, ...}`) and discard stale results where `msg.path != fp.currentPath`.
- Loading state: a `fp.loading` bool rendering a "loading…" row so Enter on a slow dir doesn't look dead.
- `os.Stat` calls on the input path: same async treatment OR keep synchronous only if they're cheap-path checks — the audit flags them; stat on the *typed* path (not readdir) is usually fast, but a hung FUSE mount stalls it too — put both behind the same async boundary for consistency (a single `resolvePathCmd` returning a `pathResolvedMsg`).

## Commands you will need

| Purpose    | Command                                    | Expected on success |
|------------|--------------------------------------------|---------------------|
| Build      | `go build ./internal/ui/common`            | exit 0              |
| Unit tests | `go test ./internal/ui/common -count=1`    | all pass            |
| Lint       | `make lint && make lint-strict-new`        | exit 0              |
| Harness    | `go run ./cmd/amux-harness -mode center -frames 1 -warmup 0 -dump-frame /tmp/fp.txt` (if a filepicker surface is reachable in the harness — check `cmd/amux-harness -h` for a relevant mode) | renders |
| Full gate  | `make devcheck`                            | exit 0              |

## Scope

**In scope**:
- `internal/ui/common/filepicker_navigation.go` — async load/stat.
- `internal/ui/common/filepicker.go` (or wherever `Update`/`View` live — `ls internal/ui/common/filepicker*` for the full set) — loading state + message handling.
- `internal/ui/common/filepicker*_test.go` — async-load tests.

**Out of scope**:
- `internal/app` callers — the picker's Update signature (`(Model, tea.Cmd)`) is standard; callers already handle `tea.Cmd` returns. Verify a caller doesn't ignore the returned cmd (check how the dialog wraps it — `grep -rn 'filePicker\|FilePicker' internal/app | grep -v _test | head`).
- The `os.Stat` in code paths that are already async.
- Any visual redesign of the picker — "loading…" uses the simplest existing row style.

## Git workflow

- Branch: `advisor/019-filepicker-async` off `main`.
- Commit style: `perf: move filepicker directory I/O off the update loop`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add the async load message + cmd

Add to the filepicker package:

```go
type directoryLoadedMsg struct {
	path    string
	entries []os.DirEntry
	err     error
}

func loadDirectoryCmd(path string) tea.Cmd {
	return func() tea.Msg {
		entries, err := os.ReadDir(path)
		return directoryLoadedMsg{path: path, entries: entries, err: err}
	}
}
```

Split `loadDirectory` into the pure entry-processing function (filter/sort — keep it on the UI goroutine, it's CPU-only) and the I/O (now in the cmd). Set `fp.loading = true` where the synchronous call used to happen; return the cmd.

**Verify**: `go build ./internal/ui/common` → exit 0 (Update returns cmd — check its signature already supports returning `(Model, tea.Cmd)`; bubbletea sub-models in this repo do).

### Step 2: Handle `directoryLoadedMsg` in Update

In Update's switch: `case directoryLoadedMsg:` — if `msg.path != fp.currentPath` discard (stale); else `fp.loading = false`, run the existing filter/sort pipeline on `msg.entries`, handle `msg.err` the same way the sync version did (empty entries — check current error handling: it silently returns at :21-23; mirror that).

**Verify**: `go build ./internal/ui/common` → exit 0.

### Step 3: Same treatment for the `os.Stat` input-path checks

At :162/:239/:291 — wrap in `resolvePathCmd(path)` returning `pathResolvedMsg{input, info, err}`, handled in Update (with the input-path stale guard). If two of the three are trivially local-path checks a reviewer would keep synchronous, keep the smallest honest version — but the typed-path stat is user-controlled and deserves the same async boundary; implement uniformly unless a case genuinely can't wait.

**Verify**: `go build ./internal/ui/common` → exit 0; `go test ./internal/ui/common -count=1` → pass.

### Step 4: Loading row in View

Render "loading…" (match the picker's row style — e.g. dimmed) while `fp.loading`. Then `go test` + harness if a relevant mode exists.

**Verify**: `go test ./internal/ui/common -count=1` → pass; `make devcheck` → exit 0.

## Test plan

- New tests in `filepicker*_test.go`: (a) async load delivers entries via message (drive `loadDirectoryCmd` → feed msg into Update → entries populated); (b) stale-path msg discarded; (c) error path leaves entries empty + `loading` cleared; (d) Enter/backspace paths produce the cmd, not a synchronous call.
- Structural pattern: existing filepicker tests — check `internal/ui/common/filepicker*_test.go` for how they drive `Update`.

## Done criteria

- [ ] No `os.ReadDir`/`os.Stat` executes inside the filepicker's synchronous Update path (grep confirms — all fs calls live inside `tea.Cmd` closures).
- [ ] Stale-path results are discarded; loading state renders.
- [ ] `go test ./internal/ui/common -count=1` exits 0 with new tests.
- [ ] `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- FilePicker's Update signature can't return a `tea.Cmd` (would ripple to callers) — report; that's a bigger refactor than scoped.
- The modal's caller discards returned cmds — check the dialog wrapper first; if true, the async results never arrive and the fix needs the wrapper updated (then it's in-scope-adjacent — report before expanding).
- `os.Stat` sites turn out to be on paths that are already inside a cmd — skip those; don't wrap twice.

## Maintenance notes

- The stale-result guard (`msg.path != fp.currentPath`) is the correctness hinge — a reviewer should check every async path tags its result.
- If the picker ever pre-fetches subdirectories, the same cmd pattern extends naturally — keep IO out of Update as the rule.
- `internal/ui/common` is a leaf package: the result messages must stay package-local (no `internal/messages` import — that layer is for app-routed messages).
