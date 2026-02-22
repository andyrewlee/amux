---
name: amux
description: Orchestrate AI coding agents via amux with managed workspaces, git worktrees, and async job queues.
metadata:
  { "openclaw": { "emoji": "🔀", "os": ["darwin", "linux"], "requires": { "bins": ["amux", "tmux"] } } }
---

# amux Skill

Orchestrate AI coding agents using `amux` — a workspace and agent lifecycle manager built on tmux. All commands support `--json` for structured output.

## CRITICAL: Always Monitor and Report Back

**Never fire-and-forget.** The user expects you to manage the full lifecycle: start the agent, wait for it to finish, summarize what happened, and suggest next steps. The user may be on their phone — they should never have to ask "how did it go?"

**Every agent interaction follows this loop:**

1. **Start or send** — Launch the agent or send a follow-up instruction (for OpenClaw, prefer `scripts/openclaw-step.sh`; otherwise use `--wait` for bounded steps and `agent watch` with heartbeat for long-running work)
2. **Confirm** — Immediately tell the user what you did ("Started Codex on the doctor workspace. Monitoring...")
3. **Summarize** — Prefer `response.delta` (new output since your send/run prompt). Fall back to `response.content` if delta is empty. Mention specific files changed, errors hit, or questions the agent is asking.
4. **Next steps** — Offer actionable follow-ups: create a PR, run tests, send another instruction, stop the agent.

### OpenClaw Runtime Guardrails

When running `amux` through OpenClaw `exec`/`process` tools, avoid monitor deadlocks:

1. Prefer `scripts/openclaw-step.sh` for `run`/`send` steps. It performs exactly one bounded wait and returns normalized JSON with `status`, `summary`, `next_action`, and `suggested_command`.
2. For multi-step coding turns, prefer `scripts/openclaw-turn.sh` to enforce step caps, timeout-streak stops, milestone coalescing, and a guaranteed final summary payload.
3. For `agent run --wait` and `agent send --wait`, set tool `timeout` to at least `--wait-timeout + 90s` (minimum 180s) to cover startup + wait.
4. When calling through OpenClaw `exec`, set `yieldMs` to at least `wait-timeout + 60000` (milliseconds). Use `timeout` at least 45s larger than `yieldMs`.
5. Prefer `--wait` over `agent watch` for normal coding steps. `agent watch` is long-lived and can be killed by tool timeouts.
6. If `exec` returns `Command still running`, keep polling the same process until it reaches a terminal status (`completed`/`failed`/`killed`); do not launch a second overlapping `amux` command for that step.
7. If the process exits with timeout/SIGKILL and no output, retry once with a higher tool timeout and immediately send an interim user update.
8. If `response.status` is `timed_out`, summarize `response.summary` (fallback: `latest_line`/`delta`), then continue with one follow-up `agent send --wait` step (prefer send over raw capture in chat loops).
9. Never pass workspace **name** to `--workspace`. Always use `workspace create` JSON `data.id` (workspace_id).
10. Do not chain `workspace create && agent run` in one shell command. Run them as separate steps so workspace_id parsing is explicit and robust.
11. Use a bounded step budget: prefer 45-90s per `--wait` step (default 60s). If a step reaches timeout, immediately send a partial progress update and continue or conclude; do not loop silently.
12. If the same process remains `running` after 3 polls with no new output, send one interim status update and continue polling every 10-15s until terminal status.
13. Use `process log` for additional visibility when needed, but keep one authoritative process per step.
14. Always finish with a user-facing completion message before overall run timeout, even when partial: include what completed, what timed out, and one clear next action.
15. Keep each OpenClaw turn short: target at most 2-3 bounded amux steps per turn. If more work remains, stop and return a partial summary plus one explicit "continue" command.
16. Reserve time for the final response: after ~180s of tool work, stop launching new tools and emit a final text summary immediately.
17. On two consecutive `timed_out` step statuses, stop the turn and return a concise partial result + `suggested_command` instead of continuing loops.
18. If `response.status` is `needs_input` and the hint indicates local permission-mode selection (e.g. bypass permissions prompt), tell the user it is blocked by interactive permissions and switch to a non-interactive assistant (typically `codex`) for continuation.
19. Before `workspace create`, validate the repo path with `git -C <repo> rev-parse --verify HEAD`; if invalid, do not continue with that path.
20. For `workspace create`, always pass an explicit absolute repo path (`--repo`/`--project`) from user context; never rely on `.` in orchestrator workspaces.
21. Parse workspace id from `data.id` (fallback `data.workspace_id` for compatibility). Never continue with an empty workspace id.
22. Some channels may not support `message read` via OpenClaw tools. Never rely on `message read` in coding loops.
23. With `openclaw agent --deliver`, `status: ok` plus empty `result.payloads` is expected when updates were sent through `message` tool; treat this as success, not failure.
24. Even when delivering updates to a chat channel, always end with a final plain-text assistant summary so local operators also get a non-empty terminal result.
25. Do not run concurrent long agent turns on the same OpenClaw agent lane/session key; queueing can add large delays and confuse progress reporting.
26. If you must call `amux --json agent capture`, branch on `data.status`; treat `session_exited` as a terminal state to summarize, not an orchestration crash.

