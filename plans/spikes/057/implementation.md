# Plan 057 build handoff: search retained script output

Self-contained build plan for the GO decision in `decision.md`. Every
behavioral claim is pinned by a case in `interaction-cases.json` (cited as
`case:<id>`).

## Scoped files

- `internal/ui/common/output_dialog.go` — search state, key handling,
  match recompute, footer/match rendering. Only file with feature logic.
- `internal/ui/common/output_dialog_search_test.go` — new test file (keeps
  `output_dialog_test.go` and the 500-line cap intact).
- `internal/app/app_overlays.go` — `handleRunOutputInput`: gate the `a`
  intercept on `!dlg.Editing()`; pass `consumePaste = dlg.Editing()`.
- `internal/app/app_workspace_scripts.go` — `handleShowScriptOutput`:
  `SetSearchable(true)` on the `O` dialog.
- `internal/app/app_run_output.go` — `handleRunOutputOpened`:
  `SetSearchable(true)` on the `R` dialog.
- `internal/app/app_workspace_status.go` — **unchanged**; status stays
  non-searchable (`case:non-searchable-flavor-unchanged`).
- `internal/app/app_run_output_search_test.go` — new app-level test for the
  attach gate and paste routing (or extend the existing run-output test file
  if it stays under the 500-line cap).

## Minimum query state (additions to `OutputDialog`)

```go
searchable bool     // opt-in via SetSearchable; default false
editing    bool     // query-edit mode vs browse mode
query      string   // literal, case-insensitive; single-line
matches    []match  // {line int; startRune, endRune int} — rune spans
matchIdx   int      // -1 when query empty or 0 matches
wrapped    bool     // last n/N jump wrapped (footer indicator, one jump)
```

New exported surface: `SetSearchable(bool)`, `Editing() bool`. Nothing else
leaves the package — the app only needs to know whether the dialog is
editing (for the `a` gate and paste routing).

## Sanitization / index rules

- Match against `d.lines` — already sanitized at ingestion by
  `sanitizeOutputLines`; never re-read raw content (`case:evicted-history-not-searchable`).
- Literal case-insensitive substring: lowercase both haystack and query
  (`strings.EqualFold`-style fold on lowercased runes is fine; document the
  choice — no Unicode case-folding subtleties beyond `strings.ToLower`
  per-rune, matching the simplicity of the feature).
- Store rune offsets: compute byte offsets via `strings.Index` on
  lowercased text, then convert each byte offset to a rune index over the
  original line once (`case:unicode-content-match`).
- Recompute happens (a) on every query keystroke while editing — live
  preview of `0 matches`/count is allowed but selection does not jump until
  accept — and (b) inside `SetContent` whenever `query != ""`
  (`case:refresh-recomputes-matches`, `case:refresh-evicts-selected-match`).
- Selection repair after recompute: if `matchIdx`'s old line index still
  holds a match, keep it; else select the first match at-or-after that line
  (nearest surviving match), else `matchIdx = -1`
  (`case:refresh-evicts-selected-match`).

## Overlay routing order (app side)

In `handleRunOutputInput` (app_overlays.go:105):

```go
d := a.overlays.runOutput
if a.overlays.runOutputAttachable && d != nil && d.Visible() && !d.Editing() {
    if kp, ok := msg.(tea.KeyPressMsg); ok && kp.String() == "a" { ... }
}
updated, consumed := handleOverlayInput(d, msg, cmds, d != nil && d.Editing())
```

That is the entire app-side routing change: `a` attach and paste-fallthrough
both key off `Editing()` (`case:attach-key-isolation`,
`case:attach-still-works-while-browsing`, `case:paste-into-query`,
`case:paste-falls-through-when-browsing`).

## Dialog Update contract (browse vs edit)

Edit mode consumes ALL input and never emits `OutputDialogResult`:

- printable chars → append to `query`, recompute matches
  (`case:literal-command-chars-in-query`)
- `backspace`/`ctrl+u`/`ctrl+w` → delete; empty query is allowed
  (`case:empty-query-clears`)
- `enter` → accept: exit edit mode; if matches exist jump+highlight match 1
  and disengage follow (`following=false`, same as a manual scroll);
  empty query clears search state (`case:accept-jumps-to-first-match`,
  `case:empty-query-clears`)
- `esc` → exit edit mode only, query retained
  (`case:esc-exits-edit-then-closes`)
- `tea.PasteMsg` → append pasted text; treat embedded `\n`/`\r` as accept
  (`case:paste-into-query`)

Browse mode adds exactly two bindings on top of today's map:

- `/` → enter edit mode, only when `searchable` (`case:open-edit-mode`,
  `case:non-searchable-flavor-unchanged`)
- `n`/`N` → next/prev match, inert when `matchIdx == -1`; wrap sets
  `wrapped=true` and footer shows `(wrapped)` for that jump
  (`case:next-prev-cycle`, `case:wrap-indicator`, `case:no-match-feedback`,
  `case:duplicate-lines-distinct-matches`)
