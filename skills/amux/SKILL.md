---
name: amux
description: Orchestrate AI coding agents via amux with managed workspaces, git worktrees, and async job queues.
metadata:
  { "openclaw": { "emoji": "🔀", "os": ["darwin", "linux"], "requires": { "bins": ["amux", "tmux"] } } }
---

# amux Skill

Orchestrate AI coding agents using `amux` — a workspace and agent lifecycle manager built on tmux. All commands support `--json` for structured output.

## When to Use

- User wants to start, manage, or interact with a coding agent (Claude, Codex, Aider, etc.)
- User wants to create an isolated workspace with a git worktree for a task
- User wants to monitor agent progress, send follow-up instructions, or stop agents
- User wants to run multiple agents in parallel on different tasks

## Quick Start

```bash
# 1. Create a workspace (auto-creates git worktree)
amux --json workspace create my-feature --project ~/my-project

# 2. Start a coding agent in the workspace
amux --json agent run --workspace <workspace_id> --assistant claude --prompt "Add dark mode support"

# 3. Watch agent output (NDJSON stream)
amux agent watch <session_name> --lines 100

# 4. Send follow-up instructions
amux --json agent send --agent <agent_id> --text "Also add tests" --enter

# 5. Stop the agent gracefully
amux --json agent stop --agent <agent_id> --graceful
```

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
amux --json workspace create <name> --project <path>
```

Returns `data.workspace_id` and `data.root` (the worktree path). **Save the `workspace_id`** — you need it for all agent commands.

The `root` path is the filesystem path to the workspace. Use it to read/write files directly.

### List workspaces

```bash
amux --json workspace list [--project <path>]
```

### Remove a workspace

```bash
amux --json workspace remove <workspace_id>
```

## Agent Lifecycle

### Start an agent

```bash
amux --json agent run --workspace <workspace_id> --assistant claude [--prompt "..."]
```

Returns `data.session_name` and `data.agent_id`. **Save both** — `session_name` is used for capture/watch, `agent_id` for send/stop.

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

### Send text to an agent

```bash
amux --json agent send --agent <agent_id> --text "your instructions" --enter
```

Use `--enter` to simulate pressing Enter after the text. Use `--async` for non-blocking send with job tracking.

### Stop an agent

```bash
amux --json agent stop --agent <agent_id> --graceful
```

`--graceful` sends Ctrl-C first, waits for clean exit, then force-kills if needed.

## Watching Agent Output

### Stream output with `agent watch`

`agent watch` is a long-running command that emits NDJSON (one JSON object per line):

```bash
amux agent watch <session_name> [--lines 100] [--interval 500ms] [--idle-threshold 5s]
```

**Event types:**

| Event | Meaning | Key Fields |
|---|---|---|
| `snapshot` | Initial full capture | `content`, `hash` |
| `delta` | New lines since last change | `new_lines`, `hash` |
| `idle` | No changes for `--idle-threshold` | `idle_seconds`, `hash` |
| `exited` | Session no longer exists | (none) |

**Example output:**
```json
{"type":"snapshot","content":"$ claude\nClaude Code v1.0...","hash":"abc123","ts":"2026-02-14T10:00:00Z"}
{"type":"delta","new_lines":["Working on dark mode...","Created src/theme.ts"],"hash":"def456","ts":"2026-02-14T10:00:05Z"}
{"type":"idle","idle_seconds":10,"hash":"def456","ts":"2026-02-14T10:00:15Z"}
{"type":"exited","ts":"2026-02-14T10:01:00Z"}
```

**Usage pattern — run in background and monitor:**

```bash
# Start watching in a background process
amux agent watch <session> &

# Or use the helper script for polling without watch:
scripts/poll-agent.sh --session <session_name> --timeout 120
```

When an `idle` event arrives, the agent has likely finished its current task. When `exited` arrives, the session is gone.

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
amux --json workspace create frontend --project ~/app
amux --json workspace create backend --project ~/app

# Start agents in each
amux --json agent run --workspace ws-frontend --assistant claude --prompt "Add dark mode to React components"
amux --json agent run --workspace ws-backend --assistant claude --prompt "Add /api/theme endpoint"

# Monitor both
amux agent watch <frontend-session> &
amux agent watch <backend-session> &
```

Each workspace gets its own git worktree branch, so agents don't conflict.

## Error Handling

Always check the `ok` field in JSON responses:

```bash
result=$(amux --json agent run --workspace bad-id --assistant claude 2>&1)
if echo "$result" | jq -e '.ok' > /dev/null 2>&1; then
  session=$(echo "$result" | jq -r '.data.session_name')
else
  error=$(echo "$result" | jq -r '.error.message')
  # Handle error
fi
```

Common error codes: `init_failed`, `not_found`, `usage_error`, `capture_failed`.

## Diagnostics

```bash
amux --json status        # Health check
amux --json doctor        # Full diagnostics
amux --json capabilities  # Machine-readable feature list
```

## Rules & Best Practices

1. **Always use `--json`** for all amux commands when calling from scripts or agents
2. **Save `workspace_id`, `session_name`, and `agent_id`** from creation responses — you need them for subsequent commands
3. **Use `--graceful`** when stopping agents to allow clean shutdown
4. **Use `--idempotency-key`** on mutating commands when retries are possible
5. **Use `agent watch`** for continuous monitoring instead of polling `agent capture`
6. **Check `ok` field** in every JSON response before accessing `data`
7. **Use `--async`** for send operations when you don't need to block on delivery
8. **Access files via the workspace root path** — no special amux command needed
9. **One workspace per task** — create separate workspaces for independent work items
10. **Stop agents when done** — don't leave idle agents running