### OpenClaw one-step wrapper (recommended)

```bash
# 1. Start a bounded step (run) and read normalized fields
step=$(skills/amux/scripts/openclaw-step.sh run \
  --workspace <workspace_id> \
  --assistant codex \
  --prompt "Add dark mode support" \
  --wait-timeout 60s \
  --idle-threshold 10s)

# 2. Summarize with top-level `summary`; branch on `status`
echo "$step" | jq -r '.summary'
echo "$step" | jq -r '.status'
echo "$step" | jq -r '.next_action'
echo "$step" | jq -r '.suggested_command'
agent_id=$(echo "$step" | jq -r '.agent_id')
```

### Follow-up step (send)

```bash
# 1. Send one bounded follow-up step
step=$(skills/amux/scripts/openclaw-step.sh send \
  --agent <agent_id> \
  --text "Also add tests" \
  --enter \
  --wait-timeout 60s \
  --idle-threshold 10s)

# 2. Post concise update and continue based on status
echo "$step" | jq -r '.summary'
echo "$step" | jq -r '.status'
```

**Always** run exactly one bounded step, then post a user-facing summary. Never just say "sent" and go silent.

If a step times out with no visible output yet, `openclaw-step.sh` now performs a short post-timeout capture recovery pass and may set:
- `recovered_from_capture: true`
- `suggested_command` with an exact bounded follow-up send command.
- `idempotency_key` auto-generated by default (disable with `OPENCLAW_STEP_AUTO_IDEMPOTENCY=false`).
- secret-safe output redaction for common tokens/credentials before channel delivery.
- `verbosity` controls via `OPENCLAW_STEP_VERBOSITY=quiet|normal|detailed` (with `OPENCLAW_STEP_DETAIL_LINES` override).
- `delivery` metadata (`key`, `action`, `priority`, `retry_after_seconds`, `replace_previous`, `drop_pending`) for edit-vs-send orchestration.
- context-aware `quick_actions` (tests/lint/security/review) with `callback_data` (`qa:*`), plus `quick_action_map`/`quick_action_prompts` for deterministic button-to-command mapping.
- `openclaw.presentation.chunks`/`openclaw.presentation.chunks_meta` for continuation-aware chunk delivery on the selected channel.
- channel payloads under `openclaw.channels.<channel_id>` and selected output under `openclaw.presentation`.
- inline button scope control via `OPENCLAW_INLINE_BUTTONS_SCOPE=off|dm|group|all|allowlist` (default `allowlist`).

Use response fields in this order for mobile updates:
1. `summary` (top-level)
2. `response.delta_compact` (clean, de-chromed content)
3. `response.delta` (raw fallback)

### Multi-step turn wrapper (recommended)

