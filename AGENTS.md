# AGENTS

- Do not commit or push unless the user explicitly requests it.
- Tech stack: Go TUI built on Bubble Tea v2 (Charm); styling via Lip Gloss; terminal parsing via `internal/vterm`.
- Entry points: `cmd/amux` (app) and `cmd/amux-harness` (headless render/perf).
- E2E: PTY-driven tests live in `internal/e2e` and exercise the real binary.
- Work autonomously: validate changes with tests and use the harness/emulator to verify UI behavior without a human.
