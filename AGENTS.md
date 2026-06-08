# AGENTS

- Do not commit or push unless the user explicitly requests it.
- Tech stack: Go TUI built on Bubble Tea v2 (Charm); styling via Lip Gloss; terminal parsing via `internal/vterm`.
- Entry points: `cmd/amux` (app) and `cmd/amux-harness` (headless render/perf).
- E2E: PTY-driven tests live in `internal/e2e` and exercise the real binary.
- Work autonomously: validate changes with tests and use the harness/emulator to verify UI behavior without a human. The harness is render-only — it does not exercise tmux, the PTY, or a real agent.
- Lint-driven workflow: run `make devcheck` for all non-trivial changes. Note `make devcheck` passes even when the real-tmux e2e tests skip, so it does not by itself verify input/send behavior.
- Input/send/tmux/agent changes: run `make verify-loop`. It drives a real keystroke through amux's actual input path into a real raw-mode agent and asserts the bytes (including a literal carriage return) arrive intact — a green run means a real agent received the input end-to-end, which `make devcheck` and the harness cannot prove.
- Formatting baseline includes `gofumpt`; use `make fmt` for style-only cleanup.
- Phase 2 strict lint: run `make lint-strict-new` for changed-code ratcheting before finalizing substantial edits.
- Phase 3 CI gate is automated (no PR-body parsing). For local confidence, run path-relevant checks (`make harness-presets`, `go test ./internal/tmux ./internal/e2e`) when touching those areas.
- Lint policy source of truth: `LINTING.md`.
- Release: use `make release VERSION=vX.Y.Z` (runs tests + harness, tags, pushes). Tag push triggers GitHub Actions release.
