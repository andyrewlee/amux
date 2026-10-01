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
# x/tools v0.44.0 — bump both pins when .golangci-version bumps. Enforced by
# `make check-fmt-versions` (scripts/check_fmt_versions.sh).
GOFUMPT ?= go run mvdan.cc/gofumpt@v0.9.2
GOIMPORTS ?= go run golang.org/x/tools/cmd/goimports@v0.44.0
# LOCAL_PREFIXES mirrors `formatters.settings.goimports.local-prefixes` in
# .golangci.yml/.golangci.strict.yml — keep in sync.
LOCAL_PREFIXES ?= github.com/andyrewlee/amux
STRICT_RATCHET_LINTERS := --enable funlen --enable gocyclo --enable nestif

# GOLANGCI resolves to the repo-local pinned golangci-lint (built from source by
# `make lint-tools` into the gitignored ./.cache/bin) when it exists AND reports
# the exact version in .golangci-version; otherwise it falls back to a PATH
# golangci-lint. A PATH fallback is tolerated with a warning, but
# .cache/bin (gitignored, built from the pinned source) is preferred so local
# diagnostics match the pinned tool exactly.
#
# The probe is scoped to the lint targets via a target-specific := assignment so
# that unrelated targets (build/test/run/vet/...) never pay the shell-out cost,
# and the := form evaluates it exactly once per lint invocation (a plain
# recursive GOLANGCI = $(shell ...) would re-run the probe on every $(GOLANGCI)
# expansion, which lint-strict-new/lint-strict-base reference multiple times).
GOLANGCI ?= golangci-lint
lint lint-strict lint-strict-new lint-strict-base check-golangci-version: GOLANGCI := $(shell want=`tr -d '[:space:]' < .golangci-version 2>/dev/null | sed 's/^v//'`; local="$$PWD/.cache/bin/golangci-lint"; have=`"$$local" version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 | sed 's/^v//'`; if [ -x "$$local" ] && [ "$$have" = "$$want" ]; then echo "$$local"; else echo golangci-lint; fi)

.PHONY: build install test test-race test-race-tmux soak fuzz tidy-check govulncheck windows-build ci ci-nightly ci-tmux-matrix bench lint lint-tools lint-strict lint-strict-new lint-strict-base lint-config-drift check-golangci-version check-file-length check-fmt-config check-fmt-versions fmt fmt-check vet clean run dev devcheck verify-loop tmux-skip-check help release-check release-tag release-push release harness-center harness-sidebar harness-monitor harness-presets harness-smoke harness-golden perf-check doctor

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
	@packages=$$(./scripts/test_pkgs.sh --exclude-app) || exit 1; \
	test -n "$$packages" || exit 1; \
	echo "go test $$(printf '%s' "$$packages" | tr '\n' ' ')"; \
	go test $$packages
	@$(MAKE) --no-print-directory tmux-skip-check

# test-race mirrors the former CI "Test (race)" step: `go test -race` over
# the shared package set from scripts/test_pkgs.sh (excludes internal/tmux,
# e2e, and pty). Note this is wider than
# `make test`/`make devcheck`, which run the script's --exclude-app variant
# (internal/app is deferred to tmux-skip-check locally). Race runs are slow;
# that is why this is a separate target rather than part of devcheck (same
# reasoning as verify-loop).
test-race:
	@filtered=$$(./scripts/test_pkgs.sh) || exit 1; \
	echo "go test -race $$(printf '%s' "$$filtered" | tr '\n' ' ')"; \
	go test -race $$filtered

# test-race-tmux covers the packages test_pkgs.sh excludes plus the real-tmux
# integration tests in app/pty — they need a real tmux server and run under
# -race here (the former tmux-e2e CI job's race leg). Without tmux they skip.
test-race-tmux:
	go test -race ./internal/tmux ./internal/e2e ./internal/app ./internal/pty

# soak runs the build-tagged sustained-workload test (PTY ingest + message
# pump under load for minutes). Part of `make ci-nightly` — also run before
# landing render/ingest changes. Duration knobs: AMUX_SOAK_DURATION=2m (Go duration)
# or AMUX_SOAK_MINUTES=10; default 5m. -timeout must exceed the duration.
soak:
	go test -tags=soak ./internal/app -run TestSoakHarnessPTY -count=1 -timeout 20m

