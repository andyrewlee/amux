# Sprites + amux: plan + resources (handoff doc)

## Purpose

Document the design intent, external references, and a concrete plan to add Sprites support to amux while renaming the “sandbox” concept to “computer” and making Daytona and Sprites interchangeable for end users.

## Core philosophy (from Sprites + Fly)

- Sprites are durable, persistent Linux computers that hibernate when idle and wake quickly; the goal is to avoid ephemeral sandboxes and let agents keep state between runs.
- Checkpoint/restore is first‑class: save filesystem state quickly, restore in seconds; suitable for iterative agent work.
- Agents want “computers,” not stateless containers. State should be in the computer, not externalized to object stores.
- Sessions should survive disconnects; exec APIs should support long‑running commands and reattachment.
- Avoid adding UX features that Sprites itself does not support (e.g., no “auto‑delete on exit”).

## External references (source of truth)

Use these URLs as the canonical references when making design choices.

### Sprites product + philosophy
- https://fly.io/blog/code-and-let-live/
- https://simonwillison.net/2026/Jan/9/sprites-dev/
- https://sprites.dev/

### Sprites REST API (primary technical source)
- https://sprites.dev/api/sprites
- https://sprites.dev/api/sprites/exec
- https://sprites.dev/api/sprites/checkpoints
- https://sprites.dev/api/sprites/policies
- https://sprites.dev/api/sprites/proxy

### Sprites docs (concepts / auth)
- https://docs.sprites.dev/api/rest/
- https://docs.sprites.dev/concepts/checkpoints/

### Daytona docs (capabilities + API)
- https://www.daytona.io/docs/api
- https://www.daytona.io/docs/volumes
- https://www.daytona.io/docs/snapshots
- https://daytona.github.io/daytona/api/sdk/typescript/

### Docker docs (local provider baseline)
- https://docs.docker.com/engine/storage/volumes/
- https://docs.docker.com/reference/cli/docker/container/exec/
- https://docs.docker.com/reference/cli/docker/container/attach/
- https://docs.docker.com/reference/cli/docker/checkpoint/
- https://docs.docker.com/engine/containers/run/
- https://docs.docker.com/engine/network/

## Sprites API essentials (condensed)

### Auth + base URL
- Base URL: https://api.sprites.dev
- Auth header: Authorization: Bearer $SPRITES_TOKEN

