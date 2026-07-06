BINARY_NAME := amux
MAIN_PACKAGE := ./cmd/amux
.DEFAULT_GOAL := build

HARNESS_FRAMES ?= 300
HARNESS_WARMUP ?= 30
HARNESS_WIDTH ?= 160
HARNESS_HEIGHT ?= 48
HARNESS_SCROLLBACK_FRAMES ?= 600
GOFUMPT ?= go run mvdan.cc/gofumpt@v0.9.2
STRICT_RATCHET_LINTERS := --enable funlen --enable gocyclo --enable nestif

# GOLANGCI resolves to the repo-local pinned golangci-lint (built from source by
# `make lint-tools` into the gitignored ./.cache/bin) when it exists AND reports
# the exact version in .golangci-version; otherwise it falls back to a PATH
# golangci-lint. This keeps CI unaffected: CI has no ./.cache/bin binary (it is
# gitignored and uses golangci-lint-action), so it resolves to the PATH binary
# the action installs.
#
# The probe is scoped to the lint targets via a target-specific := assignment so
# that unrelated targets (build/test/run/vet/...) never pay the shell-out cost,
# and the := form evaluates it exactly once per lint invocation (a plain
# recursive GOLANGCI = $(shell ...) would re-run the probe on every $(GOLANGCI)
# expansion, which lint-strict-new/lint-ci-parity reference multiple times).
GOLANGCI ?= golangci-lint
lint lint-strict lint-strict-new lint-ci-parity check-golangci-version: GOLANGCI := $(shell want=`tr -d '[:space:]' < .golangci-version 2>/dev/null | sed 's/^v//'`; local="$$PWD/.cache/bin/golangci-lint"; have=`"$$local" version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 | sed 's/^v//'`; if [ -x "$$local" ] && [ "$$have" = "$$want" ]; then echo "$$local"; else echo golangci-lint; fi)

.PHONY: build install test bench lint lint-tools lint-strict lint-strict-new lint-ci-parity check-golangci-version check-file-length fmt fmt-check vet clean run dev devcheck verify-loop tmux-skip-check help release-check release-tag release-push release harness-center harness-sidebar harness-monitor harness-presets harness-golden perf-check

build:
	go build -o $(BINARY_NAME) $(MAIN_PACKAGE)

install: build
	cp $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)

test:
	@packages=$$(go list ./...) || exit 1; \
	filtered=$$(printf '%s\n' "$$packages" | grep -v -E '/internal/(tmux|e2e|app)$$') || exit 1; \
	echo "go test $$(printf '%s' "$$filtered" | tr '\n' ' ')"; \
	go test $$filtered
	@$(MAKE) --no-print-directory tmux-skip-check

devcheck:
	go vet ./...
	@packages=$$(go list ./...) || exit 1; \
	filtered=$$(printf '%s\n' "$$packages" | grep -v -E '/internal/(tmux|e2e|app)$$') || exit 1; \
	echo "go test $$(printf '%s' "$$filtered" | tr '\n' ' ')"; \
	go test $$filtered
	@$(MAKE) --no-print-directory tmux-skip-check
	$(MAKE) lint

# tmux-skip-check is the single `make test`/`make devcheck` execution of the
# real-tmux package set excluded from the main go test sweep:
# internal/tmux, internal/e2e, and internal/app. Keep this package list coupled
# to the exclusion regex above. The -v output exposes per-test `--- SKIP:`
# lines, failures propagate, and skipped real-tmux coverage still prints the
# same non-fatal NOTE unless STRICT_TMUX=1.
tmux-skip-check:
	@output=$$(mktemp); trap 'rm -f "$$output"' EXIT INT TERM; \
	if ! go test ./internal/tmux ./internal/e2e ./internal/app -v >"$$output" 2>&1; then \
		cat "$$output"; \
		exit 1; \
	fi; \
	skipped=$$(awk '\
		/^[[:space:]]+[^[:space:]]+\.go:[0-9]+:/ { reason=$$0 } \
		/^--- SKIP:/ { \
			if (reason !~ /cannot start PTY-backed tmux attach|client never attached|signal permissions restricted in this environment|tmux version does not emit DEC 2026 synchronized-output markers/) skipped++; \
			reason=""; \
		} \
		END { print skipped + 0 }' "$$output"); \
	if [ "$$skipped" -gt 0 ]; then \
		if [ "$${STRICT_TMUX:-}" = "1" ]; then \
			echo "ERROR: $$skipped real-tmux/e2e tests skipped while STRICT_TMUX=1 (tmux is expected to be present here)."; \
			exit 1; \
		fi; \
		echo "NOTE: $$skipped real-tmux/e2e tests skipped (tmux server unavailable or environment-restricted) — run inside tmux and use \`make verify-loop\` to exercise input/send end-to-end."; \
	fi

