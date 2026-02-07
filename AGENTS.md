# AGENTS

- Do not commit or push unless the user explicitly requests it.
- Tech stack: Go TUI built on Bubble Tea v2 (Charm); styling via Lip Gloss; terminal parsing via `internal/vterm`.
- Entry points: `cmd/amux` (app) and `cmd/amux-harness` (headless render/perf).
- E2E: PTY-driven tests live in `internal/e2e` and exercise the real binary.
- Work autonomously: validate changes with tests and use the harness/emulator to verify UI behavior without a human.
- Lint-driven workflow: run `make devcheck` for all non-trivial changes.
- Formatting baseline includes `gofumpt`; use `make fmt` for style-only cleanup.
- Phase 2 strict lint: run `make lint-strict-new` for changed-code ratcheting before finalizing substantial edits.
- Phase 3 CI gate is automated (no PR-body parsing). For local confidence, run path-relevant checks (`make harness-presets`, `go test ./internal/tmux ./internal/e2e`) when touching those areas.
- Lint policy source of truth: `LINTING.md`.
- Release: use `make release VERSION=vX.Y.Z` (runs tests + harness, tags, pushes). Tag push triggers GitHub Actions release.