### Sprite management
- POST /v1/sprites: create (name, wait_for_capacity, url_settings.auth)
- GET /v1/sprites: list with prefix, pagination
- GET /v1/sprites/{name}: get details
- PUT /v1/sprites/{name}: update url_settings.auth
- DELETE /v1/sprites/{name}: destroy
- Sprite status: cold | warm | running
- GET /v1/sprites/{name}/check: health check
- POST /v1/sprites/{name}/upgrade: upgrade sprite config
- GET /v1/sprites/{name}/*: HTTP proxy to sprite environment

### Exec (command execution)
- WSS /v1/sprites/{name}/exec: interactive or non‑TTY; sessions persist across disconnects
- WSS query params: cmd (repeatable), id (attach), path, tty, stdin, cols, rows, max_run_after_disconnect, env (replaces default env)
- JSON messages: resize, session_info, exit, port_opened/port_closed
- Binary protocol (non‑TTY): multiplexed streams with prefix byte
- POST /v1/sprites/{name}/exec: non‑TTY exec via HTTP
- GET /v1/sprites/{name}/exec: list sessions
- WSS /v1/sprites/{name}/exec/{session_id}: attach
- POST /v1/sprites/{name}/exec/{session_id}/kill

### Checkpoints
- POST /v1/sprites/{name}/checkpoint: create checkpoint (NDJSON streaming progress)
- GET /v1/sprites/{name}/checkpoints: list
- GET /v1/sprites/{name}/checkpoints/{checkpoint_id}: get details
- POST /v1/sprites/{name}/checkpoints/{checkpoint_id}/restore: restore (NDJSON streaming progress)
- Semantics: checkpoint is live/fast (no downtime). Captures filesystem, packages, services/policies, config; excludes running processes, network connections, in‑memory state. Copy‑on‑write storage; last 5 checkpoints exposed in /.sprite/checkpoints/; checkpoints are per‑sprite (not portable).

### Network policy
- GET /v1/sprites/{name}/policy/network
- POST /v1/sprites/{name}/policy/network
- Rules: domain patterns (exact + wildcard), allow/deny, includes
- Policies apply immediately; blocked DNS lookups return REFUSED.

### Proxy (port access)
- WSS /v1/sprites/{name}/proxy, JSON init {host, port}, then raw TCP relay

## amux current state (inventory)

### Key packages and files
- internal/computer/ARCHITECTURE.md: provider-agnostic sandbox design; Daytona default
- internal/computer/provider.go: Provider interface for sandbox backends
- internal/computer/provider_daytona.go: Daytona provider implementation
- internal/computer/sandbox.go: EnsureSandbox (create/reuse), auto‑stop
- internal/computer/credentials.go: credentials stored on computer filesystem
- internal/computer/sync.go + sync_incremental.go: workspace sync to remote
- internal/computer/snapshot.go: builds pre‑installed agent snapshots
- internal/computer/workspace.go: workspace meta stored in .amux/workspace.json
- internal/cli/sandbox.go + aliases.go: CLI commands (amux sandbox run, etc.)
- internal/cli/status.go + setup.go + auth.go + doctor_enhanced.go

### Behavior (today)
- “Sandbox” is the top‑level concept and naming.
- Workspace state is synced to remote via tar or incremental sync.
- Credentials are persisted on the computer filesystem (no volumes needed).
- Snapshots are image builds (not live checkpoint/restore).
- Auto‑stop defaults to 30 minutes.

## Goal state (user requirements)

- Add Sprites as a first‑class provider.
- Add Docker (local) as a provider option.
- Rename “sandbox” to “computer” everywhere (user‑facing and internal APIs).
- Make Sprites and Daytona interchangeable at the UX level.
- Align amux UX and semantics to Sprites philosophy (durable computers, persistent state, checkpoint/restore, attachable sessions).
- No “ephemeral” auto‑delete mode (Sprites does not provide this); deletion is explicit.

## Proposed unified “computer” model

### Core interface (provider-agnostic)
- Computer lifecycle: Create, Get, List, Delete, Start, Stop
- Exec: command execution with TTY + non‑TTY
- Exec sessions: list, attach, kill
- Checkpoints: create, list, restore, get
- Network policy: get, set
- Proxy: port tunnel (TCP)
- Preview URLs: optional
- Volume support: optional (for Daytona compatibility)

### Mapping
- Sprites: native support for exec sessions, checkpoints, network policy, proxy.
- Daytona: implement/approximate missing features via snapshots or volumes; gaps documented.
- Docker: local provider; use volumes + exec/attach; gaps documented.

### Default scoping (initial)
- Default computer scope: **project** (one computer per repo per provider).
- Rationale: matches current TUI mental model (shared local machine) while keeping remote compute manageable.
- Future: add optional scope modes (worktree/custom) after core provider parity is stable.
 - Docker default: project scope. Use bind‑mount common parent if feasible; otherwise fall back to volume + sync for active worktree.

## Capability matrix (Sprites as target experience)

Legend: ✅ native support, ⚠️ possible with workaround, ❌ not available (document to users)

| Capability | Sprites | Daytona | Docker (local) |
| --- | --- | --- | --- |
| Durable computer disk (persist across restarts) | ✅ | ⚠️ volumes (FUSE object storage) | ⚠️ volumes/binds |
| Resume same computer by name/id | ✅ | ✅ | ✅ (container id/name) |
| Exec command (non‑TTY) | ✅ | ✅ | ✅ |
| Interactive exec (TTY) | ✅ | ✅ | ✅ |
| List exec sessions | ✅ | ✅ (process sessions) | ❌ (no built‑in listing) |
| Attach to existing exec session | ✅ | ⚠️ (SSH + tmux/screen) | ⚠️ (tmux/screen in container) |
| Checkpoint create | ✅ | ⚠️ (if Daytona backup/restore exists) | ⚠️ experimental |
| Checkpoint restore | ✅ | ⚠️ (if Daytona restore exists) | ⚠️ experimental |
| Network policy: domain allow/deny | ✅ | ❌ | ❌ |
| Network isolation (coarse) | ✅ | ✅ | ✅ |
| Raw TCP proxy | ✅ | ❌ | ⚠️ host port mapping |
| HTTP preview URL | ✅ (sprite URL) | ✅ (preview links) | ⚠️ host port mapping + local proxy |
| Desktop/VNC | ❌ (not core) | ✅ | ⚠️ (custom) |

Notes:
- Goal is “as close as possible,” but we will not force unsupported features. amux must surface capabilities clearly in CLI/help and errors.
- Daytona checkpoint/restore is a key unknown; verify in docs/SDK or treat as not supported until confirmed.

## Gaps and design implications (Daytona vs Sprites)

- **Persistent workspace**: Sprites keeps writable disk; Daytona currently uses sync. To align, add a workspace volume (durable) and reduce or eliminate “sync every time” as default.
- **Checkpoint/restore**: Sprites has live checkpoints. Daytona “snapshots” are Dockerfile‑based images (templates) rather than live filesystem checkpoints. Docs show backup endpoints but no documented restore flow. Plan: emulate checkpoints via volume+manifest or tar/rsync, unless Daytona exposes true volume snapshots. 
- **Exec sessions**: Sprites supports durable exec sessions with attach. Daytona has “process sessions” endpoints, but docs do not describe WebSocket attach/PTY resume semantics. Plan: map to process sessions where possible; for interactive, use SSH + tmux/screen to allow reattach.
- **Network policy**: Sprites provides domain‑based allow/deny policies that apply immediately. Daytona docs show network allow/block settings at sandbox creation (CIDR/IP allowlists) but not dynamic DNS policy. Plan: emulate via in‑sandbox DNS tooling if needed.
- **Proxy**: Sprites offers raw TCP proxy. Daytona exposes preview URLs (HTTP) and desktop/VNC; no documented raw TCP proxy. Plan: add SSH port forward as a compatibility path.

## Plan (phased)

1) **Design + terminology**
   - Introduce “computer” as the canonical term across docs/CLI.
   - Add backwards‑compatible “sandbox” aliases for commands and config to avoid breakage.

2) **New provider interface**
   - Replace/extend `Provider` to match Sprites semantics: exec sessions, checkpoints, proxy, network policy.
   - Add feature flags for optional capabilities.

3) **Sprites provider**
   - Implement Sprites client (HTTP + WS) with auth via SPRITES_TOKEN.
   - Support create/list/get/update/destroy, exec (TTY + non‑TTY), session list/attach/kill, checkpoints, proxy, network policy.

4) **Daytona alignment**
   - Add durable workspace volume by default; keep sync only as opt‑in or fallback.
   - Provide checkpoint emulation or Daytona snapshot integration if available.
   - Optional: tmux‑based exec sessions for reattach.

5) **Docker provider**
   - Implement local Docker provider (containers + volumes + exec/attach).
   - Use volumes as the durable “computer disk” by default.
   - For project scope: bind‑mount common parent when possible; else volume + sync.
   - Optional: tmux/screen for attachable sessions; expose gaps.

6) **CLI + UX**
   - Rename commands: `amux computer ...` (with `amux sandbox ...` alias).
   - Update aliases (`amux claude`, etc.) to use “computer” naming in help text.
   - Update config keys, env var names (with compatibility shim).
   - Add capability reporting in CLI (e.g., `amux computer capabilities <provider>`).
   - Default computer scope: project (per‑repo). Avoid multiple scope modes until core UX is stable.

7) **Docs + help**
   - Update README, architecture doc, doctor output, and setup messages.
   - Document provider capability gaps and recommended workarounds.

8) **Tests + migration**
   - Update tests or add new ones for provider selection and new commands.
   - Rename `.amux/workspace.json` to `.amux/computer.json` with no backward‑compatibility.

## Open questions / decisions needed

- **Package rename**: YES — rename `internal/computer` → `internal/computer` (full rename for consistency).
- **Workspace meta file**: `.amux/workspace.json` is a local per‑workspace metadata file that stores the workspace ID, provider, remote sandbox/computer ID, config hash, agent, and created_at. It’s used to reuse/restore the same remote environment across runs. Need to decide whether to rename it to `.amux/computer.json` with backward‑compatible loading.
- **Default provider**: none. User must choose a provider; both Daytona and Sprites can be set up and selected explicitly.
- **Interchangeability goal**: as close as possible; prefer emulation over “not supported”. Daytona doc review suggests potential gaps (live checkpoint/restore, dynamic DNS policy, raw TCP proxy, attachable PTY sessions). We should validate these and design compatibility shims.
 - **No backward compatibility**: confirmed. Rename `.amux/workspace.json` → `.amux/computer.json` with no legacy loading.
 - **Default scope**: project (per repo).
 - **No ephemeral mode**: confirmed (Sprites does not offer auto‑delete).

## Discussion topics (pending)

- Confirm Daytona feature parity from docs and refine the compatibility layer to cover any gaps.

## Next steps

- Discuss workspace metadata rename and Daytona parity findings.
- Once approved, implement per the plan in order, with Sprites API and philosophy as the guiding reference.
