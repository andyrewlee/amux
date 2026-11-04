# amux skill — driving amux from inside a hosted pane

You are running inside an amux-hosted terminal (`AMUX_ENV=1`). amux is a
TUI orchestrator for parallel coding agents, each in an isolated git
worktree. The verbs below are the supported headless surface; everything
else is the tmux seam (last section).

## Environment

- `AMUX_ENV=1` — you are in an amux pane.
- `AMUX_BIN_PATH` — absolute path to the amux binary. Prefer
  `"$AMUX_BIN_PATH"` over `amux`: amux may not be on your `PATH`.
- `AMUX_WORKSPACE_NAME`, `AMUX_WORKSPACE_ROOT`, `AMUX_WORKSPACE_BRANCH` —
  your workspace's name, checkout root, and branch.
- `ROOT_WORKSPACE_PATH` — the repository's main checkout.

## Report your status (Program Status Protocol, OSC 7501)

```sh
status() {
  printf '\e]7501;state=%s:msg=%s\e\\' "$1" "$(printf '%s' "$2" | base64 | tr -d '\n')"
}
status working "Refactoring the parser"
status blocked "Which migration should I generate?"
status done "All tests green"
```

Or the validated form (also from any script, not just your pane):

```sh
"$AMUX_BIN_PATH" report --state blocked --kind question --app my-tool --message "needs a decision"
"$AMUX_BIN_PATH" report --clear
```

States: `idle`, `working`, `done`, `blocked`, `error`. `blocked` accepts
`--kind permission|question|auth`. `working`/`blocked` end at your next
shell prompt or process exit; `done`/`error` survive until you report
again, exit, or clear.

## See the fleet

```sh
"$AMUX_BIN_PATH" agent ls            # workspace  type  state  session
"$AMUX_BIN_PATH" agent ls --json     # one object per session
```

State is your siblings' reported state when they report, else amux's
scan-derived state, else `-`.

## Wait on a sibling

```sh
"$AMUX_BIN_PATH" wait --state blocked,error,done [--workspace NAME] [--timeout 30s]
```

Exits 0 on the first match (prints `session=… state=… kind=… app=…
message=…`), 1 if tmux itself is unusable, 2 on timeout or a usage error.
A stored `working`/`blocked` report from a pane whose process has exited
never matches.

## Send input to a session

```sh
"$AMUX_BIN_PATH" send --workspace NAME --text "review the diff please" --enter
"$AMUX_BIN_PATH" send --session NAME --keys escape,up,cr
```

`--enter` submits with a literal carriage return (raw-mode-agent safe) as
a separate, delayed command — the same discipline amux uses internally.
`--text` sends bytes verbatim (leading dashes included); `--keys` sends
named keys (`cr escape space tab up down left right home end pgup pgdn
bspace delete insert f1..f12 c-x s-x m-x` — only names tmux resolves;
`cr` is the raw submit byte). Exactly one live session must match the
target; ambiguity is an error that lists the candidates, and a session
whose pane process has exited is refused — input would have no reader.

## The tmux seam (advanced)

Every hosted session carries `@amux_*` tags and lives on the amux tmux
server. The full external contract — session-name grammar, tag keys,
input delivery rules, lifetimes — is `docs/ORCHESTRATION.md` in the amux
repository. Prefer the verbs above; they encode the contract's rules for
you.
