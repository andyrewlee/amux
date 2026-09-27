# Plan 057 decision: search retained script output

## Evidence

`internal/ui/common/output_dialog.go` is the single read-only viewer behind
three callers that all share `a.overlays.runOutput`:

| Caller | Flavor | File | Refresh | Extras |
|---|---|---|---|---|
| `O` script transcripts | static | `internal/app/app_workspace_scripts.go:225` (`handleShowScriptOutput`) | none — content is a `composeScriptOutputView` composite of the last setup/archive/on-done tails | `runOutputAttachable=false` |
| `R` live run output | live tail | `internal/app/app_run_output.go:127` (`handleRunOutputOpened`) | `runOutputTickInterval` tick → `handleRunOutputTick` → off-loop tmux capture → `handleRunOutputRefreshed` → `SetContent` (app_workspace_scripts.go:194); stops when the session dies | `runOutputAttachable=true`; app intercepts `a` to attach |
| `i` workspace status | static | `internal/app/app_workspace_status.go:113` | none | `runOutputAttachable=false` |

Key facts that shape the design:

- Input order is fixed by `overlayChain` (app_overlays.go:84): registry →
  file picker → settings → env → projectEnv → scripts →
  `handleRunOutputInput`. The run-output slot first intercepts `a` **only**
  when `runOutputAttachable` (app_overlays.go:105-111), then delegates to
  `handleOverlayInput(dlg, msg, cmds, consumePaste=false)` — so today a
  `tea.PasteMsg` is *not* consumed by the dialog and falls through to the
  focused terminal.
- The dialog already sanitizes at ingestion (`sanitizeOutputLines`,
  output_dialog.go:51): stored `d.lines` are display-safe, so search indexes
  exactly what the user sees — no raw-ANSI exposure.
- Content bounds are small and fixed: lifecycle transcripts ≤ 64 KiB each
  (`scriptOutputTailBytes`, script_output.go:15) composed into one document;
  run tail is `RunScriptSessionTail(session, 400)` — ~400 lines. A linear
  scan per keystroke is cheap; no index structure is warranted.
- Existing keys in `Update` (output_dialog.go:106-134): `esc`/`enter` close,
  `f` toggles follow, `up`/`k`, `down`/`j`, `pgup`, `pgdown` scroll and
  disengage follow, `g` top, `G` bottom + re-engages follow.
- `OutputDialogResult{}` is emitted only on close; `Show()`/`Hide()`/
  `Visible()` drive the overlay slot's consumption check.
- Plan 038 (DONE) stabilized what the viewer sees: persisted tails merge
  into `LastScriptOutputs` for `O`, and `R` re-reads the live/remain-on-exit
  pane tail. No pending data-shape change blocks this design.

## State model

The dialog gains one orthogonal axis on top of `visible`/`following`:

```
closed ──Show()──► browsing ──"/"──► editing query
                     │  ▲              │ Enter accepts → jump to match 1
                     │  │              │ Esc            → browsing (query kept)
                     │  └──────────────┘
                     │  n / N cycle matches (browse mode only)
                     │  Esc closes dialog (existing contract)
                     └── any scroll key keeps working; f toggles follow
```

States: **closed** → **browsing** → **editing query** → back to **browsing
(matches)**, orthogonal to follow. The minimal query state is `editing bool`,
`query string`, `matches []match` (match = line index + rune span), and
`matchIdx int` (index into `matches`, -1 when none).

Opt-in: `O` and `R` opt in (both are script-output surfaces where users hunt
for errors); the `i` status view does not — its content is a fixed-field
status panel where search adds nothing, and the plan recommends exclusion.
The dialog exposes `SetSearchable(true)`; the two script flavors set it, the
status caller does not. A future caller must decide explicitly (maintenance
note in the plan).

## Interaction contract

Resolved collisions and edge states:

| Input | Browse mode | Query-edit mode |
|---|---|---|
| `/` | enter edit mode | literal `/` in query |
| printable text incl. `a` `f` `j` `k` `g` `G` `n` `N` | existing bindings (`f`/`j`/`k`/`g`/`G`); `n`/`N` = next/prev match when a query is active | appended to query; matches recompute live |
| `a` | attach (R only, app intercept) | **literal `a`** — the app intercept in `handleRunOutputInput` is gated on `!dlg.Editing()` |
| paste (`tea.PasteMsg`) | falls through to terminal (existing `consumePaste=false`) | **consumed into the query** — the slot passes `consumePaste = dlg.Editing()`; multi-line paste collapses to the first line (queries are single-line) |
| `enter` | close dialog (unchanged) | accept query, stay open, jump to match 1 |
| `esc` | close dialog (unchanged) | leave edit mode only; a second `esc` closes |
| `backspace`/`ctrl+u`/`ctrl+w` | inert | standard line-edit delete |
| `f` | toggle follow | literal `f` |
| scroll keys | scroll, disengage follow | n/a while editing (keys go to query) |