```bash
turn=$(skills/amux/scripts/openclaw-turn.sh run \
  --workspace <workspace_id> \
  --assistant codex \
  --prompt "Refactor the parser and add tests" \
  --max-steps 3 \
  --turn-budget 180 \
  --wait-timeout 60s \
  --idle-threshold 10s)

echo "$turn" | jq -r '.overall_status'
echo "$turn" | jq -r '.summary'
echo "$turn" | jq -r '.next_action'
echo "$turn" | jq -r '.openclaw.presentation.chunks[]'
```

`openclaw-turn.sh` output includes:
- `overall_status` (`completed|needs_input|timed_out|session_exited|partial|partial_budget`)
- `events` (raw step payloads), `milestones` (coalesced concise updates)
- `delivery` + `progress_updates` (+ per-step progress percent) for outbox-style edit/coalesce behavior.
- `verbosity` controls via `OPENCLAW_TURN_VERBOSITY=quiet|normal|detailed`.
- `quick_actions` with `callback_data` (`qa:*`) plus `quick_action_map`/`quick_action_prompts`.
- `openclaw.channels` and `openclaw.presentation` for channel-specific rendering payloads.
- `channel.chunks`/`channel.chunks_meta` + channel button metadata.

### OpenClaw DX control plane (project/workspace lifecycle)

Use `skills/amux/scripts/openclaw-dx.sh` when the user is coding through OpenClaw on any channel and needs end-to-end lifecycle UX (not just one prompt turn):

```bash
# Guided next-step recommendation (best for first-time mobile users)
skills/amux/scripts/openclaw-dx.sh guide [--project /abs/repo/path] [--workspace <workspace_id>] [--task "refactor ..."] [--assistant codex] [--channel slack]

# Add/select project
skills/amux/scripts/openclaw-dx.sh project add --cwd --workspace mobile --assistant codex
skills/amux/scripts/openclaw-dx.sh project add --path /abs/repo/path
skills/amux/scripts/openclaw-dx.sh project list --query api
skills/amux/scripts/openclaw-dx.sh project pick --name api

# One-shot kickoff (register project -> create workspace -> start coding turn)
skills/amux/scripts/openclaw-dx.sh workflow kickoff --project /abs/repo/path --name refactor --assistant codex --prompt "Fix highest-impact tech debt"

# Project or nested workspace decision
# Terminology:
# - project workspace: created directly from a project
# - nested workspace: created from a parent workspace (`--from-workspace`), still starting from the project's default branch
skills/amux/scripts/openclaw-dx.sh workspace decide --project /abs/repo/path --task "Refactor checkout state" --assistant codex --name refactor
skills/amux/scripts/openclaw-dx.sh workspace create --name mobile --project /abs/repo/path --assistant codex
skills/amux/scripts/openclaw-dx.sh workspace create --name refactor --from-workspace <workspace_id> --scope nested --assistant codex

# Start/continue coding turns
skills/amux/scripts/openclaw-dx.sh start --workspace <workspace_id> --assistant codex --prompt "..."
skills/amux/scripts/openclaw-dx.sh continue --workspace <workspace_id> --text "..." --enter

# Status/alerts and terminal flows
skills/amux/scripts/openclaw-dx.sh status
skills/amux/scripts/openclaw-dx.sh alerts
skills/amux/scripts/openclaw-dx.sh status --include-stale   # include stale-session alerts when explicitly desired
skills/amux/scripts/openclaw-dx.sh terminal run --workspace <workspace_id> --text "npm run dev" --enter
skills/amux/scripts/openclaw-dx.sh terminal logs --workspace <workspace_id> --lines 120

# Cleanup, review, ship
skills/amux/scripts/openclaw-dx.sh cleanup --older-than 24h
skills/amux/scripts/openclaw-dx.sh review --workspace <workspace_id> --assistant codex
skills/amux/scripts/openclaw-dx.sh git ship --workspace <workspace_id> --message "feat: ..." [--push]

# Dual-pass multi-agent handoff (implement -> review)
skills/amux/scripts/openclaw-dx.sh workflow dual --workspace <workspace_id> --implement-assistant claude --review-assistant codex

# Assistant readiness
skills/amux/scripts/openclaw-dx.sh assistants
```

