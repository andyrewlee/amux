# Configuration

amux reads a single user config file at `~/.amux/config.json` (optional —
missing or malformed sections fall back to defaults). This document covers
each config section and the environment variables amux honors.

## The `ui` section

```json
{
  "ui": {
    "show_keymap_hints": true,
    "theme": "gruvbox",
    "tmux_server": "amux",
    "tmux_config": "/path/to/tmux.conf",
    "tmux_sync_interval": "30s",
    "notify_on_done": true,
    "viewer_command": "nvim"
  }
}
```

| JSON key             | Type   | Meaning                                                                        |
|----------------------|--------|--------------------------------------------------------------------------------|
| `show_keymap_hints`  | bool   | Show keybinding hints in the UI. Default `false`.                              |
| `theme`              | string | Theme ID. Default `"gruvbox"`.                                                 |
| `tmux_server`        | string | tmux server (`-L` socket) name for amux's sessions. Empty = tmux default.       |
| `tmux_config`        | string | Path to a tmux config file (`-f`). Empty = tmux default.                       |
| `tmux_sync_interval` | string | Duration between tmux session reconciliations (e.g. `"30s"`). Default `7s`; minimum `500ms` — lower values are clamped up. |
| `notify_on_done`     | bool   | Ring the terminal bell when an agent finishes. Default `false`.                 |
| `viewer_command`     | string | Shell command the file viewer tab runs as `<command> -- <file>` (e.g. `nvim`, `less -R`). Default `"vim"`. Expects a TUI program — a GUI command opens nothing visible in the pane. |

The tmux keys map to environment variables of the same purpose —
`AMUX_TMUX_SERVER`, `AMUX_TMUX_CONFIG`, `AMUX_TMUX_SYNC_INTERVAL` — which amux
also accepts directly. A non-empty config value wins over the environment.

## Environment variables

Variables injected **into** agents (`AMUX_WORKSPACE_*`, `AMUX_PORT`,
`AMUX_PORT_RANGE`) and the debugging surface (`AMUX_LOG_LEVEL`,
`AMUX_PTY_TRACE`, `AMUX_PPROF`, `AMUX_DEBUG_SIGNALS`, `AMUX_PROFILE*`,
`AMUX_ENABLE_OSC52_CLIPBOARD`, `AMUX_MAX_ATTACHED_*`, `AMUX_ALLOW_GIT_HOOKS`)
are documented in the README's environment section, which also covers the
custom env layering — repo `env` (trust-gated, **scripts only**) < project
env (`~/.amux/project-env.json`, press `E`) < workspace env (press `e`).
Interactive sessions (agents, sidebar terminals) get the injected vars plus
the project and workspace layers — never repo `env`, even when trusted.
Workspace env edits persist as a field-scoped transaction on
`workspace.json`: only the `env` map is rewritten, so an `e` edit cannot
revert a rename, script change, or tab save committed in the meantime. The
rest:

| Variable                  | Meaning                                                                                    |
|---------------------------|--------------------------------------------------------------------------------------------|
| `AMUX_WORKSPACES_ROOT`    | Relocate the workspace worktree root (default `~/.amux/workspaces`). The only knob for it — there is no config key. amux re-exports the resolved value under the same name so internal packages see one consistent root. |
| `AMUX_TMUX_SERVER`        | Same as `ui.tmux_server`.                                                                  |
| `AMUX_TMUX_CONFIG`        | Same as `ui.tmux_config`.                                                                  |
| `AMUX_TMUX_SYNC_INTERVAL` | Same as `ui.tmux_sync_interval`.                                                           |
| `AMUX_LOG_RETENTION_DAYS` | Days of `~/.amux/logs` retention. Default 14.                                              |
| `AMUX_PPROF_ALLOW_REMOTE` | With `AMUX_PPROF` set, bind pprof on all interfaces instead of loopback. Off by default because pprof endpoints expose internals — set `=1` only on trusted networks. |
| `AMUX_PERF_LOG_DIR`       | Directory for perf snapshot output (harness/CI use).                                       |
| `AMUX_E2E_BIN`            | Path to a prebuilt binary for `internal/e2e` tests (test-only).                            |

## The `assistants` map

