# Plan 033: In-app browser for saved transcripts (`~/.amux/transcripts/`)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/app/app_prefix.go internal/ui/common/ internal/messages/messages_events.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none (pairs naturally with plans/019 — async file listing — but doesn't require it)
- **Category**: direction
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`t f` writes `~/.amux/transcripts/<ws>-<ts>.txt`, but nothing in-app reads that directory — transcript export is one-directional. Reviewing "what did the agent do in that workspace last week" requires leaving amux for `ls`/grep. The building blocks all exist: the `common` file picker, the `viewer_command` file-viewer tab (`messages.OpenFileInVim`), and the read-only output dialog. Wiring picker → viewer-tab closes the loop with shipped components.

## Current state

- `internal/app/app_prefix.go:341` — `t f` writes `~/.amux/transcripts/<ws>-<ts>.txt` (read the surrounding code for the dir helper + filename shape).
- `internal/messages/messages_events.go:91-95` — `messages.OpenFileInVim` — the message that opens the file-viewer tab (check its fields; it's the existing "open a file in a tab" surface despite the name).
- `internal/ui/common/filepicker*.go` — the FilePicker component (modal, dirs + files, autocomplete) — plans/019 makes its I/O async; this plan can land before or after.
- The prefix key table at `app_prefix.go:33-52` — where a transcripts-browser binding would live (e.g. under `t` sub-commands — read the table structure first; sub-keys under `t` exist for transcript ops already).
- `viewer_command` config — the viewer tab opens files with the configured viewer; transcripts are text — reuse.

## Commands you will need

| Purpose    | Command                                   | Expected on success |
|------------|-------------------------------------------|---------------------|
| Build      | `go build ./internal/app ./internal/ui/common` | exit 0           |
| Unit tests | `go test ./internal/app -count=1`         | all pass            |
| Harness    | `go run ./cmd/amux-harness -dump-frame /tmp/tr.txt` (appropriate mode) | renders |
| Full gate  | `make devcheck`                           | exit 0              |

## Scope

**In scope**:
- A transcripts picker entry point (key in the `t` prefix family — e.g. `t o`/`t b` "open/browse" — check the table for a free key).
- Picker rooted at `~/.amux/transcripts` (the dir helper — find where the write path computes it, likely `internal/app/app_prefix.go` near :341 or a `transcriptsDir()` helper).
- Selecting a file → `messages.OpenFileInVim` (or the current file-viewer message — verify the type name/fields).
- Tests + one README controls-table line.

**Out of scope**:
- plans/031's persisted *script* transcripts — different store, different surface (`O` viewer already covers them; don't merge).
- FilePicker internals — plans/019's domain.
- Transcript cleanup/retention UI — separate question; out.

## Git workflow

- Branch: `advisor/033-transcript-browser` off `main`.
- Commit style: `feat: browse saved transcripts in-app`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Locate the surfaces

Read `app_prefix.go:33-52` (key table) + `:330-350` (transcript write — the dir path helper), `messages_events.go:91-95` (`OpenFileInVim` fields), and how `FilePicker` is currently launched/handled elsewhere (`grep -rn 'FilePicker' internal/app internal/ui --include='*.go' | grep -v _test | head`).

**Verify**: you can name the exact message type + fields to open a file, and the dir constant/helper.

### Step 2: Bind the picker

Add the key (resolve conflicts in the table), open `FilePicker` rooted at the transcripts dir — files-only mode (`directoriesOnly=false`? — check the picker's options), sorted newest-first if the picker supports it (else accept alpha sort; filename embeds timestamp so alpha ≈ chrono anyway if the timestamp is in the name — check the `<ws>-<ts>.txt` format).

**Verify**: `go build ./internal/app` → exit 0; harness frame shows the picker.

### Step 3: Selection → viewer tab

On picker-confirm, emit the existing file-open message with the absolute path. Missing dir (`~/.amux/transcripts` doesn't exist yet) → toast "no transcripts saved yet" rather than an empty picker — check how other "empty state" cases toast.

**Verify**: `go test ./internal/app -run 'Transcript' -v` → pass.

### Step 4: Docs + gates

README controls-table row for the key; `make devcheck` → exit 0.

## Test plan

- New: key → picker opens rooted at transcripts dir; confirm → file-open message emitted; missing dir → toast.
- Structural pattern: existing `t f` transcript-save tests + other picker-launch tests.

## Done criteria

- [ ] A key opens a file picker over `~/.amux/transcripts`.
- [ ] Confirming opens the file in the existing viewer tab.
- [ ] Missing/empty dir produces a toast, not an empty modal.
- [ ] `go test ./internal/app -count=1` exits 0; `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The transcripts dir isn't a fixed location (e.g. configurable) — root the picker at whatever the write path resolves, not a hardcoded `~/.amux`.
- `OpenFileInVim` isn't the right viewer message (e.g. it forces vim vs the configured viewer) — use the message the file-viewer tab actually consumes.
- The picker can't be rooted at an absolute dir without component changes — that's a FilePicker limitation; report rather than hacking the path.

## Maintenance notes

- If transcript naming changes, the picker's sort key comment should note the timestamp-in-name assumption.
- This surface would naturally gain a "delete transcript" verb later — keep the picker generic enough for a second action row.
