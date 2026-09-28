#!/usr/bin/env bash
# Single source for the "no real tmux required" test package set shared by
# .github/workflows/ci.yml (Test / Test (race) / macos-build steps), the
# Makefile's test-race target, and — via --exclude-app — `make
# test`/`make devcheck`.
#
# Bare output excludes internal/tmux, internal/e2e, and internal/pty, which
# need a real tmux server (they run in the tmux-e2e CI job / tmux-skip-check).
# --exclude-app additionally drops internal/app: the local sweep defers that
# package to tmux-skip-check, while CI covers it in the main test job.
#
# Race coverage for the excluded real-tmux packages (and the real-tmux tests
# inside app) lives in `make test-race-tmux` and the tmux-e2e CI job —
# keep any new real-tmux package in BOTH the default and --exclude-app sets.
set -euo pipefail

filter='/internal/(tmux|e2e|pty)$'
case "${1:-}" in
	"")
		;;
	--exclude-app)
		filter='/internal/(tmux|e2e|app|pty)$'
		;;
	*)
		echo "usage: $0 [--exclude-app]" >&2
		exit 2
		;;
esac

go list ./... | grep -v -E "$filter"
