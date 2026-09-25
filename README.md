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

Both write actions are available from the UI, each behind an explicit confirmation:

- **Commit** — press `c` in the Changes sidebar to stage everything in the workspace and commit it with a message you type. The commit lands on the workspace's own branch; amux never pushes.
- **Merge** — press `M` on a workspace row in the dashboard to merge its branch into its base with `git merge --no-ff`. The merge happens in the project's primary checkout, so amux first checks that the base branch is already checked out there and refuses otherwise rather than moving your HEAD. On a conflict it lists the conflicted files and offers to abort; resolving them is yours to do in the terminal.

Note that amux runs git with repository hooks disabled (see `AMUX_ALLOW_GIT_HOOKS` below), so your own `pre-commit` or `pre-merge` hooks will not fire on these actions.

Shelving puts a workspace away without deleting it: press `S` on a live
workspace row in the dashboard to shelve it — the worktree is removed, while
the branch, workspace metadata, and scripts are kept. Shelve runs the repo's
`archive` hook first if one is configured (it's a repo-supplied command, so
the usual trust gating applies), and it's best-effort like delete: a failed
archive never strands the shelve. Shelved workspaces list in their own
dashboard section — `Enter` on a shelved row restores it (recreates the
worktree and re-runs `setup-workspace`), and `D` purges it for good (deletes
the branch and the record behind a confirmation).

For cleaning up a fleet at once, `space` marks a workspace row (`●`) and
`esc` clears marks; `S` with marks present shelves the marked set after one
confirmation listing the names — each workspace shelves sequentially through
the same guarded path a single shelve uses, and a summary toast reports the
outcome when the batch drains.

## Architecture quick tour

Start with [`ARCHITECTURE.md`](ARCHITECTURE.md) for the repo-level package map and dependency direction. Then `internal/app/ARCHITECTURE.md` covers lifecycle, PTY flow, tmux tagging, and persistence invariants, and `internal/app/MESSAGE_FLOW.md` documents message boundaries and command discipline.

External orchestration: the supported tmux-level contract is documented in [docs/ORCHESTRATION.md](docs/ORCHESTRATION.md).

## Features

- **Parallel agents**: Launch multiple agents within main repo and within workspaces
- **No wrappers**: Works with Claude Code, Codex, OpenCode, Droid, Cursor, Pi, Oh My Pi, Antigravity, FX, Grok, Amp, Cline, Devin, and Prime Agent
- **Keyboard + mouse**: Can be operated with just the keyboard or with a mouse
- **All-in-one tool**: Run agents, view diffs, and access terminal

## Controls

`Ctrl-Space` opens the command palette at the bottom of the screen (the toolbar
button does the same; `Esc` cancels). Commands are short key sequences:

| Sequence | Action |
|----------|--------|
| `q` | quit (confirmation dialog) |
| `a` | add project |
| `d` | delete workspace |
| `S` | Settings |
| `K` | cleanup tmux sessions |
| `h` / `l` | focus pane left / right |
| `n` | jump the dashboard cursor to the next workspace needing attention — a done badge or an agent that rang BEL (agents like Claude Code ring it on notification/permission events) |
| `t a` | new agent tab |
| `t t` | new terminal tab |
| `t n` / `t p` | next / previous tab |
| `t x` | close tab |
| `t d` | detach tab (keeps the tmux session) |
| `t r` | reattach detached tab |
| `t s` | restart tab |
| `t y` | copy transcript (scrollback + screen) of the focused terminal tab |
| `t f` | save the focused terminal tab's full transcript to a file (default `~/.amux/transcripts/`) |
| `1`–`9` | jump to center tab by position |

When a center terminal tab is focused, `d` instead scrolls down a page and `u`
scrolls up — the palette shows the substitute meanings in that context. Prefix +
prefix again sends a literal `Ctrl-Space` (NUL) to the focused terminal.

Global keys:

- `Ctrl+C` in a terminal tab sends the agent's interrupt sequence (agents that
  need more than one `^C`, like Claude, get the configured count) — it is not a
  quit key; quit is `C-Space q`.
- `Ctrl+N` / `Ctrl+P` (or `Ctrl+]`) cycle center terminal tabs; `Ctrl+W` closes
  the current one.
- `PgUp` / `PgDn` scroll the focused terminal's scrollback a page at a time;
  the mouse wheel scrolls whichever pane the pointer is over (`Shift`+wheel
  forces amux scrollback even when the hosted app claims the wheel).

