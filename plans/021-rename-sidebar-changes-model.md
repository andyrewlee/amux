# Plan 021: Rename `sidebar.Model` to `ChangesModel` (it is the Changes-view model, not the sidebar's)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/ui/sidebar/ internal/app/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: tech-debt
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`internal/ui/sidebar` declares three model types: `Model` (the Changes file list), `TabbedSidebar` (the actual sidebar embedded as `a.sidebar`), and `TerminalModel` (`a.sidebarTerminal`). `sidebar.Model` reads as the sidebar's own model — it is one tab's content model. Every read of `sidebar.Model`/`m.changes` requires discovering the inversion; renaming while the surface is internal is cheap.

## Current state

`internal/ui/sidebar/model.go:26` — `type Model struct` (the Changes view).
`internal/ui/sidebar/tabs.go:46` — `type TabbedSidebar struct` (the real sidebar — wraps tabs).
`internal/ui/sidebar/terminal.go:83` — `type TerminalModel struct`.

Callers referencing `sidebar.Model` outside the package: `grep -rn 'sidebar\.Model\|sidebar\.New\b' internal/app --include='*.go' | grep -v _test` — collect the full list; in-package refs are `Model` unqualified.

Optional symmetry (do only if trivially consistent): `TerminalModel` → `SidebarTerminalModel` — same inversion. Decide based on call-site clarity; if `sidebar.TerminalModel` reads fine (it does — "terminal model in the sidebar package"), leave it. The required rename is `Model` → `ChangesModel`; the optional one is judgment, not mandate.

## Commands you will need

| Purpose    | Command                                        | Expected on success |
|------------|------------------------------------------------|---------------------|
| Build      | `go build ./...`                               | exit 0              |
| Tests      | `go test ./internal/ui/sidebar ./internal/app -count=1` | all pass |
| Lint       | `make lint && make lint-strict-new`            | exit 0              |
| Full gate  | `make devcheck`                                | exit 0              |

## Scope

**In scope**:
- `internal/ui/sidebar/*.go` — the type and its in-package references.
- `internal/app/*.go` — qualified references (`sidebar.Model`, constructor `sidebar.New*` if named for it).
- Any test files referencing the type.

**Out of scope**:
- `TabbedSidebar` (already correctly named).
- `TerminalModel` rename — optional, only if it completes the symmetry cleanly (see above).
- Behavior — pure rename, zero logic edits.

## Git workflow

- Branch: `advisor/021-sidebar-model-rename` off `main`.
- Commit style: `refactor: rename sidebar.Model to ChangesModel`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Rename the type

`Model` → `ChangesModel` in `internal/ui/sidebar/model.go` including its constructor (`New` → likely `NewChangesModel` or `NewChanges` — check what the constructor is called and rename to match the type; e.g. `func New()` → `func NewChangesModel()`). Update doc comments referencing "the sidebar model" where they now mean the changes model.

**Verify**: `go build ./internal/ui/sidebar` → errors list every caller to update.

### Step 2: Update all references

In-package: receivers stay `m` (convention). Out-of-package: `sidebar.Model` → `sidebar.ChangesModel`, constructor calls updated. Let compile errors drive the enumeration; then `grep -rn 'sidebar\.Model\b\|sidebar\.New\b' internal/ cmd/` → zero hits.

**Verify**: `go build ./...` → exit 0.

### Step 3: Field/receiver clarity pass (minimal)

Where `a.sidebar` wraps the tabbed sidebar and calls into the changes model (e.g. `m.sidebar.changesModel` or `TabbedSidebar.Changes()`), check the field/accessor names still read correctly — rename `changes` → keep or clarify only if obviously confusing post-rename. Do NOT cascade renames beyond clarity needs.

**Verify**: `go test ./internal/ui/sidebar ./internal/app -count=1` → all pass.

### Step 4: Gates

**Verify**: `make devcheck` → exit 0; `make lint-strict-new` → exit 0.

## Test plan

- No new tests — rename verified by compilation + full suite.
- `go test ./internal/ui/sidebar ./internal/app -count=1` → all pass.

## Done criteria

- [ ] `sidebar.Model` no longer exists; the Changes-view model is `sidebar.ChangesModel`.
- [ ] `grep -rn 'sidebar\.Model\b' internal/` → no matches.
- [ ] `go build ./...`, `go test` on touched packages, `make devcheck` all pass.
- [ ] No behavioral diff (`git diff` shows only identifier changes + comment touch-ups).
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `Model` is referenced by reflection/tag strings (e.g. `%T` in tests, golden names) — grep for literal `"Model"` strings in sidebar tests; if the type name is asserted, update those.
- The rename collides with an existing `ChangesModel` symbol — pick the next-clear name (`ChangesListModel`) and note it.
- `TerminalModel` rename turns out to be required for clarity at every call site — fold it in (same commit ok) or defer to a follow-up; judgment call, don't split hairs.

## Maintenance notes

- Package readers now map: `ChangesModel` = Changes tab content, `TabbedSidebar` = the sidebar itself, `TerminalModel` = sidebar terminal tab — keep new models named by their view (`XModel`), never `Model`.
