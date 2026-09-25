# Plan 001: Clamp the CSI ICH count before `insertChars` shifts cells

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/vterm/lineedit.go internal/vterm/lineedit_test.go internal/vterm/csi.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`insertChars` is the only line-edit op in `internal/vterm` with no clamp on its count parameter. A CSI `ESC[<N>@` (ICH) with `N` near `math.MaxInt64` makes `v.CursorX+n` overflow to a negative value; the shift loop `for i := v.Width - 1; i >= v.CursorX+n; i--` then never terminates (when `CursorX >= 1` the wrapped threshold is negative so `i` decrements through `MinInt64`, wraps to `MaxInt64`, and the loop is truly infinite). The write path holds the per-tab mutex, so one ~24-byte escape sequence in an agent's output wedges the pane and, transitively, the whole TUI — and the sequence can also be replayed from tmux scrollback on every reattach, making it a persistent denial of service. Every sibling op already clamps; this is a lone omission.

## Current state

- `internal/vterm/lineedit.go` — the line-edit ops (`insertLines`, `deleteLines`, `insertChars`, `deleteChars`, `eraseChars`). `insertChars` at lines 59-83 is the target.
- `internal/vterm/csi.go` — CSI dispatch; `case '@': p.vt.insertChars(p.getParam(0, 1))` at ~line 214-215 passes the parsed param straight through. Params parse via `strconv.Atoi` (~line 141), so `9223372036854775807` (MaxInt64) is accepted.
- `internal/ui/center/tab_actor_write.go` — the write path that calls the parser while holding `tab.mu` (lines ~22-27, ~75-78); a hung parse blocks every other caller of that mutex (input, resize, close, `AppendOutput`/`SeedForTrim` in `internal/ui/center/model_input_lifecycle_pty.go`).
- `internal/vterm/vterm_capture.go:67` and `internal/vterm/vterm_scroll.go:176-212` — tmux `capture-pane` bytes are replayed through the same parser, so the payload persists in scrollback across reattach.

The vulnerable function (internal/vterm/lineedit.go:59-83):

```go
// insertChars inserts n blank chars at cursor, shifting content right
func (v *VTerm) insertChars(n int) {
	if v.CursorY >= len(v.Screen) {
		return
	}
	line := v.Screen[v.CursorY]
	normalizeLine(line)

	// Shift right
	for i := v.Width - 1; i >= v.CursorX+n; i-- {
		if i < len(line) && i-n >= 0 {
			line[i] = line[i-n]
		}
	}
	...
```

The sibling that already does it right — `deleteChars` (lineedit.go:86-97), the pattern to match:

```go
	// Clamp n to the cells from the cursor to end of line (xterm DCH
	// semantics: DCH never affects cells left of the cursor).
	if remaining := v.Width - v.CursorX; n > remaining {
		n = remaining
	}
	if n <= 0 {
		return
	}
```

Repo conventions: no comments unless load-bearing (match the `deleteChars` comment style — a two-line semantic note is in-style here). Test files live next to sources (`internal/vterm/lineedit_test.go` exists). Formatter is `gofumpt` (`make fmt`).

## Commands you will need

| Purpose   | Command                                        | Expected on success |
|-----------|------------------------------------------------|---------------------|
| Build     | `go build ./internal/vterm`                    | exit 0              |
| Unit test | `go test ./internal/vterm -count=1`            | all pass            |
| Lint      | `make lint`                                    | exit 0              |
| Fmt check | `make fmt-check` (or `gofumpt -l` shows nothing)| exit 0 / no output |
| Full gate | `make devcheck`                                | exit 0              |

## Scope

**In scope** (the only files you should modify):
- `internal/vterm/lineedit.go`
- `internal/vterm/lineedit_test.go` (add regression tests)

**Out of scope** (do NOT touch, even though they look related):
- `internal/vterm/csi.go` — the dispatch site is correct; the clamp belongs inside `insertChars` where the sibling ops keep theirs.
- `insertLines`, `deleteLines`, `eraseChars`, `scrollUp`, `scrollDown` — verified already-clamped or bounded; do not "helpfully" add more clamps.
- `internal/ui/center/tab_actor_write.go`, `model_input_lifecycle_pty.go` — the mutex discipline is correct; the bug is the arithmetic, not the locking.
- Any behavior change to small `n` — the clamp must be a no-op for every `n <= v.Width - v.CursorX`.

## Git workflow