`openclaw-dx.sh` emits normalized JSON with:
- `status` + `summary` for quick mobile updates.
- `quick_actions` + `openclaw.actions` (`map`, `prompts`, `fallback`).
- `openclaw.channels` and `openclaw.presentation` for channel-specific rendering.
- channel-specific button metadata in `openclaw.channels.<channel_id>`.
- `data.alerts` (needs-input/session/stale-session signals) for proactive follow-up prompts.
- `workflow.kickoff` and `workflow.dual` for one-command lifecycle/handoff orchestration.

### OpenClaw exec/process pattern (required for long steps)

```json
{
  "tool": "exec",
  "command": "skills/amux/scripts/openclaw-step.sh run --workspace <workspace_id> --assistant codex --prompt \"...\" --wait-timeout 60s --idle-threshold 10s",
  "workdir": "/Users/andrewlee/founding/amux",
  "yieldMs": 120000,
  "timeout": 180
}
```

If this backgrounds, continue polling the returned `sessionId`:

```json
{ "tool": "process", "action": "poll", "sessionId": "<sessionId>" }
```

Only move to the next step after terminal process status and a user-facing summary.

### The standard flow (direct amux --wait)

```bash
# Start and wait in one command
result=$(amux --json agent run --workspace <workspace_id> --assistant codex --prompt "Add dark mode support" --wait --wait-timeout 300s --idle-threshold 10s)
agent_id=$(echo "$result" | jq -r '.data.agent_id')
summary=$(echo "$result" | jq -r '.data.response.summary // .data.response.latest_line // .data.response.delta // ""')
```

### Checking response status

The `response` object tells you what happened:
- `response.status: "idle" | "needs_input" | "timed_out" | "session_exited"` — canonical machine-readable status (preferred)
- `response.timed_out: true` — agent didn't go idle within the timeout. Capture output separately.
- `response.session_exited: true` — the agent's session ended. Show last output and offer to restart.
- `response.idle_seconds > 0` — agent went idle normally. Read `response.content` for the full output.
- `response.changed: true|false` — whether pane output changed after the send/run prompt.
- `response.summary` — single-line canonical summary for chat/push notifications.
- `response.delta` — only the new text since the prompt/send baseline (best for chat replies).
- `response.latest_line` — one-line summary of the newest output (best for push notifications).
- `response.needs_input` — true when output looks like a confirmation/question prompt.
- `response.input_hint` — best-effort line to show the user when `needs_input=true`.

### If the agent asks a question

If `response.status` is `needs_input` (or `response.needs_input=true`), the agent is blocked on an approval/prompt right now. Read `response.delta` first, then `response.content` if needed, and ask the user a direct follow-up question immediately.

### If the agent errors or exits

If `response.session_exited` is true, or if the response is missing:
- Tell the user the agent stopped
- Show the last output so they can see what happened
- Offer to restart with `agent run`

## When to Use

- User wants to start, manage, or interact with a coding agent (Claude, Codex, Aider, etc.)
- User wants to create an isolated workspace with a git worktree for a task
- User wants to monitor agent progress, send follow-up instructions, or stop agents
- User wants to run multiple agents in parallel on different tasks

## JSON Envelope

All `--json` commands return a structured envelope:

```json
{
  "ok": true,
  "data": { ... },
  "error": null,
  "meta": { "generated_at": "...", "amux_version": "..." },
  "schema_version": "amux.cli.v1"
}
```

On error: `ok` is `false`, `error` has `code`, `message`, and optional `details`.

**Always use `--json`** for programmatic access. Check `ok` field before accessing `data`.

## Workspace Management

### Create a workspace

```bash
amux --json workspace create <name> --project <path> [--assistant <name>]
```

Returns `data.id` (workspace id) and `data.root` (the worktree path). **Save the workspace id** — you need it for all agent commands. If `--assistant` is omitted, amux uses the configured default assistant.

The `root` path is the filesystem path to the workspace. Use it to read/write files directly.

### List workspaces

```bash
amux --json workspace list [--repo <path>]
```

