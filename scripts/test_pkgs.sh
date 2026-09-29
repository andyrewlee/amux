#!/usr/bin/env bash
# Single source for the "no real tmux required" test package set shared by
# `make test-race` and — via --exclude-app — `make test`/`make devcheck`.
#
# Bare output excludes internal/tmux, internal/e2e, and internal/pty, which
# need a real tmux server (they run via `make test-race-tmux`/
# tmux-skip-check). --exclude-app additionally drops internal/app: the
# no-tmux sweep defers that package to tmux-skip-check.
#
# Race coverage for the excluded real-tmux packages (and the real-tmux tests
# inside app) lives in `make test-race-tmux` —
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
