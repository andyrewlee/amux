# External orchestration contract

This document specifies the supported way to drive amux agents from another
process. It is a behavioral contract: the naming, discovery, input, and state
conventions described here are what external orchestrators may rely on, and the
pinning test in `internal/tmux/session_contract_test.go` exists to force a
conversation (doc + release notes) whenever any of it changes.

## Status of this contract

amux has no command-line interface. The entire CLI was removed on purpose in
PR #204 (commit `c76dce7c`, "chore: remove unsupported CLI and OpenClaw"), which
deleted everything under `internal/cli/` and the `amux agent …` subcommands.
Since then the binary accepts no arguments except `--version`/`-v`; any other
argument prints an error and exits non-zero (`cmd/amux/main.go`). `amux` is a
terminal UI that must be launched directly on a real TTY.

External orchestration is nonetheless a real, current workflow: the maintainer
drives amux agents from outside the process. The **only** supported control
surface for that is the tmux layer underneath amux. amux hosts every agent in a
tmux session, tags those sessions with `@amux_*` options, and reattaches to them
across restarts. An external tool talks to the *same* tmux server, targeting
those sessions with stock tmux commands (`list-sessions`, `send-keys`,
`show-options`). This document is the contract for doing that.

Compatibility: the session-name grammar and the `@amux_*` tag keys below are
treated as a public seam. A change to `SessionName`, `sanitize`, or the tag keys
must update this document and call out the break in release notes. It is not an
internal refactor you can make silently — the pinning test will fail first.

## Session naming grammar

amux builds every session name with `tmux.SessionName(parts...)`
(`internal/tmux/tmux.go`). Each part is trimmed, dropped if empty, `sanitize`-d,
and the surviving parts are joined with `-`. If nothing survives, the name is
the bare string `amux`.

`sanitize` (same file) does, per part:

- lowercase the whole part;
- keep `a`–`z`, `0`–`9`, `-`, and `_` as-is;
- replace every other byte with `-`;
- trim leading and trailing `-`.

The call sites (grep `SessionName(` under `internal/`) all pass `"amux"` as the
first part, the workspace ID as the second, and a per-tab identifier as the
third:

```
amux-<workspaceID>-<tabPart>
```

- `<workspaceID>` is `data.Workspace.ID()`: `hex(sha256(repo+root)[:8])`
  (`internal/data/workspace.go`) — 16 lowercase hex characters, deterministic
  for a given repo+root, and **never contains `-`**. sanitize is a no-op on it.
- `<tabPart>` identifies the pane within the workspace. For an interactive agent
  tab it is the tab ID `tab-<prefix>-<counter>` (`internal/ui/center/model_tab.go`,
  `formatTabID`), where `<prefix>` is 4 random bytes hex-encoded **per amux
  process** and `<counter>` is a base-36 in-process counter. Viewer and other
  panes use different literals (e.g. `viewer`).

Worked example — workspace ID `9f8e7d6c5b4a3210`, tab ID `tab-1a2b3c4d-5`:

```
SessionName("amux", "9f8e7d6c5b4a3210", "tab-1a2b3c4d-5")
  => "amux-9f8e7d6c5b4a3210-tab-1a2b3c4d-5"
```

**Guaranteed** (relied on by amux itself and pinned by the contract test):

- Every amux session name begins with the literal prefix `amux-` (or is exactly
  `amux`).
- The workspace ID is the first `-`-delimited segment after `amux-` and is
  recovered by `activity.WorkspaceIDFromSessionName`
  (`internal/app/activity/fetch.go`): strip `amux-`, split on `-`, take the first
  field. This round-trips because workspace IDs are dashless hex — see the caveat
  below.

**Not guaranteed** — do not build these into an orchestrator:

- The internal structure or value of `<tabPart>`. Because the tab-ID prefix is
  randomized **per amux process**, tab session names are **not predictable and
  not stable across an amux restart for newly created tabs**. (An *existing*
  session keeps its name for as long as its tmux session lives, because amux
  reattaches by discovering the live session and reading its `@amux_tab` tag — it
  does not regenerate the name.) You cannot construct a tab's session name a
  priori; you must discover it (next section).
- Session ordering, or that any given session exists at all — agent sessions are
  created and killed over a workspace's life.
- Round-trip of a workspace-ID value that itself contains `-`.
  `WorkspaceIDFromSessionName` returns only the first segment, so a dash in the
  ID position would truncate. Real workspace IDs are dashless hex, so this does
  not occur in practice; it is pinned as a known caveat, not a bug to "fix" in
  the parser.

## Discovering sessions

Do not construct tab names; enumerate the live sessions on amux's tmux server
and filter by prefix and tags.

Resolve the server name the way amux does (`tmux.DefaultOptions`,
`internal/tmux/tmux.go`):

- server: `$AMUX_TMUX_SERVER` if set and non-empty, else `amux`;
- config: `$AMUX_TMUX_CONFIG` if set and non-empty, else `/dev/null`.

amux itself sets these env vars from config
(`UI.TmuxServer` / `UI.TmuxConfigPath`, `internal/app/app_init.go`), so an
orchestrator launched in the same environment as amux should honor whatever is
already set and only fall back to the defaults above.

