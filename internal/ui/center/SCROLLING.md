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

### Frame atomicity (why agents don't flicker)

A flush can land mid-repaint: the parser may have consumed a `2J` clear but
not yet the repaint that follows, and rendering that state shows a torn or
blank frame. Two mechanisms prevent that:

1. The session bootstrap (`internal/tmux/command.go`) advertises the `sync`
   terminal feature for amux's client TERM before attaching, so tmux wraps
   each redraw in DEC 2026 markers (`ESC[?2026h` … `ESC[?2026l`).
2. `internal/vterm` freezes `RenderBuffers()` at the sync-begin snapshot until
   sync-end. Bytes parsed inside the window mutate the live buffers (and mark
   lines dirty) but are never rendered mid-frame.

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
