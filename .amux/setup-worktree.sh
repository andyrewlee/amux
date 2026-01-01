#!/bin/sh
set -eu

wt="${AMUX_WORKTREE_ROOT:-}"
repo="${AMUX_REPO_ROOT:-}"

if [ -z "$wt" ] || [ -z "$repo" ]; then
  echo "AMUX_WORKTREE_ROOT or AMUX_REPO_ROOT not set" >&2
  exit 1
fi

# Create redirect to main repo's .beads
mkdir -p "$wt/.beads"
redirect="$wt/.beads/redirect"
if [ ! -f "$redirect" ]; then
  printf "%s\n" "$repo/.beads" > "$redirect"
fi

# Locally ignore redirect file to avoid untracked noise
if gitdir=$(git -C "$wt" rev-parse --git-dir 2>/dev/null); then
  exclude="$gitdir/info/exclude"
  mkdir -p "$(dirname "$exclude")"
  touch "$exclude"
  grep -qxF ".beads/redirect" "$exclude" || printf "\n.beads/redirect\n" >> "$exclude"
fi