Search semantics:

- **Match rule**: literal case-insensitive substring over the sanitized
  `d.lines` — same text the user sees. Match offsets are stored as rune
  spans, never byte offsets, so display-cell rendering and Unicode content
  are consistent.
- **Feedback**: footer shows `/query` while editing; after accept it shows
  `match k/N` (or `0 matches`), the current match's line is centered in the
  viewport and highlighted. `n`/`N` wrap with a `(wrapped)` indicator in the
  footer for one jump. Empty query (`/` + Enter, or deleting to empty)
  clears the search state entirely.
- **Follow interaction**: entering edit mode does not change `following`,
  but *accepting a query* jumps the viewport to a match — which is a manual
  scroll and therefore disengages follow (same rule as `j`/`k`). `f` or `G`
  in browse mode re-engages follow; the query text and match list are
  retained.
- **Refresh semantics**: `SetContent` recomputes matches against the new
  sanitized snapshot (recompute, not freeze — a stale hit list pointing at
  shifted lines is worse than recompute at these sizes: ≤ ~400 lines or
  3×64 KiB). If the currently selected match's line content survives at the
  same index it stays selected; otherwise selection moves to the nearest
  remaining match; if none survive the footer shows `0 matches` with the
  query retained (the user can see the stream is still live). Content the
  tail buffer has evicted is never searchable — matches only ever index
  `d.lines`.
- **Narrow layouts**: the query echo truncates at the footer width with an
  ellipsis; matches below a minimum usable width still jump correctly
  (centering is offset math, not rendering).
- **Duplicate lines**: each occurrence is its own match; `n`/`N` visit every
  occurrence, including identical adjacent lines.
- **Truncation markers**: section headers and `(no output)` placeholders are
  ordinary lines — searchable like anything else, which is desirable
  ("failed —" in a section header is a legitimate search target).

Mockups (synthetic output):

```
browsing, no query                    editing                      accepted, match 2/4
┌ Run output — api ────────┐          ┌ Run output — api ────────┐   ┌ Run output — api ────────┐
│ ...                      │          │ ...                      │   │ ...                      │
│ 48 migrations applied    │          │ 48 migrations applied    │   │ 48 migrations applied    │
│ error: EADDRINUSE :::6200│          │ error: EADDRINUSE :::6200│   │ error: EADDRINUSE :::6200│  ◀ highlighted
│ listening on :6200       │          │ listening on :6200       │   │ retrying in 500ms        │
│                          │          │                          │   │ error: EADDRINUSE :::6201│
│ up/down scroll  f follow │          │ /err█                    │   │ match 2/4  n/N nav       │
│ esc close                │          │ esc done  enter accept   │   │ f follow  esc close      │
└──────────────────────────┘          └──────────────────────────┘   └──────────────────────────┘

no match                             refresh evicted the selected match
┌ Run output — api ────────┐          ┌ Run output — api ────────┐
│ ...                      │          │ ... (new tail lines)     │
│ retrying in 500ms        │          │ error: EADDRINUSE :::6202│
│                          │          │ listening on :6202       │
│ /panic█                  │          │                          │
│ 0 matches  esc done      │          │ 0 matches (/err)         │
└──────────────────────────┘          └──────────────────────────┘
```

## Alternatives

- **Freeze the match snapshot on open** — rejected: `R` refreshes every
  tick; a frozen hit list drifts from `d.lines` immediately and would need
  its own eviction logic anyway. Recompute-on-refresh is simpler and honest.
- **Regex/fuzzy queries** — rejected per plan scope: literal substring covers
  the error-hunting use case; regex invites catastrophic-input edge cases
  for zero measured demand.
- **Separate search widget (textinput bubble)** — rejected: the query is one
  line with no cursors/selection; a 40-line hand-rolled editor over `string`
  state keeps the dependency footprint and test surface minimal.
- **Search every OutputDialog flavor including status** — rejected: status
  is a fixed-field panel; the plan recommends exclusion and opt-in keeps the
  blast radius to script surfaces.
- **Blocking the `a` attach key globally while the dialog is open** —
  rejected: users legitimately attach mid-browse; gating on edit mode
  preserves it.

## Decision

**GO.** Bounded inputs (sanitized lines, ≤ ~400-line tail or 3×64 KiB
composite), an existing single-owner overlay slot with a clean opt-in flag,
and one real collision (`a`) with a one-line gate. Estimated build effort M.

Handoff: `implementation.md` in this directory is a self-contained build
plan; `interaction-cases.json` holds the executable case matrix each future
test maps to.