- `enter`/`esc`/scroll/`f`/`g`/`G` — **unchanged**
  (`case:enter-in-browse-still-closes`, `case:follow-pause-and-resume`)

## Rendering

- Footer swaps help text while editing to `/query█` + `esc done · enter
  accept`; truncate the echo at footer width with `…`
  (`case:narrow-layout`).
- Accepted state footer: `match k/N` or `0 matches`, `(wrapped)` suffix on
  the jump that wrapped; keep `f follow`/`esc close` hints when they fit.
- Highlight: current match's substring span gets the existing dialog's
  accent/inverse style; only the *selected* match highlights (multi-match
  full-buffer highlighting is scope creep — note it in the code comment as
  intentionally deferred).
- Centering: set `offset` so the match line is mid-viewport, clamped;
  this is offset math independent of render width
  (`case:narrow-layout`).

## Tests → case map

Extend `internal/ui/common/output_dialog_test.go` patterns (new file
`output_dialog_search_test.go`, driving `Update` with `tea.KeyPressMsg` /
`tea.PasteMsg` and asserting footer lines + `OutputDialogResult` presence):

| Case IDs | Test |
|---|---|
| open-edit-mode, literal-command-chars-in-query, esc-exits-edit-then-closes, enter-in-browse-still-closes | `TestOutputDialog_SearchEditMode` |
| accept-jumps-to-first-match, no-match-feedback, empty-query-clears | `TestOutputDialog_SearchAccept` |
| next-prev-cycle, wrap-indicator, duplicate-lines-distinct-matches | `TestOutputDialog_SearchNavigation` |
| refresh-recomputes-matches, refresh-evicts-selected-match, evicted-history-not-searchable | `TestOutputDialog_SearchRefresh` |
| follow-pause-and-resume | extend `TestOutputDialog_FollowPinsToBottomOnRefresh` pattern |
| paste-into-query, paste-falls-through-when-browsing | `TestOutputDialog_SearchPaste` |
| unicode-content-match | `TestOutputDialog_SearchUnicode` (extend `TestOutputDialog_SanitizesContent` fixture) |
| narrow-layout, section-header-searchable, non-searchable-flavor-unchanged | `TestOutputDialog_SearchEdge` |
| attach-key-isolation, attach-still-works-while-browsing | `TestRunOutput_SearchBlocksAttachKey` (app-level, beside existing `runOutputAttachable` coverage — drive `handleRunOutputInput` directly) |

Also extend `TestOutputDialog_ShowScrollClose` with one `/`-open + esc-esc
close assertion so the close contract regression is covered
(`case:esc-exits-edit-then-closes`).

## Verification

```bash
go test ./internal/ui/common -run 'TestOutputDialog' -count=1
go test ./internal/app -run 'TestRunOutput|TestShowScriptOutput|TestWorkspaceStatus' -count=1
go test -race ./internal/ui/common ./internal/app -count=1
make lint-strict-new
env -u AMUX_WORKSPACES_ROOT make devcheck
make verify-loop          # overlay input routing changed — real keystroke gate
make harness-presets      # footer/help render changed
make perf-check           # render path touched (footer line)
```

Expected: all dialog tests green; `verify-loop` passes unchanged (search
adds no PTY path); perf p95 within baseline noise (one extra substring scan
only on keystroke, never on the render hot path).

## STOP conditions

- If `Editing()` state must leak into `overlayChain` beyond
  `handleRunOutputInput` (e.g. another overlay needs it), STOP — the opt-in
  boundary is wrong and needs redesign.
- If paste cannot be scoped to edit mode without changing
  `handleOverlayInput`'s signature for other overlays, STOP and re-spike —
  do not change the shared slot contract for this feature.
- If recompute-on-keystroke measurably lags on the 3×64 KiB composite (the
  largest input), STOP and reassess — do not reach for an index structure
  in v1; either debounce or defer the feature.
- If `enter`-accept vs `enter`-close cannot be made unambiguous in the
  footer affordance (users closing the dialog mid-query), STOP — that is a
  UX contract failure, not a polish issue.

## Documentation (same change)

- `README.md` — keybinding table: add `/` search, `n`/`N` next/prev under
  the run-output/script-output viewer entry; note status view excludes it.
- `docs/CONFIG.md` — no config key added; add one line to the script-output
  section noting search is case-insensitive and searches the retained tail
  only (dropped history is not searchable).
- `docs/ORCHESTRATION.md` — lifecycle scripts section: mention `O` viewer
  search for hunting transcript errors.

## Maintenance notes

- New `OutputDialog` callers must choose `SetSearchable` explicitly; the
  default is off. Searchable is appropriate only for free-form transcript
  content, not fixed-field panels.
- If a future caller enables search, audit its overlay slot for app-level
  key intercepts — the `a`-gate pattern must be repeated per intercept.
