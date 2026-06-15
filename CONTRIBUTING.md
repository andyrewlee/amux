# Contributing to amux

## Development

```bash
git clone https://github.com/andyrewlee/amux.git
cd amux
./scripts/install-hooks.sh
make lint-tools   # one-time: builds the pinned golangci-lint into ./.cache/bin
make run
```

Run `make lint-tools` once before your first `make devcheck` or `git commit`.
It builds the linter version pinned in `.golangci-version` into the gitignored
`./.cache/bin`; a stock `golangci-lint` from `PATH` is likely the wrong major
version (v2.x) and fails confusingly against this repo's v1 config. See
[LINTING.md](LINTING.md) for the full rationale.

Run the local checks that mirror CI:

```bash
make devcheck
```

`make devcheck` is the required pre-PR gate: it runs vet, tests, and lint (including file-length checks).

For the hot-reload inner loop, `make dev` runs [`air`](https://github.com/air-verse/air) using the repo's `.air.toml`. Install it once with:

```bash
go install github.com/air-verse/air@latest
```

Ensure `$(go env GOPATH)/bin` is on your `PATH`; otherwise `make dev` prints this same install hint and exits.

For style-only cleanup, run:

```bash
make fmt
```

Before opening larger PRs, also run strict ratcheted lint on changed code:

```bash
make lint-strict-new
```

Pull requests are CI-gated (automated). For local confidence before opening a PR:

- always: `make devcheck`, `make lint-strict-new`
- if touching `internal/ui/`, `internal/vterm/`, or `cmd/amux-harness/`: `make harness-presets`
- if touching `internal/tmux/`, `internal/e2e/`, or `internal/pty/`: `go test ./internal/tmux ./internal/e2e`
- if touching the agent input/send path (`internal/tmux/send.go`, `internal/pty/`, agent keystroke forwarding): `make verify-loop` — proves a real agent receives keystrokes end-to-end (incl. a literal CR); `make devcheck` does not, since the real-tmux tests skip there

Architecture references:

- `ARCHITECTURE.md` (repo-level package map and dependency direction)
- `internal/app/ARCHITECTURE.md`
- `internal/app/MESSAGE_FLOW.md`

### Harness

`cmd/amux-harness` renders the real UI without a TTY for deterministic perf and
render checks. `make harness-presets` runs heavier local confidence presets for
center/sidebar/monitor. CI uses shorter direct invocations; to reproduce a CI
failure, run the matching mode with the CI shape, e.g. center:

```bash
go run ./cmd/amux-harness -mode center -frames 5 -warmup 1 -tabs 8 -width 160 -height 48 -hot-tabs 2 -payload-bytes 64 -newline-every 4
```

#### Inspecting a rendered frame

To see exactly what the UI rendered (instead of guessing), dump the final frame
with `-dump-frame`:

```bash
go run ./cmd/amux-harness -mode center -frames 1 -warmup 0 -dump-frame /tmp/frame.txt
```

The file contains the raw ANSI bytes the agent sees — `cat /tmp/frame.txt` to
eyeball it, `diff` two dumps to spot a regression, or feed it into a golden.

#### Rendering an overlay

Adding or altering a dialog/overlay is the most common UI change. The harness can
put the App into an overlay state so the frame exercises `composeOverlays`
instead of only the base pane. Pass `-overlay` (or set `HarnessOptions.Overlay`):

```bash
go run ./cmd/amux-harness -mode center -frames 1 -warmup 0 -overlay dialog -dump-frame /tmp/frame.txt
```

Supported overlays are the deterministic, filesystem-independent ones:
`dialog` (confirm dialog), `settings` (settings dialog), and `prefix` (prefix
command palette). The file picker (reads the real filesystem) and the toast
(wall-clock-gated visibility) are intentionally excluded because their frames are
not byte-stable. Each overlay has a golden frame
(`internal/app/testdata/golden/overlay_*.frame`) guarded by
`TestHarnessGoldenFrames`; regenerate after an intentional overlay render change
with `go test ./internal/app -run Golden -update` and commit the refreshed
`testdata`.

See `go doc ./cmd/amux-harness` for all `-mode` values, flags, and the
`AMUX_PPROF` profiling hook.

## Release

Versioning follows SemVer and tags are `vX.Y.Z`. Pushing a tag triggers the GitHub Actions release job.

Fast path:

```bash
git pull --ff-only
make release VERSION=v0.0.5
```

Manual steps:

```bash
make release-check
git tag -a v0.0.5 -m "v0.0.5"
git push origin v0.0.5
```

Notes:

- `make release` runs `release-check`, creates an annotated tag, and pushes it. The worktree must be clean.
- Release builds use the commit timestamp for `main.date`, which keeps the timestamp deterministic for a given commit. If you need strict bit-for-bit reproducibility, consider adding `-trimpath` and a stable build ID to the build flags.

### Homebrew tap

The Homebrew tap lives in `andyrewlee/homebrew-amux` and auto-bumps the formula after a release.

- After `make release VERSION=vX.Y.Z`, the tap workflow updates `Formula/amux.rb` (daily at 06:00 UTC).
- To update immediately, run the **Bump amux formula** workflow in the tap repo.
- Users upgrade with `brew upgrade amux`.
