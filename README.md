<p align="center">
  <img width="339" height="105" alt="Screenshot 2026-01-20 at 1 00 23 AM" src="https://github.com/user-attachments/assets/fdbefab9-9f7c-4e08-a423-a436dda3c496" />  
</p>

<p align="center">Parallel coding-agent orchestration for git worktrees in a fast TUI.</p>

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
  <a href="#features">Features</a> ·
  <a href="#configuration">Configuration</a> ·
  <a href="#development">Development</a> ·
  <a href="#community">Community</a>
</p>

![amux TUI preview](https://github.com/user-attachments/assets/f5c4647e-a6ee-4d62-b548-0fdd73714c90)

## What is amux?

amux is a terminal UI for running multiple coding agents in parallel with first-class support for git worktrees. Run real agent CLIs—no wrappers, no SDKs—organized, fast, and visible while you work.

## Quick start

```bash
curl -fsSL https://raw.githubusercontent.com/andyrewlee/amux/main/install.sh | sh
```

Or with Go:

```bash
go install github.com/andyrewlee/amux/cmd/amux@latest
```

Then run `amux` to open the dashboard.

## Features

- **Parallel agents**: Launch multiple agents side-by-side, each in its own worktree
- **No wrappers**: Works with Claude Code, Codex, Gemini, Amp, OpenCode, and Droid
- **Keyboard + mouse**: Vim-style navigation or click around
- **Built-in diffs**: Review file changes and commit history without leaving amux

## Configuration

Create `.amux/worktrees.json` in your project to run setup commands for new worktrees:

```json
{
  "setup-worktree": [
    "npm install",
    "cp $ROOT_WORKTREE_PATH/.env.local .env.local"
  ]
}
```

Worktrees are stored in `~/.amux/worktrees/<project>/`.

## Development

```bash
git clone https://github.com/andyrewlee/amux.git
cd amux
make run
```

## Community

Join the [Discord](https://discord.gg/Dswc7KFPxs) for questions and discussion.

## License

MIT
