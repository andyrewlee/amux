BINARY_NAME := amux
MAIN_PACKAGE := ./cmd/amux
.DEFAULT_GOAL := build

HARNESS_FRAMES ?= 300
HARNESS_WARMUP ?= 30
HARNESS_WIDTH ?= 160
HARNESS_HEIGHT ?= 48
HARNESS_SCROLLBACK_FRAMES ?= 600
GOFUMPT ?= go run mvdan.cc/gofumpt@v0.9.2

.PHONY: build test bench lint lint-strict lint-strict-new pr-checklist check-file-length fmt fmt-check vet clean run dev devcheck help release-check release-tag release-push release harness-center harness-sidebar harness-monitor harness-presets

build:
	go build -o $(BINARY_NAME) $(MAIN_PACKAGE)

test:
	go test -v ./...

devcheck:
	go vet ./...
	go test ./...
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
		golangci-lint run -c .golangci.strict.yml --new-from-rev "$(BASE)"; \
	else \
		echo "Running strict lint on current unstaged/staged changes (--new)"; \
		golangci-lint run -c .golangci.strict.yml --new; \
	fi

pr-checklist:
	@test -n "$(BASE)" || (echo "BASE is required (e.g. BASE=origin/main)"; exit 1)
	@test -n "$(HEAD)" || (echo "HEAD is required (e.g. HEAD=HEAD)"; exit 1)
	@test -n "$(BODY)" || (echo "BODY is required and should point to a markdown file"; exit 1)
	./scripts/ci/verify_pr_checklist.sh "$(BASE)" "$(HEAD)" "$(BODY)"

check-file-length:
	@echo "Checking file lengths (max 500 lines)..."
	@find . -name '*.go' -exec wc -l {} + | awk '!/total$$/ && $$1 > 500 { print "ERROR: " $$2 " has " $$1 " lines (max 500)"; found=1 } END { if(found) exit 1 }'

fmt:
	$(GOFUMPT) -w .
	gofmt -w .
	goimports -w .

fmt-check:
	@test -z "$$($(GOFUMPT) -l .)" || ($(GOFUMPT) -l .; exit 1)
	@test -z "$$(gofmt -l .)" || (gofmt -l .; exit 1)

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
	@echo "  pr-checklist - Validate PR checklist markdown (requires BASE, HEAD, BODY)"
	@echo "  check-file-length - Check Go file lengths only (max 500 lines)"
	@echo "  fmt        - Format code with gofumpt, gofmt, and goimports"
	@echo "  fmt-check  - Check gofumpt/gofmt formatting (for CI)"
	@echo "  vet        - Run go vet"
	@echo "  clean      - Remove build artifacts"
	@echo "  run        - Build and run"
	@echo "  dev        - Run with hot reload (requires air)"
	@echo "  bench      - Run rendering benchmarks"
	@echo "  harness-center  - Run center harness preset"
	@echo "  harness-sidebar - Run sidebar harness preset (deep scrollback)"
	@echo "  harness-monitor - Run monitor harness preset"
	@echo "  harness-presets - Run all harness presets"
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
