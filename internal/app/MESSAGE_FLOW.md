# Message Flow and Taxonomy

This document defines message boundaries used by the app and clarifies which
messages may originate outside the Bubble Tea update loop.

## Taxonomy

### External Messages

External messages are produced by goroutines, IO, or long-running commands.
They must enter the app through the external message pump, never by direct
state mutation.

Examples:
- PTY output (`center.PTYOutput`, `messages.SidebarPTYOutput`)
- File/state watcher events (`messages.FileWatcherEvent`, `messages.StateWatcherEvent`)
- Background supervisor errors (`messages.Error` from workers)
- tmux discovery/sync results

Rules:
- External messages are enqueued via `App.enqueueExternalMsg`.
- External messages never mutate state directly; they are handled in `Update`.

### Internal Messages

Internal messages are produced by UI interactions or by commands triggered
inside the update loop.

Examples:
- Key/mouse input
- Dialog results
- UI-only actions (focus changes, toggles, local commands)

Rules:
- Internal messages may be generated synchronously in Update.
- Long-running work must still be wrapped in a `tea.Cmd`.

## Command Discipline

- Anything that touches disk, runs external commands, or waits on IO belongs in
  a `tea.Cmd`.
- Update handlers should be quick state transitions plus command scheduling.
- If work might block, wrap it in a command and return a message.

## Error Reporting

- Use the central error helper in `App` to log + toast + emit `messages.Error`.
- `messages.Error` is handled in one place to keep error UX consistent.