List sessions and their tags in one call, mirroring amux's own reader
(`internal/tmux/tags.go`):

```sh
tmux -L "${AMUX_TMUX_SERVER:-amux}" list-sessions \
  -F '#{session_name}|#{@amux_workspace}|#{@amux_tab}|#{@amux_type}'
```

Keep the lines whose `session_name` starts with `amux-`. The `@amux_workspace`
tag is the authoritative workspace ID for a session (equivalently, parse it out
of the name per the grammar above).

## Sending input

amux does **not** use `tmux send-keys` internally — it attaches a PTY to each
session and writes bytes directly. An external orchestrator, which is not
attached, drives a session with `tmux send-keys` against the same server:

```sh
# Target the session by EXACT name. The '=' prefix stops tmux from
# prefix-matching e.g. "...-tab-1a2b3c4d-1" onto "...-tab-1a2b3c4d-10".
tmux -L "${AMUX_TMUX_SERVER:-amux}" send-keys -t '=amux-<ws>-<tabPart>' -l 'your text here'
```

The `=` exact-match prefix on the target is not optional: amux uses it for every
session-scoped `send-keys`/`has-session`/`kill-session` for exactly this reason
(`sessionTarget` in `internal/tmux/tmux.go`). (Note: tmux's `set-option` and
`show-options` reject the `=` prefix, so tag reads/writes use the bare name —
see `exactSessionOptionTarget` in the same file. Prefix collisions are still
avoided there because names carry both workspace and tab IDs.)

Two hard-won caveats apply when the target is a raw-mode agent (Claude Code,
Codex, and friends run in raw mode). amux earned these the hard way and pins
them with the close-the-loop e2e tests — `internal/e2e/closeloop_test.go` drives
a real keystroke through amux's input path into the raw-mode agent in
`internal/e2e/fakeagent`, asserting the bytes (including a literal carriage
return, `0x0D`) arrive intact:

1. **Submit with a literal carriage return, not the named `Enter` key.** A raw-mode
   TUI never sees the named key translated to `0x0D`. Send the CR explicitly:

   ```sh
   tmux -L "$srv" send-keys -t '=<session>' -H 0D
   ```

2. **Separate the text from the submit.** Send the line first, then the CR as a
   second command with a brief pause between them; a CR fused onto the end of the
   text can be lost before the agent is ready to read it. amux inserts a small
   delay between the keystrokes and the Enter in its own send path for the same
   reason.

## Reading state

amux stores per-session metadata as tmux session options (`@amux_*`). Read one
with `tmux show-options -t <bare-name> -v <key>` (bare name, no `=` — see above)
or read them all with `tmux show-options -t <bare-name>`. The keys, set at
session creation (`appendSessionTags`, `internal/tmux/command.go`) and updated at
runtime (`internal/tmux/tags.go`, `internal/ui/center/model_input_lifecycle.go`):

| Key | Meaning | Value format |
|-----|---------|--------------|
| `@amux` | marks the session as amux-managed | literal `1` |
| `@amux_workspace` | workspace ID | 16-char hex |
| `@amux_tab` | tab ID | `tab-<prefix>-<counter>` |
| `@amux_type` | session type | string (e.g. agent, viewer) |
| `@amux_assistant` | assistant/agent name | string |
| `@amux_created_at` | session creation time | Unix **seconds** |
| `@amux_instance` | amux instance ID | string |
| `@amux_session_owner` | owning amux instance | string |
| `@amux_session_lease_ms` | ownership lease timestamp | Unix **milliseconds** |
| `@amux_last_output_at` | last observed agent output | Unix **milliseconds** |
| `@amux_last_input_at` | last input amux delivered | Unix **milliseconds** |

Note the unit split: `@amux_created_at` is in seconds; the activity/lease
timestamps are in milliseconds. amux parses these back with
`activity.ParseLastOutputAtTag` (`internal/app/activity/fetch.go`). The owner and
lease tags coordinate ownership between concurrent amux instances; an external
orchestrator should treat them as read-only telemetry and not forge them.

## Option B: a minimal CLI (recorded, not recommended)

If the tmux contract above proves insufficient for a concrete orchestration need,
a minimal command surface — something like `amux send` and `amux ls` — could be
reintroduced. The deleted implementation is recoverable from `git show c76dce7c^`
(the parent of the removal commit), including the old `agent send` execute path
and session listing, to be trimmed down rather than rewritten. If this is ever
done, treat the "Sending input" and "Reading state" sections above as the
behavioral spec for the new commands so the two surfaces stay consistent.

This is **not** recommended by default. The CLI was removed precisely because a
second supported surface is a recurring cost: it needs its own tests, its own
docs, and its own backward-compatibility guarantees, in perpetuity, on top of the
TUI. The tmux seam already provides discovery, input, and state with zero extra
maintained surface. Revisit this only when a specific orchestrator requirement is
demonstrably unmet by tmux — name that requirement in the proposal — not merely
because a CLI would be more convenient than raw tmux commands.