(`--project` is accepted as a compatibility alias, but prefer `--repo`.)

### Remove a workspace

```bash
amux --json workspace remove <workspace_id>
```

## Agent Lifecycle

### Start an agent

```bash
amux --json agent run --workspace <workspace_id> --assistant claude [--prompt "..."] [--wait] [--wait-timeout 120s] [--idle-threshold 10s]
```

Returns `data.session_name` and `data.agent_id`. **Save both** — `session_name` is used for capture/watch, `agent_id` for send/stop.

With `--wait` (requires `--prompt`): blocks until the agent responds and goes idle, then returns the response in `data.response`. This is the preferred flow — one command to start, send a prompt, and get the result.

Supported assistants: `claude`, `codex`, `aider`, `goose`, `amp`, `cline`, `roo`, `gemini-cli`, `claude-cli`, `custom`.

### List running agents

```bash
amux --json agent list [--workspace <workspace_id>]
```

### Capture agent output (point-in-time snapshot)

```bash
amux --json agent capture <session_name> [--lines 50]
```

Returns `data.content` with the terminal output.

`agent capture --json` also includes chat-friendly signals:
- `data.status: "captured" | "session_exited"`
- `data.summary` and `data.latest_line` for concise updates
- `data.needs_input` and `data.input_hint` when prompts/questions are detected

### Send text to an agent

```bash
amux --json agent send --agent <agent_id> --text "your instructions" --enter [--wait] [--wait-timeout 120s] [--idle-threshold 10s]
```

Use `--enter` to simulate pressing Enter after the text. Use `--wait` to block until the agent responds and goes idle (returns response in `data.response`). Use `--async` for non-blocking send with job tracking. `--wait` and `--async` are mutually exclusive.

`agent send` JSON response includes:
- `sent` — whether the job status is completed.
- `delivered` — whether this invocation actually delivered text to tmux (false for replayed/already-completed jobs).

### Stop an agent

```bash
amux --json agent stop --agent <agent_id> --graceful
```

`--graceful` sends Ctrl-C first, waits for clean exit, then force-kills if needed.

## Waiting and Monitoring

### --wait flag (recommended)

The `--wait` flag on `agent run` and `agent send` is the simplest way to wait for an agent. It blocks until the agent responds and goes idle, then returns the captured output in `data.response`.

```bash
# Start and wait in one command
amux --json agent run --workspace <ws_id> --assistant claude --prompt "fix the bug" --wait --wait-timeout 300s --idle-threshold 10s

# Send and wait in one command
amux --json agent send --agent <agent_id> --text "add tests" --enter --wait --wait-timeout 300s --idle-threshold 10s
```

- `--wait-timeout 120s` — max time to wait (default 120s). Returns `response.timed_out: true` on timeout.
- `--idle-threshold 10s` — time of no output change to consider "idle" (default 10s).

### wait-for-idle.sh (legacy)

For cases where you need to wait separately from the send:

```bash
skills/amux/scripts/wait-for-idle.sh --session <session_name> [--timeout 300] [--idle-threshold 10]
```

### agent watch (NDJSON streaming)

For real-time monitoring. Emits events as they happen:

```bash
amux agent watch <session_name> [--lines 100] [--interval 500ms] [--idle-threshold 5s] [--heartbeat 10s]
```

**Event types:**

| Event | Meaning | Key Fields |
|---|---|---|
| `snapshot` | Initial full capture | `content`, `hash`, `latest_line`, `summary` |
| `delta` | New lines since last change | `new_lines`, `hash`, `latest_line`, `summary`, `needs_input`, `input_hint` |
| `idle` | No changes for `--idle-threshold` | `idle_seconds`, `hash`, `latest_line`, `summary`, `needs_input`, `input_hint` |
| `heartbeat` | Periodic keepalive while unchanged | `heartbeat_seconds`, `hash`, `latest_line`, `summary`, `needs_input`, `input_hint` |
| `exited` | Session no longer exists | (none) |

Set `--heartbeat 0` to disable heartbeat events.

