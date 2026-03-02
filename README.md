<p align="center">
  <img width="339" height="105" alt="Screenshot 2026-01-20 at 1 00 23 AM" src="https://github.com/user-attachments/assets/fdbefab9-9f7c-4e08-a423-a436dda3c496" />
</p>

<p align="center">Run parallel coding agents from any device — web, mobile, desktop</p>

<p align="center">
  <a href="https://github.com/andyrewlee/medusa/releases">
    <img src="https://img.shields.io/github/v/release/andyrewlee/medusa?style=flat-square" alt="Latest release" />
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/andyrewlee/medusa?style=flat-square" alt="License" />
  </a>
  <img src="https://img.shields.io/badge/Go-1.24.2-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go version" />
  <a href="https://discord.gg/Dswc7KFPxs">
    <img src="https://img.shields.io/badge/Discord-5865F2?style=flat-square&logo=discord&logoColor=white" alt="Discord" />
  </a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#server-mode">Server mode</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#features">Features</a> ·
  <a href="#configuration">Configuration</a>
</p>

![Medusa TUI preview](https://github.com/user-attachments/assets/f5c4647e-a6ee-4d62-b548-0fdd73714c90)

## What is Medusa?

Medusa manages multiple coding agents in parallel with a worktree-first model. It supports two modes:

- **TUI mode** — terminal UI running agents locally via tmux (the original experience)
- **Server mode** — HTTP server with a web UI, accessible from any device (web, mobile, desktop)

Server mode uses Claude Code's structured JSON streaming for a rich conversation UI instead of raw terminal output. Sessions persist across server restarts and sync seamlessly between devices.

## Prerequisites

- **Go 1.24+**
- **git** and **tmux**
- **Claude Code CLI** (`npm install -g @anthropic-ai/claude-code`)
- **Node.js 22+** (for building the web UI)

## Quick start

### TUI mode (original)

```bash
curl -fsSL https://raw.githubusercontent.com/andyrewlee/medusa/main/install.sh | sh
```

Or with Go:

```bash
go install github.com/andyrewlee/medusa/cmd/medusa@latest
```

Then run `medusa` to open the dashboard.

### Server mode

#### Option 1: Build from source

```bash
git clone https://github.com/andyrewlee/medusa.git
cd medusa

# Build the web UI
cd web && npm install && npm run build && cd ..

# Build the server (embeds the web UI into the binary)
go build -o medusa-server ./cmd/medusa-server/

# Start the server
./medusa-server --port 8420
```

#### Option 2: Install script

```bash
curl -fsSL https://raw.githubusercontent.com/andyrewlee/medusa/main/deploy/install-server.sh | bash
medusa-server --port 8420
```

#### Option 3: Docker

```bash
docker build -f Dockerfile.server -t medusa-server .
docker run -p 8420:8420 \
  -v /path/to/repos:/repos \
  -v medusa-data:/home/medusa/.medusa \
  -e ANTHROPIC_API_KEY=sk-... \
  medusa-server
```

Or with Docker Compose:

```bash
ANTHROPIC_API_KEY=sk-... docker compose -f docker-compose.server.yml up -d
```

### Connecting to the server

When the server starts, it prints an auth token:

```
Medusa server starting on http://0.0.0.0:8420
Auth token: mds_a1b2c3d4e5f6...
Web UI: http://localhost:8420
```

The token is also saved to `~/.medusa/server_token`.

**Web browser:** Open `http://your-host:8420` and enter the auth token.

**API:** Use the token as a Bearer token:

```bash
curl -H "Authorization: Bearer mds_a1b2c3d4..." http://localhost:8420/api/v1/projects
```

## Server mode

### Architecture

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│  Web Client  │  │  TUI Client  │  │ Mobile (PWA) │
│  React + TS  │  │  Go + views  │  │  React + TS  │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                 │
       └────────┬────────┴────────┬────────┘
                │   HTTPS + WSS   │
        ┌───────┴─────────────────┴───────┐
        │        Medusa Server (Go)       │
        │  REST API + WebSocket + SSE     │
        │  Service Layer + Claude JSONL   │
        └─────────────────────────────────┘
```

The server manages all state — repos, workspaces, agent processes, conversation history. Clients are thin and stateless.

### Claude integration

Instead of wrapping Claude in a terminal, the server uses Claude Code's structured JSON streaming:

```bash
claude --output-format stream-json --input-format stream-json --session-id <uuid>
```

This gives the web UI rich, structured data: markdown text, tool call cards, code diffs, cost tracking — all rendered natively rather than as terminal output.

### Cross-device sync

All conversation state lives on the server. Start a session on your phone, continue on your desktop:

1. Client connects → gets full conversation history via REST
2. Client subscribes → receives new messages in real-time via WebSocket
3. Multiple clients can view the same tab simultaneously

### Session persistence

Sessions survive server restarts:

1. Conversation history is stored in `~/.medusa/sessions/` as JSONL
2. On restart, tabs appear as "stopped" with full history
3. Click "Resume" — the server spawns Claude with `--resume <session-id>`
4. Claude loads its context from `~/.claude/projects/` and continues

### API reference

All endpoints under `/api/v1/`. Requires `Authorization: Bearer <token>`.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/projects` | List all projects with workspaces |
| POST | `/projects` | Add a git repository |
| POST | `/workspaces` | Create a new workspace |
| POST | `/workspaces/:wsID/tabs` | Launch an agent tab |
| POST | `/tabs/:tabID/prompt` | Send a message to Claude |
| GET | `/tabs/:tabID/history` | Get conversation history |
| WS | `/tabs/:tabID/ws` | Real-time message streaming |
| WS | `/tabs/:tabID/pty` | Raw PTY streaming (non-Claude) |
| SSE | `/events` | Global state change events |

### Server flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8420` | HTTP listen port |
| `--bind` | `0.0.0.0` | Bind address |
| `--tls-cert` | | TLS certificate file |
| `--tls-key` | | TLS private key file |

### Running as a systemd service

```bash
sudo cp deploy/medusa-server.service /etc/systemd/system/
# Edit the service file to set ANTHROPIC_API_KEY and User
sudo systemctl enable --now medusa-server
```

## How it works

Each worktree tracks a repo checkout and its metadata. For local workflows, worktrees are typically backed by git worktrees on their own branches so agents work in isolation and you can merge changes back when done.

## Features

- **Parallel agents**: Launch multiple agents within main repo and within worktrees
- **No wrappers**: Works with Claude Code, Codex, Gemini, Amp, OpenCode, and Droid
- **Keyboard + mouse**: Can be operated with just the keyboard or with a mouse
- **All-in-one tool**: Run agents, view diffs, and access terminal
- **Server mode**: Access from any device — web, mobile, desktop
- **Structured Claude UI**: Rich conversation view with markdown, tool cards, code diffs
- **Cross-device sync**: Start on mobile, continue on desktop seamlessly
- **Session persistence**: Resume sessions after server restart

## Configuration

Create `.medusa/workspaces.json` in your project to run setup commands for new workspaces:

```json
{
  "setup-workspace": [
    "npm install",
    "cp $ROOT_WORKSPACE_PATH/.env.local .env.local"
  ]
}
```

Worktree metadata is stored in `~/.medusa/workspaces/<workspace-id>/workspace.json` and local worktree directories live under `<repo>/.medusa/workspaces/`.

## Development

```bash
git clone https://github.com/andyrewlee/medusa.git
cd medusa

# TUI
make run

# Server (with live web UI reload)
cd web && npm run dev          # Terminal 1: Vite dev server on :5173
go run ./cmd/medusa-server/    # Terminal 2: Go server on :8420
# Vite proxies /api requests to the Go server automatically
```