# verify-loop drives a real keystroke through amux's actual input path into a
# real raw-mode agent and asserts the bytes (including a literal carriage
# return) arrive intact. This is the gate to run for any change to the
# send/Enter/tmux/agent input path: unlike `make devcheck` (which passes even
# when the real-tmux tests skip) and the render-only harness, a green run here
# means a real agent actually received the input end-to-end. Requires git and
# tmux, and fails before running tests if either is unavailable.
verify-loop:
	@command -v git >/dev/null 2>&1 || { echo "make verify-loop: git is required" >&2; exit 1; }
	@command -v tmux >/dev/null 2>&1 || { echo "make verify-loop: tmux is required" >&2; exit 1; }
	@server="amux-verify-loop-check-$$$$"; \
	if ! tmux -L "$$server" new-session -d -s probe "sleep 5" >/dev/null 2>&1; then \
		echo "make verify-loop: tmux is installed but unusable" >&2; \
		exit 1; \
	fi; \
	tmux -L "$$server" kill-server >/dev/null 2>&1 || true
	go test ./internal/e2e -run 'TestCloseLoopKeystrokeDeliveryToRawAgent|TestFakeAgentRecordsRawCarriageReturn' -count=1 -v

bench:
	go test -bench=. -benchmem ./internal/ui/compositor/ -run=^$$

harness-center:
	go run ./cmd/amux-harness -mode center -tabs 16 -hot-tabs 2 -payload-bytes 64 -frames $(HARNESS_FRAMES) -warmup $(HARNESS_WARMUP) -width $(HARNESS_WIDTH) -height $(HARNESS_HEIGHT)

harness-monitor:
	go run ./cmd/amux-harness -mode monitor -tabs 16 -hot-tabs 4 -payload-bytes 64 -frames $(HARNESS_FRAMES) -warmup $(HARNESS_WARMUP) -width $(HARNESS_WIDTH) -height $(HARNESS_HEIGHT)

harness-sidebar:
	go run ./cmd/amux-harness -mode sidebar -tabs 16 -hot-tabs 1 -payload-bytes 64 -newline-every 1 -frames $(HARNESS_SCROLLBACK_FRAMES) -warmup $(HARNESS_WARMUP) -width $(HARNESS_WIDTH) -height $(HARNESS_HEIGHT)

harness-presets: harness-center harness-sidebar harness-monitor

# harness-golden runs the byte-exact golden-frame snapshot tests. Pure render
# (no tmux/PTY): it builds each harness preset, drives it to the final frame,
# and diffs view.Content against internal/app/testdata/golden/*.frame. This
# catches border/color/off-by-one/truncation regressions that -assert-min-visible
# misses. Regenerate goldens after an intentional render change with:
#   go test ./internal/app -run Golden -update
harness-golden:
	go test ./internal/app -count=1 -run Golden

# perf-check runs the host-native perf self-check: it drives each harness
# preset and compares the measured p95 against the checked-in baselines for the
# current ${GOOS}_${GOARCH} (on this machine, DARWIN_ARM64_*). It is the local
# gate for render-path changes that PR-time CI does not cover for darwin-arm64.
# Set PERF_STRICT=1 to fail (rather than silently skip) when a baseline for a
# preset is missing. Re-baseline the DARWIN_ARM64_* values on the dev machine
# before relying on this as a hard gate; the checked-in numbers may be
# placeholders.
perf-check:
	bash scripts/perf_compare.sh

