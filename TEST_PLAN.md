# Amux Manual Test Plan & Documentation

This document serves as both a test plan and a reference for amux - a tool for
running AI coding agents in ephemeral Daytona sandboxes with persistent
credentials and CLI caches.

---

## Table of Contents

1. [Overview](#overview)
2. [Core Concepts](#core-concepts)
   - [Fresh Sandboxes Per Run](#fresh-sandboxes-per-run)
   - [Worktree ID](#worktree-id)
   - [Persistent Credentials & CLI Cache](#persistent-credentials--cli-cache)
   - [Worktree Runtime (TUI)](#worktree-runtime-tui)
   - [Config + Metadata Files](#config--metadata-files)
3. [User Stories + DX Decisions](#user-stories--dx-decisions)
4. [Daytona Setup & Authentication](#daytona-setup--authentication)
5. [Sandbox Lifecycle](#sandbox-lifecycle)
6. [Workspace Sync System](#workspace-sync-system)
7. [Coding Agents Reference](#coding-agents-reference)
8. [Test Procedures](#test-procedures)
9. [Troubleshooting](#troubleshooting)

---

## Overview

### What is Amux?

Amux runs AI coding agents in disposable Daytona sandboxes. Each run:

1. Creates a new Daytona sandbox
2. Syncs your local workspace to the sandbox
3. Mounts a persistent volume for credentials and CLI downloads
4. Runs the agent interactively via SSH or exec
5. Deletes the sandbox on exit (unless `--keep`)

### Why Fresh Sandboxes?

- **Clean environments**: no drift across runs
- **Easy cleanup**: sandboxes are deleted by default
- **Persistent creds**: you still login once, thanks to a persistent volume
- **Multi-agent**: run different agents without local installs

---

## Core Concepts

### Fresh Sandboxes Per Run

Amux creates a **new sandbox per run**. Use `--keep` only when you want to leave
one running for debugging or reuse.

### Worktree ID

**Worktree ID** (`amux.worktreeId`):
- Computed from: SHA256 hash of absolute working directory path (first 16 hex chars)
- Purpose: isolates workspace files per directory
- Used for: workspace path in sandbox (`~/.amux/workspaces/{worktreeId}/repo/`)

**Example**:
```
/home/user/project-a        -> worktreeId: x1y2z3w4v5u6t7s8
/home/user/project-b        -> worktreeId: p9o8i7u6y5t4r3e2

# Inside the sandbox:
~/.amux/workspaces/x1y2z3w4v5u6t7s8/repo/  -> project-a files
~/.amux/workspaces/p9o8i7u6y5t4r3e2/repo/  -> project-b files
```

### Persistent Credentials & CLI Cache

Credentials and CLI installs are stored on a persistent volume (default
`amux-persist`) mounted at `/amux`, then symlinked into the sandbox home
directory.

Persisted paths include:
- `~/.claude/`, `~/.codex/`, `~/.gemini/`, `~/.factory/`
- `~/.config/`, `~/.local/`, `~/.npm/`, `~/.npmrc`

This means:
- You authenticate once
- CLI downloads stay cached
- New sandboxes start fast

Resetting persistence is done by **rotating to a new volume** (see
`amux sandbox reset`). Old volumes are kept in Daytona for manual cleanup.

### Worktree Runtime (TUI)

Each worktree can run in **Local** or **Sandbox** mode:
- **Local**: agents + terminal run on your machine.
- **Sandbox**: agents + terminal run in a shared Daytona sandbox for that worktree.

Runtime selection is stored in the per-worktree metadata file and drives
which environment the TUI uses.

### Config + Metadata Files

**Local config file**: `~/.amux/config.json`
```json
{
  "daytonaApiKey": "...",
  "daytonaApiUrl": "https://api.daytona.io",
  "defaultSnapshotName": "amux-agents",
  "persistenceVolumeName": "amux-persist",
  "settingsSync": {
    "enabled": false,
    "claude": true,
    "codex": true
  }
}
```

**Global sandbox metadata**: `~/.amux/sandbox.json`
```json
{
  "sandboxes": {
    "worktree-id-here": {
      "sandboxId": "sandbox-uuid-here",
      "agent": "claude",
      "provider": "daytona",
      "createdAt": "2026-01-13T10:30:00Z",
      "worktreeId": "...",
      "project": "my-repo"
    }
  }
}
```

**Worktree metadata**: `~/.amux/worktrees-metadata/<worktreeId>/worktree.json`
```json
{
  "name": "feature-branch",
  "branch": "feature-branch",
  "runtime": "sandbox",
  "assistant": "claude"
}
```

---

## User Stories + DX Decisions

### 1) First-time setup
**Story:** New user wants to run Claude in a sandbox with minimal friction.
**Flow:**
- `amux setup` prompts for API key and optionally creates a snapshot.
- `amux sandbox run claude` starts a new sandbox and handles login once.
**DX Decision:** Login is only required on first use; credentials persist via
`/amux` volume.

### 2) Daily workflow (targeted repo change)
**Story:** Developer runs one short task and exits.
**Flow:**
- `amux sandbox run <agent>` creates a fresh sandbox each time.
- Workspace sync happens automatically.
- Sandbox is deleted on exit.
**DX Decision:** New sandbox per run is the default to keep environments clean.

### 3) Quick agent switch
**Story:** Developer uses multiple agents in the same repo.
**Flow:**
- `amux sandbox run codex`, then `amux sandbox run claude`.
- Shared persistence keeps CLIs installed and credentials available.
**DX Decision:** Persistence is shared across agents so switching is instant.

### 4) Parallel sandboxes across repos
**Story:** Developer jumps between multiple repos quickly.
**Flow:**
- Each repo spins a new sandbox per run.
- Worktree ID isolates file paths inside each sandbox.
**DX Decision:** Isolation is handled per run; no shared sandbox state.

### 5) Long-running task
**Story:** Developer wants to keep a sandbox alive.
**Flow:**
- `amux sandbox run <agent> --keep`
- `amux status` shows the sandbox and `amux ssh` attaches.
**DX Decision:** `--keep` enables longer sessions, but is opt-in.

### 6) Preview URL + logs (web apps)
**Story:** Developer runs a Next.js dev server and wants a browser URL + logs.
**Flow:**
- `amux sandbox run claude --preview 3000 --record`
- Inside the agent, run `npm run dev` (listening on `0.0.0.0:3000`).
- `amux sandbox logs -f` tails the latest recorded session log.
**DX Decision:** Preview URLs are one flag away, and log recording is opt-in but
persistent so you can tail from another terminal (even after exit via a
short-lived log reader sandbox).

### 7) Credential reset or clean slate
**Story:** Developer wants to clear all agent logins and cached CLIs.
**Flow:**
- `amux sandbox reset` rotates to a new persistence volume.
- New sandboxes start clean; old volume remains in Daytona.
**DX Decision:** Reset is fast and safe (no destructive delete), while still
allowing a full wipe by deleting old volumes in Daytona UI.

### 8) Settings sync
**Story:** Developer wants local preferences inside the sandbox.
**Flow:**
- `amux settings sync --enable --claude --git`
- `amux sandbox run claude --sync-settings`
**DX Decision:** Settings sync is explicit and opt-in to avoid surprises.

### 9) TUI integration (worktree-level runtime)
**Story:** Developer wants to choose "Sandbox" in the TUI and run agents.
**Flow:**
- Worktree runtime is set to **Sandbox** or **Local** (worktree-level).
- When Sandbox is selected, the TUI creates or attaches to a single sandbox
  for that worktree.
- All agent tabs and the bottom-right terminal share the same sandbox.
- Persistence volume is mounted, so agents are ready instantly.
**DX Decision:** Keep CLI semantics simple so the TUI can reuse them directly,
while sharing a single sandbox per worktree in the TUI.

### 10) TUI-only users (current)
**Story:** Developer never uses the CLI and configures everything in TUI.
**Flow:**
- Switch worktree runtime to **Sandbox** in TUI.
- Complete setup wizard (Daytona API key).
- Open multiple agent tabs (e.g., Claude + Codex) in the same sandbox.
- Use the shared sandbox terminal for shell commands.
- Switch worktree back to Local to sync changes down.
**DX Decision:** TUI writes to the same config as CLI, so setup is one-time.

### 11) CLI-only users (current)
**Story:** Developer never uses the TUI and relies entirely on CLI.
**Flow:**
- `amux setup` or `amux auth login`
- `amux sandbox run <agent>` for per-run sandboxes
- `amux sandbox update`, `amux sandbox reset`, `amux sandbox logs`
**DX Decision:** CLI remains the source of truth for automation and scripts.

### 12) TUI + CLI users (current)
**Story:** Developer uses CLI for setup or automation and TUI for daily runs.
**Flow:**
- CLI creates/updates `~/.amux/config.json`.
- TUI reads the same config and uses Sandbox mode without re-setup.
- CLI commands (e.g., `amux sandbox logs`) can inspect the same sandboxes.
**DX Decision:** One shared config; both front-ends stay in sync.

---

## Daytona Setup & Authentication

### First-Time Setup

#### Step 1: Get Your API Key

1. Go to your Daytona dashboard
2. Navigate to Settings > API Keys
3. Create a new API key
4. Copy the key (you won't see it again)

#### Step 2: Configure Amux

**Option A: Interactive Setup (Recommended)**
```bash
./amux setup
```

**Option B: Environment Variable**
```bash
export DAYTONA_API_KEY=your-api-key-here
```

**Option C: Direct Login**
```bash
./amux auth login
# Enter your Daytona API key when prompted
```

**Option D: TUI Setup**
- Open the TUI.
- Activate a worktree and set runtime to **Sandbox**.
- Enter your Daytona API key when prompted.

#### Step 3: Verify Setup

```bash
./amux doctor
```

Expected output:
```
Running diagnostics...
OK: All checks passed
```

---

## Sandbox Lifecycle

### Create + Run (default)

```bash
amux sandbox run claude
```

- Creates a new sandbox
- Syncs your workspace
- Runs Claude
- Deletes the sandbox on exit

### Keep a Sandbox

```bash
amux sandbox run claude --keep
```

- Keeps the sandbox after the session exits
- Useful for debugging or long-running tasks

### Status / SSH / Exec

```bash
amux status
amux ssh
amux exec -- ls -la
```

### List / Remove

```bash
amux sandbox ls
amux sandbox rm [sandbox-id]
```

---

## Workspace Sync System

Amux syncs your local workspace to the sandbox so agents can access and modify
files.

**Sync Methods:**
1. **Full Sync** (default first time)
   - Creates a tarball of your workspace
   - Uploads and extracts in the sandbox
   - Respects `.amuxignore` patterns

2. **Incremental Sync** (subsequent runs)
   - Computes file hashes and timestamps
   - Only transfers changed files
   - Much faster for large repos

**Skip sync:**
```bash
amux sandbox run claude --no-sync
```

---

## Coding Agents Reference

This section is the per-agent walkthrough and test checklist.

### Common amux behavior (all agents)
- `amux sandbox run <agent>` installs the CLI if missing, then runs it.
- Credentials and CLI caches persist via the `/amux` volume (see persistence section).
- Pass API keys into the sandbox with `--env KEY=...` (host env vars are not forwarded).
- Auto-login runs only when: credentials mode is not `none` **and** no agent args
  are passed after `--`.
- `amux sandbox update <agent>` forces a reinstall/update in the sandbox.

### Claude Code (Anthropic)
**Install (amux):** `curl -fsSL https://claude.ai/install.sh | bash`

**First-time auth options:**
- `amux sandbox run claude`, then complete the CLI login flow.
- API key auth: `--env ANTHROPIC_API_KEY=...`, `--env ANTHROPIC_AUTH_TOKEN=...`,
  or `--env CLAUDE_API_KEY=...`.
 - You can re-authenticate inside Claude Code with `/login`.

**amux behavior:**
- No auto-login command is run; Claude prompts interactively.
- Credential detection file: `~/.claude/.credentials.json` (persisted).

**Updates:**
- Claude auto-updates on startup.
- amux only installs if missing; `amux sandbox update claude` forces reinstall.
 - Manual update inside sandbox: `claude update`.

**Test checklist:**
- First run prompts login; second run does not.
- `--env ANTHROPIC_API_KEY=...` (or token) skips login.
- `amux sandbox update claude` reinstalls.

### Codex CLI (OpenAI)
**Install (amux):** `npm install -g @openai/codex@latest`

**First-time auth options:**
- amux auto-runs `codex login` when no `OPENAI_API_KEY` is passed via `--env`.
- Device auth is the default; set `AMUX_CODEX_DEVICE_AUTH=0` to disable.
- API key auth: `--env OPENAI_API_KEY=...` (stored in `~/.codex/auth.json`).
 - Codex also supports ChatGPT account login on first run.

**amux behavior:**
- Codex TUI2 is auto-enabled unless `AMUX_CODEX_TUI2=0` or you already pass TUI2 flags.
- Credential detection file: `~/.codex/auth.json` (persisted).

**Updates:**
- Codex does not auto-update; amux re-checks roughly every 24 hours.
- `--update` or `amux sandbox update codex` forces reinstall immediately.
 - Manual update inside sandbox: `npm i -g @openai/codex@latest` (or `codex --upgrade`).

**Test checklist:**
- Auto-login runs on first launch without API key.
- `--env OPENAI_API_KEY=...` skips login.
- `AMUX_CODEX_DEVICE_AUTH=0` disables device auth.
- `AMUX_CODEX_TUI2=0` disables auto TUI2 flags.
- Update path triggers npm reinstall.

### OpenCode (Open Source)
**Install (amux):** `curl -fsSL https://opencode.ai/install | bash`

**First-time auth options:**
- amux auto-runs `opencode auth login` when no credentials are present.
- OpenCode reads providers from env or `.env`.

**amux behavior:**
- Credential detection file: `~/.local/share/opencode/auth.json` (persisted).
- amux does not infer auth from env vars; to skip login, use `--credentials none`.

**Updates:**
- amux only installs if missing; `amux sandbox update opencode` forces reinstall.

**Test checklist:**
- Auto-login runs on first launch.
- `--credentials none` skips auto-login even without creds.
- Persisted auth prevents re-login.
- Update path forces reinstall.

### Amp (Sourcegraph)
**Install (amux):** `curl -fsSL https://ampcode.com/install.sh | bash`

**First-time auth options:**
- amux auto-runs `amp login` when no credentials are present (and no `AMP_API_KEY` passed).
- API key auth: `--env AMP_API_KEY=...` (token from ampcode.com/settings).
 - Running `amp` directly also prompts for login on first run.

**amux behavior:**
- Credential detection file: `~/.config/amp/secrets.json` (persisted).
- If `AMP_API_KEY` is set, auto-login is skipped.

**Updates:**
- amux does not reinstall on every run.
- `amux sandbox update amp` forces reinstall.

**Test checklist:**
- Auto-login runs on first launch.
- `--env AMP_API_KEY=...` skips login.
- Update path forces reinstall.

### Gemini CLI (Google)
**Install (amux):** `npm install -g @google/gemini-cli@latest`

**First-time auth options:**
- Run `amux sandbox run gemini` and complete the CLI sign-in flow.
- API key auth: `--env GEMINI_API_KEY=...` or `--env GOOGLE_API_KEY=...`.
- Vertex AI auth: set `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION`, plus one of:
  - ADC (`gcloud auth application-default login`)
  - Service account JSON via `GOOGLE_APPLICATION_CREDENTIALS=...`
  - Google Cloud API key via `GOOGLE_API_KEY=...`

**amux behavior:**
- No auto-login command is run; Gemini prompts interactively.
- Credential detection file: `~/.gemini/oauth_creds.json` (persisted).

**Updates:**
- amux only installs if missing; `amux sandbox update gemini` forces reinstall.

**Test checklist:**
- First run prompts sign-in; second run does not.
- `--env GEMINI_API_KEY=...` (or `GOOGLE_API_KEY`) skips login.
- Update path forces reinstall.

### Droid (Factory)
**Install (amux):** `curl -fsSL https://app.factory.ai/cli | sh`

**First-time auth options:**
- Run `amux sandbox run droid` and complete onboarding if prompted.
- The CLI supports `/login` for interactive login.
- API key auth: `--env FACTORY_API_KEY=...` for headless usage.

**amux behavior:**
- No auto-login command is run.
- Credential detection file: `~/.factory/config.json` (persisted).

**Updates:**
- amux only installs if missing; `amux sandbox update droid` forces reinstall.

**Test checklist:**
- First run prompts onboarding; second run does not.
- `--env FACTORY_API_KEY=...` skips login.
- Update path forces reinstall.

### Shell
Built-in bash shell (no auth, no updates).

---

## Test Procedures

### 1. Smoke Test (Fresh Run)

```bash
amux sandbox run shell
```

- Expect sandbox creation spinner
- Expect workspace sync
- Expect shell prompt in sandbox
- Exit and confirm sandbox deletion

### 2. Credential Persistence

1. Run Claude and login once:
   ```bash
   amux sandbox run claude
   ```
2. Exit.
3. Run again:
   ```bash
   amux sandbox run claude
   ```
4. Expect **no login prompt** (credentials persisted via volume).

### 3. CLI Cache Persistence

1. Run Codex once:
   ```bash
   amux sandbox run codex
   ```
2. Exit.
3. Run Codex again:
   ```bash
   amux sandbox run codex
   ```
4. Expect no lengthy reinstall (CLI should be cached in `/amux`).

### 4. `--keep` Workflow

```bash
amux sandbox run shell --keep
amux status
amux ssh
amux sandbox rm --project
```

- Status should show a running sandbox
- SSH should connect
- rm should delete sandbox + metadata

### 5. Workspace Sync Round-Trip

1. Run:
   ```bash
   amux sandbox run shell
   ```
2. Modify a file inside the sandbox.
3. Exit.
4. Verify changes were synced back locally.

### 6. Settings Sync

```bash
amux settings sync --enable --claude --git
amux sandbox run claude --sync-settings
```

- Confirm settings files are copied into sandbox
- Ensure secrets are filtered

### 7. Preview + Logs

```bash
amux sandbox run claude --preview 3000 --record
# In a second terminal:
amux sandbox logs -f
```

- Preview URL should print (and open if not suppressed)
- Logs should stream once the server starts
- After exit, `amux sandbox logs` should still work (spins a log reader sandbox)

### 8. Snapshot (Optional)

```bash
amux snapshot create --agents claude,codex
amux sandbox run claude --snapshot amux-agents
```

- Expect faster startup
- Verify agents are preinstalled

### 9. Reset Persistence

```bash
amux sandbox reset
amux sandbox run claude
```

- Expect a fresh login prompt
- New sandboxes should use the new volume

### 10. CLI-only Users (Current)

```bash
amux setup
amux sandbox run claude
amux sandbox update codex
amux sandbox logs -f
```

- CLI should fully configure and run sandboxes without any TUI usage
- All features should work without TUI state

### 11. TUI-only Users (Current)

TUI flow:
1. Switch worktree runtime to **Sandbox**.
2. Complete setup wizard (Daytona API key).
3. Open multiple agent tabs in the same worktree.
4. Use the shared sandbox terminal for shell commands.
5. Switch worktree back to Local to sync changes down.

- All sandbox config is set in TUI
- All agent tabs share the same sandbox
- Terminal reflects sandbox filesystem

### 12. TUI + CLI Users (Current)

Hybrid flow:
1. Run `amux setup` in CLI.
2. Open TUI and set worktree runtime to **Sandbox**.
3. Create multiple agent tabs (shared sandbox).
4. Use CLI to inspect the same sandbox (`amux status`, `amux sandbox logs`).

- TUI and CLI read/write the same `~/.amux/config.json`
- No duplicate setup required

---

## Troubleshooting

- **API key missing**: run `amux auth login` or set `DAYTONA_API_KEY`
- **SSH not found**: install OpenSSH client locally
- **Sandbox not found**: run `amux sandbox run <agent>` to create one
- **Credentials missing after run**: check `/amux` mount and symlinks
- **Sync issues**: try `--no-sync` and inspect `.amuxignore`
