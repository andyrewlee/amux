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
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go version" />
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

amux is a terminal UI for running multiple coding agents in parallel with a workspace-first model that can import git worktrees.

## Prerequisites

amux requires [tmux](https://github.com/tmux/tmux) (minimum 3.2). Each agent runs in its own tmux session for terminal isolation and persistence.

## Quick start

```bash
brew tap andyrewlee/amux
brew install amux
```

Or via the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/andyrewlee/amux/main/install.sh | sh
```

Or with Go (requires Go 1.26 or newer; contributors should use the patched
toolchain pinned in `go.mod`):

```bash
go install github.com/andyrewlee/amux/cmd/amux@latest
```

Then run `amux` to open the dashboard.

## How it works

Each workspace tracks a repo checkout and its metadata. For local workflows, workspaces are typically backed by git worktrees on their own branches so agents work in isolation and you can merge changes back when done.

## Architecture quick tour

Start with [`ARCHITECTURE.md`](ARCHITECTURE.md) for the repo-level package map and dependency direction. Then `internal/app/ARCHITECTURE.md` covers lifecycle, PTY flow, tmux tagging, and persistence invariants, and `internal/app/MESSAGE_FLOW.md` documents message boundaries and command discipline.

## Features

- **Parallel agents**: Launch multiple agents within main repo and within workspaces
- **No wrappers**: Works with Claude Code, Codex, Gemini, Amp, OpenCode, and Droid
- **Keyboard + mouse**: Can be operated with just the keyboard or with a mouse
- **All-in-one tool**: Run agents, view diffs, and access terminal

## Configuration

Create `.amux/workspaces.json` in your project to define commands that amux runs for its workspaces:

```json
{
  "setup-workspace": [
    "npm install",
    "cp $ROOT_WORKSPACE_PATH/.env.local .env.local"
  ],
  "run": "npm start",
  "archive": "tar -czf archive.tar.gz ."
}
```

- `setup-workspace` — commands run once when a new workspace is created.
- `run` — the command started for a workspace's run script.
- `archive` — the command run when a workspace is archived.

Because these commands come from the repository, amux runs them only after you trust the repo. The first time a repo's `.amux/workspaces.json` would run (and every time its contents change), amux records the approved content of the file; until then those project-supplied scripts are skipped and you are notified, rather than executing arbitrary commands chosen by the repo's author. Editing `.amux/workspaces.json` invalidates the approval, so changed commands are re-gated until you trust the file again. (Run/archive scripts you enter yourself in the amux UI are your own input and are never gated.)

Workspace metadata is stored in `~/.amux/workspaces-metadata/<workspace-id>/workspace.json`, and local worktree directories live under `~/.amux/workspaces/<project>/<workspace>`. Trusted-repo approvals are recorded in `~/.amux/trusted-scripts.json`.

## Platform Support

AMUX requires `tmux` and is supported on Linux/macOS. Windows is not supported.

## Development

```bash
git clone https://github.com/andyrewlee/amux.git
cd amux
./scripts/install-hooks.sh   # one-time: enables the pre-commit + pre-push git hooks
make lint-tools              # one-time: builds the pinned golangci-lint into ./.cache/bin
make run
```

Run `./scripts/install-hooks.sh` once after cloning. It points `core.hooksPath`
at `.githooks`, enabling the pre-commit fmt/lint/file-length checks and the
pre-push lint-parity gate the project relies on for quality.

Run `make lint-tools` once before your first `make devcheck` or `git commit`.
It builds the linter pinned in `.golangci-version` into the gitignored
`./.cache/bin`; a stock `golangci-lint` from `PATH` may be a different version
from CI and produce different diagnostics. See [LINTING.md](LINTING.md) and
[CONTRIBUTING.md](CONTRIBUTING.md) for details.

## Operations

- Logs are written to `~/.amux/logs/amux-YYYY-MM-DD.log` (default retention 14 days). Override retention with `AMUX_LOG_RETENTION_DAYS`.
- Log verbosity: set `AMUX_LOG_LEVEL=debug` (accepts `debug`/`info`/`warn`/`error`; default `info`) to change what gets written to the log — `debug` is the first thing to try when reporting or diagnosing a problem.
- Attached-tab limit: set `AMUX_MAX_ATTACHED_AGENT_TABS` (default 6; `0` disables the limit) to change how many agent tabs keep live PTYs attached concurrently.
- Git hooks: amux runs git with repo hooks and `core.fsmonitor` disabled so a checked-out repository cannot execute code just because amux touched it; set `AMUX_ALLOW_GIT_HOOKS=1` if your workflow needs repo hooks (e.g. git-lfs).
- OSC 52 clipboard: set `AMUX_ENABLE_OSC52_CLIPBOARD=1` to let agent terminal output copy to your clipboard via OSC 52 (off by default because terminal output is untrusted; payloads over 64 KiB are ignored).
- Perf profiling: set `AMUX_PROFILE=1` to emit periodic timing/counter snapshots; adjust cadence with `AMUX_PROFILE_INTERVAL_MS` (default 5000).
- pprof: set `AMUX_PPROF=1` (or a port like `6061`) to expose `net/http/pprof` on `127.0.0.1`.
- Debug signals: set `AMUX_DEBUG_SIGNALS=1` and send `SIGUSR1` to dump goroutines into the log.
- PTY tracing: set `AMUX_PTY_TRACE=1` or a comma-separated assistant list; traces write to the log dir (or OS temp dir if logging is disabled). The trace captures both directions of the pipeline — agent→amux output is tagged `RECV` and amux→agent input (keystrokes, pastes, the delayed Enter/CR) is tagged `SEND` — so send-path issues like a dropped Enter can be debugged at the byte level.
