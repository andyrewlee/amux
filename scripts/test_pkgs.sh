#!/usr/bin/env bash
# Single source for the "no real tmux required" test package set shared by
# .github/workflows/ci.yml (Test / Test (race) / macos-build steps) and the
# Makefile's test-race target. Excludes internal/tmux, internal/e2e, and
# internal/pty, which need a real tmux server (they run in the tmux-e2e CI
# job / tmux-skip-check).
#
# NOTE: `make test`/`make devcheck` deliberately use a wider filter that also
# excludes internal/app — tmux-skip-check runs that package separately. This
# script is the CI package set only; do not use it to replace the devcheck
# filter without accounting for the app-package split.
#
# Race coverage for the excluded real-tmux packages (and the real-tmux tests
# inside app) lives in `make test-race-tmux` and the tmux-e2e CI job —
# keep any new real-tmux package on BOTH lists.
set -euo pipefail

go list ./... | grep -v -E '/internal/(tmux|e2e|pty)$'