# tidy-check fails when go.mod/go.sum are
# not tidy. Note it runs `go mod tidy`, so an untidy module is rewritten in
# your working tree — inspect `git diff go.mod go.sum` on failure.
tidy-check:
	go mod tidy
	git diff --exit-code go.mod go.sum

# govulncheck scans for known vulnerabilities. GOVULNCHECK_VERSION is the
# single source for the pin.
GOVULNCHECK_VERSION ?= v1.8.0
govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# AIR_VERSION pins the rebuild-on-save runner used by `make dev` — same
# pin-everything discipline as GOVULNCHECK_VERSION above.
AIR_VERSION ?= v1.67.4

# windows-build catches Windows-only build breaks (os-specific imports,
# syscalls) locally via cross-compile.
windows-build:
	GOOS=windows GOARCH=amd64 go build ./...

# ci is the complete local CI gate set — this project runs no GitHub Actions
# (the former .github/workflows/ci.yml coverage moved here in full). It runs:
#   devcheck (vet + tests + lint + file-length + lint-config-drift, and its
#           embedded tmux-skip-check under STRICT_TMUX=1 so a real-tmux skip
#           fails like the old CI assert), test-race (the wide -race sweep),
#   test-race-tmux (the former tmux-e2e job's race leg on tmux/e2e/app/pty),
#   tidy-check, govulncheck, windows-build, harness-smoke (the former CI
#   test job's three quick harness asserts).
# `ci` exercises whichever tmux is installed locally; for the tmux version
# matrix (ubuntu-22.04 apt floor + from-source 3.6a, the old tmux-e2e matrix
# job) run `make ci-tmux-matrix` — it replays the matrix in docker. Strict
# changed-code lint is enforced by the pre-push hook's `lint-strict-base`.
ci:
	STRICT_TMUX=1 FMT_VERSION_STRICT=1 $(MAKE) devcheck test-race test-race-tmux tidy-check govulncheck windows-build harness-smoke

# ci-tmux-matrix replays the old tmux-e2e CI matrix job in Linux containers:
# the real-tmux race suite + strict skip check against ubuntu-22.04's apt tmux
# (3.2a distro floor) and a sha256-pinned from-source tmux (see MATRIX in
# scripts/ci_tmux_matrix.sh). Requires a running docker daemon.
ci-tmux-matrix:
	bash scripts/ci_tmux_matrix.sh

# fuzz runs the three fuzz targets for FUZZ_TIME each — without this gate the
# corpus never mutates (the seeds alone run inside `make test`). vterm is the
# most fuzz-worthy surface in the repo: a hand-rolled stateful parser on
# arbitrary PTY bytes. Part of ci-nightly; not devcheck/pre-push (soak-class).
FUZZ_TIME ?= 30s
fuzz:
	go test -fuzz=FuzzANSIParser -fuzztime=$(FUZZ_TIME) ./internal/vterm
	go test -fuzz=FuzzRenderInvariant -fuzztime=$(FUZZ_TIME) ./internal/vterm
	go test -fuzz=FuzzParseStatusPorcelain -fuzztime=$(FUZZ_TIME) ./internal/git

# ci-nightly mirrors the former nightly workflow: a full-tree race run plus
# the soak workload. Heavy — for pre-landing confidence, not every commit.
ci-nightly:
	go test -race ./... -timeout 30m
	$(MAKE) soak
	$(MAKE) fuzz

# harness-smoke mirrors the former CI test job's three quick harness asserts.
harness-smoke:
	go run ./cmd/amux-harness -mode center -frames 5 -warmup 1 -tabs 8 -width 160 -height 48 -hot-tabs 2 -payload-bytes 64 -newline-every 4 -assert-min-visible 100
	go run ./cmd/amux-harness -mode sidebar -frames 5 -warmup 1 -tabs 8 -width 160 -height 48 -hot-tabs 2 -payload-bytes 64 -newline-every 4 -assert-min-visible 100
	go run ./cmd/amux-harness -mode monitor -frames 5 -warmup 1 -tabs 8 -width 160 -height 48 -hot-tabs 2 -payload-bytes 64 -newline-every 4 -assert-min-visible 100

