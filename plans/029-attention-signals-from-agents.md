# Plan 029: Route agent bell/notification signals into the attention surface (design + minimal implementation)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/vterm/ internal/app/activity/ internal/ui/dashboard/ internal/ui/center/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: direction
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

amux's core value is attention routing across N agents — but it currently eats the agents' own escalation channel. The vterm swallows BEL (`parser.go:312-313` — literally `// Ignore`) and drops notification OSC sequences (`osc.go` handles only titles 0/1/2, cwd 7, clipboard 52). An agent that stalls on a permission prompt (Claude Code emits BEL on notification events) is invisible: output-rate decays working→done→idle within `DoneWindow` (30s) and never edges. The receiving surface already exists — done badge, `n` attention jump, `bellCmd`, toasts — this plan opens the channel from terminal bytes to that surface.

## Current state

The emit side (untrusted terminal output → parser):

- `internal/vterm/parser.go:312-313` — `case b == 0x07: // Bell — Ignore`.
- `internal/vterm/osc.go:17-50` — `dispatchOSC` handles cmd 0/1/2 (title), 7 (cwd), 52 (clipboard); everything else silently ignored, including OSC 9/99/777 notification families. `p.vt.setPendingClipboard` shows the pending-value pattern for parser→consumer handoff (see `PendingClipboard` accessors — find how vterm exposes pending state to the center tab).
- `internal/data/agent_state.go:9-13` — state model is `idle|working|done` only; no "blocked/needs-input" concept.

The receive side (already built):

- `internal/ui/dashboard/dashboard_navigation.go:246-252` — done badge + `n` jump to next attention.
- `internal/ui/dashboard/model.go:20-28` — `bellCmd`.
- Toast infrastructure — `messages.Toast` handled in `app_input.go`.
- Activity layer — `internal/app/activity/types.go` (`DoneWindow`, state transitions), `internal/app/activity/logic.go`.

Precedent for opt-in untrusted-terminal features: `AMUX_ENABLE_OSC52_CLIPBOARD` (README:~296) — OSC52 is gated behind an env flag because terminal output is hostile input. This feature should inherit the same posture for any payload-bearing variant.

## Design decision the plan must make (decide in Step 1, document in the plan/PR)

Option A (recommended start): **BEL only, count-based** — no payload parsing. `Parser` counts BEL per parse chunk; the tab surfaces a "needs attention" edge into the existing done-badge/attention-jump path. Zero payload-sanitization surface, works for every agent that rings BEL.

Option B: BEL + OSC 9/99/777 text — real notification payloads, requires sanitizing + capping text and gating behind an env flag like OSC52.

Scope this plan to Option A end-to-end; write the OSC-9 acceptance criteria so Option B is an obvious follow-up, not a redesign.

## Commands you will need

| Purpose      | Command                                          | Expected on success |
|--------------|--------------------------------------------------|---------------------|
| Build        | `go build ./internal/vterm ./internal/ui/center ./internal/app` | exit 0 |
| Unit tests   | `go test ./internal/vterm ./internal/app ./internal/ui/center ./internal/ui/dashboard -count=1` | all pass |
| Harness      | `go run ./cmd/amux-harness -mode center -frames 1 -warmup 0 -dump-frame /tmp/bel.txt` | renders |
| E2E (real)   | `go test ./internal/e2e -count=1` (if a BEL-driving scenario is added) | pass |
| Full gate    | `make devcheck`                                  | exit 0              |

## Scope

**In scope**:
- `internal/vterm/parser.go` (+ whatever pending-state accessor mirrors `PendingClipboard`).
- `internal/vterm/*_test.go` — BEL emission tests.
- `internal/ui/center/` — tab plumbing from parser to a message (wherever `PendingClipboard` is consumed today — mirror that path).
- `internal/messages/` — a `AgentAttention`-ish message type OR reuse of an existing activity message — check `internal/messages/messages_events.go` for the right vocabulary.
- `internal/app/` — the message handler that drives the attention badge/jump (dashboard already renders badges for `done`; wire the new edge into the same surface, not a new visual system).
- `internal/app/activity/` — only if the "blocked" edge belongs in the state model (see Step 3 design call).

**Out of scope**:
- OSC 9/99/777 payload parsing — Option B follow-up.
- `agent_state.go` enum changes unless Step 3 justifies them — prefer reusing `done`/badge surface over new states.
- Per-agent notification text, rate-limit policy beyond a simple coalesce — keep the minimal version honest.
- `README` feature documentation — add one line only if the surface becomes user-visible (it will — the `n` jump target list / docs table; keep it minimal).

