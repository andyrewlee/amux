BINARY_NAME := amux
MAIN_PACKAGE := ./cmd/amux
.DEFAULT_GOAL := build

HARNESS_FRAMES ?= 300
HARNESS_WARMUP ?= 30
HARNESS_WIDTH ?= 160
HARNESS_HEIGHT ?= 48
HARNESS_SCROLLBACK_FRAMES ?= 600
# GOFUMPT/GOIMPORTS pins must match the versions bundled in the golangci-lint
# pinned by .golangci-version (CI runs `golangci-lint fmt --diff` against its
# bundled formatters): golangci-lint v2.12.2 vendors gofumpt v0.9.2 and
# x/tools v0.44.0 — bump both pins when .golangci-version bumps.
GOFUMPT ?= go run mvdan.cc/gofumpt@v0.9.2
GOIMPORTS ?= go run golang.org/x/tools/cmd/goimports@v0.44.0
# LOCAL_PREFIXES mirrors `formatters.settings.goimports.local-prefixes` in
# .golangci.yml/.golangci.strict.yml — keep in sync.
LOCAL_PREFIXES ?= github.com/andyrewlee/amux
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

.PHONY: build install test test-race test-race-tmux soak tidy-check govulncheck windows-build ci bench lint lint-tools lint-strict lint-strict-new lint-ci-parity lint-config-drift check-golangci-version check-file-length check-fmt-config fmt fmt-check vet clean run dev devcheck verify-loop tmux-skip-check help release-check release-tag release-push release harness-center harness-sidebar harness-monitor harness-presets harness-golden perf-check doctor

build:
	go build -o $(BINARY_NAME) $(MAIN_PACKAGE)

# install drops the binary into $(PREFIX)/bin; override PREFIX for a custom
# location (e.g. `make install PREFIX=$HOME/.local`). If the destination isn't
# writable it tries sudo, then falls back to GOPATH/bin with a PATH hint.
PREFIX ?= /usr/local
install: build
	@dest="$(PREFIX)/bin"; \
	if [ -w "$$dest" ]; then \
		install -m 755 $(BINARY_NAME) "$$dest/$(BINARY_NAME)"; \
		echo "installed $$dest/$(BINARY_NAME)"; \
	elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then \
		sudo install -m 755 $(BINARY_NAME) "$$dest/$(BINARY_NAME)"; \
		echo "installed $$dest/$(BINARY_NAME)"; \
	else \
		dest="$$(go env GOPATH)/bin"; \
		mkdir -p "$$dest"; \
		install -m 755 $(BINARY_NAME) "$$dest/$(BINARY_NAME)"; \
		echo "$(PREFIX)/bin not writable; installed $$dest/$(BINARY_NAME) (ensure it is on PATH)"; \
	fi

test:
	@packages=$$(go list ./...) || exit 1; \
	filtered=$$(printf '%s\n' "$$packages" | grep -v -E '/internal/(tmux|e2e|app|pty)$$') || exit 1; \
	echo "go test $$(printf '%s' "$$filtered" | tr '\n' ' ')"; \
	go test $$filtered
	@$(MAKE) --no-print-directory tmux-skip-check

# test-race mirrors CI's "Test (race)" step: `go test -race` over CI's package
# set, which excludes only internal/e2e and internal/tmux — the single source
# for that set is scripts/test_pkgs.sh, shared with .github/workflows/ci.yml.
# Note this is wider than `make test`/`make devcheck` (their filter also
# excludes internal/app). Race runs are slow; that is why this is a separate
# target rather than part of devcheck (same reasoning as verify-loop).
test-race:
	@filtered=$$(./scripts/test_pkgs.sh) || exit 1; \
	echo "go test -race $$(printf '%s' "$$filtered" | tr '\n' ' ')"; \
	go test -race $$filtered

# test-race-tmux covers the packages test_pkgs.sh excludes plus the real-tmux
# integration tests in app/pty — they need a real tmux server and run under
# -race here and in the tmux-e2e CI job. Without tmux the tests skip cleanly.
test-race-tmux:
	go test -race ./internal/tmux ./internal/e2e ./internal/app ./internal/pty

# soak runs the build-tagged sustained-workload test (PTY ingest + message
# pump under load for minutes). Not part of CI — run before landing
# render/ingest changes. Duration knobs: AMUX_SOAK_DURATION=2m (Go duration)
# or AMUX_SOAK_MINUTES=10; default 5m. -timeout must exceed the duration.
soak:
	go test -tags=soak ./internal/app -run TestSoakHarnessPTY -count=1 -timeout 20m

# tidy-check mirrors CI's "Tidy check" step: it fails when go.mod/go.sum are
# not tidy. Note it runs `go mod tidy`, so an untidy module is rewritten in
# your working tree — inspect `git diff go.mod go.sum` on failure.
tidy-check:
	go mod tidy
	git diff --exit-code go.mod go.sum

