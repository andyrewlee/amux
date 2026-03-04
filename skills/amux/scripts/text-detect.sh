# text-detect.sh — Shared text-detection helpers for assistant scripts.
# Sourced (not executed); do not add shebang or set -euo pipefail.

text_has_reply_option_number() {
  local text="$1"
  local number="$2"
  if [[ -z "${text// }" || -z "${number// }" ]]; then
    return 1
  fi
  printf '%s\n' "$text" | grep -Eiq "(^|[[:space:]])${number}[.)][[:space:]]+"
}

text_has_reply_option_letter() {
  local text="$1"
  local letter="$2"
  local upper lower
  if [[ -z "${text// }" || -z "${letter// }" ]]; then
    return 1
  fi
  upper="$(printf '%s' "$letter" | tr '[:lower:]' '[:upper:]')"
  lower="$(printf '%s' "$letter" | tr '[:upper:]' '[:lower:]')"
  printf '%s\n' "$text" | grep -Eiq "(^|[[:space:]])(${upper}|${lower})[.)][[:space:]]+"
}

text_has_yes_no_prompt() {
  local text="$1"
  local lower
  if [[ -z "${text// }" ]]; then
    return 1
  fi
  lower="$(printf '%s' "$text" | tr '[:upper:]' '[:lower:]')"
  if [[ "$lower" == *"(y/n)"* || "$lower" == *"[y/n]"* || "$lower" == *"(yes/no)"* || "$lower" == *"[yes/no]"* || "$lower" == *"yes or no"* || "$lower" == *"reply yes"* || "$lower" == *"reply no"* ]]; then
    return 0
  fi
  return 1
}

text_has_press_enter_prompt() {
  local text="$1"
  local lower
  if [[ -z "${text// }" ]]; then
    return 1
  fi
  lower="$(printf '%s' "$text" | tr '[:upper:]' '[:lower:]')"
  if [[ "$lower" == *"press enter"* || "$lower" == *"hit enter"* || "$lower" == *"press return"* || "$lower" == *"hit return"* || "$lower" == *"just press enter"* || "$lower" == *"enter to continue"* ]]; then
    return 0
  fi
  return 1
}
