# Plan 006: Collapse per-identity `list-sessions` calls and defer `AllSessionMeta` in tmux discovery

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/app/app_tmux_discover.go internal/app/app_tmux_sync.go internal/tmux/tags.go internal/app/services.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: perf
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`sessionsWithWorkspaceTag` issues one `tmux list-sessions` subprocess **per workspace-identity form** (up to 3 forms per workspace) even though every call uses the identical format string — only the client-side match value (`@amux_workspace`) differs. The 7-second sync tick therefore forks up to 3 byte-identical subprocesses, and workspace activation repeats it for agent + sidebar discovery. Separately, `AllSessionMeta` (another `list-sessions` fork) runs unconditionally before the row loop but is only consulted when a discovered row lacks `@amux_created_at` — one wasted subprocess on every tick in steady state. Together: up to 4 wasted tmux forks per tick per workspace, the exact subprocess-spawn pattern the rest of the refactor eliminated.

## Current state

`internal/app/app_tmux_discover.go`:

```go
	rows, err := sessionsWithWorkspaceTag(svc, wsIDForms, "@amux_type", "agent",
		[]string{"@amux_assistant", "@amux_created_at"}, opts)
	...
	// One batched metadata call replaces a per-session SessionCreatedAt
	// probe per row.
	meta, _ := svc.AllSessionMeta(opts)          // :66 — unconditional fork
	var tabs []data.TabInfo
	for _, row := range rows {
		...
		if createdAt == 0 {
			if m, ok := meta[row.Name]; ok {     // only consumer of meta
				createdAt = m.CreatedAt
			}
		}
```

and `sessionsWithWorkspaceTag` (:203-227):

```go
	for _, form := range wsIDForms {
		match := map[string]string{
			"@amux":           "1",
			"@amux_workspace": form,
		}
		...
		rows, err := svc.SessionsWithTags(match, keys, opts)   // one list-sessions per form
```

Supporting facts (verified):

- `internal/tmux/tags.go:72-95` — `SessionsWithTags` builds the `-F` format from sorted key *names* only and matches values client-side; so one call requesting `@amux_workspace` as a returned key can serve all forms (match `row.Tags["@amux_workspace"]` against the `wsIDForms` set).
- `internal/data/workspace.go:160-177` — `WorkspaceIdentitySet` returns up to 3 forms (persisted ID, MetadataID, ComputedID).
- Callers of the tick path: `internal/app/app_tmux_sync.go:27-34` (`handleTmuxSyncTick`, 7s) and the workspace-activation discoveries at `app_tmux_discover.go:58` (agents) and `:140` (sidebar).
- `svc.AllSessionMeta` → `internal/tmux/clients.go:72-76` — a second `list-sessions` subprocess.

Interfaces: `svc` is `TmuxOps` (`internal/app/services.go:25-54`); `SessionsWithTags(match map[string]string, keys []string, opts tmux.Options)` — check its exact signature before writing (the `match` map and `keys` are as shown). The fakes live in `internal/testutil/tmuxops/fakes.go` — they will need the same signature updates if you change the interface (prefer NOT changing the interface: a single `SessionsWithTags` call with `@amux_workspace` in `keys` and an empty/partial `match` map needs no signature change — verify the semantics allow omitting a key from `match`).

## Commands you will need

| Purpose      | Command                                          | Expected on success |
|--------------|--------------------------------------------------|---------------------|
| Build        | `go build ./internal/app ./internal/tmux`        | exit 0              |
| Unit test    | `go test ./internal/app -count=1`                | all pass            |
| Tmux tests   | `go test ./internal/tmux -count=1`               | all pass or documented skip |
| Lint         | `make lint`                                      | exit 0              |
| Strict lint  | `make lint-strict-new`                           | exit 0 (changed-code ratchet) |
| Full gate    | `make devcheck`                                  | exit 0              |

## Scope

**In scope**:
- `internal/app/app_tmux_discover.go` — `sessionsWithWorkspaceTag` and the `meta` fetch site.
- `internal/app/app_tmux_discover_test.go` or the test file covering discovery (find via `grep -rln 'sessionsWithWorkspaceTag\|AllSessionMeta' internal/app --include='*_test.go'`).
- `internal/testutil/tmuxops/fakes.go` — ONLY if tests need a new fake behavior (e.g. counting `SessionsWithTags` calls to prove the single-fork property).

**Out of scope**:
- `internal/tmux/tags.go`, `clients.go` — the tmux primitives are correct; this is a caller-side batching fix.
- `internal/app/services.go` interface changes — keep `TmuxOps` unchanged if at all possible (a signature change ripples to every fake).
- The sidebar/agent discovery *call sites* at `:58`/`:140` — they already funnel through `sessionsWithWorkspaceTag`.
- plans/018's dead-method pruning (`ContentHash` etc.) — separate plan; if both touch `services.go`, land that one after this.