## Git workflow

- Branch: `advisor/029-attention-signals` off `main`.
- Commit style: `feat: route agent bell into attention surface`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Emit a pending bell flag in vterm

Mirror `PendingClipboard`'s pattern exactly: `Parser`/`VTerm` gains `pendingBell bool` (or a count if multiple BELs per chunk matter — a bool suffices for "attention edge"; decide by looking at how clipboard pending is consumed). On `case b == 0x07:` set it instead of ignoring. Add `func (v *VTerm) TakePendingBell() bool` (read-and-clear — match `PendingClipboard`'s accessor naming).

**Verify**: `go build ./internal/vterm` → exit 0; unit test: `Write([]byte("\a"))` → `TakePendingBell()` true, second take false. `go test ./internal/vterm -run 'Bell' -v` → pass.

### Step 2: Surface the flag as a tab→app message

Find where `PendingClipboard` (or equivalent per-flush parser state) is drained — likely `internal/ui/center` per-flush handling (`grep -rn 'PendingClipboard\|pendingClipboard' internal/ui/center internal/app`). Drain `TakePendingBell` there and emit a `tea.Msg` — e.g. `messages.AgentAttention{Workspace, Tab, Kind: "bell"}` (check `internal/messages/messages_events.go` for the vocabulary — reuse an existing activity/attention message if one fits; don't mint a duplicate).

**Verify**: `go build ./internal/ui/center` → exit 0.

### Step 3: Wire into the attention surface

In `internal/app` handle the message: mark the workspace/tab as attention-needed so `n` jumps to it and the badge shows. Decide here whether "bell attention" maps onto the existing `done` edge (simplest — treat BEL as an implicit done-attention) or needs a distinct `blocked` edge in `activity` (more semantic, more code). Prefer mapping onto the existing surface for Option A; a distinct edge only if the badge/jump flow can't express it without conflating with "done".

Rate-limit: coalesce — a BEL arriving while the workspace is already attention-flagged is a no-op (no counter UI in Option A).

**Verify**: `go build ./internal/app` → exit 0; `go test ./internal/app -run 'Attention|Activity' -v` → pass.

### Step 4: End-to-end check

Drive a BEL through the real path: unit-level — a tab actor write containing `\a` produces the attention message; e2e-level if feasible (an agent emitting `\a` — the fake agent can echo it; check `internal/e2e/fakeagent`). Assert the badge/jump observable.

**Verify**: `go test ./internal/app ./internal/ui/center -count=1` → pass; `make devcheck` → exit 0.

## Test plan

- New: vterm `TakePendingBell` unit test; center drain test; app attention-edge test (badge set + `n` targets it); coalesce test (second BEL while flagged = no duplicate).
- Structural patterns: `PendingClipboard` tests in `internal/vterm`, attention/badge tests in `internal/app` + `internal/ui/dashboard`.
- Edge: BEL inside a large ANSI stream (mid-sequence) — parser must still flag it (the byte-level dispatch already handles this; test a mixed chunk).

## Done criteria

- [ ] BEL in agent output surfaces a visible attention signal reachable via `n`.
- [ ] No new state in `agent_state.go` unless explicitly justified in the PR (Option A maps onto existing surface).
- [ ] The flag is read-and-clear per flush; coalesced while flagged.
- [ ] `go test ./internal/vterm ./internal/ui/center ./internal/app -count=1` exits 0; `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- `PendingClipboard`'s drain path doesn't exist where expected (drift) — trace how parser pending state actually reaches the app layer and adapt.
- The attention surface requires a distinct `blocked` state (reviewer/product call) — that converts Option A into Option B territory; implement the state minimally or report for product sign-off.
- Any OSC payload parsing appears necessary to satisfy the finding — that IS Option B; scope-stop and report.
- BEL turns out to be emitted constantly by a common agent (false-edge spam) — the coalesce may need a time-window; if observed in e2e, report the rate rather than silently debouncing.

## Maintenance notes

- Option B (OSC 9/99/777 payloads) builds on this: the emit side becomes `pendingAttention{kind, text}` with sanitization + `AMUX_ENABLE_*` gating — keep `TakePendingBell`'s shape compatible (e.g. it becomes `TakePendingAttention` in the follow-up; the app-side surface doesn't change).
- Security posture: terminal output is untrusted; never interpolate agent bytes into shell commands, paths, or unstyled UI text (the badge has no payload in Option A — keep it that way).
- Reviewer focus: the coalesce semantics and whether `n`-jump ordering treats bell-attention correctly vs done-attention.
