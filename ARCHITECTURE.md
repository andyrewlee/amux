# Architecture

amux is a terminal UI (Bubble Tea v2) for running and orchestrating coding
agents. Each agent runs in its own tmux session hosted in a pseudo-terminal;
amux parses that terminal output with a built-in emulator, composes it with the
surrounding UI, and renders the result. Two binaries share the `internal`
packages: `cmd/amux` (the interactive app) and `cmd/amux-harness` (a headless
renderer used for deterministic perf and render testing).

For the runtime detail of the app package — lifecycle, PTY flow, tmux activity
tagging, and persistence invariants — see
[internal/app/ARCHITECTURE.md](internal/app/ARCHITECTURE.md). For the message
boundaries and command discipline between the app pump and the panes, see
[internal/app/MESSAGE_FLOW.md](internal/app/MESSAGE_FLOW.md). For the
streaming/scrolling model — flush pipeline, DEC 2026 frame atomicity, viewport
anchoring, and drag auto-scroll — see
[internal/ui/center/SCROLLING.md](internal/ui/center/SCROLLING.md).

## Dependency direction

Dependencies point downward. Most UI code reaches OS and process boundaries
through the tmux/pty/git/data layers. UI packages may still own
interaction-local adapters when the behavior is part of a widget boundary, such
as clipboard writes and file-picker filesystem reads in `internal/ui/common`,
per-tab PTY tracing in `internal/ui/center`, and shell plumbing in the sidebar
terminal. Shared process, persistence, git, tmux, and PTY behavior — including the
shared PTY/tmux read-loop and session plumbing in `internal/ui/ptyio` — belongs in
the lower layers. The test for where a read belongs is *who else consumes the
data*: a leaf model may self-fetch data only it renders inside its own async
Cmd (diff hunks in `internal/ui/diff`, branch changes/ahead-behind in the
sidebar's branch mode), but shared data — git status is the example, owned by
the app's cache and refresh dedup — must be requested via a message
(`messages.GitStatusRequest`) so the app's single-writer path serves it.

```
            cmd/amux            cmd/amux-harness
                \                   /
                 v                 v
              internal/app  (message pump, services, tmux-activity lease)
                 |  \
                 |   `-- internal/ui/{dashboard, center, sidebar, diff}
                 |              |
                 |              v
                 |       internal/ui/{compositor, layout, common, ptyio, theme}
                 v              |
   internal/{tmux, pty, git,    v
     data, update, config,  internal/vterm   (terminal emulator)
     supervisor, process}
                 \              /
                  v            v
        internal/{safego, perf, logging, messages, validation}
```

### External render-engine coupling

`internal/ui/compositor` imports `github.com/charmbracelet/ultraviolet`
directly on the core render path (`vtermlayer.go`, `pool.go`,
`canvas_drawable.go`). ultraviolet has no semver releases; its go.mod
pseudo-version is whatever `charm.land/bubbletea/v2` requires (MVS picks
bubbletea's pin). Keep the pin in lockstep with bubbletea: upgrade bubbletea
and let `go mod tidy` pull the matching ultraviolet — never bump ultraviolet
on its own, because a mismatched pair can break rendering with no compiler
warning.

## Packages

The table is hand-maintained; keep it in sync when adding or moving a package.

| Package | Responsibility | Entry points |
|---------|----------------|--------------|
| `cmd/amux` | App entrypoint: flag parsing, terminal setup, tmux socket janitor | `main.go` |
| `cmd/amux-harness` | Headless render/perf harness (no TTY) for CI and local profiling | `main.go` |
| `internal/app` | Bubble Tea root: message pump, orchestration, layout, tmux-activity leader lease | `app_core.go`, `app_init.go` |
| `internal/app/activity` | Agent-activity detection logic and per-session lease state | `logic.go`, `types.go` |
| `internal/app/workspacesvc` | Workspace service: create/delete/shelve workspaces, project load/prune, persistence + script state, path helpers. App-specific seams (mutation-in-flight checks, tmux cleanup) arrive via `Deps` callbacks — no import back into `internal/app` | `workspace_service.go`, `services.go` |
| `internal/ui/center` | Center pane: agent tab strip, per-tab PTY I/O, diff viewer, selection | `model.go`, `tab_actor.go` |
| `internal/ui/sidebar` | Sidebar pane: workspace file tree + embedded tmux terminal | `terminal.go` |
| `internal/ui/dashboard` | Dashboard pane: project/workspace tree and toolbar | `model.go` |
| `internal/ui/diff` | Scrollable, syntax-aware git diff viewer (a center tab) | `model.go` |
| `internal/ui/compositor` | Composes vterm snapshots + UI layers into a frame; delta ANSI | `canvas.go` |
| `internal/ui/layout` | Pane geometry and layout modes | `manager.go` |
| `internal/ui/common` | Shared widgets (dialogs, file picker), selection, clipboard; re-exports theme | `dialog.go`, `theme_reexport.go` |
| `internal/ui/ptyio` | Shared PTY/tmux plumbing: read loop, output filtering/trimming, flush/chunk tuning consts, session bootstrap/restore | `doc.go`, `pty_reader.go`, `tuning.go` |
| `internal/ui/theme` | Color palette, theme registry, icons, and lipgloss styles | `colors.go`, `theme.go`, `icons.go` |
| `internal/vterm` | Terminal emulator: ANSI/VT parsing → cell grid + scrollback → ANSI | `vterm.go` |
| `internal/tmux` | tmux CLI wrapper: sessions, capture, resize, activity tags | `tmux.go` |
| `internal/pty` | Pseudo-terminals backing hosted agents (Agent, Terminal) | `agent.go` |
| `internal/git` | git worktree-per-workspace model: worktrees, branches, diff, watcher | `operations.go`, `workspace.go` |
| `internal/data` | Persistence + shared records: workspace records via WorkspaceStore, project registry (lockfile, recovery), AgentState enum, canonical path helpers | `workspace_store.go`, `registry.go` |
| `internal/fsatomic` | Crash-safe single-file writes: temp-write, fsync, atomic rename-over (with .bak restore on Windows) | `fsatomic.go` |
| `internal/update` | Self-update: version check, download, verify, install | `updater.go` |
| `internal/config` | Configuration: assistants, UI settings, resolved paths | `config.go` |
| `internal/supervisor` | Named background workers with restart/backoff and error surfacing | `supervisor.go` |
| `internal/process` | Agent process lifecycle: process-group teardown, `AMUX_*` env injection, per-workspace port allocation, project run-script model + trust hashing | `treekill_unix.go`, `env.go`, `scripts.go` |
| `internal/safego` | Panic-safe goroutine helpers with a pluggable panic handler | `safego.go` |
| `internal/pprofhttp` | Opt-in pprof HTTP server wiring with explicit mux and timeouts | `server.go` |
| `internal/perf` | Opt-in counters/timers for the harness and perf baselines | `perf.go` |
| `internal/logging` | File-based logger; the output channel for internal packages | `logger.go` |
| `internal/messages` | Shared Bubble Tea message vocabulary between pump and panes | `messages.go` |
| `internal/validation` | Input/path guards (assistant, base ref, project path, workspace) | `validation.go` |
| `internal/shellutil` | Shared shell-quoting primitive (POSIX single-quote escaping) | `shellutil.go` |
| `internal/testutil` | Shared test polling helpers (deadline/poll loops with consistent failure messaging) | `wait.go` |
| `internal/e2e` | PTY-driven end-to-end tests exercising the real binary | (tests) |
