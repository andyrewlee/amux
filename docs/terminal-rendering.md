# Terminal Rendering and Flicker Mitigation

This document describes how amux renders PTY output and the guardrails in place to prevent flicker or corrupt terminal state, especially when switching worktrees.

## Rendering Pipeline

1. PTY output is read per tab and buffered in `pendingOutput`.
2. Buffered bytes are flushed on a short quiet interval (or a max interval) into the virtual terminal (`vterm.VTerm`).
3. `vterm` parses ANSI sequences, updates its screen buffers, and `Render()` converts the current state into ANSI output for Bubble Tea.
4. The app wraps each frame in DEC synchronized output (`ESC[?2026h` ... `ESC[?2026l`) to reduce flicker in the host terminal.

Key files:
- `internal/ui/center/model.go` (PTY buffering and flush scheduling)
- `internal/vterm/*` (ANSI parsing, screen buffers, and rendering)
- `internal/app/app.go` (top-level synchronized output wrapping)

## Synchronized Output (DEC mode 2026)

The child terminal can request synchronized output via `ESC[?2026h` / `ESC[?2026l`.
`vterm` snapshots the screen while sync mode is active and renders the snapshot until sync ends.
This prevents partial-frame updates from appearing as flicker.

## PTY Read Loop Guard (One Reader Per Tab)

Each tab must have **exactly one** active PTY read loop. Multiple readers on the same PTY can:
- deliver output out of order,
- split ANSI sequences unpredictably, and
- increase redraw frequency,
leading to visible flicker (especially with full-screen UIs like Claude Code).

The `readerActive` guard in `internal/ui/center/model.go` ensures that `StartPTYReaders()` cannot start duplicate loops if it is called multiple times (for example, after opening agents in other worktrees).

If you change the reader lifecycle, preserve the invariant: **one reader per tab**.

## Worktree Switching Edge Cases

- Tabs persist per worktree and PTYs keep running even when the worktree is not active. This keeps state current when you return.
- Flush timing is slightly relaxed for alt-screen or synchronized output to avoid partial updates.
- If flicker worsens after opening additional worktrees, verify that only one reader is active per tab and that output is not being reordered.

## Debugging Checklist

- Confirm only one reader is active per tab.
- Verify synchronized output mode toggles correctly in `vterm` when a child uses DEC 2026.
- Watch for frequent redraws from background tabs; consider coalescing updates if needed.