- Branch: `advisor/001-clamp-insertchars` off `main`.
- Commit style from `git log`: `fix: <imperative>` e.g. `fix: clamp insertChars count to prevent integer-overflow hang`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add the clamp to `insertChars`

In `internal/vterm/lineedit.go`, at the top of `insertChars` — after the `CursorY` bounds guard and before `line := v.Screen[v.CursorY]` — add the same two-part guard `deleteChars` uses (clamp to `v.Width - v.CursorX`, early-return on `n <= 0`). This handles both the overflow case (huge `n` clamps to `remaining`, which is `<= v.Width` and can never wrap `v.CursorX+n`) and non-positive params.

```go
	// Clamp n to the cells from the cursor to end of line (xterm ICH
	// semantics: ICH never affects cells left of the cursor).
	if remaining := v.Width - v.CursorX; n > remaining {
		n = remaining
	}
	if n <= 0 {
		return
	}
```

Also add a defensive `if v.CursorX < 0 { return }` (or clamp `CursorX` to 0) only if `deleteChars` does — check; do not invent extra guards the sibling doesn't have. Keep it symmetrical.

**Verify**: `go build ./internal/vterm` → exit 0.

### Step 2: Regression tests

In `internal/vterm/lineedit_test.go`, add tests alongside the existing insert/delete tests:

1. **Hang regression**: create a `VTerm` (find the existing test constructor — e.g. `NewVTerm(80, 24, ...)`; match how neighboring tests build one), write text to place `CursorX >= 1` (e.g. `Write([]byte("ab"))` puts the cursor at column 2), then `Write([]byte("\x1b[9223372036854775807@"))`. The call must return promptly — run it under a `time.AfterFunc`/deadline or simply rely on the test completing (Go test timeout) — asserting the screen content afterwards shows `Width - CursorX` blanks inserted, not a hang.
2. **Boundary**: `ESC[<Width>@` and `ESC[<Width+1>@` produce identical results to an unclamped-past-width insert (full line of blanks from cursor).
3. **Zero/negative**: `ESC[0@` is a no-op.
4. **CursorX=0 with MaxInt64**: `ESC[9223372036854775807@` at column 0 must also complete promptly (clamped path, erase whole line).

If no `lineedit_test.go` table exists for insertChars, model the test after the `deleteChars` cases in the same file (they exercise the same loop shapes).

**Verify**: `go test ./internal/vterm -run 'InsertChars|ICH' -count=1 -v` → all pass, including the new cases. Then `go test ./internal/vterm -count=1` → all pass.

### Step 3: Full gate

**Verify**: `make devcheck` → exit 0 (runs vet, package tests, lint, file-length, drift checks).

## Test plan

- New tests in `internal/vterm/lineedit_test.go`: the four cases in Step 2.
- Structural pattern: the `deleteChars` clamp tests in the same file (they already cover `n > remaining` for the sibling op).
- The existing `TestFuzzANSIParser` / equivalence harness in `internal/vterm` must still pass unchanged — the clamp must not alter any small-n behavior.
- Verification: `go test ./internal/vterm -count=1` → all pass.

## Done criteria

- [ ] `insertChars` clamps `n` to `v.Width - v.CursorX` and early-returns on `n <= 0`, mirroring `deleteChars`.
- [ ] `go test ./internal/vterm -count=1` exits 0 with new regression tests passing.
- [ ] A manual check feeding `ESC[9223372036854775807@` (with cursor at column >= 1) returns promptly (covered by the new test).
- [ ] `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified (`git status`).
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `insertChars` already contains a clamp (drift — the fix may have landed).
- `deleteChars` does NOT have the `remaining`/`n <= 0` guard (the codebase changed; reassess instead of copying the pattern).
- The regression test hangs even *with* the clamp — the loop has a second unbounded path; report instead of layering more guards.
- `strconv.Atoi` no longer parses the param at `csi.go` (param cap added upstream) — verify the overflow is still reachable via a different path or report the finding as moot.

## Maintenance notes

- Any future line-edit op added to this file must get the same `remaining`-clamp — the pattern is "clamp to cells affected before looping". A reviewer should check that.
- `internal/vterm` parses untrusted terminal output; when reviewing future parser PRs, every `for` loop whose bound contains `Cursor+param` arithmetic is an overflow candidate.
- The fuzz harness (`TestFuzzANSIParser` or similar in this package) never found this because it only explores param values near `Width`; if a bounded-time fuzz variant is ever added, include `math.MaxInt64`-scale params.
