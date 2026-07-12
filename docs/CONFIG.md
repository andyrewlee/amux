# Configuration: assistants

amux ships a built-in roster of AI coding agents, but you are not limited to it.
The user config file lets you **override a built-in's launch command** or **add
a brand-new assistant** (for example a company-internal CLI or a tool amux does
not know about yet). This document describes that `assistants` config.

## Where the config lives

amux reads a single user config file at:

```
~/.amux/config.json
```

The file is optional. A missing file, malformed JSON, or a broken `assistants`
section falls back to the built-in defaults; a valid `assistants` section is
merged on top of them. (This is per-user global config, distinct from the
per-project `.amux/workspaces.json` described in the README.)

## The `assistants` map

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

## Adding a custom assistant

Give the new key a **non-empty `command`**. That is the only requirement — a new
name with a command becomes a real, usable assistant:

```json
{
  "assistants": {
    "mytool": { "command": "mytool --interactive" }
  }
}
```

After this, `mytool`:

- **appears in the assistant picker** (the agent-selection dialog), listed after
  the built-in agents;
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

The built-in roster (default names) is: `claude`, `codex`, `gemini`, `amp`,
`opencode`, `droid`, `cline`, `cursor`, `pi`.
