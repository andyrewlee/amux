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

.PHONY: build install test bench lint lint-strict lint-strict-new lint-ci-parity check-golangci-version check-file-length fmt fmt-check vet clean run dev devcheck verify-loop help release-check release-tag release-push release harness-center harness-sidebar harness-monitor harness-presets harness-golden

build:
	go build -o $(BINARY_NAME) $(MAIN_PACKAGE)

install: build
	cp $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)

test:
	go test ./...

devcheck:
	go vet ./...
	go test ./...
	$(MAKE) harness-golden
	$(MAKE) lint

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

check-golangci-version:
	@command -v golangci-lint >/dev/null 2>&1 || (echo "golangci-lint is required (install: https://golangci-lint.run/welcome/install/)"; exit 1)
	@want_raw="$$(cat .golangci-version)"; \
	want="$${want_raw#v}"; \
	have_raw="$$(golangci-lint version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1)"; \
	have="$${have_raw#v}"; \
	if [ "$$have" != "$$want" ]; then \
		echo "WARNING: golangci-lint $${have_raw:-unknown} installed but .golangci-version pins $$want_raw (CI uses $$want_raw; diagnostics may differ)"; \
	fi

lint: check-golangci-version
	golangci-lint run
	$(MAKE) check-file-length

lint-strict: check-golangci-version
	golangci-lint run -c .golangci.strict.yml

lint-strict-new: check-golangci-version
	@if [ -n "$(BASE)" ]; then \
		echo "Running strict lint against changes since $(BASE)"; \
		golangci-lint run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new-from-rev "$(BASE)" --timeout=10m; \
	else \
		echo "Running strict lint on current unstaged/staged changes (--new)"; \
		golangci-lint run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new --timeout=10m; \
	fi

lint-ci-parity: check-golangci-version # CACHE_ROOT defaults to a gitignored local directory (/.cache/).
	@command -v golangci-lint >/dev/null 2>&1 || (echo "golangci-lint is required (install: https://golangci-lint.run/welcome/install/)"; exit 1)
	@BASE_REF="$${BASE_REF:-origin/main}"; \
	CACHE_ROOT="$${CACHE_ROOT:-$$(pwd)/.cache}"; \
	GO_CACHE_DIR="$$CACHE_ROOT/go-build"; \
	GOLANGCI_CACHE_DIR="$$CACHE_ROOT/golangci-lint"; \
	mkdir -p "$$GO_CACHE_DIR" "$$GOLANGCI_CACHE_DIR"; \
	if git rev-parse --verify "$$BASE_REF" >/dev/null 2>&1; then \
		BASE=$$(git merge-base HEAD "$$BASE_REF"); \
		echo "Running CI-parity strict lint against changes since $$BASE_REF ($$BASE)"; \
		OUTPUT=$$(mktemp); trap 'rm -f "$$OUTPUT"' EXIT INT TERM; \
		if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" golangci-lint run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new-from-rev "$$BASE" --timeout=10m >"$$OUTPUT" 2>&1; then \
				cat "$$OUTPUT"; \
				if grep -q "no go files to analyze" "$$OUTPUT"; then \
					echo "golangci-lint test loader failed locally; retrying with --tests=false"; \
					if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" golangci-lint run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new-from-rev "$$BASE" --timeout=10m --tests=false; then \
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
		if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" golangci-lint run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new --timeout=10m >"$$OUTPUT" 2>&1; then \
			cat "$$OUTPUT"; \
			if grep -q "no go files to analyze" "$$OUTPUT"; then \
				echo "golangci-lint test loader failed locally; retrying with --tests=false"; \
				if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" golangci-lint run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) --new --timeout=10m --tests=false; then \
					exit 1; \
				fi; \
			else \
				exit 1; \
			fi; \
		fi; \
		trap - EXIT INT TERM; rm -f "$$OUTPUT"; \
	fi

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
	air

help:
	@echo "Available targets:"
	@echo "  build      - Build the binary"
	@echo "  test       - Run all tests"
	@echo "  lint       - Run golangci-lint and file length checks (max 500 lines)"
	@echo "  lint-strict - Run stricter lint profile across the whole repo"
	@echo "  lint-strict-new - Run stricter lint profile only on changed code (optionally BASE=<git-rev>)"
	@echo "  lint-ci-parity - Run strict changed-code lint using merge-base with BASE_REF (default origin/main)"
	@echo "  check-file-length - Check Go file lengths only (max 500 lines)"
	@echo "  fmt        - Format code with gofumpt and goimports"
	@echo "  fmt-check  - Check gofumpt formatting (for CI)"
	@echo "  vet        - Run go vet"
	@echo "  clean      - Remove build artifacts"
	@echo "  run        - Build and run"
	@echo "  dev        - Run with hot reload (requires air)"
	@echo "  verify-loop - Drive a real keystroke through amux into a raw-mode agent (close-the-loop input gate; requires tmux)"
	@echo "  bench      - Run rendering benchmarks"
	@echo "  harness-center  - Run center harness preset"
	@echo "  harness-sidebar - Run sidebar harness preset (deep scrollback)"
	@echo "  harness-monitor - Run monitor harness preset"
	@echo "  harness-presets - Run all harness presets"
	@echo "  harness-golden  - Run byte-exact golden-frame snapshot tests (pure render; -update to regenerate)"
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