# govulncheck scans for known vulnerabilities. GOVULNCHECK_VERSION is the
# single source for the pin — CI's Govulncheck step calls this target rather
# than carrying its own env var.
GOVULNCHECK_VERSION ?= v1.8.0
govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# windows-build mirrors CI's "Windows cross-compile" step — catches
# Windows-only build breaks (os-specific imports, syscalls) locally.
windows-build:
	GOOS=windows GOARCH=amd64 go build ./...

# ci is the local mirror of the CI `test` job's gate set: devcheck (vet +
# tests + lint + file-length + lint-config-drift) plus the race, tidy,
# govulncheck, and windows-build gates. Keep this target in sync with
# .github/workflows/ci.yml — if CI gains a gate, add it here. Coverage map:
# CI test job = lint + fmt --diff + file-length + vet + windows-build + tidy +
# test + test-race + govulncheck + harness smoke + lint-config-drift.
# Not mirrored locally: the three harness smoke steps (run via release-check
# or `make harness-presets`); the tmux-e2e, lint-strict-pr, and macos-build
# jobs (host-specific).
ci: devcheck test-race tidy-check govulncheck windows-build

devcheck:
	go vet ./...
	@packages=$$(go list ./...) || exit 1; \
	filtered=$$(printf '%s\n' "$$packages" | grep -v -E '/internal/(tmux|e2e|app|pty)$$') || exit 1; \
	echo "go test $$(printf '%s' "$$filtered" | tr '\n' ' ')"; \
	go test $$filtered
	@$(MAKE) --no-print-directory tmux-skip-check
	$(MAKE) lint-config-drift
	$(MAKE) check-fmt-config
	$(MAKE) lint

# tmux-skip-check is the single `make test`/`make devcheck` execution of the
# real-tmux package set excluded from the main go test sweep:
# internal/tmux, internal/e2e, internal/app, and internal/pty. Keep this
# package list coupled to the exclusion regex above. The -v output exposes
# per-test `--- SKIP:` lines, failures propagate, and skipped real-tmux
# coverage still prints the same non-fatal NOTE unless STRICT_TMUX=1.
tmux-skip-check:
	@output=$$(mktemp); trap 'rm -f "$$output"' EXIT INT TERM; \
	if ! go test ./internal/tmux ./internal/e2e ./internal/app ./internal/pty -v >"$$output" 2>&1; then \
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
	@output=$$(mktemp); trap 'rm -f "$$output"' EXIT INT TERM; \
	if ! go test ./internal/e2e -run 'TestCloseLoopKeystrokeDeliveryToRawAgent|TestFakeAgentRecordsRawCarriageReturn' -count=1 -v >"$$output" 2>&1; then \
		cat "$$output"; \
		exit 1; \
	fi; \
	cat "$$output"; \
	for t in TestCloseLoopKeystrokeDeliveryToRawAgent TestFakeAgentRecordsRawCarriageReturn; do \
		if ! grep -qE -- "--- PASS: $$t[ (]" "$$output"; then \
			echo "make verify-loop: $$t did not PASS (missing, renamed, or skipped) — the input gate is vacuous" >&2; \
			exit 1; \
		fi; \
	done

# doctor checks that this host can build, test, and run amux: required tools
# present and usable (go at the go.mod version floor, git, a working tmux
# server), plus warn-only environment hints (pre-commit hooks path,
# golangci-lint for the lint targets). Exits nonzero only when a REQUIRED
# tool is missing or unusable.
doctor:
	@fail=0; \
	if command -v go >/dev/null 2>&1; then \
		gov=$$(go version | awk '{print $$3}' | sed 's/^go//'); \
		need=$$(awk '/^go [0-9]/{print $$2; exit}' go.mod); \
		if [ "$$(printf '%s\n%s\n' "$$need" "$$gov" | sort -t. -k1,1n -k2,2n -k3,3n | head -1)" = "$$need" ]; then \
			echo "ok   go $$gov (>= $$need required)"; \
		else \
			echo "FAIL go $$gov < $$need (go.mod)"; fail=1; \
		fi; \
	else \
		echo "FAIL go: not installed"; fail=1; \
	fi; \
	if command -v git >/dev/null 2>&1; then \
		echo "ok   git $$(git --version | awk '{print $$3}')"; \
	else \
		echo "FAIL git: not installed"; fail=1; \
	fi; \
	if command -v tmux >/dev/null 2>&1; then \
		server="amux-doctor-check-$$$$"; \
		if tmux -L "$$server" -f /dev/null new-session -d -s probe "sleep 5" >/dev/null 2>&1; then \
			echo "ok   tmux $$(tmux -V | awk '{print $$2}') (server probe passed)"; \
			tmux -L "$$server" kill-server >/dev/null 2>&1 || true; \
		else \
			echo "FAIL tmux is installed but cannot start a server"; fail=1; \
		fi; \
	else \
		echo "FAIL tmux: not installed (real-tmux tests and verify-loop need it)"; fail=1; \
	fi; \
	hooks=$$(git config --local core.hooksPath 2>/dev/null || true); \
	if [ "$$hooks" = ".githooks" ]; then \
		echo "ok   core.hooksPath=.githooks"; \
	else \
		echo "warn core.hooksPath is '$${hooks:-unset}' — run scripts/install-hooks.sh to enable the repo's pre-commit hooks"; \
	fi; \
	if [ -x .cache/bin/golangci-lint ] || command -v golangci-lint >/dev/null 2>&1; then \
		echo "ok   golangci-lint present"; \
	else \
		echo "warn golangci-lint not found — fetched on demand by make lint"; \
	fi; \
	if [ "$$fail" -ne 0 ]; then exit 1; fi; \
	echo "doctor: environment looks good"

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
# preset is missing. Baselines were measured on the target hosts (see
# PERF_BASELINES.md); after an intentional render-path change, re-baseline by
# running PERF_STRICT=1 make perf-check three times on a quiescent host and
# committing the per-preset medians to scripts/perf_baselines.env.
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
	$(GOLANGCI) run --timeout=10m
	$(GOLANGCI) fmt --diff
	$(MAKE) check-file-length

