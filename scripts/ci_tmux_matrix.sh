#!/usr/bin/env bash
# Local replacement for the deleted GitHub Actions tmux-e2e matrix job: runs
# `go test -race` over the real-tmux packages plus `STRICT_TMUX=1 make
# tmux-skip-check` inside Linux containers, once per tmux version in MATRIX.
#
#   apt  — ubuntu-22.04's stock tmux (3.2a): the distro floor. Catches reliance
#          on newer tmux features (this leg caught pane_dead_status, which does
#          not exist before tmux 3.3).
#   3.6a — from-source build of a sha256-pinned tarball: catches bleeding-edge
#          regressions the floor can't see.
#
# To extend the matrix, append the version to MATRIX and add its tarball hash
# to tmux_sha (`shasum -a 256 tmux-<ver>.tar.gz`). Requires a running docker
# daemon; images are built once and cached, go build/module caches persist in
# the amux-tmux-matrix-* volumes.
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v docker >/dev/null 2>&1; then
  echo "ci-tmux-matrix: docker is required (not on PATH)" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "ci-tmux-matrix: docker daemon is not running" >&2
  exit 1
fi

GO_VERSION="$(awk '/^toolchain go/{sub(/^go/, "", $2); print $2; exit}' go.mod)"
GO_VERSION="${GO_VERSION:-$(awk '/^go [0-9]/{print $2; exit}' go.mod)}"
[ -n "$GO_VERSION" ] || { echo "ci-tmux-matrix: could not read Go version from go.mod" >&2; exit 1; }

case "$(uname -m)" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64)  ARCH=amd64 ;;
  *) echo "ci-tmux-matrix: unsupported arch $(uname -m)" >&2; exit 1 ;;
esac

tmux_sha() {
  case "$1" in
    3.6a) echo "b6d8d9c76585db8ef5fa00d4931902fa4b8cbe8166f528f44fc403961a3f3759" ;;
    *) return 1 ;;
  esac
}

MATRIX=(apt 3.6a)
fail=0
for leg in "${MATRIX[@]}"; do
  echo "=== tmux matrix leg: $leg ==="
  args=(--build-arg "GO_VERSION=$GO_VERSION" --build-arg "TARGETARCH=$ARCH"
        --build-arg "TMUX=$leg")
  if [ "$leg" != apt ]; then
    sha="$(tmux_sha "$leg")" || { echo "ci-tmux-matrix: no pinned sha256 for tmux $leg" >&2; exit 1; }
    args+=(--build-arg "TMUX_SHA256=$sha")
  fi
  docker build "${args[@]}" -f scripts/ci/tmux-matrix.Dockerfile \
    -t "amux-tmux-matrix:$leg" scripts/ci

  # Repo mounts read-only; git marks it dubious-ownership under the container
  # root user, so safe.directory it for go vcs stamping and any git-driven
  # tests. STRICT_TMUX fails the run if any real-tmux test skips.
  # --init gives the container a real PID 1 that reaps orphaned grandchildren
  # (CI hosts have one; without it, killed tmux panes leave zombie members so
  # the process-group checks in TestKillSession_* see a "still alive" group).
  # LANG/LC_ALL come from the image: without a UTF-8 locale tmux sanitizes
  # control bytes in format output (\x1f separators → "_"), breaking parsers.
  if ! docker run --rm --init \
      -v "$PWD":/src:ro -w /src \
      -v amux-tmux-matrix-gobuild:/cache/go-build \
      -v amux-tmux-matrix-gomod:/go/pkg/mod \
      -e STRICT_TMUX=1 \
      "amux-tmux-matrix:$leg" \
      bash -euc '
        git config --global --add safe.directory /src
        tmux -V
        go test -race ./internal/tmux -count=1 &&
        go test -race ./internal/e2e -count=1 &&
        go test -race ./internal/app -count=1 &&
        go test -race ./internal/pty -count=1 &&
        make tmux-skip-check'; then
    echo "=== leg $leg FAILED ===" >&2
    fail=1
  fi
done
exit "$fail"