Sidebar (Changes tab, focused):

- `1` / `2` switch the sidebar between Changes and Project views.
- `j`/`k` or arrows move, `enter`/`space`/`o` opens a diff, `g` refreshes,
  `/` filters.
- `c` commit, `b` branch mode, `e` workspace env, `E` project env, `s`
  scripts, `r` run/stop the workspace run script, `R` run output, `O` script
  output, `u` re-run setup, `i` workspace status. Inside the live run-output viewer, `a`
  attaches an interactive tab to the newest alive run session (closing the
  tab detaches — the script keeps running).
- Workspace lifecycle keys (`S` shelve, `space` mark, `esc` clear marks) are on
  dashboard rows — see [How it works](#how-it-works).

Set `"ui": { "show_keymap_hints": true }` in `~/.amux/config.json` to show an
in-app hint bar.

**Self-update**: amux checks for a new release at startup; when one is
available a self-update item appears in Settings (`C-Space S`). Updates are
minisign-verified before being applied.

## Configuration

Create `.amux/workspaces.json` in your project to define commands that amux runs for its workspaces:

```json
{
  "setup-workspace": [
    "npm install",
    "cp $ROOT_WORKSPACE_PATH/.env.local .env.local"
  ],
  "run": "npm start",
  "on-done": "osascript -e 'display notification \"agent done\"'",
  "archive": "tar -czf archive.tar.gz .",
  "env": {
    "GOPRIVATE": "git.example.com/*"
  }
}
```

- `setup-workspace` — commands run once when a new workspace is created. If a
  run fails transiently or you edit the config later, press `u` in the Changes
  sidebar to run it again (repo-supplied commands re-check trust first).
- `run` — the command started for a workspace's run script. Press `r` in the
  Changes sidebar to start it, and `r` again to stop it; a `[run]` marker sits
  next to the branch name while it is live. The script runs in its own tmux
  session, so its output survives the process: press `R` to view the captured
  tail (live while running, or after it exits — a crashed run keeps its output
  and reports the exit status in a toast). Bind your dev server to `AMUX_PORT`
  so parallel workspaces don't collide.
- `on-done` — a fire-once hook run when one of the workspace's agent sessions
  crosses the working→done edge (the same transition that rings the
  `notify_on_done` bell and posts the "Agent finished" toast). It runs
  detached in the workspace root with `AMUX_SESSION` set to the session name.
  Only a strict working→done transition fires it — an agent that was already
  finished when amux started does not trigger the hook.
- `archive` — the command run when a workspace is archived, i.e. just before its
  worktree is deleted. It runs to completion (up to two minutes) in the worktree
  while that directory still exists, after the run script has been stopped. It
  is best-effort: if it fails or the repo isn't trusted yet, amux warns you and
  deletes the workspace anyway, so a broken archive script can never strand a
  workspace. Write it to tolerate running twice — if the delete itself fails
  after the script has run, retrying the delete runs it again.

`setup-workspace`, `archive`, and `on-done` each record a bounded tail of
their combined output on every run — press `O` in the Changes sidebar to view
the last run of each for the shown workspace. Transcripts persist under the
workspace's metadata dir (`script-transcripts.json`), so `O` still answers
"why did setup fail" after a restart. A failing `on-done` hook also
posts a warning toast (it used to be invisible); `R` stays the live surface
for `run`.

For the full operational picture press `i` in the Changes sidebar: a
read-only snapshot of the workspace's allocated port range, run-session
state, script config (repo vs. yours) and its trust verdict, custom env key
names (never values), lifecycle state, and open tabs.

You can also set per-workspace scripts yourself: press `s` in the Changes
sidebar to open the workspace scripts editor, which stores `setup`, `run`,
`archive`, and `on-done` commands on the workspace record plus a run mode (`nonconcurrent`
stops a previous run before starting a new one; `concurrent` lets them
overlap). A repo-provided `.amux/workspaces.json` script still wins over the
per-workspace one when both exist — the editor's fields are the fallback.
Commands entered here are your own input, so they run without the repo trust
prompt described below.

### Environment available to workspace scripts

`setup-workspace`, `run`, and `archive` scripts run with these variables set, in addition to your normal shell environment:

| Variable | Meaning |
|----------|---------|
| `AMUX_WORKSPACE_NAME` | The workspace name |
| `AMUX_WORKSPACE_ROOT` | The workspace worktree path |
| `AMUX_WORKSPACE_BRANCH` | The workspace's git branch |
| `ROOT_WORKSPACE_PATH` | The source repository root (also shown in the example above) |
| `AMUX_PORT` | An allocated per-workspace port — bind dev servers here to avoid collisions across parallel workspaces |
| `AMUX_PORT_RANGE` | The `start-end` port range allocated to this workspace |

On top of those injected values, custom environment layers in for all script
spawns (`setup-workspace`, `run`, `on-done`, `archive`), lowest to highest
precedence:

1. **Repo `env`** — the `.amux/workspaces.json` map above (trust-gated).
2. **Project env** — per-repo variables you own, shared by every workspace of
   the project. Press `E` in the Changes sidebar to edit; stored user-side in
   `~/.amux/project-env.json`, so it never touches the repo and is the right
   home for secrets (API keys, tokens). No trust prompt — it is your input.
3. **Workspace env** — per-workspace variables via `e` in the Changes sidebar.
   Always wins.

For ordinary keys (e.g. `PATH`, `GOFLAGS`) the highest layer's value wins
outright — amux replaces rather than merges. The injected `AMUX_*`/`ROOT_*`
names are reserved: custom layers can never override them.

Agent sessions and sidebar terminals get the same injected variables plus the
two user-controlled layers — project env and workspace env — so an agent sees
`AMUX_PORT`, your project secrets, and per-workspace overrides exactly as a
`run` script would. The one deliberate narrowing: the **repo `env` layer does
not apply** to interactive sessions, even for a trusted repo. Repo env is
repo-chosen content gated for one-shot scripts; a long-lived agent or shell
acting with your credentials is a bigger blast radius, so approving a repo's
scripts never widens what the repo can inject into your agents.

Because these commands come from the repository, amux runs them only after you trust the repo. The first time a repo's `.amux/workspaces.json` would run (and every time its contents change), amux records the approved content of the file; until then those project-supplied scripts are skipped and you are notified, rather than executing arbitrary commands chosen by the repo's author. Editing `.amux/workspaces.json` invalidates the approval, so changed commands are re-gated until you trust the file again. (Run/archive scripts you enter yourself in the amux UI are your own input and are never gated.)

Workspace metadata is stored in `~/.amux/workspaces-metadata/<workspace-id>/workspace.json`, and local worktree directories live under `~/.amux/workspaces/<project>/<workspace>`. Trusted-repo approvals are recorded in `~/.amux/trusted-scripts.json`, and the
project-level environment map in `~/.amux/project-env.json`.

Assistants: the AI agents amux can launch are configured per-user in `~/.amux/config.json`. You can add your own or override a built-in — see [docs/CONFIG.md](docs/CONFIG.md).

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
- Terminal-tab limit: set `AMUX_MAX_ATTACHED_TERMINAL_TABS` (default 6; `0` disables the limit) to change how many sidebar terminals keep live PTYs attached; least-recently-used background terminals detach automatically, stay alive in tmux, and re-attach when their workspace is selected.
- Git hooks: amux runs git with repo hooks and `core.fsmonitor` disabled so a checked-out repository cannot execute code just because amux touched it; set `AMUX_ALLOW_GIT_HOOKS=1` if your workflow needs repo hooks to run. git-lfs works either way — its clean/smudge filters are never disabled, and amux points `core.hooksPath` at `/dev/null` (not at an empty value) so lfs cannot leave stray hook files in a workspace root.
- OSC 52 clipboard: set `AMUX_ENABLE_OSC52_CLIPBOARD=1` to let agent terminal output copy to your clipboard via OSC 52 (off by default because terminal output is untrusted; payloads over 64 KiB are ignored).
- Perf profiling: set `AMUX_PROFILE=1` to emit periodic timing/counter snapshots; adjust cadence with `AMUX_PROFILE_INTERVAL_MS` (default 5000).
- pprof: set `AMUX_PPROF=1` (or a port like `6061`) to expose `net/http/pprof` on `127.0.0.1`.
- Debug signals: set `AMUX_DEBUG_SIGNALS=1` and send `SIGUSR1` to dump goroutines into the log.
- PTY tracing: set `AMUX_PTY_TRACE=1` or a comma-separated assistant list; traces write to the log dir (or OS temp dir if logging is disabled). The trace captures both directions of the pipeline — agent→amux output is tagged `RECV` and amux→agent input (keystrokes, pastes, the delayed Enter/CR) is tagged `SEND` — so send-path issues like a dropped Enter can be debugged at the byte level.