lint-strict: check-golangci-version
	$(GOLANGCI) run -c .golangci.strict.yml --timeout=10m
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

lint-ci-parity: check-golangci-version # CACHE_ROOT defaults to a gitignored repo-local directory (./.cache/).
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

# check-file-length runs scripts/check_file_length.sh — the single source
# shared with ci.yml's "File length guard" step.
check-file-length:
	@./scripts/check_file_length.sh

# check-fmt-config guards the LOCAL_PREFIXES ↔ .golangci.yml local-prefixes
# mirror (two consumers, two formats — kept in sync by this drift check, same
# role as lint-config-drift). lint-config-drift already forces strict.yml to
# carry the same block, so checking the baseline is enough.
check-fmt-config:
	@yml=$$(awk '/local-prefixes:/{getline; gsub(/[ \t-]/, "", $$0); print; exit}' .golangci.yml); \
	if [ "$$yml" != "$(LOCAL_PREFIXES)" ]; then \
		echo "ERROR: Makefile LOCAL_PREFIXES ($(LOCAL_PREFIXES)) != .golangci.yml local-prefixes ($$yml)"; \
		exit 1; \
	fi

# lint-config-drift guards the invariant that .golangci.strict.yml is
# .golangci.yml verbatim plus strict-only additions (every baseline line is
# present in strict). golangci-lint v2 has no config `extends`, so this is the
# only thing stopping the shared region from silently drifting apart when
# someone edits a shared rule in only one file. Uses process substitution,
# hence the target-specific bash SHELL (the Makefile's default recipe shell
# is /bin/sh, which on some platforms is dash and doesn't support `<(...)`).
lint-config-drift: SHELL := bash
lint-config-drift: ## Fail if strict lint config drifts from the baseline
	@missing="$$(comm -23 <(sort .golangci.yml) <(sort .golangci.strict.yml))"; \
	if [ -n "$$missing" ]; then \
		echo "ERROR: .golangci.strict.yml is missing baseline lines (drift):"; \
		echo "$$missing"; \
		exit 1; \
	fi

fmt:
	$(GOFUMPT) -extra -w .
	$(GOIMPORTS) -local $(LOCAL_PREFIXES) -w .

fmt-check:
	@test -z "$$($(GOFUMPT) -extra -l .)" || ($(GOFUMPT) -extra -l .; exit 1)
	@test -z "$$($(GOIMPORTS) -local $(LOCAL_PREFIXES) -l .)" || ($(GOIMPORTS) -local $(LOCAL_PREFIXES) -l .; exit 1)

vet:
	go vet ./...

clean:
	rm -f $(BINARY_NAME)

run: build
	./$(BINARY_NAME)

# dev runs air to rebuild on save and surface compile errors. It does NOT host
# the TUI: air launches the rebuilt binary with stdin on /dev/null, which fails
# amux's stdin/stdout/stderr TTY check. Run `make run` in a real terminal for the
# TUI; keep `make dev` in a second pane for build feedback while you edit.
dev:
	@command -v air >/dev/null 2>&1 || { \
		echo "make dev: 'air' not found on PATH." >&2; \
		echo "Install the rebuild-on-save runner with:" >&2; \
		echo "  go install github.com/air-verse/air@latest" >&2; \
		echo "(then ensure \$$(go env GOPATH)/bin is on your PATH)." >&2; \
		exit 1; \
	}
	air

