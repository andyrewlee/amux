#!/usr/bin/env bash
# File-length guard (max 500 lines per .go file), shared by `make
# check-file-length` and ci.yml's "File length guard" step — single source so
# the two can't drift. Prunes non-project trees: .git internals and any local
# build caches/vendor dirs must not trip the cap on code we don't own.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "Checking file lengths (max 500 lines)..."
find . \( -path './.git' -o -path './.cache' -o -path './vendor' \) -prune \
	-o -name '*.go' -exec wc -l {} + \
	| awk '!/total$/ && $1 > 500 { print "ERROR: " $2 " has " $1 " lines (max 500)"; found=1 } END { if(found) exit 1 }'