devcheck:
	go vet ./...
	$(MAKE) test
	$(MAKE) lint-config-drift
	$(MAKE) check-fmt-config
	$(MAKE) check-fmt-versions
	$(MAKE) lint

# tmux-skip-check is the single `make test`/`make devcheck` execution of the
# real-tmux package set excluded from the main go test sweep:
# internal/tmux, internal/e2e, internal/app, and internal/pty. Keep this
# package list coupled to scripts/test_pkgs.sh --exclude-app (the sweep's
# package source). The -v output exposes
# per-test `--- SKIP:` lines, failures propagate, and skipped real-tmux
# coverage still prints the same non-fatal NOTE unless STRICT_TMUX=1.
# -count=1 is load-bearing: the tmux environment is not part of Go's test
# cache key, so without it this environment-detector gate can replay stale
# `-v` output and false-green (or false-fail STRICT_TMUX).
tmux-skip-check:
	@output=$$(mktemp); trap 'rm -f "$$output"' EXIT INT TERM; \
	if ! go test -count=1 ./internal/tmux ./internal/e2e ./internal/app ./internal/pty -v >"$$output" 2>&1; then \
		cat "$$output"; \
		exit 1; \
	fi; \
	skipped=$$(awk '\
		/^[[:space:]]+[^[:space:]]+\.go:[0-9]+:/ { reason=$$0 } \
		/^--- SKIP:/ { \
			if (reason ~ /cannot start PTY-backed tmux attach|client never attached|signal permissions restricted in this environment|tmux version does not emit DEC 2026 synchronized-output markers/) whitelisted++; \
			else skipped++; \
			reason=""; \
		} \
		END { print (skipped + 0) " " (whitelisted + 0) }' "$$output"); \
	nonwhitelisted=$$(echo "$$skipped" | cut -d' ' -f1); \
	whitelisted=$$(echo "$$skipped" | cut -d' ' -f2); \
	if [ "$$nonwhitelisted" -gt 0 ]; then \
		if [ "$${STRICT_TMUX:-}" = "1" ]; then \
			echo "ERROR: $$nonwhitelisted real-tmux/e2e tests skipped while STRICT_TMUX=1 (tmux is expected to be present here)."; \
			exit 1; \
		fi; \
		echo "NOTE: $$nonwhitelisted real-tmux/e2e tests skipped (tmux server unavailable or environment-restricted) — run inside tmux and use \`make verify-loop\` to exercise input/send end-to-end."; \
	fi; \
	if [ "$$whitelisted" -gt 0 ]; then \
		echo "NOTE: $$whitelisted real-tmux/e2e tests skipped with whitelisted reasons (tmux attach/EPERM/sync-marker) — whitelisted skips are exempt from STRICT_TMUX but not invisible."; \
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
# server >= 3.2 — internal/tmux EnsureAvailable's floor), plus warn-only
# environment hints (pre-commit hooks path, golangci-lint for the lint
# targets, air for `make dev`, curl for check-fmt-versions/install.sh,
# docker for ci-tmux-matrix — keep these rows in lockstep with the gates'
# tool set). Exits nonzero only when a REQUIRED tool is missing or unusable.
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
		tmuxv=$$(tmux -V | awk '{print $$2}' | sed 's/^next-//; s/[a-zA-Z].*$$//'); \
		tmajor=$${tmuxv%%.*}; tminor=$${tmuxv#*.}; tminor=$${tminor%%.*}; \
		if [ "$${tmajor:-0}" -lt 3 ] || { [ "$${tmajor:-0}" -eq 3 ] && [ "$${tminor:-0}" -lt 2 ]; }; then \
			echo "FAIL tmux $${tmuxv:-unparseable} < 3.2 (amux requires >= 3.2)"; fail=1; \
		else \
			server="amux-doctor-check-$$$$"; \
			if tmux -L "$$server" -f /dev/null new-session -d -s probe "sleep 5" >/dev/null 2>&1; then \
				echo "ok   tmux $$tmuxv (>= 3.2, server probe passed)"; \
				tmux -L "$$server" kill-server >/dev/null 2>&1 || true; \
			else \
				echo "FAIL tmux $$tmuxv is installed but cannot start a server"; fail=1; \
			fi; \
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
		glbin=$$( [ -x .cache/bin/golangci-lint ] && echo .cache/bin/golangci-lint || command -v golangci-lint ); \
		glv=$$("$$glbin" version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
		glpin=$$(tr -d '[:space:]' < .golangci-version 2>/dev/null | sed 's/^v//'); \
		if [ -n "$$glpin" ] && [ "$${glv#v}" != "$$glpin" ]; then \
			echo "warn golangci-lint $$glv resolved but .golangci-version pins $$glpin (run 'make lint-tools'; diagnostics may differ)"; \
		else \
			echo "ok   golangci-lint $$glv present"; \
		fi; \
	else \
		echo "warn golangci-lint not found — fetched on demand by make lint"; \
	fi; \
	if command -v air >/dev/null 2>&1; then echo "ok   air present"; else echo "warn air not found — 'make dev' needs it (install with AIR_VERSION pin from Makefile)"; fi; \
	if command -v curl >/dev/null 2>&1; then echo "ok   curl present"; else echo "warn curl not found — check-fmt-versions and install.sh need it"; fi; \
	if command -v docker >/dev/null 2>&1; then echo "ok   docker present"; else echo "warn docker not found — 'make ci-tmux-matrix' needs it"; fi; \
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
# current ${GOOS}_${GOARCH} (on this machine, DARWIN_ARM64_*). There is no CI
# gate for perf — this target is the whole check, run it for render-path
# changes.
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

# run-strict-lint executes one strict-profile golangci run over the diff
# selector in $1 (`--new` or `--new-from-rev <rev>`), capturing output for the
# test-loader fallback. Requires the caller's shell to have GO_CACHE_DIR and
# GOLANGCI_CACHE_DIR set (lint-strict-base does so once up front). Kept as a
# define so the merge-base and fallback branches share the mktemp/trap
# plumbing exactly.
define run-strict-lint
	OUTPUT=$$(mktemp); trap 'rm -f "$$OUTPUT"' EXIT INT TERM; \
	if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" $(GOLANGCI) run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) $(1) --timeout=10m >"$$OUTPUT" 2>&1; then \
		cat "$$OUTPUT"; \
		if grep -q "no go files to analyze" "$$OUTPUT"; then \
			echo "golangci-lint test loader failed locally; retrying with --tests=false"; \
			if ! GOCACHE="$$GO_CACHE_DIR" GOLANGCI_LINT_CACHE="$$GOLANGCI_CACHE_DIR" $(GOLANGCI) run -c .golangci.strict.yml $(STRICT_RATCHET_LINTERS) $(1) --timeout=10m --tests=false; then \
				exit 1; \
			fi; \
		else \
			exit 1; \
		fi; \
	fi; \
	trap - EXIT INT TERM; rm -f "$$OUTPUT";
endef

lint-strict-base: check-golangci-version # CACHE_ROOT defaults to a gitignored repo-local directory (./.cache/).
	@command -v $(GOLANGCI) >/dev/null 2>&1 || (echo "golangci-lint is required: run 'make lint-tools' to build the pinned version locally, or install from https://golangci-lint.run/welcome/install/"; exit 1)
	@BASE_REF="$${BASE_REF:-origin/main}"; \
	CACHE_ROOT="$${CACHE_ROOT:-$$(pwd)/.cache}"; \
	GO_CACHE_DIR="$$CACHE_ROOT/go-build"; \
	GOLANGCI_CACHE_DIR="$$CACHE_ROOT/golangci-lint"; \
	mkdir -p "$$GO_CACHE_DIR" "$$GOLANGCI_CACHE_DIR"; \
	if git rev-parse --verify "$$BASE_REF" >/dev/null 2>&1; then \
		BASE=$$(git merge-base HEAD "$$BASE_REF"); \
		if [ -z "$$BASE" ]; then \
			echo "WARNING: base ref $$BASE_REF resolves but merge-base is empty (shallow clone? unrelated histories?)"; \
			if [ "$${REQUIRE_BASE:-0}" = "1" ]; then \
				echo "ERROR: REQUIRE_BASE=1 — refusing to run a strict gate that can pass vacuously"; \
				exit 1; \
			fi; \
			$(call run-strict-lint,--new) \
		else \
			echo "Running strict lint against changes since $$BASE_REF ($$BASE)"; \
			$(call run-strict-lint,--new-from-rev "$$BASE") \
		fi; \
	else \
		echo "WARNING: base ref $$BASE_REF is unresolvable — strict lint covers only uncommitted changes"; \
		if [ "$${REQUIRE_BASE:-0}" = "1" ]; then \
			echo "ERROR: REQUIRE_BASE=1 — refusing to run a strict gate that can pass vacuously"; \
			exit 1; \
		fi; \
		$(call run-strict-lint,--new) \
	fi
	$(GOLANGCI) fmt -c .golangci.strict.yml --diff

# check-file-length runs scripts/check_file_length.sh — the single source
# shared with `make check-file-length` (run inside `make lint`).
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

# check-fmt-versions verifies the GOFUMPT/GOIMPORTS pins above still equal the
# formatter versions vendored inside the pinned golangci-lint — otherwise
# `make fmt` and `golangci-lint fmt --diff` disagree (fmt/lint split-brain).
# It fetches golangci-lint's go.mod, so unreachable network is a NOTE under
# devcheck and a failure under `ci` (which exports FMT_VERSION_STRICT=1).
check-fmt-versions:
	@fumpt_ver=$$(echo "$(GOFUMPT)" | sed -n 's/.*@\([^[:space:]]*\)$$/\1/p'); \
	tools_ver=$$(echo "$(GOIMPORTS)" | sed -n 's/.*@\([^[:space:]]*\)$$/\1/p'); \
	./scripts/check_fmt_versions.sh "$$fumpt_ver" "$$tools_ver"

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
		echo "  go install github.com/air-verse/air@$(AIR_VERSION)" >&2; \
		echo "(then ensure \$$(go env GOPATH)/bin is on your PATH)." >&2; \
		exit 1; \
	}
	air

help:
	@echo "Available targets:"
	@echo "  build      - Build the binary"
	@echo "  install    - Build and install into PREFIX/bin (default /usr/local; falls back to GOPATH/bin)"
	@echo "  test       - Run the non-tmux package sweep, then the real-tmux packages via tmux-skip-check (skips cleanly without tmux)"
	@echo "  test-race  - Run go test -race over the shared package set (slow)"
	@echo "  test-race-tmux - Run go test -race on the real-tmux packages (tmux, e2e, app, pty; skips cleanly sans tmux)"
	@echo "  soak       - Run the sustained-workload soak test (PTY ingest + msgpump; AMUX_SOAK_DURATION=2m or AMUX_SOAK_MINUTES=10; default 5m)"
	@echo "  fuzz       - Fuzz the vterm parser and porcelain parser for FUZZ_TIME each (default 30s)"
	@echo "  tidy-check - Run go mod tidy and fail if go.mod/go.sum change"
	@echo "  govulncheck - Scan for known vulnerabilities with the pinned govulncheck"
	@echo "  windows-build - Cross-compile GOOS=windows GOARCH=amd64"
	@echo "  ci         - The complete local CI gate: STRICT_TMUX devcheck + test-race + test-race-tmux + tidy-check + govulncheck + windows-build + harness-smoke"
	@echo "  ci-nightly - Full-tree race run + soak (the heavy pre-landing gate)"
	@echo "  ci-tmux-matrix - Real-tmux race suite in docker across the tmux version matrix (apt floor + latest source build)"
	@echo "  harness-smoke - Quick harness asserts for the three presets"
	@echo "  devcheck   - vet + tests + lint-config checks + lint (warns when real-tmux/e2e tests skip)"
	@echo "  lint       - Run golangci-lint and file length checks (max 500 lines)"
	@echo "  lint-tools - Build the pinned golangci-lint (.golangci-version) into ./.cache/bin (idempotent)"
	@echo "  lint-strict - Run stricter lint profile across the whole repo"
	@echo "  lint-strict-new - Run stricter lint profile only on changed code (optionally BASE=<git-rev>)"
	@echo "  lint-strict-base - Run strict changed-code lint using merge-base with BASE_REF (default origin/main)"
	@echo "  lint-config-drift - Fail if .golangci.strict.yml is missing lines present in .golangci.yml (baseline drift)"
	@echo "  check-fmt-config - Fail if Makefile LOCAL_PREFIXES drifts from .golangci.yml local-prefixes"
	@echo "  check-fmt-versions - Fail if GOFUMPT/GOIMPORTS pins drift from golangci-lint's vendored formatters"
	@echo "  check-file-length - Check Go file lengths only (max 500 lines)"
	@echo "  check-golangci-version - Warn if the resolved golangci-lint differs from the .golangci-version pin"
	@echo "  fmt        - Format code with gofumpt and goimports"
	@echo "  fmt-check  - Check gofumpt + goimports formatting"
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
	@echo "  help       - Show this list"
	@echo "  release-push  - Push the tag to origin (VERSION=vX.Y.Z)"
	@echo "  release       - release-check + release-tag + release-push"

# release-check is the pre-tag gate: the full `ci` gate set so a tag can't
# ship a tree that fails CI, then a fail-closed release-toolchain preflight —
# goreleaser (major must match the .goreleaser-version pin), minisign, and
# the two publish-time env vars — plus `goreleaser check` so a broken release
# config fails pre-tag. There is no GitHub Actions release job: release-push
# only pushes the tag — run `goreleaser release --clean` locally (with
# GITHUB_TOKEN and a minisign key) to publish artifacts.
release-check: ci
	@command -v goreleaser >/dev/null 2>&1 || { \
		echo "goreleaser is required for release-check"; \
		echo "  install the pinned version: go install github.com/goreleaser/goreleaser/v2@$$(cat .goreleaser-version)"; \
		exit 1; \
	}
	@command -v minisign >/dev/null 2>&1 || { \
		echo "minisign is required for release-check"; \
		echo "  install via the pinned script: scripts/install_minisign.sh"; \
		exit 1; \
	}
	@test -n "$$GITHUB_TOKEN" || { echo "GITHUB_TOKEN is required (publishes the GitHub release)"; exit 1; }
	@test -n "$$MINISIGN_SECRET_KEY_FILE" || { echo "MINISIGN_SECRET_KEY_FILE is required (signs checksums.txt)"; exit 1; }
	@want_major=$$(sed -n 's/^v\([0-9]*\)\..*/\1/p' .goreleaser-version); \
	have_major=$$(goreleaser --version 2>/dev/null | sed -n 's/^.*[Vv]ersion[: ]*v\{0,1\}\([0-9][0-9]*\)\..*/\1/p' | head -1); \
	if [ -z "$$want_major" ]; then echo ".goreleaser-version is malformed"; exit 1; fi; \
	if [ "$$want_major" != "$$have_major" ]; then \
		echo "goreleaser major mismatch: .goreleaser-version pins v$$want_major.x but installed is v$$have_major.x"; \
		echo "  install the pinned version: go install github.com/goreleaser/goreleaser/v2@$$(cat .goreleaser-version)"; \
		exit 1; \
	fi
	goreleaser check

release-tag:
	@test -n "$(VERSION)" || (echo "VERSION is required (e.g. VERSION=v0.0.5)" && exit 1)
	@[ -z "$$(git status --porcelain)" ] || (echo "Working tree not clean (staged/unstaged/untracked). Commit or stash changes before tagging." && exit 1)
	@git tag -a "$(VERSION)" -m "$(VERSION)"
	@echo "Created tag $(VERSION)"

release-push:
	@test -n "$(VERSION)" || (echo "VERSION is required (e.g. VERSION=v0.0.5)" && exit 1)
	@git push origin "$(VERSION)"

release: release-check release-tag release-push
