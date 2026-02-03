BINARY_NAME := amux
MAIN_PACKAGE := ./cmd/amux
.DEFAULT_GOAL := build

.PHONY: build test bench lint fmt fmt-check vet clean run dev help release-check release-tag release-push release

build:
	go build -o $(BINARY_NAME) $(MAIN_PACKAGE)

test:
	go test -v ./...

bench:
	go test -bench=. -benchmem ./internal/ui/compositor/ -run=^$$

lint: test
	golangci-lint run
	@echo "Checking file lengths (max 500 lines)..."
	@find . -name '*.go' -exec wc -l {} + | awk '!/total$$/ && $$1 > 500 { print "ERROR: " $$2 " has " $$1 " lines (max 500)"; found=1 } END { if(found) exit 1 }'

fmt:
	gofmt -w .
	goimports -w .

fmt-check:
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
	@echo "  lint       - Run golangci-lint and check file lengths (max 500 lines)"
	@echo "  fmt        - Format code with gofmt and goimports"
	@echo "  fmt-check  - Check formatting (for CI)"
	@echo "  vet        - Run go vet"
	@echo "  clean      - Remove build artifacts"
	@echo "  run        - Build and run"
	@echo "  dev        - Run with hot reload (requires air)"
	@echo "  bench      - Run rendering benchmarks"
	@echo "  release-check - Run tests and harness smoke checks"
	@echo "  release-tag   - Create an annotated tag (VERSION=vX.Y.Z)"
	@echo "  release-push  - Push the tag to origin (VERSION=vX.Y.Z)"
	@echo "  release       - release-check + release-tag + release-push"

release-check: test
	go run ./cmd/amux-harness -mode center -frames 5 -warmup 1
	go run ./cmd/amux-harness -mode sidebar -frames 5 -warmup 1

release-tag:
	@test -n "$(VERSION)" || (echo "VERSION is required (e.g. VERSION=v0.0.5)" && exit 1)
	@[ -z "$$(git status --porcelain)" ] || (echo "Working tree not clean (staged/unstaged/untracked). Commit or stash changes before tagging." && exit 1)
	@git tag -a "$(VERSION)" -m "$(VERSION)"
	@echo "Created tag $(VERSION)"

release-push:
	@test -n "$(VERSION)" || (echo "VERSION is required (e.g. VERSION=v0.0.5)" && exit 1)
	@git push origin "$(VERSION)"

release: release-check release-tag release-push
