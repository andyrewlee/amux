#!/usr/bin/env bash
# Docs-drift gate — narrow contract pairs, not a general verifier. Three
# checks:
#   1. Every sequence in app_prefix.go's prefixCommandTable is documented in
#      README.md's Controls table (a rebound/dropped row used to ship green).
#   2. docs/STORAGE.md names the real metadata root (workspaces-metadata),
#      never the live-worktree root, for the record/transcript/tombstone rows.
#   3. CONTRIBUTING.md's harness examples carry --assert-min-visible, the
#      render assert the harness gate relies on.
# Extend by adding grep pairs in the same style; keep it dumb on purpose.
set -euo pipefail
cd "$(dirname "$0")/.."

failures=0

fail() {
	echo "FAIL: $1"
	failures=$((failures + 1))
}

ok() {
	echo "ok: $1"
}

# --- Check 1: prefixCommandTable sequences ↔ README Controls table ----------

# The table block runs from the header row to the first blank line.
readme_table=$(sed -n '/| Sequence | Action |/,/^$/p' README.md)
if [ -z "$readme_table" ]; then
	fail "README.md: could not locate the Controls '| Sequence | Action |' table"
fi

# Pull Sequence fields out of the table literal: []string{"t", "a"} -> "t a".
sequences=$(sed -n '/^var prefixCommandTable/,/^}/p' internal/app/app_prefix.go \
	| grep -o 'Sequence: \[\]string{[^}]*}' \
	| sed -e 's/Sequence: \[\]string{//; s/}//' \
	| tr -d '",' \
	| tr -s ' ' \
	| sed 's/^ //; s/ $//')

if [ -z "$sequences" ]; then
	fail "internal/app/app_prefix.go: extracted zero Sequence entries — the prefixCommandTable literal shape changed; update this script"
else
	missing=0
	while IFS= read -r seq; do
		[ -z "$seq" ] && continue
		if ! printf '%s\n' "$readme_table" | grep -qF "\`$seq\`"; then
			fail "README.md Controls table does not document prefix sequence \`$seq\` — add a row or remove the command"
			missing=$((missing + 1))
		fi
	done <<EOF
$sequences
EOF
	# The 1..9 tab-jump branch is generated, not a table row — pin the
	# backticked `1`..`9` row directly.
	if ! printf '%s\n' "$readme_table" | grep -qF '`1`' || ! printf '%s\n' "$readme_table" | grep -qF '`9`'; then
		fail "README.md Controls table does not document the \`1\`–\`9\` tab-jump row"
		missing=$((missing + 1))
	fi
	[ "$missing" -eq 0 ] && ok "prefix command table sequences documented in README"
fi

# --- Check 2: STORAGE.md metadata root -------------------------------------

if grep -nF '~/.amux/workspaces/<id>' docs/STORAGE.md >/dev/null 2>&1; then
	fail "docs/STORAGE.md names ~/.amux/workspaces/<id>/ for metadata files — the metadata root is ~/.amux/workspaces-metadata/<id>/"
else
	count=$(grep -cF 'workspaces-metadata/<id>' docs/STORAGE.md || true)
	if [ "$count" -lt 3 ]; then
		fail "docs/STORAGE.md lists $count workspaces-metadata/<id> rows; expected workspace.json + script-transcripts.json + .deleting"
	else
		ok "STORAGE.md metadata rows name workspaces-metadata"
	fi
fi

# --- Check 3: CONTRIBUTING harness assert flag ------------------------------

if ! grep -qF 'assert-min-visible' CONTRIBUTING.md; then
	fail "CONTRIBUTING.md harness example drops --assert-min-visible — copy-pasting it disables the render assert the gate relies on"
else
	ok "CONTRIBUTING.md harness example carries --assert-min-visible"
fi

if [ "$failures" -gt 0 ]; then
	echo "docs-drift check failed: $failures problem(s)" >&2
	exit 1
fi
echo "docs-drift check: all contract pairs aligned"
