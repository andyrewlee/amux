#!/usr/bin/env bash
# format-capture.sh — Strip ANSI escape codes and format terminal output.
#
# Usage: format-capture.sh [--strip-ansi] [--last-answer] [--trim]
#
# Reads from stdin. Options:
#   --strip-ansi    Remove ANSI escape sequences (default: on)
#   --last-answer   Extract only the last answer block (heuristic: text after last prompt)
#   --trim          Trim leading/trailing blank lines
#
# Example:
#   amux --json agent capture <session> | jq -r '.data.content' | format-capture.sh --strip-ansi --trim

set -euo pipefail

STRIP_ANSI=true
LAST_ANSWER=false
TRIM=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --strip-ansi)   STRIP_ANSI=true; shift ;;
    --last-answer)  LAST_ANSWER=true; shift ;;
    --trim)         TRIM=true; shift ;;
    *)              echo "Unknown flag: $1" >&2; exit 2 ;;
  esac
done

input=$(cat)

# Strip ANSI escape codes
if [[ "$STRIP_ANSI" == "true" ]]; then
  # Remove all ANSI escape sequences: CSI (ESC[), OSC (ESC]), and simple ESC sequences
  input=$(echo "$input" | sed \
    -e 's/\x1b\[[0-9;]*[a-zA-Z]//g' \
    -e 's/\x1b\][^\x07]*\x07//g' \
    -e 's/\x1b\][^\x1b]*\x1b\\//g' \
    -e 's/\x1b[()][0-9A-B]//g' \
    -e 's/\x1b[=>]//g' \
    -e 's/\r//g')
fi

# Extract last answer block
if [[ "$LAST_ANSWER" == "true" ]]; then
  # Heuristic: look for common prompt patterns and take text after the last one
  # Matches: $, >, >>>, %, #, claude>, and similar prompts
  last_prompt_line=$(echo "$input" | grep -n '^\([$>%#]\|>>>\|.*[>$#%] \)' | tail -1 | cut -d: -f1 || echo "")
  if [[ -n "$last_prompt_line" ]]; then
    total_lines=$(echo "$input" | wc -l)
    if [[ $last_prompt_line -lt $total_lines ]]; then
      input=$(echo "$input" | tail -n +"$((last_prompt_line + 1))")
    fi
  fi
fi

# Trim leading/trailing blank lines
if [[ "$TRIM" == "true" ]]; then
  input=$(echo "$input" | sed '/./,$!d' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}')
fi

echo "$input"
