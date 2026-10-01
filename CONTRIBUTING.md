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
`./.cache/bin`; a stock `golangci-lint` from `PATH` may be a different version
than the pin and produce different diagnostics. See [LINTING.md](LINTING.md) for the
full rationale.

The minimum supported Go family is **1.26** (the `go` directive in `go.mod`).
The `toolchain` directive in `go.mod` pins the patched Go 1.26 toolchain used
for local checks and releases. With the standard `GOTOOLCHAIN=auto`
setting, the `go` command switches to that patched toolchain automatically. If
you force `GOTOOLCHAIN=local`, install the pinned patch release yourself before
running repo checks.

Run the fast local checks:

```bash
make devcheck
```

`make devcheck` is the required pre-PR gate: it runs vet, tests, and lint (including file-length checks). This project has **no GitHub Actions** — CI is entirely local. The complete gate is:

```bash
make ci
```

`make ci` runs `devcheck` under `STRICT_TMUX=1` (a real-tmux test skip fails instead of passing silently) plus `make test-race` (race sweep over the shared package set), `make test-race-tmux` (race over the real-tmux packages), `make tidy-check` (`go mod tidy` cleanliness), `make govulncheck` (vulnerability scan with the pinned govulncheck), `make windows-build` (a cross-compile smoke only — Windows is a compile-checked non-tier, not a supported platform: tmux doesn't exist there outside WSL, so no artifacts ship and no test runner exists; the build leg exists to keep POSIX-only imports honest), and `make harness-smoke` (quick render asserts for all three presets). It exercises whichever tmux is installed locally.

For the tmux **version matrix** — ubuntu-22.04's apt tmux (3.2a floor) plus a sha256-pinned from-source build — run it in docker:

```bash
make ci-tmux-matrix
```

For the former nightly workload (full-tree race run plus the soak test):

```bash
make ci-nightly
```

For the inner loop, launch the TUI with `make run` in a real terminal — amux requires stdin, stdout, and stderr to all be TTYs, so it only runs directly in your terminal. `air` cannot host the TUI: it launches the rebuilt binary with stdin on `/dev/null`, which fails that TTY check, so `make dev` is not a hot-reload TUI loop. Use it instead for automatic rebuilds and compile-error feedback while you edit — run `make dev` in a second pane alongside `make run`. It runs [`air`](https://github.com/air-verse/air) with the repo's `.air.toml` and rebuilds on save. Install it once with the pinned version from the Makefile (`AIR_VERSION`, shown in `make dev`'s missing-tool hint):

```bash
go install "github.com/air-verse/air@$(sed -n 's/^AIR_VERSION ?= //p' Makefile)"
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

Pull requests are gated by the local gates above plus the git hooks — nothing runs server-side. For local confidence before opening a PR:

- always: `make devcheck`, `make lint-strict-new`
- if touching concurrency (supervisor workers, PTY read loops, watchers, activity leases, anything with goroutines/channels/mutexes): `make test-race` — `make devcheck` does not run the race detector, so this is the most common local-pass/late-fail surprise
- after any dependency change (adding/removing an import, editing `go.mod`): `make tidy-check`
- before opening a PR you want green end-to-end: `make ci` (the full local CI gate; slow)
- if touching `internal/ui/`, `internal/vterm/`, or `cmd/amux-harness/`: `make harness-presets`
- if touching `internal/tmux/`, `internal/e2e/`, or `internal/pty/`: `go test ./internal/tmux ./internal/e2e`
- for race coverage on the real-tmux packages (`test_pkgs.sh` excludes them from `make test-race`): `make test-race-tmux` — skips cleanly without tmux; `make ci` runs it too
- before landing PTY-ingest or render-pipeline changes: `make soak` — runs `TestSoakHarnessPTY` (a `soak`-build-tagged sustained synthetic-PTY workload through the app, exercising the message pump under load for 5m by default; `AMUX_SOAK_DURATION=2m` or `AMUX_SOAK_MINUTES=10` to adjust). Also part of `make ci-nightly`
- after touching `internal/vterm` parsing or `internal/git` porcelain parsing: `make fuzz` — runs the three fuzz targets (`FuzzANSIParser`, `FuzzRenderInvariant`, `FuzzParseStatusPorcelain`) for `FUZZ_TIME` each (default 30s). Without it the corpus never mutates — the seeds alone run inside `make test`. Also part of `make ci-nightly`
- if touching the agent input/send path (`internal/pty/terminal.go`, `internal/ui/center/tab_actor_write.go`, `internal/pty/`, agent keystroke forwarding): `make verify-loop` — proves a real agent receives keystrokes end-to-end (incl. a literal CR); `make devcheck` does not, since the real-tmux tests skip there

Merging PRs: with no server-side CI, the hook gates run on the committer's machine — a web-UI squash merge runs *none* of them. For dependabot (grouped gomod bumps) and external-contributor PRs, validate locally before merging: `gh pr checkout N`, `make ci`, then merge. Squash-merging a dependency bump without a local pass can land a broken `go.mod` on `main`.

Dev-side environment variables:

- `STRICT_TMUX=1` — set on hosts where tmux is expected to work (`make ci`
  sets it automatically): turns `make tmux-skip-check`'s skip count from a
  non-fatal NOTE into a failure, so real-tmux tests can never silently skip.
- `AMUX_SOAK_DURATION` / `AMUX_SOAK_MINUTES` — soak-test duration knobs (see the
  `make soak` bullet above).
- `AMUX_E2E_BIN` — point `internal/e2e` tests at a prebuilt binary instead of
  the per-run build.
- `AMUX_SKIP_LINT=1` — skip only the golangci-lint steps in the pre-commit
  and pre-push hooks (`make lint` / `make lint-strict-base`). Every other gate
  still runs: formatting, lint-config-drift, check-fmt-config, the staged
  file-length guard, the harness, and the e2e suite. Scoped escape hatch —
  prefer it over `--no-verify`, which would also disable all of those.
- `AMUX_SKIP_HARNESS=1` — skip only the pre-push harness run; strict lint,
  the e2e suite, and the missing-tmux warning still apply.
- `AMUX_SKIP_RACE=1` — skip only the pre-push race smoke (`go test -race` on
  the data-flocking, ptyio, and msgpump race tests); strict lint, the
  harness, and the e2e suite still apply.
- `AMUX_LINT_BASE_REF=<ref>` — override the pre-push strict-lint base ref
  (default `origin/main`).
- `AMUX_HARNESS_CENTER_ARGS="..."` — override the pre-push harness args
  (default `--mode=center --tabs=8 --frames=200 --warmup=20 --hot-tabs=1
  --payload-bytes=128`).

E2e fixture isolation: `StartPTYSession` pins `HOME` *and*
`AMUX_WORKSPACES_ROOT` under the test's temporary home, so a parent shell
running inside amux cannot redirect fixture worktrees into a real workspace
root. Tests that deliberately relocate the root may pass an explicit
`PTYOptions.Env` override — it wins last — but must point it at a test-owned
temporary directory.

`make doctor` probes the host for everything the above needs: go ≥ the go.mod
floor, git, a working tmux server, the pre-commit hooks path, and lint tools.

Architecture references:

- `ARCHITECTURE.md` (repo-level package map and dependency direction)
- `internal/app/ARCHITECTURE.md`
- `internal/app/MESSAGE_FLOW.md`

### Harness

`cmd/amux-harness` renders the real UI without a TTY for deterministic perf and
render checks. `make harness-presets` runs heavier local confidence presets for
center/sidebar/monitor. `make harness-smoke` (part of `make ci`) uses shorter
direct invocations, e.g. center:

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
`dialog` (confirm dialog), `settings` (settings dialog), `prefix` (prefix
command palette), `error` (the error overlay), and `input` (input dialog). The
file picker (reads the real filesystem) and the toast (wall-clock-gated
visibility) are intentionally excluded because their frames are not byte-stable. Each overlay has a golden frame
(`internal/app/testdata/golden/overlay_*.frame`) guarded by
`TestHarnessGoldenFrames`; regenerate after an intentional overlay render change
with `go test ./internal/app -run Golden -update` and commit the refreshed
`testdata`.

See `go doc ./cmd/amux-harness` for all `-mode` values, flags, and the
`AMUX_PPROF` profiling hook.

## Release

Versioning follows SemVer and tags are `vX.Y.Z`. There is no automated release job — releases are built and published locally. `make release` runs `release-check` (the full `make ci` gate plus a fail-closed release-toolchain preflight and `goreleaser check`), tags, and pushes the tag; pushing a tag does **not** trigger anything. To publish artifacts, run `goreleaser release --clean` yourself with `GITHUB_TOKEN` (for the GitHub Release) and `MINISIGN_SECRET_KEY_FILE` (a local file holding the signing key — never commit it) set in the environment.

The release toolchain is pinned: goreleaser's major version is pinned by `.goreleaser-version` — install it with `go install github.com/goreleaser/goreleaser/v2@$(cat .goreleaser-version)` — and minisign by `scripts/install_minisign.sh` (version + sha256-verified download; supports linux tarballs and the macOS arm64 zip — on intel macs or other OSes, `brew install minisign` works too since `release-check` only needs the binary). `release-check` fails without either tool or the two env vars, and on a goreleaser major-version mismatch.

All file-pinned tools (`.golangci-version`, `.goreleaser-version`, `GOVULNCHECK_VERSION`, `AIR_VERSION` in the Makefile, `MINISIGN_VERSION` in `scripts/install_minisign.sh`) are reported on by `scripts/check_tool_versions.sh`, which `make ci-nightly` runs last — it prints `NOTE tool: pinned X, latest Y` lines and never fails; stale pins are a prompt to bump deliberately, not a gate.

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
