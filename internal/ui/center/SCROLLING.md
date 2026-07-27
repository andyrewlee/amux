# Streaming and scrolling

This document is the map for the two most regression-prone behaviors in the
center pane: how streamed agent output becomes rendered frames, and how the
user's scroll/selection state survives that stream. Read it before touching
`model_scrolled_history.go`, `model_input_mouse.go`, `tab_actor*.go`,
`internal/ui/ptyio`, or `internal/vterm`'s scroll/sync/capture code.

## The pipeline: PTY bytes → rendered frame

```
tmux client PTY
  → ptyio.RunPTYReader        coalesce reads (size + FrameInterval ticker)
  → ptyio.ForwardPTYMsgs      merge consecutive PTYOutput msgs per tab
  → center Update(PTYOutput)  append to tab.PendingOutput, debounce PTYFlush
  → Update(PTYFlush)          quiet-period defer, take a bounded chunk
  → tab actor (WriteOutput)   noise filter → vterm.Write (parse)
  → TerminalLayerWithCursorOwner   version-keyed snapshot cache
  → compositor VTermLayer     cell diffing happens below, in ultraviolet
```

Every hop coalesces or throttles; none of them may reorder or drop output
bytes (`tabEventWriteOutput` is never shed — see the invariants in
`tab_actor.go`).

### Wide glyphs (why lines don't drift left)

`VTermLayer.DrawAt` writes only the *base* cell of a wide glyph. Ultraviolet's
`Line.Set` stamps the zero-width placeholder for the second column itself, and
it reads a write *to* a placeholder as "something is overwriting half of a wide
glyph" — so writing the continuation cell explicitly blanks the base we just
drew and leaves an orphan placeholder. The renderer emits nothing for a
zero-width cell, so every cell after it lands one column to the left.

A wide glyph is two cells — a base (`Width == 2`) and a continuation
(`Width == 0`) — and a renderer must draw exactly one column per cell. Both
halves can be orphaned, and each drifts the line a different way:

- **Orphan continuation** (zero-width cell with no wide base at `x-1`, e.g. a
  half-erased glyph): renderers emit nothing for it, so the line drifts
  **left**. Ask `vterm.IsWideContinuation(row, x)`; draw a blank when false.
- **Widowed base** (`Width == 2` whose continuation was overwritten, or pushed
  out of the viewport by a resize — `resizeRows` keeps rows wider than the
  viewport): it draws two columns where the buffer owns one, so the line drifts
  **right**. Ask `vterm.HasWideContinuation(row, x, visibleWidth)`; substitute a
  blank when false.

Both guards are required at all four renderers: `VTermLayer.DrawAt`,
`compositor.Canvas.DrawScreen`, and `vterm`'s `renderRow` /
`renderWithScrollbackFrom`. `normalizeLine` enforces the same pair of rules on
the write side (against `len(line)`), and the erase ops plus `putChar`'s
wide-glyph wrap call it — or clear the stale half directly — so neither shape
normally reaches a snapshot at all. The renderer guards are defense in depth,
and they are load-bearing for rows that are legitimately wider than the
viewport, where the write side cannot know the render width.

Regression tests: `internal/ui/compositor/vtermlayer_widecell_test.go`,
`internal/vterm/render_wide_orphan_test.go`,
`internal/vterm/render_wide_widow_test.go`,
`internal/vterm/erase_wide_normalize_test.go`,
`internal/ui/center/model_scrolled_history_wide_test.go` (the scrolled chat
path), and the composed-frame test `internal/app/harness_wide_glyph_test.go`.

### Frame atomicity (why agents don't flicker)

A flush can land mid-repaint: the parser may have consumed a `2J` clear but
not yet the repaint that follows, and rendering that state shows a torn or
blank frame. Three mechanisms prevent that:

1. The session bootstrap (`internal/tmux/command.go`) advertises the `sync`
   terminal feature for amux's client TERM before attaching, so tmux wraps
   each redraw in DEC 2026 markers (`ESC[?2026h` … `ESC[?2026l`).
2. `internal/vterm` freezes `RenderBuffers()` at the sync-begin snapshot until
   sync-end. Bytes parsed inside the window mutate the live buffers (and mark
   lines dirty) but are never rendered mid-frame.
3. `ptyio`'s flush policy ends flushes on region boundaries
   (`scanSyncFrames`): a chunk stops at the last completed region, and when
   nothing has completed the flush waits rather than writing an opening marker
   with no body.

(3) is what makes (2) work, and it is not optional. tmux writes the client
socket in ≤1KB chunks, so a redraw larger than that spans several reads — on a
captured streaming pane roughly half the reads (46% of 567) leave the terminal
inside an open region. The freeze in (2) then holds the *previous* frame, so a
flush that ends there shows stale content and reports every line dirty (a full
repaint) until the next flush closes the region. Replaying that captured stream
through the flush path: 84 of 456 flushes (18%) opened a mid-frame gap without
(3), none with it, at an identical flush count.

Measure this with the vterm parser, not by counting marker bytes: markers split
across reads make a byte counter drift, and it drifts in the direction that
overstates the problem.

