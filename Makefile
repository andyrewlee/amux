BINARY_NAME := amux
MAIN_PACKAGE := ./cmd/amux
.DEFAULT_GOAL := build

HARNESS_FRAMES ?= 300
HARNESS_WARMUP ?= 30
HARNESS_WIDTH ?= 160
HARNESS_HEIGHT ?= 48
HARNESS_SCROLLBACK_FRAMES ?= 600
GOFUMPT ?= go run mvdan.cc/gofumpt@v0.9.2
OPENCLAW_AMUX_SKILL_SRC ?= $(CURDIR)/skills/amux

.PHONY: build install openclaw-sync test bench lint lint-strict lint-strict-new lint-ci-parity check-file-length fmt fmt-check vet clean run dev devcheck help release-check release-tag release-push release harness-center harness-sidebar harness-monitor harness-presets tmux-doctor tmux-prune

build:
	go build -o $(BINARY_NAME) $(MAIN_PACKAGE)

install: build
	cp $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)

openclaw-sync: install
	./$(BINARY_NAME) dev openclaw-sync --skill-src "$(OPENCLAW_AMUX_SKILL_SRC)" $(if $(strip $(OPENCLAW_DEFAULT_WORKSPACE)),--main-workspace "$(OPENCLAW_DEFAULT_WORKSPACE)") $(if $(strip $(OPENCLAW_DEV_WORKSPACE)),--dev-workspace "$(OPENCLAW_DEV_WORKSPACE)")

test:
	go test -v ./...

DEVCHECK_TEST_FLAGS ?= -v

devcheck:
	go vet ./...
	# internal/cli can run for a few minutes; keep per-test output visible so
	# local runs don't appear hung.
	go test $(DEVCHECK_TEST_FLAGS) ./...
	$(MAKE) lint

bench:
	go test -bench=. -benchmem ./internal/ui/compositor/ -run=^$$

harness-center:
	go run ./cmd/amux-harness -mode center -tabs 16 -hot-tabs 2 -payload-bytes 64 -frames $(HARNESS_FRAMES) -warmup $(HARNESS_WARMUP) -width $(HARNESS_WIDTH) -height $(HARNESS_HEIGHT)

harness-monitor:
	go run ./cmd/amux-harness -mode monitor -tabs 16 -hot-tabs 4 -payload-bytes 64 -frames $(HARNESS_FRAMES) -warmup $(HARNESS_WARMUP) -width $(HARNESS_WIDTH) -height $(HARNESS_HEIGHT)

harness-sidebar:
	go run ./cmd/amux-harness -mode sidebar -tabs 16 -hot-tabs 1 -payload-bytes 64 -newline-every 1 -frames $(HARNESS_SCROLLBACK_FRAMES) -warmup $(HARNESS_WARMUP) -width $(HARNESS_WIDTH) -height $(HARNESS_HEIGHT)

harness-presets: harness-center harness-sidebar harness-monitor

lint:
	@command -v golangci-lint >/dev/null 2>&1 || (echo "golangci-lint is required (install: https://golangci-lint.run/welcome/install/)"; exit 1)
	golangci-lint run
	$(MAKE) check-file-length

lint-strict:
	@command -v golangci-lint >/dev/null 2>&1 || (echo "golangci-lint is required (install: https://golangci-lint.run/welcome/install/)"; exit 1)
	golangci-lint run -c .golangci.strict.yml

lint-strict-new:
	@command -v golangci-lint >/dev/null 2>&1 || (echo "golangci-lint is required (install: https://golangci-lint.run/welcome/install/)"; exit 1)
	@if [ -n "$(BASE)" ]; then \
		echo "Running strict lint against changes since $(BASE)"; \
		golangci-lint run -c .golangci.strict.yml --new-from-rev "$(BASE)" --timeout=10m; \
	else \
		echo "Running strict lint on current unstaged/staged changes (--new)"; \
		golangci-lint run -c .golangci.strict.yml --new --timeout=10m; \
	fi

lint-ci-parity: # CACHE_ROOT defaults to a gitignored local directory (/.cache/).
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
		if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" golangci-lint run -c .golangci.strict.yml --new-from-rev "$$BASE" --timeout=10m >"$$OUTPUT" 2>&1; then \
				cat "$$OUTPUT"; \
				if grep -q "no go files to analyze" "$$OUTPUT"; then \
					echo "golangci-lint test loader failed locally; retrying with --tests=false"; \
					if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" golangci-lint run -c .golangci.strict.yml --new-from-rev "$$BASE" --timeout=10m --tests=false; then \
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
		if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" golangci-lint run -c .golangci.strict.yml --new --timeout=10m >"$$OUTPUT" 2>&1; then \
			cat "$$OUTPUT"; \
			if grep -q "no go files to analyze" "$$OUTPUT"; then \
				echo "golangci-lint test loader failed locally; retrying with --tests=false"; \
				if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" golangci-lint run -c .golangci.strict.yml --new --timeout=10m --tests=false; then \
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
	$(GOFUMPT) -w .
	goimports -w .

fmt-check:
	@test -z "$$($(GOFUMPT) -l .)" || ($(GOFUMPT) -l .; exit 1)

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
	@echo "  install    - Build and install amux to /usr/local/bin"
	@echo "  openclaw-sync - Build+install amux, then run native OpenClaw skill sync"
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
	@echo "  bench      - Run rendering benchmarks"
	@echo "  harness-center  - Run center harness preset"
	@echo "  harness-sidebar - Run sidebar harness preset (deep scrollback)"
	@echo "  harness-monitor - Run monitor harness preset"
	@echo "  harness-presets - Run all harness presets"
	@echo "  tmux-doctor - Diagnose tmux/PTY pressure and stale sessions"
	@echo "  tmux-prune - Prune detached/orphaned amux tmux sessions"
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

tmux-doctor:
	go run ./cmd/amux doctor tmux

tmux-prune:
	go run ./cmd/amux doctor tmux --prune --yes --older-than 6h
