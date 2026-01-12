# Amux Manual Test Plan & Documentation

This document serves as both a test plan and comprehensive documentation for amux - a tool for running AI coding agents in disposable cloud computers.

---

## Table of Contents

1. [Overview](#overview)
2. [Core Concepts](#core-concepts)
3. [Provider Setup & Authentication](#provider-setup--authentication)
   - [Daytona](#daytona-provider)
   - [Sprites](#sprites-provider)
   - [Docker](#docker-provider)
4. [Credentials System Deep Dive](#credentials-system-deep-dive)
5. [Computer Lifecycle](#computer-lifecycle)
6. [Workspace Sync System](#workspace-sync-system)
7. [Coding Agents Reference](#coding-agents-reference)
   - [Claude](#claude-claude-code)
   - [Codex](#codex-openai-codex-cli)
   - [OpenCode](#opencode)
   - [Amp](#amp-sourcegraph-amp)
   - [Gemini](#gemini-google-gemini-cli)
   - [Droid](#droid-factory-droid)
   - [Shell](#shell-interactive-bash)
8. [Test Procedures](#test-procedures)
9. [Troubleshooting](#troubleshooting)

---

## Overview

### What is Amux?

Amux provides ONE shared cloud computer for running AI coding agents. Instead of running agents locally, amux:

1. Creates a remote computer (via Daytona, Sprites, or Docker)
2. Syncs your local workspace to the remote computer
3. Sets up credential directories in the computer's home directory
4. Runs the agent interactively via SSH or exec
5. Preserves credentials as long as the computer exists

### Why a Shared Computer?

- **Simplicity**: One computer for all your projects
- **Workspace isolation**: Each project gets its own workspace directory (via worktreeID)
- **Reproducibility**: Start fresh anytime with `--recreate`
- **Security**: Agents run in sandboxed environments
- **Persistence**: Credentials persist with the computer
- **Multi-agent**: Run different agents without local installation conflicts

---

## Core Concepts

### One Shared Computer

Amux uses ONE shared computer for all your projects. This is simpler than per-project computers:

- **Computer name**: `amux` (fixed name)
- **One per provider**: One shared computer per provider (Daytona, Sprites, or Docker)
- **Workspace isolation**: Each project gets its own workspace via worktreeID

### Worktree ID

**Worktree ID** (`amux.worktreeId`):
- Computed from: SHA256 hash of absolute working directory path (first 16 hex chars)
- Purpose: Isolates workspace files per directory
- Stability: Different for each project/worktree location
- Used for: Workspace path in sandbox (`~/.amux/workspaces/{worktreeId}/repo/`)

**Example**:
```
# Different projects get different workspace directories inside the shared computer
/home/user/project-a        → worktreeId: x1y2z3w4v5u6t7s8
/home/user/project-b        → worktreeId: p9o8i7u6y5t4r3e2

# Inside the shared computer:
~/.amux/workspaces/x1y2z3w4v5u6t7s8/repo/  → project-a files
~/.amux/workspaces/p9o8i7u6y5t4r3e2/repo/  → project-b files
```

### Credentials Storage

Credentials are stored directly on the computer's home directory (not in a separate volume):
- `~/.claude/` for Claude
- `~/.codex/` for Codex
- `~/.config/gh/` for GitHub CLI
- etc.

Credentials persist as long as the shared computer exists. If you delete the computer, you'll need to re-authenticate.

### Configuration Storage

**Local config file**: `~/.amux/config.json`
```json
{
  "daytonaApiKey": "...",
  "daytonaApiUrl": "https://api.daytona.io",
  "spritesToken": "...",
  "spritesApiUrl": "https://api.sprites.dev",
  "defaultSnapshotName": "amux-agents",
  "settingsSync": {
    "enabled": false,
    "claude": true,
    "codex": true
  }
}
```

**Note**: Agent API keys (Anthropic, OpenAI, etc.) are NOT stored in config. Agents authenticate interactively inside the sandbox via OAuth/browser login on first run. This is more secure and matches how the native CLIs work.

**Global computer metadata**: `~/.amux/computer.json`
```json
{
  "computers": {
    "daytona": {
      "computerId": "sandbox-uuid-here",
      "configHash": "cfg123hash456",
      "agent": "claude",
      "provider": "daytona",
      "createdAt": "2024-01-15T10:30:00Z"
    }
  },
  "provider": "daytona"
}
```

Note: Computer metadata is stored globally (not per-project) since there's one shared computer.

---

## Provider Setup & Authentication

### Choosing a Provider

| Feature | Daytona | Sprites | Docker |
|---------|---------|---------|--------|
| **Location** | Cloud | Cloud | Local |
| **SSH Access** | Yes | No | No |
| **Preview URLs** | Yes | Yes | No |
| **Desktop/VNC** | Yes | No | No |
| **Snapshots** | Yes | No | No |
| **Auto-stop** | Yes | No | No |
| **Checkpoints** | No | Yes | No |
| **Network Policy** | No | Yes | No |
| **Offline Use** | No | No | Yes |
| **Best For** | Full experience | Ephemeral tasks | Local dev/testing |

**Credentials behavior**: All providers store credentials directly on the computer's filesystem. Credentials persist as long as the computer exists.

---

## Daytona Provider

Daytona is the recommended provider for the full amux experience. It provides cloud-based disposable computers with persistent volumes, SSH access, preview URLs, and desktop support.

### First-Time Setup

#### Step 1: Get Your API Key

1. Go to your Daytona dashboard
2. Navigate to Settings → API Keys
3. Create a new API key
4. Copy the key (you won't see it again)

#### Step 2: Configure Amux

**Option A: Interactive Setup (Recommended)**
```bash
./amux setup
```

This wizard will:
1. Prompt for your Daytona API key (provider authentication)
2. Optionally build a snapshot with pre-installed agents

**Note**: Agent authentication (Claude, Codex, etc.) happens inside the computer on first run via OAuth/browser login—not during setup.

**Option B: Environment Variable**
```bash
export DAYTONA_API_KEY=your-api-key-here
export AMUX_PROVIDER=daytona
```

**Option C: Direct Login**
```bash
./amux auth login
# Enter your Daytona API key when prompted
```

#### Step 3: Verify Setup

```bash
./amux doctor
```

Expected output:
```
Running diagnostics...

✓ Daytona API key configured
✓ Daytona API accessible
✓ Default snapshot available

✓ All checks passed
```

### Second Time Onwards

Once configured, amux reads credentials from `~/.amux/config.json` automatically. Just run:

```bash
./amux claude  # or any agent
```

No prompts, no setup needed.

### GitHub Authentication (Optional)

To enable `git push` from within the computer:

```bash
# First, create a computer if you haven't already
./amux computer run claude

# Then run GitHub auth (requires existing computer)
./amux auth login gh
```

This:
1. Connects to your existing shared computer
2. Runs GitHub's device code authentication flow
3. Stores the GitHub token on the computer's filesystem
4. Git push works in all your projects using this computer

**Test it:**
```bash
./amux claude
# Inside computer:
gh auth status
git push origin main  # Should work!
```

### Daytona-Specific Features

**Preview URLs** (expose ports publicly):
```bash
# Start a dev server on port 3000 inside sandbox, then:
./amux computer preview 3000
# Opens browser to https://your-sandbox.daytona.app
```

**Desktop/VNC** (for browser-based agents):
```bash
./amux computer desktop
# Opens browser to VNC session
```

**Snapshots** (pre-built images):
```bash
# List available snapshots
./amux snapshot ls

# Use a specific snapshot
./amux claude --snapshot my-custom-snapshot
```

**Auto-stop** (save resources):
```bash
# Stop after 30 minutes idle (default)
./amux claude --auto-stop 30

# Disable auto-stop
./amux claude --auto-stop 0
```

### Daytona Test Procedure

```bash
# Prerequisites
export DAYTONA_API_KEY=your-key
export AMUX_PROVIDER=daytona
go build -o amux ./cmd/amux

# 1. Setup and diagnostics
./amux setup --skip-snapshot  # Skip snapshot build for faster testing
./amux doctor

# 2. First run (creates computer, installs agent)
./amux claude
# Wait for install, then exit with Ctrl+D

# 3. Cached run (should be fast)
./amux claude
# Exit

# 4. Force update
./amux claude --update
# Exit

# 5. Workspace sync
echo "test-file" > test-sync.txt
./amux claude
# Inside: cat test-sync.txt  (should show "test-file")
# Exit
rm test-sync.txt

# 6. SSH access
./amux ssh
# Exit

# 7. Exec command
./amux exec "uname -a"
./amux exec "pwd"

# 8. Preview URL
./amux computer preview 8080 --no-open

# 9. Status
./amux status

# 10. GitHub auth (optional)
./amux auth login gh
./amux ssh
# Inside: gh auth status

# 11. Cleanup
./amux computer rm --project
./amux computer ls  # Should be empty
```

---

## Sprites Provider

Sprites provides ephemeral cloud computers. Credentials are stored on the computer's filesystem (same as Daytona and Docker).

### First-Time Setup

#### Step 1: Get Your Token

1. Go to sprites.dev dashboard
2. Navigate to Settings → API Tokens
3. Create a new token
4. Copy the token

#### Step 2: Configure Amux

**Option A: Environment Variable**
```bash
export SPRITES_TOKEN=your-token-here
export AMUX_PROVIDER=sprites
```

**Option B: Interactive Login**
```bash
./amux auth login sprites
# Enter your Sprites token when prompted
```

#### Step 3: Verify

```bash
./amux doctor
```

### Second Time Onwards

```bash
export AMUX_PROVIDER=sprites
./amux claude
```

Credentials are read from `~/.amux/config.json`.

### Credential Persistence

All providers (including Sprites) store credentials directly on the computer's filesystem:

- While the computer exists, credentials persist (you can run agents multiple times)
- When the computer is **deleted**, credentials are lost and you must re-authenticate
- This is the same behavior across Daytona, Sprites, and Docker

**Alternative**: To skip OAuth entirely, pass API keys via environment variables:
```bash
./amux claude -e ANTHROPIC_API_KEY=sk-ant-xxx
./amux codex -e OPENAI_API_KEY=sk-xxx
./amux gemini -e GEMINI_API_KEY=xxx
```

### Sprites-Specific Features

**Checkpoints** (save/restore state):
```bash
# Create checkpoint
./amux checkpoint create "before-refactor"

# List checkpoints
./amux checkpoint ls

# Restore checkpoint
./amux checkpoint restore cp-abc123
```

**Network Policy** (control outbound access):
```bash
# View current policy
./amux network-policy get

# Set policy
./amux network-policy set --allow github.com --allow api.anthropic.com
```

**Exec Sessions** (list running processes):
```bash
./amux exec-sessions ls
./amux exec-sessions attach session-id
```

### Sprites Test Procedure

```bash
# Prerequisites
export SPRITES_TOKEN=your-token
export AMUX_PROVIDER=sprites
go build -o amux ./cmd/amux

# 1. First run
./amux claude
# Note: May need to authenticate Claude inside sandbox
# Exit

# 2. Workspace sync
echo "test" > test.txt
./amux claude
# Inside: cat test.txt
# Exit
rm test.txt

# 3. Preview URL
./amux computer preview 3000 --no-open

# 4. Exec
./amux exec "ls -la"

# 5. Status
./amux status

# 6. Cleanup
./amux computer rm --project
```

---

## Docker Provider

Docker runs computers locally on your machine. Best for offline development or testing.

### First-Time Setup

#### Step 1: Install Docker

```bash
# macOS
brew install docker

# Linux
curl -fsSL https://get.docker.com | sh

# Verify
docker version
```

That's it. No API keys needed.

#### Step 2: Use Docker Provider

```bash
export AMUX_PROVIDER=docker
./amux claude
```

### How Docker Provider Works

1. Creates a Docker container named `amux`
2. Runs `sleep infinity` to keep container alive
3. Executes commands via `docker exec`

### Credential Persistence

Docker stores credentials directly on the container's filesystem (in home directory). Credentials persist as long as the container exists. If you delete the container, you'll need to re-authenticate.

### Docker Limitations

- **No preview URLs**: Can't expose ports publicly
- **No SSH**: Uses `docker exec` instead
- **No desktop/VNC**: Terminal only
- **No snapshots**: Uses Docker images directly
- **No auto-stop**: Container runs until stopped

### Docker Test Procedure

```bash
# Prerequisites
docker version  # Verify Docker works
export AMUX_PROVIDER=docker
go build -o amux ./cmd/amux

# 1. First run
./amux claude
# Exit

# 2. Cached run
./amux claude
# Exit

# 3. Workspace sync
echo "test" > test.txt
./amux claude
# Inside: cat test.txt
# Exit
rm test.txt

# 4. Exec
./amux exec "uname -a"

# 5. Status
./amux status

# 6. List
./amux computer ls

# 7. Cleanup
./amux computer rm --project

# Verify container removed
docker ps -a | grep amux
```

---

## Credentials System Deep Dive

### How Credentials Work

AI coding agents authenticate via OAuth/browser login on first run—just like using them locally. Credentials are stored directly on the computer's home directory.

**How agent authentication works:**
1. First run: Agent prompts for login (opens browser for OAuth)
2. You complete authentication in your browser
3. Agent stores OAuth token in its config directory (e.g., `~/.claude/`)
4. Credentials persist on the computer's filesystem
5. Future runs: Already authenticated, no prompts

**Optional API key alternative:** Some agents let you skip OAuth by providing an API key. You can either:
- Pass it via environment variable: `./amux claude -e ANTHROPIC_API_KEY=sk-ant-xxx`
- Or configure it inside the agent on first login when prompted

Most users should use the default OAuth flow—it's simpler and more secure.

### Credentials Storage Structure

Credentials are stored directly in the computer's home directory:

```
~/ (computer home directory)
├── .claude/               # Claude Code credentials
│   ├── .credentials.json
│   └── settings.json
├── .codex/                # OpenAI Codex credentials
│   └── auth.json
├── .config/codex/         # Codex configuration
│   └── config.toml
├── .local/share/opencode/ # OpenCode credentials
│   └── auth.json
├── .config/amp/           # Sourcegraph Amp config
├── .local/share/amp/      # Amp data
├── .gemini/               # Google Gemini credentials
│   └── oauth_creds.json
├── .factory/              # Factory Droid credentials
│   └── config.json
├── .config/gh/            # GitHub CLI credentials
│   └── hosts.yml
└── .gitconfig             # Git configuration
```

### Credential Persistence

All providers store credentials directly on the computer's filesystem:

| Provider | Credentials Location | Persistence |
|----------|---------------------|-------------|
| Daytona | Computer home dir (`~/`) | As long as computer exists |
| Docker | Container home dir (`~/`) | As long as container exists |
| Sprites | Computer home dir (`~/`) | As long as computer exists |

**Key point**: Credentials persist as long as the shared computer exists. If you delete the computer with `amux computer rm`, you'll need to re-authenticate.

### Credentials Mode

Control credentials behavior with `--credentials`:

```bash
# Auto (default): Set up credential directories for agents, skip for shell
./amux claude --credentials auto

# Computer: Always set up credential directories
./amux shell --credentials computer

# None: Skip credential directory setup
./amux claude --credentials none
```

### Settings Sync (Opt-in)

Sync your local agent settings to the computer:

```bash
# Enable settings sync
./amux settings sync enable

# Check status
./amux settings sync status

# Run agent (settings auto-synced)
./amux claude
```

**What gets synced**:
- `~/.claude/settings.json` → Claude preferences
- `~/.config/codex/config.toml` → Codex configuration
- `~/.gitconfig` → Git user.name, user.email, aliases (safe keys only)

**What doesn't sync**:
- API keys (security)
- OAuth tokens (security)
- Credential files (security)

---

## Computer Lifecycle

### States

Computers have four states:

| State | Description | Can Run Commands? |
|-------|-------------|-------------------|
| `pending` | Being created | No |
| `started` | Running and ready | Yes |
| `stopped` | Paused (Daytona) or exited (Docker) | No (must start first) |
| `error` | Failed state | No |

### Lifecycle Flow

```
                    ┌─────────────┐
                    │   (none)    │
                    └──────┬──────┘
                           │ amux claude (first time)
                           ▼
                    ┌─────────────┐
                    │   pending   │
                    └──────┬──────┘
                           │ computer ready
                           ▼
┌──────────────┐    ┌─────────────┐    ┌──────────────┐
│   stopped    │◄───│   started   │───►│   (deleted)  │
└──────┬───────┘    └─────────────┘    └──────────────┘
       │                   ▲                   ▲
       │ amux claude       │                   │
       │ (auto-restart)    │                   │
       └───────────────────┘                   │
                                               │
                           amux computer rm ───┘
```

### Auto-Stop (Daytona Only)

Daytona computers auto-stop after idle timeout:

```bash
# Default: 30 minutes
./amux claude

# Custom: 60 minutes
./amux claude --auto-stop 60

# Disabled
./amux claude --auto-stop 0
```

**What happens when auto-stopped**:
1. Computer transitions to `stopped` state
2. Compute resources freed (cost savings)
3. Volumes and data preserved
4. Next `amux claude` automatically restarts it

### Stop vs Delete

**Stop** (Daytona only):
- Computer still exists
- Data preserved
- Can be restarted
- Resources freed
- `./amux computer stop` (if implemented) or auto-stop

**Delete**:
- Computer destroyed
- `~/.amux/computer.json` metadata removed
- Must create new computer next time
- **Credentials are lost** (stored on computer filesystem)
- `./amux computer rm`

### Recreate

Force a fresh computer:

```bash
./amux claude --recreate
```

This:
1. Deletes the existing computer
2. Creates a new one with current config
3. Reinstalls the agent
4. Syncs workspace fresh

**When to use**:
- Config changes not taking effect
- Computer in bad state
- Want a clean environment

### Configuration Hash

Amux tracks computer configuration via a hash. If config changes, it auto-recreates:

**Tracked in hash**:
- User-specified volumes
- Auto-stop interval
- Snapshot

**Not tracked** (doesn't trigger recreate):
- Environment variables
- Agent arguments

---

## Workspace Sync System

### How Sync Works

When you run an agent, amux syncs your local workspace to the remote computer:

```
Local: /home/user/myproject/
         ↓ (tar + gzip + upload)
Remote: /home/daytona/.amux/workspaces/{worktreeId}/repo/
```

### What Gets Synced

**Included**:
- All files in your project directory
- Subdirectories (recursively)
- Symlinks (as symlinks)
- File permissions

**Excluded by default**:
- `node_modules/`
- `.next/`
- `dist/`
- `build/`
- `.turbo/`
- `.amux/`
- `.git/` (unless `--include-git`)

### Custom Ignore Patterns

Create `.amuxignore` in your project root:

```
# .amuxignore
vendor
*.log
tmp
coverage
.cache
```

Format:
- One pattern per line
- Lines starting with `#` are comments
- Empty lines ignored
- Matches any path component (not glob patterns)

### Incremental Sync

For large projects, amux uses incremental sync:

1. Generates manifest of local files (path, size, hash, mtime)
2. Compares with remote manifest
3. Only uploads changed files
4. Falls back to full sync if >50% changed

**Manifest location**: `{workspace}/.amux-manifest.json`

**Force full sync**: Delete the manifest or use `--recreate`

### Skip Sync

For debugging or when you've already synced:

```bash
./amux claude --no-sync
```

### Sync Direction

Currently, sync is **upload-only** (local → remote). To pull changes back:

```bash
./amux computer download  # If implemented
# Or manually:
./amux exec "cat /path/to/file" > local-file
```

---

## Coding Agents Reference

### Claude (Claude Code)

**What it is**: Anthropic's official AI coding assistant.

**Installation**: npm
```bash
npm install -g @anthropic-ai/claude-code@latest
```

**Authentication (default: OAuth/browser login)**:
On first run, Claude opens your browser to authenticate via Anthropic's OAuth flow. This is the recommended approach—same as using Claude Code locally.

**Credential storage**:
- `~/.claude/.credentials.json` → OAuth tokens
- `~/.claude/settings.json` → Preferences

**First-time auth inside sandbox**:
```bash
./amux claude
# Claude opens browser for OAuth authentication
# Complete login in browser
# Return to terminal - you're authenticated
# Credentials saved to volume for future sessions
```

**Optional API key alternative**:
```bash
# Skip OAuth by providing API key
./amux claude -e ANTHROPIC_API_KEY=sk-ant-xxx
```

**Special flags**:
- Supports TTY wrapping (auto-detected)
- Reads `CLAUDE.md` for project context

**Environment variables**:
- `ANTHROPIC_API_KEY`: Skip OAuth, use API key directly (optional)
- `ANTHROPIC_AUTH_TOKEN`: Use existing OAuth token
- `AMUX_TTY_WRAP`: Control TTY wrapping (0/1/auto)

**Update mechanism**:
- TTL: 24 hours
- Marker: `/amux/.installed/claude`
- Force: `./amux claude --update`

**Test procedure**:
```bash
./amux claude
# First run: Wait for install, complete OAuth
# Inside: Ask "what files are in this directory?"
# Verify it can see your synced files
# Exit with Ctrl+D

./amux claude
# Second run: Should be fast (cached)
# Verify still authenticated
```

---

### Codex (OpenAI Codex CLI)

**What it is**: OpenAI's coding agent.

**Installation**: npm
```bash
npm install -g @openai/codex@latest
```

**Authentication (default: device code flow)**:
On first run, Codex uses device code authentication (similar to GitHub CLI). You'll see a code to enter at OpenAI's website.

**Credential storage**:
- `~/.codex/auth.json` → Auth tokens
- `~/.config/codex/config.toml` → Configuration

**First-time auth inside sandbox**:
```bash
./amux codex
# Codex shows device code and URL
# Visit URL, enter code, authorize
# Return to terminal - you're authenticated
# Credentials saved to volume for future sessions
```

**Optional API key alternative**:
```bash
# Skip device auth by providing API key
./amux codex -e OPENAI_API_KEY=sk-xxx
```

**Special flags**:
- TUI2 mode enabled by default (`--enable tui2`)
- Disable with `AMUX_CODEX_TUI2=0`

**Environment variables**:
- `OPENAI_API_KEY`: Use API key directly
- `AMUX_CODEX_TUI2`: Control TUI2 feature (0/1)

**Config quirks**:
- Uses TOML format (not JSON)
- Requires file-based credential storage
- Config auto-patched to use file storage

**Test procedure**:
```bash
./amux codex
# First run: Install, then authenticate
# Inside: codex login --device-auth
# Verify TUI2 interface appears
# Exit

./amux codex
# Second run: Should be authenticated
```

---

### OpenCode

**What it is**: Open-source AI coding agent supporting multiple backends.

**Installation**: curl or npm
```bash
# Primary
curl -fsSL https://opencode.ai/install | bash

# Fallback
npm install -g opencode-ai@latest
```

**Authentication**:
OpenCode supports multiple AI providers. Each backend has its own auth method (OAuth or API key depending on the provider you choose).

**Credential storage**:
- `~/.config/opencode/` → Config directory
- `~/.local/share/opencode/` → Data directory

**First-time auth inside sandbox**:
```bash
./amux opencode
# Run: opencode auth login
# Choose your preferred backend
# Complete authentication for that provider
# Credentials saved to volume for future sessions
```

**Multi-backend support**:
OpenCode can use multiple AI providers. Configure inside:
```bash
opencode config set provider anthropic
# or: openai, gemini
```

**Test procedure**:
```bash
./amux opencode
# Configure backend
# Verify it works
# Exit

./amux opencode
# Should remember configuration
```

---

### Amp (Sourcegraph Amp)

**What it is**: Sourcegraph's enterprise AI coding agent.

**Installation**: curl or npm
```bash
# Primary
curl -fsSL https://ampcode.com/install.sh | bash

# Fallback
npm install -g @sourcegraph/amp@latest
```

**Authentication**:
Amp uses its own login flow. Run `amp login` inside the sandbox on first use.

**Credential storage**:
- `~/.config/amp/` → Config
- `~/.local/share/amp/` → Data
- `~/.amp/bin/amp` → Binary location

**First-time auth inside sandbox**:
```bash
./amux amp
# Run: amp login
# Follow authentication flow
# Credentials saved to volume for future sessions
```

**Special notes**:
- Installs to `~/.amp/bin/` (custom location)
- PATH automatically includes `~/.amp/bin`

**Test procedure**:
```bash
./amux amp
# Authenticate
# Verify it works
# Exit
```

---

### Gemini (Google Gemini CLI)

**What it is**: Google's Gemini AI coding agent.

**Installation**: npm
```bash
npm install -g @google/gemini-cli@latest
```

**Authentication (default: OAuth/browser login)**:
On first run, Gemini opens your browser for Google OAuth. This is the default and recommended flow.

**Credential storage**:
- `~/.gemini/` → Config and credentials
- `~/.gemini/oauth_creds.json` → OAuth tokens

**First-time auth inside sandbox**:
```bash
./amux gemini
# Gemini opens browser for Google OAuth
# Sign in with your Google account
# Return to terminal - you're authenticated
# Credentials saved to volume for future sessions
```

**Optional API key alternative**:
```bash
# Skip OAuth by providing API key
./amux gemini -e GEMINI_API_KEY=xxx
# Or: -e GOOGLE_API_KEY=xxx
```

**Test procedure**:
```bash
./amux gemini
# Complete auth flow
# Verify it works
# Exit
```

---

### Droid (Factory Droid)

**What it is**: Factory's AI coding agent.

**Installation**: curl
```bash
curl -fsSL https://app.factory.ai/cli | sh
```

**Authentication**:
Droid uses an in-agent login command. Run `/login` inside the Droid interface on first use.

**Credential storage**:
- `~/.factory/` → Config directory
- `~/.factory/bin/droid` → Binary location

**First-time auth inside sandbox**:
```bash
./amux droid
# Inside agent, run: /login
# Follow authentication flow
# Credentials saved to volume for future sessions
```

**Special notes**:
- Installs to `~/.factory/bin/`
- Login via `/login` command inside the agent (not CLI flag)

**Test procedure**:
```bash
./amux droid
# Run /login inside
# Verify it works
# Exit
```

---

### Shell (Interactive Bash)

**What it is**: Plain bash shell (not an AI agent).

**Installation**: None (built-in)

**Credentials**: Inherits all mounted credentials

**Use cases**:
- Debugging sandbox environment
- Manual operations
- Running arbitrary commands

**Special notes**:
- No credentials setup by default (`--credentials auto` = none for shell)
- Use `--credentials computer` to set up credential directories

**Test procedure**:
```bash
./amux shell
# Verify you're in the sandbox
uname -a
pwd
ls -la
exit

# With credentials setup
./amux shell --credentials computer
# Verify credential directories exist
ls ~/.claude ~/.codex
```

---

## Test Procedures

### Prerequisites

```bash
# Build
go build -o amux ./cmd/amux

# Choose provider
export AMUX_PROVIDER=daytona  # or sprites, docker

# Set credentials (provider-specific)
export DAYTONA_API_KEY=xxx    # for daytona
export SPRITES_TOKEN=xxx       # for sprites
# (nothing for docker)
```

### Quick Smoke Test (5 minutes)

```bash
# 1. Doctor check
./amux doctor

# 2. Run agent
./amux claude
# Exit immediately

# 3. Run again (should be fast)
./amux claude
# Exit

# 4. Cleanup
./amux computer rm --project
```

### Full Test Suite (30 minutes)

#### Phase 1: Setup & Diagnostics

```bash
# 1.1 Provider requirement
unset AMUX_PROVIDER
./amux status
# Expected: Error about provider required

export AMUX_PROVIDER=daytona  # or your provider

# 1.2 Doctor
./amux doctor
# Expected: All checks pass

# 1.3 Deep doctor (Daytona only)
./amux doctor --deep
# Expected: Creates temp computer, runs health checks, cleans up
```

#### Phase 2: Computer Lifecycle

```bash
# 2.1 First run (fresh install)
./amux claude
# Expected: Creates computer, syncs workspace, installs Claude
# Exit

# 2.2 Cached run
./amux claude
# Expected: Reuses computer, skips install, fast startup
# Exit

# 2.3 Force update
./amux claude --update
# Expected: Reinstalls Claude to latest version
# Exit

# 2.4 Recreate
./amux claude --recreate
# Expected: Deletes old computer, creates new one
# Exit
```

#### Phase 3: Workspace Sync

```bash
# 3.1 Create test file
echo "amux-test-content" > amux-test-file.txt

# 3.2 Run agent and verify
./amux claude
# Inside sandbox:
cat amux-test-file.txt
# Expected: Shows "amux-test-content"
# Exit

# 3.3 Cleanup
rm amux-test-file.txt
```

#### Phase 4: Multiple Agents

```bash
# 4.1 Switch to different agent
./amux codex
# Expected: Installs Codex in same computer
# Exit

# 4.2 Switch back
./amux claude
# Expected: Claude still installed, fast startup
# Exit
```

#### Phase 5: Access Methods

```bash
# 5.1 SSH (Daytona only)
./amux ssh
# Expected: Raw shell access
# Exit

# 5.2 Exec
./amux exec "uname -a"
# Expected: Shows Linux info

./amux exec "pwd"
# Expected: Shows workspace path

# 5.3 Status
./amux status
# Expected: Shows computer state, agent, etc.
```

#### Phase 6: Provider Features

```bash
# 6.1 Preview URL (Daytona/Sprites)
./amux computer preview 3000 --no-open
# Expected: Prints preview URL

# 6.2 Desktop (Daytona only)
./amux computer desktop --no-open
# Expected: Prints VNC URL

# 6.3 Computer list
./amux computer ls
# Expected: Shows current computer
```

#### Phase 7: Cleanup

```bash
# 7.1 Remove project computer
./amux computer rm --project
# Expected: Computer deleted

# 7.2 Verify
./amux computer ls
# Expected: Empty or no project computer

# 7.3 Verify metadata cleaned
ls .amux/
# Expected: computer.json removed or empty
```

### Agent-Specific Tests

For each agent, verify:

1. **Installation**: First run installs successfully
2. **Authentication**: Can log in and persist credentials
3. **Functionality**: Can answer a simple coding question
4. **Persistence**: Second run doesn't require re-auth
5. **Update**: `--update` flag reinstalls

```bash
# Template for each agent:
./amux {agent}
# Complete first-time auth if needed
# Ask: "What files are in this directory?"
# Verify it responds correctly
# Exit

./amux {agent}
# Verify no re-auth needed
# Exit

./amux {agent} --update
# Verify reinstall happens
# Exit
```

---

## Troubleshooting

### "Provider is required"

```bash
# Solution: Set AMUX_PROVIDER
export AMUX_PROVIDER=daytona
```

### "API key not configured" (provider key)

This refers to the provider API key (Daytona or Sprites), not agent API keys.

```bash
# Solution: Run setup or set environment variable
./amux setup
# or
export DAYTONA_API_KEY=xxx  # for Daytona
export SPRITES_TOKEN=xxx    # for Sprites
```

### "Computer not found"

The computer was deleted or never created.

```bash
# Solution: Create a new computer
./amux claude --recreate
```

### Agent not installing

```bash
# Check npm is available in sandbox
./amux exec "which npm"

# Check install logs
./amux exec "cat /amux/.installed/claude" 2>/dev/null || echo "Not installed"

# Force reinstall
./amux claude --update
```

### Credentials lost after computer deletion

Credentials are stored on the computer's filesystem. If you delete the computer, credentials are lost.

```bash
# Workaround: Pass API keys via environment to skip OAuth
./amux claude -e ANTHROPIC_API_KEY=sk-ant-xxx
./amux codex -e OPENAI_API_KEY=sk-xxx
./amux gemini -e GEMINI_API_KEY=xxx
```

This applies to all providers (Daytona, Sprites, Docker).

### Workspace not syncing

```bash
# Check ignore patterns
cat .amuxignore

# Check for large files
du -sh * | sort -h

# Force full sync
./amux claude --recreate
```

### Computer in error state

```bash
# Check status
./amux status

# Delete and recreate
./amux computer rm --project
./amux claude
```

### SSH connection failed

```bash
# Daytona: Check SSH access is supported
./amux doctor

# Verify computer is running
./amux status

# Try exec instead
./amux exec "echo hello"
```

---

## Test Results Template

| Test | Daytona | Sprites | Docker | Notes |
|------|---------|---------|--------|-------|
| Provider requirement | | | | |
| Doctor | | | | |
| First run (claude) | | | | |
| Cached run | | | | |
| Force update | | | | |
| Workspace sync | | | | |
| SSH access | | | N/A | |
| Exec command | | | | |
| Preview URL | | | N/A | |
| Desktop | | N/A | N/A | |
| Computer list | | | | |
| Computer remove | | | | |
| Codex agent | | | | |
| OpenCode agent | | | | |
| Amp agent | | | | |
| Gemini agent | | | | |
| Droid agent | | | | |
| Shell access | | | | |
| GitHub auth | | | | |
| Settings sync | | | | |
| Credentials persist | | | | |

**Status Legend**: ✓ Pass | ✗ Fail | - Skipped | N/A Not Applicable

**Note**: Credentials persist on all providers as long as the computer exists.