help:
	@echo "Available targets:"
	@echo "  build      - Build the binary"
	@echo "  test       - Run all tests"
	@echo "  test-race  - Run go test -race over CI's package set (slow; mirrors CI's race gate)"
	@echo "  test-race-tmux - Run go test -race on the real-tmux packages (tmux, e2e, app, pty; skips cleanly sans tmux)"
	@echo "  soak       - Run the sustained-workload soak test (PTY ingest + msgpump; AMUX_SOAK_DURATION=2m or AMUX_SOAK_MINUTES=10; default 5m)"
	@echo "  tidy-check - Run go mod tidy and fail if go.mod/go.sum change (mirrors CI's tidy gate)"
	@echo "  govulncheck - Scan for known vulnerabilities with the CI-pinned govulncheck (mirrors CI's vuln gate)"
	@echo "  ci         - Full local CI mirror: devcheck + test-race + tidy-check + govulncheck"
	@echo "  devcheck   - vet + tests + golden harness + lint (warns when real-tmux/e2e tests skip)"
	@echo "  lint       - Run golangci-lint and file length checks (max 500 lines)"
	@echo "  lint-tools - Build the pinned golangci-lint (.golangci-version) into ./.cache/bin (idempotent)"
	@echo "  lint-strict - Run stricter lint profile across the whole repo"
	@echo "  lint-strict-new - Run stricter lint profile only on changed code (optionally BASE=<git-rev>)"
	@echo "  lint-ci-parity - Run strict changed-code lint using merge-base with BASE_REF (default origin/main)"
	@echo "  lint-config-drift - Fail if .golangci.strict.yml is missing lines present in .golangci.yml (baseline drift)"
	@echo "  check-file-length - Check Go file lengths only (max 500 lines)"
	@echo "  fmt        - Format code with gofumpt and goimports"
	@echo "  fmt-check  - Check gofumpt formatting (for CI)"
	@echo "  vet        - Run go vet"
	@echo "  clean      - Remove build artifacts"
	@echo "  run        - Build and run"
	@echo "  dev        - Rebuild + compile-error feedback on save via air; does NOT run the TUI (use 'make run')"
	@echo "  verify-loop - Drive a real keystroke through amux into a raw-mode agent (close-the-loop input gate; requires tmux)"
	@echo "  tmux-skip-check - Warn (non-fatal) when real-tmux/e2e tests silently skip (no tmux server)"
	@echo "  doctor         - Check this host can build/test/run amux (go, git, tmux probe, hooks, lint tools)"
	@echo "  bench      - Run rendering benchmarks"
	@echo "  harness-center  - Run center harness preset"
	@echo "  harness-sidebar - Run sidebar harness preset (deep scrollback)"
	@echo "  harness-monitor - Run monitor harness preset"
	@echo "  harness-presets - Run all harness presets"
	@echo "  harness-golden  - Run byte-exact golden-frame snapshot tests (pure render; -update to regenerate)"
	@echo "  perf-check      - Compare harness p95 against host baselines (DARWIN_ARM64_* here; PERF_STRICT=1 to fail on missing baseline)"
	@echo "  release-check - Full ci gate set + harness smoke + goreleaser config check"
	@echo "  release-tag   - Create an annotated tag (VERSION=vX.Y.Z)"
	@echo "  release-push  - Push the tag to origin (VERSION=vX.Y.Z)"
	@echo "  release       - release-check + release-tag + release-push"

# release-check is the pre-tag gate: the full `ci` gate set (devcheck +
# test-race + tidy + govulncheck + windows-build) so a tag can't ship a tree
# that would fail CI, plus the three harness smoke runs (the one CI-test-job
# piece `ci` doesn't mirror) and a .goreleaser.yml validation so a broken
# release config fails pre-tag rather than in release.yml. goreleaser is
# optional locally (warn-not-fail) until a later change pins it.
release-check: ci
	go run ./cmd/amux-harness -mode center -frames 5 -warmup 1
	go run ./cmd/amux-harness -mode sidebar -frames 5 -warmup 1
	go run ./cmd/amux-harness -mode monitor -frames 5 -warmup 1
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser check; \
	else \
		echo "NOTE: goreleaser not installed; skipping .goreleaser.yml validation"; \
	fi

release-tag:
	@test -n "$(VERSION)" || (echo "VERSION is required (e.g. VERSION=v0.0.5)" && exit 1)
	@[ -z "$$(git status --porcelain)" ] || (echo "Working tree not clean (staged/unstaged/untracked). Commit or stash changes before tagging." && exit 1)
	@git tag -a "$(VERSION)" -m "$(VERSION)"
	@echo "Created tag $(VERSION)"

release-push:
	@test -n "$(VERSION)" || (echo "VERSION is required (e.g. VERSION=v0.0.5)" && exit 1)
	@git push origin "$(VERSION)"

release: release-check release-tag release-push
