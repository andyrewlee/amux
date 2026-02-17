#!/usr/bin/env bash
# poll-agent.sh — Poll agent capture with change detection.
# Fallback for environments without `amux agent watch`.
#
# Usage: poll-agent.sh --session <name> [--lines 100] [--interval 2] [--timeout 120]
#
# Polls amux agent capture and prints only new/changed content.
# Exits when idle (no changes for --timeout seconds) or session disappears.

set -euo pipefail

SESSION=""
LINES=100
INTERVAL=2
TIMEOUT=120

while [[ $# -gt 0 ]]; do
  case "$1" in
    --session)  SESSION="$2"; shift 2 ;;
    --lines)    LINES="$2"; shift 2 ;;
    --interval) INTERVAL="$2"; shift 2 ;;
    --timeout)  TIMEOUT="$2"; shift 2 ;;
    *)          echo "Unknown flag: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "$SESSION" ]]; then
  echo "Usage: poll-agent.sh --session <name> [--lines 100] [--interval 2] [--timeout 120]" >&2
  exit 2
fi

last_hash=""
idle_since=""

while true; do
  result=$(amux --json agent capture "$SESSION" --lines "$LINES" 2>&1) || true

  ok=$(echo "$result" | jq -r '.ok // false' 2>/dev/null) || ok="false"
  if [[ "$ok" != "true" ]]; then
    echo '{"type":"exited","ts":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"}'
    exit 0
  fi

  content=$(echo "$result" | jq -r '.data.content // ""')
  current_hash=$(echo -n "$content" | md5sum 2>/dev/null | cut -d' ' -f1 || echo -n "$content" | md5 2>/dev/null)

  if [[ "$current_hash" != "$last_hash" ]]; then
    echo "$content"
    last_hash="$current_hash"
    idle_since=$(date +%s)
  else
    now=$(date +%s)
    if [[ -n "$idle_since" ]]; then
      elapsed=$((now - idle_since))
      if [[ $elapsed -ge $TIMEOUT ]]; then
        echo '{"type":"idle","idle_seconds":'"$elapsed"',"ts":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"}'
        exit 0
      fi
    fi
  fi

  sleep "$INTERVAL"
done
