# amux

A terminal multiplexer for AI coding agents. Manage multiple git worktrees with different AI assistants (Claude, Codex, Gemini, Amp) in a single TUI.

## Requirements

- Go 1.21+
- Git

## Quick Start

```bash
git clone https://github.com/andyrewlee/amux.git
cd amux
go build -o amux ./cmd/amux
./amux
```

## Usage

Launch `amux` and use the keyboard to navigate:

| Key | Action |
|-----|--------|
| `j/k` | Navigate up/down |
| `Enter` | Select worktree |
| `a` | Add project |
| `n` | New worktree |
| `d` | Delete worktree |
| `Ctrl+T` | Launch AI agent |
| `Ctrl+H/L` | Switch panes |
| `Esc` | Return to dashboard |
| `?` | Help |
| `Ctrl+Q` | Quit |

## Supported Agents

- **claude** - Claude Code
- **codex** - OpenAI Codex
- **gemini** - Google Gemini
- **amp** - Sourcegraph Amp
- **opencode** - SST OpenCode

## Developer Notes

- Terminal rendering and flicker mitigation: `docs/terminal-rendering.md`
