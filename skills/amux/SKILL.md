---
name: amux
description: Orchestrate AI coding agents via amux with managed workspaces, git worktrees, and async job queues. Use for coding-agent tasks (review, implement, fix, refactor, tests) and when users mention workspace + amux + assistant (codex/claude/droid/etc).
metadata:
  { "assistant": { "emoji": "🔀", "os": ["darwin", "linux"], "requires": { "bins": ["amux", "tmux"] } } }
---

# amux Skill

Use this skill when a user asks for coding-agent work in a workspace and wants activity visible in amux TUI.

## Non-Negotiable Routing Rules

1. For coding-agent tasks, route through amux only.
2. If the user says `use amux` / `using amux`, this is a strict requirement.
3. Do not run direct `codex exec`, direct repo-local review scripts, or non-amux agent flows.
4. Do not auto-retry, auto-respawn, or silently open additional tabs.
5. On timeout/failure, report what happened and ask user confirmation before retrying.

## Canonical Entry Point

Preferred single-entry wrapper:

```bash
skills/amux/scripts/assistant-dx.sh
```

Use these subcommands:

```bash
# Start bounded task
skills/amux/scripts/assistant-dx.sh task start \
  --workspace <workspace_id> \
  --assistant <assistant> \
  --prompt "<task prompt>"

# Check task status
skills/amux/scripts/assistant-dx.sh task status \
  --workspace <workspace_id> \
  --assistant <assistant>

# Convenience aliases
skills/amux/scripts/assistant-dx.sh review --workspace <workspace_id> --assistant <assistant>
skills/amux/scripts/assistant-dx.sh start --workspace <workspace_id> --assistant <assistant> --prompt "<task prompt>"

# Continue active tab explicitly
skills/amux/scripts/assistant-dx.sh continue \
  --workspace <workspace_id> \
  --assistant <assistant> \
  --text "Continue and provide status + next action." \
  --enter
```

Notes:
- `assistant-dx.sh` is deterministic: one call maps to one explicit amux operation.
- `workflow ...` commands are removed.

## Workspace/Project Helpers

```bash
skills/amux/scripts/assistant-dx.sh project list [--query <text>]
skills/amux/scripts/assistant-dx.sh project add --path <abs_repo_path>
skills/amux/scripts/assistant-dx.sh workspace list --all
skills/amux/scripts/assistant-dx.sh workspace create --name <name> --project <abs_repo_path> --assistant <assistant>
skills/amux/scripts/assistant-dx.sh status [--workspace <workspace_id>] [--assistant <assistant>]
skills/amux/scripts/assistant-dx.sh alerts [--workspace <workspace_id>] [--assistant <assistant>]
skills/amux/scripts/assistant-dx.sh terminal run --workspace <workspace_id> --text "<cmd>" --enter
skills/amux/scripts/assistant-dx.sh terminal logs --workspace <workspace_id> --lines 120
skills/amux/scripts/assistant-dx.sh git ship --workspace <workspace_id> --message "<msg>" [--push]
```

## Required Interaction Pattern

For each user request:

1. Start one bounded task step (`task start`/`review` or `continue`).
2. Report immediate status to user (started/in-progress/needs_input/completed/failed).
3. If `needs_input`, ask a direct user question.
4. If `attention`/timeout/error, show summary + last useful context and ask whether to retry.
5. Only run another step after explicit user request or confirmation.

## Status Handling

Interpret `assistant-dx.sh` response fields:

- `status: ok` -> task completed or made progress.
- `status: needs_input` -> agent is blocked on input; ask the user.
- `status: attention` -> timed out / partial / session exited; summarize and ask if retry.
- `status: command_error` -> command/infra/runtime issue; provide exact error and one suggested next command.

Always include:
- `summary`
- `next_action`
- `suggested_command` (if present)

## Assistant Selection

- Respect explicit user assistant choice (`droid`, `codex`, `claude`, etc.).
- If the chosen assistant runtime is unavailable, say so plainly and ask for fallback assistant choice.
- Do not silently switch assistants.

## Binary Pinning

If multiple binaries exist, pin explicitly:

```bash
AMUX_BIN=/absolute/path/to/amux skills/amux/scripts/assistant-dx.sh status
```

## OpenClaw Sync

After amux changes, sync binary + skill for OpenClaw:

```bash
make openclaw-sync
```

This installs `/usr/local/bin/amux` and links both:
- `~/.openclaw/workspace/skills/amux`
- `~/.openclaw/workspace-dev/skills/amux`

to this repo’s `skills/amux`.