check-golangci-version:
	@command -v $(GOLANGCI) >/dev/null 2>&1 || (echo "golangci-lint is required: run 'make lint-tools' to build the pinned version locally, or install from https://golangci-lint.run/welcome/install/"; exit 1)
	@want_raw="$$(cat .golangci-version)"; \
	want="$${want_raw#v}"; \
	have_raw="$$($(GOLANGCI) version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1)"; \
	have="$${have_raw#v}"; \
	if [ "$$have" != "$$want" ]; then \
		echo "WARNING: golangci-lint $${have_raw:-unknown} resolved but .golangci-version pins $$want_raw (run 'make lint-tools' to build the pinned version; diagnostics may differ)"; \
	fi

# lint-tools self-bootstraps the pinned golangci-lint (from .golangci-version)
# from source into the gitignored ./.cache/bin so `make lint` just works even
# when the system golangci-lint is the wrong version. Idempotent: a no-op when
# the local binary already reports the pinned version.
lint-tools:
	bash scripts/install-golangci-lint.sh

lint: check-golangci-version
	$(GOLANGCI) run
	$(GOLANGCI) fmt --diff
	$(MAKE) check-file-length

lint-strict: check-golangci-version
	$(GOLANGCI) run -c .golangci.strict.yml
	$(GOLANGCI) fmt -c .golangci.strict.yml --diff

lint-strict-new: check-golangci-version
	@if [ -n "$(BASE)" ]; then \
		echo "Running strict lint against changes since $(BASE)"; \
		$(GOLANGCI) run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new-from-rev "$(BASE)" --timeout=10m; \
	else \
		echo "Running strict lint on current unstaged/staged changes (--new)"; \
		$(GOLANGCI) run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new --timeout=10m; \
	fi
	$(GOLANGCI) fmt -c .golangci.strict.yml --diff

lint-ci-parity: check-golangci-version # CACHE_ROOT defaults to a gitignored local directory (/.cache/).
	@command -v $(GOLANGCI) >/dev/null 2>&1 || (echo "golangci-lint is required: run 'make lint-tools' to build the pinned version locally, or install from https://golangci-lint.run/welcome/install/"; exit 1)
	@BASE_REF="$${BASE_REF:-origin/main}"; \
	CACHE_ROOT="$${CACHE_ROOT:-$$(pwd)/.cache}"; \
	GO_CACHE_DIR="$$CACHE_ROOT/go-build"; \
	GOLANGCI_CACHE_DIR="$$CACHE_ROOT/golangci-lint"; \
	mkdir -p "$$GO_CACHE_DIR" "$$GOLANGCI_CACHE_DIR"; \
	if git rev-parse --verify "$$BASE_REF" >/dev/null 2>&1; then \
		BASE=$$(git merge-base HEAD "$$BASE_REF"); \
		echo "Running CI-parity strict lint against changes since $$BASE_REF ($$BASE)"; \
		OUTPUT=$$(mktemp); trap 'rm -f "$$OUTPUT"' EXIT INT TERM; \
		if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" $(GOLANGCI) run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new-from-rev "$$BASE" --timeout=10m >"$$OUTPUT" 2>&1; then \
				cat "$$OUTPUT"; \
				if grep -q "no go files to analyze" "$$OUTPUT"; then \
					echo "golangci-lint test loader failed locally; retrying with --tests=false"; \
					if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" $(GOLANGCI) run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new-from-rev "$$BASE" --timeout=10m --tests=false; then \
						exit 1; \
					fi; \
				else \
					exit 1; \
				fi; \
			fi; \
		trap - EXIT INT TERM; rm -f "$$OUTPUT"; \
	else \
		echo "Base ref $$BASE_REF not found; falling back to strict lint on current unstaged/staged changes"; \
		OUTPUT=$$(mktemp); trap 'rm -f "$$OUTPUT"' EXIT INT TERM; \
		if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" $(GOLANGCI) run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new --timeout=10m >"$$OUTPUT" 2>&1; then \
			cat "$$OUTPUT"; \
			if grep -q "no go files to analyze" "$$OUTPUT"; then \
				echo "golangci-lint test loader failed locally; retrying with --tests=false"; \
				if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" $(GOLANGCI) run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new --timeout=10m --tests=false; then \
					exit 1; \
				fi; \
			else \
				exit 1; \
			fi; \
		fi; \
		trap - EXIT INT TERM; rm -f "$$OUTPUT"; \
	fi
	$(GOLANGCI) fmt -c .golangci.strict.yml --diff