## Git workflow

- Branch: `advisor/006-tmux-discovery-fanout` off `main`.
- Commit style: `perf: collapse tmux discovery list-sessions fan-out`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: One `SessionsWithTags` call for all identity forms

Rewrite `sessionsWithWorkspaceTag` to:

1. Build `match` with the identity-form key *excluded* (`@amux: "1"` + `extraKey/extraValue` only) and `@amux_workspace` included in `keys` (dedupe if already present).
2. Issue ONE `svc.SessionsWithTags(match, keys, opts)`.
3. Filter client-side: keep a row iff `row.Tags["@amux_workspace"] ∈ wsIDForms-set`.
4. Keep the `seen`-dedup (now unnecessary across forms but harmless — session names are unique in one result set; you may keep the map for clarity or drop it since one call can't return duplicates — check `SessionsWithTags` semantics first).

Preserve the godoc explaining WHY wsIDForms exist (the restart-orphan rationale in the current comment).

**Verify**: `go build ./internal/app` → exit 0.

### Step 2: Lazy `AllSessionMeta`

Move the `meta` fetch inside the row loop lazily — fetch once, only on first use:

```go
	var meta map[string]tmux.SessionMeta   // match svc.AllSessionMeta's return type
	var metaFetched bool
	for _, row := range rows {
		...
		if createdAt == 0 {
			if !metaFetched {
				meta, _ = svc.AllSessionMeta(opts)
				metaFetched = true
			}
			if m, ok := meta[row.Name]; ok {
				createdAt = m.CreatedAt
			}
		}
		...
	}
```

Apply the same pattern at the second discovery site (`:140` sidebar path) if it shares the structure — check whether it also calls `AllSessionMeta` eagerly (`grep -n 'AllSessionMeta' internal/app/app_tmux_discover.go` for every site).

**Verify**: `go build ./internal/app` → exit 0; `go test ./internal/app -count=1` → pass.

### Step 3: Prove the subprocess count with a fake-level test

In the discovery test file, add a fake `TmuxOps` (or extend the existing fake) that counts `SessionsWithTags`/`AllSessionMeta` invocations. Assert:

- With a 3-form `wsIDForms`, discovery issues exactly **1** `SessionsWithTags` call (was 3).
- With all rows carrying `@amux_created_at`, `AllSessionMeta` is called **0** times (was 1).
- With a row missing the tag, `AllSessionMeta` is called exactly **1** time regardless of how many rows need it.

**Verify**: `go test ./internal/app -count=1 -run 'Discover|TmuxSync' -v` → pass.

### Step 4: Full gate + strict lint

**Verify**: `make devcheck` → exit 0; `make lint-strict-new` → exit 0 (this file is under the ratchet — keep nesting flat; extract a `matchIdentityForms` helper if the filter grows past two levels).

## Test plan

- New: the call-count assertions (Step 3); a gap case where `wsIDForms` has 3 entries and only one matches; a row whose `@amux_workspace` tag matches none of the forms (excluded).
- Existing: the discovery/session-contract tests in `internal/app` and `internal/tmux/session_contract_test.go` must keep passing — they pin the tag vocabulary.
- Structural pattern: `internal/app/app_tmux_discover_test.go` (or nearest equivalent) with `testutil` fakes.

## Done criteria

- [ ] One `SessionsWithTags` subprocess per discovery call regardless of identity-form count (asserted by test).
- [ ] `AllSessionMeta` runs only when a row actually lacks `@amux_created_at` (asserted by test).
- [ ] Discovery results are unchanged (same rows, same createdAt values) — the dedup/order contract holds.
- [ ] `go test ./internal/app -count=1` exits 0; `make devcheck` + `make lint-strict-new` exit 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `SessionsWithTags` can't express "return all rows with `@amux=1` regardless of workspace tag" — e.g. `match` requires every listed key to match. Then implement the inverse: keep per-form calls but memoize by format-string on the `TmuxOps` impl — STOP and report if neither works cleanly.
- `wsIDForms` is consumed elsewhere expecting the per-form iteration order (rows grouped by form) — preserve row order semantics or report.
- The sidebar site at `:140` does NOT share `sessionsWithWorkspaceTag` (drift) — apply the same collapse there or report.

## Maintenance notes

- If workspace identity ever gains a 4th form, this fix keeps the cost at one subprocess — that was the point; don't reintroduce per-form calls.
- `perf.Count` exists in `internal/perf` — if reviewers want runtime visibility, `perf.Count("tmux_discovery_rows", n)` is the house idiom (optional, not required).
- The 7s tick (`handleTmuxSyncTick`) is the main beneficiary; workspace activation is bursty — both now share the collapsed path.