A writer that brackets its frames also makes the flush quiet period redundant
for that frame — the debounce exists to guess where a frame ends — so a
completed frame flushes immediately instead of waiting the quiet period out.
That applies at the base cadence only: callers ask for a slower one by raising
the quiet period (alt-screen timing, backpressure, the inactive-tab
multipliers), and those still get the debounce. The reader's frame-interval
coalescing upstream caps how often the early flush can fire, so it lowers
latency without raising the flush rate.

Sync-end deliberately does **not** invalidate the render cache: dirty marks
accumulated during the window are preserved because frozen renders skip
`ClearDirty` (`liveRenderCacheActive()` is false), so a synced one-line append
still repaints one line, not the whole screen.

A sync-begin whose end never arrives (writer died, output trimmed under
overflow) is force-released after `vterm.SyncStallTimeout`, checked on both
`Write` and `RenderBuffers`, so the UI can freeze for at most that long.
`LoadPaneCapture*` also clears sync state on restore.

The e2e contract test is `TestTmuxDeliversSynchronizedOutputToClient`.

## Scroll state model

There is exactly one scroll offset: `vterm.ViewOffset`, per tab, measured in
lines up from the live view (`0` = live). Everything else is derived.

- Clamping: `ViewOffset` is clamped to the scrollback length of the *current
  render buffers* (frozen during sync).
- **Anchoring**: when output pushes lines into scrollback while the user is
  scrolled (`ViewOffset > 0`), `anchorViewOffsetForAddedLines` grows the
  offset so the same content stays on screen. Scrollback shrinkage (capture
  dedup, trim) adjusts it back. During sync the delta accrues in
  `syncViewOffsetDelta` and is applied at sync-end only if the user interacted
  with the viewport (`NoteSyncViewportInteraction`).
- Scrollback is capped at `vterm.MaxScrollback`; trimming shifts selection
  anchors (`shiftSelectionAfterTrim`) but not `ViewOffset` (it is measured
  from the bottom).

### Chat tabs: the history-only view

Chat agents (Claude-style) repaint their whole screen every frame, so their
scrollback is a series of captured previous frames (`CaptureNormalScreenOnClear`
capture on `2J`, deduplicated in `alt_screen_capture*.go`). Concatenating
scrollback + live screen would show the live prompt box in the middle of
history, so for chat tabs a scrolled viewport (`ViewOffset > 0`) renders a
**scrollback-only** window (`applyScrolledChatHistoryViewLocked`):

- window start = `scrollbackLen - height - effectiveOffset + 1`
- max offset = `scrollbackLen - height + 1` (a *different* max than the
  vterm's own clamp — `clampScrolledChatHistoryViewOffsetLocked` reconciles
  the two around every scroll; keep using `scrollTerminalViewLocked` rather
  than calling `Terminal.ScrollView` directly)
- screen-Y ↔ absolute-line mapping goes through
  `displayedScreenYToAbsoluteLineLocked`, which picks the chat mapping or the
  native vterm mapping.

Invariant (pinned by `TestChatTab_DragUpAutoScroll_AnchorStableWhileStreaming`):
while an agent repaints and scrollback churns through capture/dedup, an
anchored chat view must not move except by explicit scroll steps.

## Gestures: one implementation per gesture

All selection/scroll gestures are implemented once, as tab-actor handlers
(`tab_actor_selection.go`, `tab_actor.go`), and every entry point routes
through `dispatchOrHandleTabEvent`: actor queue when available, synchronous
`handleTabEvent` on the caller's goroutine otherwise. Both routes serialize on
`tab.mu`. Do not re-introduce inline fallback copies of gesture logic in
`Update` — that dual-path duplication was the primary source of drift bugs.

Follow-up work flows back through `msgSink` messages (`selectionTickRequest`,
`tabSelectionResult`, `PTYCursorRefresh`), never as return values from the
actor.

### Drag-selection auto-scroll

Dragging past the viewport edge starts a 100ms tick chain
(`common.SelectionScrollState`): each tick scrolls one line and extends the
selection to the exposed edge. The chain's messages traverse bounded queues
that may shed load (`shouldDropTabEvent`, the external message queue), so the
chain is **self-healing by design**: every drag-motion event restarts it with
the current expected tick sequence (`NeedsTick`), and `HandleTick` ignores
stale generations or duplicate sequences without stopping the current chain.
A lost tick therefore pauses auto-scroll only until the next mouse motion. Do
not "optimize" `NeedsTick` back to start-only-when-not-active; that turns any
dropped message into a dead auto-scroll for the rest of the drag.

E2E coverage: `internal/e2e/drag_select_scroll_test.go` drives real SGR mouse
input through the real binary against idle, streaming, and repainting agents.

## Where to add tests

- vterm scroll/sync/capture semantics → `internal/vterm` unit tests.
- chat-view math, gesture handlers → `internal/ui/center` (drive
  `handleTabEvent` directly, or `Update` for entry-point wiring).
- anything involving real tmux framing, mouse input, or attach/restore →
  `internal/e2e` (unit-level fakes cannot reproduce tmux's redraw framing).