check-file-length:
	@echo "Checking file lengths (max 500 lines)..."
	@find . -name '*.go' -exec wc -l {} + | awk '!/total$$/ && $$1 > 500 { print "ERROR: " $$2 " has " $$1 " lines (max 500)"; found=1 } END { if(found) exit 1 }'

fmt:
	$(GOFUMPT) -extra -w .
	goimports -w .

fmt-check:
	@test -z "$$($(GOFUMPT) -extra -l .)" || ($(GOFUMPT) -extra -l .; exit 1)

vet:
	go vet ./...

clean:
	rm -f $(BINARY_NAME)

run: build
	./$(BINARY_NAME)

dev:
	@command -v air >/dev/null 2>&1 || { \
		echo "make dev: 'air' not found on PATH." >&2; \
		echo "Install the hot-reload runner with:" >&2; \
		echo "  go install github.com/air-verse/air@latest" >&2; \
		echo "(then ensure \$$(go env GOPATH)/bin is on your PATH)." >&2; \
		exit 1; \
	}
	air

help:
	@echo "Available targets:"
	@echo "  build      - Build the binary"
	@echo "  test       - Run all tests"
	@echo "  devcheck   - vet + tests + golden harness + lint (warns when real-tmux/e2e tests skip)"
	@echo "  lint       - Run golangci-lint and file length checks (max 500 lines)"
	@echo "  lint-tools - Build the pinned golangci-lint (.golangci-version) into ./.cache/bin (idempotent)"
	@echo "  lint-strict - Run stricter lint profile across the whole repo"
	@echo "  lint-strict-new - Run stricter lint profile only on changed code (optionally BASE=<git-rev>)"
	@echo "  lint-ci-parity - Run strict changed-code lint using merge-base with BASE_REF (default origin/main)"
	@echo "  check-file-length - Check Go file lengths only (max 500 lines)"
	@echo "  fmt        - Format code with gofumpt and goimports"
	@echo "  fmt-check  - Check gofumpt formatting (for CI)"
	@echo "  vet        - Run go vet"
	@echo "  clean      - Remove build artifacts"
	@echo "  run        - Build and run"
	@echo "  dev        - Run with hot reload (requires air: go install github.com/air-verse/air@latest)"
	@echo "  verify-loop - Drive a real keystroke through amux into a raw-mode agent (close-the-loop input gate; requires tmux)"
	@echo "  tmux-skip-check - Warn (non-fatal) when real-tmux/e2e tests silently skip (no tmux server)"
	@echo "  bench      - Run rendering benchmarks"
	@echo "  harness-center  - Run center harness preset"
	@echo "  harness-sidebar - Run sidebar harness preset (deep scrollback)"
	@echo "  harness-monitor - Run monitor harness preset"
	@echo "  harness-presets - Run all harness presets"
	@echo "  harness-golden  - Run byte-exact golden-frame snapshot tests (pure render; -update to regenerate)"
	@echo "  perf-check      - Compare harness p95 against host baselines (DARWIN_ARM64_* here; PERF_STRICT=1 to fail on missing baseline)"
	@echo "  release-check - Run tests and harness smoke checks"
	@echo "  release-tag   - Create an annotated tag (VERSION=vX.Y.Z)"
	@echo "  release-push  - Push the tag to origin (VERSION=vX.Y.Z)"
	@echo "  release       - release-check + release-tag + release-push"

release-check: test
	go run ./cmd/amux-harness -mode center -frames 5 -warmup 1
	go run ./cmd/amux-harness -mode sidebar -frames 5 -warmup 1
	go run ./cmd/amux-harness -mode monitor -frames 5 -warmup 1

release-tag:
	@test -n "$(VERSION)" || (echo "VERSION is required (e.g. VERSION=v0.0.5)" && exit 1)
	@[ -z "$$(git status --porcelain)" ] || (echo "Working tree not clean (staged/unstaged/untracked). Commit or stash changes before tagging." && exit 1)
	@git tag -a "$(VERSION)" -m "$(VERSION)"
	@echo "Created tag $(VERSION)"

release-push:
	@test -n "$(VERSION)" || (echo "VERSION is required (e.g. VERSION=v0.0.5)" && exit 1)
	@git push origin "$(VERSION)"

release: release-check release-tag release-push