amux ships a built-in roster of AI coding agents, but you are not limited to it.
The user config file lets you **override a built-in's launch command** or **add
a brand-new assistant** (for example a company-internal CLI or a tool amux does
not know about yet). This section describes that `assistants` config.

## Where the config lives

amux reads a single user config file at:

```
~/.amux/config.json
```

The file is optional. A missing file, malformed JSON, or a broken `assistants`
section falls back to the built-in defaults; a valid `assistants` section is
merged on top of them. (This is per-user global config, distinct from the
per-project `.amux/workspaces.json` described in the README — whose lifecycle
scripts run under amux's teardown ordering: delete/shelve cancels and drains
in-flight setup and on-done hooks, stops the run script, then runs `archive`
before the worktree is removed.)

Loading is tolerant, but **saving is strict**: when amux persists a section
(UI settings or assistants) it first reads the existing document and refuses
to write if that document is unreadable, malformed, or a non-object root such
as a top-level `null`. A save that proceeded on a partial view could silently
drop sections it does not own, so the error is surfaced and the file is left
untouched instead. A missing or empty file is still treated as an empty
config and written normally — missing is not the same as rejected.

## The `assistants` schema

The config schema has an `assistants` object. Each **key** is the assistant
name; each **value** overrides that assistant's launch settings:

```json
{
  "assistants": {
    "mytool": { "command": "mytool --interactive", "interrupt_count": 2, "interrupt_delay_ms": 100 }
  }
}
```

The value fields (all optional) are:

| JSON key             | Type   | Meaning                                                              |
|----------------------|--------|---------------------------------------------------------------------|
| `command`            | string | Shell command amux runs to launch the assistant.                    |
| `interrupt_count`    | number | Number of Ctrl-C signals amux sends to interrupt the agent.         |
| `interrupt_delay_ms` | number | Delay, in milliseconds, between those Ctrl-C signals.               |

Defaults applied when a value is kept: `interrupt_count` falls back to `1` if it
is missing or not positive, and `interrupt_delay_ms` falls back to `0` if it is
missing or negative.

Assistant names must start with a letter or number and may contain only letters,
numbers, dots, dashes, or underscores (max 100 characters). Names are matched
case-insensitively (they are lowercased). An entry whose name fails validation
is ignored.

Settings changes apply to launches requested after the save. A launch already
dispatched — a new tab, a placeholder restore, a reattach, or a restart — runs
the `command`/interrupt values captured when it was requested, so editing an
assistant mid-launch never changes what that launch runs. Attaching to an
existing tmux session never re-runs the pane's command either: reattach binds
to the session as it is.

## Adding a custom assistant

The fastest path is in-app: open **Settings**, Tab to **Assistants**, and press
**Ctrl+A** to add a name and command (the same validation below applies, and
interrupt fields default sensibly). To do it in the file instead, give the new
key a **non-empty `command`** — that is the only requirement:

```json
{
  "assistants": {
    "mytool": { "command": "mytool --interactive" }
  }
}
```

After this, `mytool`:

- **appears in the assistant picker** (the agent-selection dialog), listed after
  the built-in agents — arrows/`tab`/`shift+tab` move through the list and any
  printable key narrows it as a fuzzy filter;
- is **treated as a chat agent**, exactly like the built-ins.

A custom entry **without** a `command` is dropped (there would be nothing to
launch), so always include one for a new name.

### Caveat: custom assistants have no brand color

The built-in agents each render with a dedicated brand color. A custom
(non-built-in) assistant does **not** get one — it falls back to the default
primary color. This is purely cosmetic; the assistant is fully functional
otherwise.

## Overriding a built-in's command

The same map overrides the built-in agents. For a built-in you only need to set
the field(s) you want to change — the command keeps its built-in default unless
you provide one. For example, to launch `claude` through a wrapper while keeping
its interrupt behavior:

```json
{
  "assistants": {
    "claude": { "command": "my-claude-wrapper" }
  }
}
```

The built-in roster (default names) is: `claude`, `codex`, `opencode`, `droid`,
`cursor`, `pi`, `omp`, `antigravity`, `fx`, `grok`, `amp`, `cline`, `devin`,
`prime-agent`.
