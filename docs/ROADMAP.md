# Roadmap

The direction this repo is heading, derived from a study of where
agent-orchestration runtimes are and what amux's differentiators are:
parallel agents in isolated worktrees of the same repo, reporting status,
delegating, resuming. amux borrows the gaps it lacks; it keeps and deepens
the worktree isolation, shelve/restore, ports, and verified state machines
the studied runtimes do not have.

Phases are user-visible milestones, not refactors. Each one lands green
with its docs.

## Phase A — Status is reported, not inferred (done)

Programs hosted in amux panes self-report their state through the Program
Status Protocol (OSC 7501); amux stores what was reported and never
re-infers it from the screen. Heuristics demote to the fallback tier for
silent agents. New states: blocked and error, surfaced in the dashboard
attention machine, the center tab strip, and `@amux_agent_state`. The
agent-facing verbs `amux report` and `amux wait` ship here. All of it
model-checked.

Merged as `54f4e91` (issue #135). Implementation notes: tmux consumes
unknown OSC before amux's vterm could see it, so interception is a
pipe-pane tap installed at every session-creation path; reports land as
`@amux_reported_*` pane options (shared across amux instances by
construction, dead with the pane) rather than a seq-guarded store — the
tap serializes writes per pane, so last-writer-wins is the only ordering
that exists.

## Phase B — Agents drive amux

The orchestrator becomes scriptable by the things it hosts:

- `amux --skill`: one-read agent docs (what a hosted agent needs to know
  to drive amux, in a single output).
- `amux agent ls`: list live agents/workspaces with their reported state.
- `amux send`: deliver input to a hosted session (the Phase A `amux wait`
  already covers the blocking half).
- Workspace **templates** as roles (reviewer, implementer, auditor)
  building on the existing run-script machinery.

The workspace-management CLI was removed on purpose in PR #204
(`c76dce7c`), and the deleted implementation is recoverable from
`git show c76dce7c^` — Phase B trims it down rather than rewriting it,
and updates the external-orchestration contract
([docs/ORCHESTRATION.md](ORCHESTRATION.md)) and its pinning test in the
same change.

## Phase C — The resume tier

Agents report a `resume_argv` through the Phase A protocol; amux stores
it on the workspace record (validation rules from the study are directly
copyable: plain command name, ≤64 args, 8 KiB total, no control
characters). Restore/shelve then offers real conversation continuity, not
just git state. E2E-prove it for Claude Code plus one other agent.

## Phase D — amuxd

Formalize the core: a versioned socket API (JSON-RPC, event stream with
seq/snapshot/heartbeat/missed discipline, a released-schema compat gate).
The real fork to decide *then*: an amux-owned daemon on amux's own ptyio
stack (sidebar terminals prove the tech) versus keeping tmux as the
process owner with an amux API layer on top. The TUI becomes just another
client. Do this after A–C, informed by real usage.

## Phase E — Delegation trees + machines

Parent/child workspace links (`--outlive-parent`), blocked-propagation to
parents, then `amux workspace create --on box` over SSH.

## Deliberately not doing

GUI, GPU renderer, plugin marketplace, kitty graphics (until a concrete
need), screen-history replay by default (kept off for secrets; same
instinct as the study).

## Order rationale

A delivers reported status and immediately makes status truthful. B is a
modest lift that changes who can use amux. C completes the persistence
story amux's shelve machinery already half-owns. D is the big
re-architecture and deserves the evidence from A–C. E compounds.