### Channel-first DX loop (proactive updates)

For mobile/chat users, keep updates push-style so they never need to ask for status:

1. Send an immediate acknowledgement with the exact plan.
2. Start work and capture `session_name`/`agent_id`.
3. For long tasks, prefer repeated bounded `openclaw-step.sh` steps; if watching, stream `agent watch --heartbeat 10s` and post short updates from `summary`/`latest_line`.
4. If `needs_input=true`, ask a direct multiple-choice question immediately (`A/B/C` style).
5. End with a completion summary in both channel output and local output: changed files, tests run, pass/fail, one next action.

### poll-agent.sh (fallback)

If `agent watch` is unavailable:

```bash
skills/amux/scripts/poll-agent.sh --session <session_name> --timeout 120
```

### format-capture.sh

Strip ANSI escape codes from captured output for cleaner reading:

```bash
amux --json agent capture <session> --lines 80 | jq -r '.data.content' | skills/amux/scripts/format-capture.sh --strip-ansi --trim
```

## Async Jobs

For non-blocking send operations:

```bash
# Send asynchronously — returns a job_id immediately
amux --json agent send --agent <agent_id> --text "..." --enter --async

# Check job status
amux --json agent job status <job_id>

# Wait for completion (blocks until done)
amux --json agent job wait <job_id>

# Cancel a pending job
amux --json agent job cancel <job_id>
```

Use `--idempotency-key <key>` on any mutating command for safe retries (7-day retention).

## File Operations

Access workspace files directly via the filesystem path returned by `workspace create` or `workspace list`:

```bash
# Get workspace root path
root=$(amux --json workspace list | jq -r '.data[] | select(.workspace_id == "my-ws") | .root')

# Read/write files directly
cat "$root/src/main.ts"
echo "new content" > "$root/src/config.ts"
```

No special amux command is needed for file access.

## Multi-Agent Orchestration

Run multiple agents on different workspaces simultaneously:

```bash
# Create separate workspaces
amux --json workspace create frontend --project ~/app --assistant claude
amux --json workspace create backend --project ~/app --assistant claude

# Start agents in each
amux --json agent run --workspace ws-frontend --assistant claude --prompt "Add dark mode to React components"
amux --json agent run --workspace ws-backend --assistant claude --prompt "Add /api/theme endpoint"

# Monitor both (wait for each to finish, then summarize)
scripts/wait-for-idle.sh --session <frontend-session> --timeout 300 &
scripts/wait-for-idle.sh --session <backend-session> --timeout 300 &
wait
```

Each workspace gets its own git worktree branch, so agents don't conflict.

## Diagnostics

```bash
amux --json status        # Health check
amux --json doctor        # Full diagnostics
amux --json capabilities  # Machine-readable feature list
```

## Error Handling

Always check the `ok` field in JSON responses:

```bash
result=$(amux --json agent run --workspace bad-id --assistant claude 2>&1)
if echo "$result" | jq -e '.ok' > /dev/null 2>&1; then
  session=$(echo "$result" | jq -r '.data.session_name')
else
  error=$(echo "$result" | jq -r '.error.message')
  # Handle error — tell the user what went wrong
fi
```

Common error codes: `init_failed`, `not_found`, `usage_error`, `capture_failed`.

## Rules & Best Practices

1. **Always monitor and report back** — never fire-and-forget. Prefer `--wait` for bounded steps; use `agent watch --heartbeat` for long tasks, then summarize results to the user.
2. **Always use `--json`** for all amux commands when calling from scripts or agents
3. **Save `workspace_id`, `session_name`, and `agent_id`** from creation responses — you need them for subsequent commands
4. **Use `--graceful`** when stopping agents to allow clean shutdown
5. **Use `--idempotency-key`** on mutating commands when retries are possible
6. **Check `ok` field** in every JSON response before accessing `data`
7. **Use `--async`** for send operations when you don't need to block on delivery
8. **Access files via the workspace root path** — no special amux command needed
9. **One workspace per task** — create separate workspaces for independent work items
10. **Stop agents when done** — don't leave idle agents running
