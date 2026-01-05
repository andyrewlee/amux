#!/bin/sh
set -eu

wt="${AMUX_WORKTREE_ROOT:-}"
repo="${ROOT_WORKTREE_PATH:-}"

if [ -z "$wt" ] || [ -z "$repo" ]; then
  echo "AMUX_WORKTREE_ROOT or ROOT_WORKTREE_PATH not set" >&2
  exit 1
fi

# Only configure beads if the main repo already uses it
if [ ! -d "$repo/.beads" ]; then
  exit 0
fi

# Create redirect to main repo's .beads
mkdir -p "$wt/.beads"
redirect="$wt/.beads/redirect"
if [ ! -f "$redirect" ]; then
  printf "%s\n" "$repo/.beads" > "$redirect"
fi

# Install beads hooks and import JSONL in the worktree when available
if command -v bd >/dev/null 2>&1 && git -C "$wt" rev-parse --git-dir >/dev/null 2>&1; then
  (cd "$wt" && bd hooks install) || true
  (cd "$wt" && bd sync --import-only) || true
fi

# Locally ignore redirect file to avoid untracked noise
if gitdir=$(git -C "$wt" rev-parse --git-dir 2>/dev/null); then
  exclude="$gitdir/info/exclude"
  mkdir -p "$(dirname "$exclude")"
  touch "$exclude"
  grep -qxF ".beads/redirect" "$exclude" || printf "\n.beads/redirect\n" >> "$exclude"
  if ! git -C "$wt" ls-files --error-unmatch ".beads/issues.jsonl" >/dev/null 2>&1; then
    grep -qxF ".beads/issues.jsonl" "$exclude" || printf "\n.beads/issues.jsonl\n" >> "$exclude"
  fi
fi
