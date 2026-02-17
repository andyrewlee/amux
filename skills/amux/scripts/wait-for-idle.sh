#!/usr/bin/env bash
# wait-for-idle.sh — Wait for an agent to become idle.
#
# Usage: wait-for-idle.sh --session <name> [--timeout 300] [--idle-threshold 10]
#
# Polls agent capture, detects idle state (no output change for --idle-threshold seconds).
# Returns last capture content when idle. Exits with error if --timeout exceeded.

set -euo pipefail

SESSION=""
TIMEOUT=300
IDLE_THRESHOLD=10
POLL_INTERVAL=2

while [[ $# -gt 0 ]]; do
  case "$1" in
    --session)        SESSION="$2"; shift 2 ;;
    --timeout)        TIMEOUT="$2"; shift 2 ;;
    --idle-threshold) IDLE_THRESHOLD="$2"; shift 2 ;;
    *)                echo "Unknown flag: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "$SESSION" ]]; then
  echo "Usage: wait-for-idle.sh --session <name> [--timeout 300] [--idle-threshold 10]" >&2
  exit 2
fi

last_hash=""
last_content=""
idle_since=""
start_time=$(date +%s)

while true; do
  now=$(date +%s)
  elapsed=$((now - start_time))
  if [[ $elapsed -ge $TIMEOUT ]]; then
    echo "Timeout after ${TIMEOUT}s waiting for idle" >&2
    # Still output last content on timeout
    if [[ -n "$last_content" ]]; then
      echo "$last_content"
    fi
    exit 1
  fi

  result=$(amux --json agent capture "$SESSION" --lines 100 2>&1) || true

  ok=$(echo "$result" | jq -r '.ok // false' 2>/dev/null) || ok="false"
  if [[ "$ok" != "true" ]]; then
    # Session gone — treat as idle
    if [[ -n "$last_content" ]]; then
      echo "$last_content"
    fi
    exit 0
  fi

  content=$(echo "$result" | jq -r '.data.content // ""')
  current_hash=$(echo -n "$content" | md5sum 2>/dev/null | cut -d' ' -f1 || echo -n "$content" | md5 2>/dev/null)

  if [[ "$current_hash" != "$last_hash" ]]; then
    last_hash="$current_hash"
    last_content="$content"
    idle_since=$(date +%s)
  else
    if [[ -n "$idle_since" ]]; then
      idle_elapsed=$((now - idle_since))
      if [[ $idle_elapsed -ge $IDLE_THRESHOLD ]]; then
        echo "$last_content"
        exit 0
      fi
    fi
  fi

  sleep "$POLL_INTERVAL"
done
