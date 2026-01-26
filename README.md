<p align="center">
  <img width="339" height="105" alt="Screenshot 2026-01-20 at 1 00 23 AM" src="https://github.com/user-attachments/assets/fdbefab9-9f7c-4e08-a423-a436dda3c496" />  
</p>

<p align="center">TUI for easily running parallel coding agents</p>

<p align="center">
  <a href="https://github.com/andyrewlee/amux/releases">
    <img src="https://img.shields.io/github/v/release/andyrewlee/amux?style=flat-square" alt="Latest release" />
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/andyrewlee/amux?style=flat-square" alt="License" />
  </a>
  <img src="https://img.shields.io/badge/Go-1.24.2-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go version" />
  <a href="https://discord.gg/Dswc7KFPxs">
    <img src="https://img.shields.io/badge/Discord-5865F2?style=flat-square&logo=discord&logoColor=white" alt="Discord" />
  </a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#features">Features</a> ·
  <a href="#configuration">Configuration</a>
</p>

![amux TUI preview](https://github.com/user-attachments/assets/f5c4647e-a6ee-4d62-b548-0fdd73714c90)

## What is amux?

amux is a terminal UI for running multiple coding agents in parallel with first-class support for git worktrees.

## Quick start

```bash
curl -fsSL https://raw.githubusercontent.com/andyrewlee/amux/main/install.sh | sh
```

Or with Go:

```bash
go install github.com/andyrewlee/amux/cmd/amux@latest
```

Then run `amux` to open the dashboard.

## How it works

Each workspace is a git worktree with its own branch. Agents work in isolation without conflicts, then you merge changes back when done.

## Features

- **Parallel agents**: Launch multiple agents within main repo and within workspaces
- **No wrappers**: Works with Claude Code, Codex, Gemini, Amp, OpenCode, and Droid
- **Keyboard + mouse**: Can be operated with just the keyboard or with a mouse
- **All-in-one tool**: Run agents, view diffs, and access terminal

## Configuration

Create `.amux/workspaces.json` in your project to run setup commands for new workspaces:

```json
{
  "setup-workspace": [
    "npm install",
    "cp $ROOT_WORKSPACE_PATH/.env.local .env.local"
  ]
}
```

Workspaces are stored in `~/.amux/workspaces/<project>/`.

## Development

```bash
git clone https://github.com/andyrewlee/amux.git
cd amux
make run
```
