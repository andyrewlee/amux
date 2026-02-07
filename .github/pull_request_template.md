## Summary

Describe the change and intended behavior.

## Quality Checklist

- [ ] devcheck
  - Ran `make devcheck`.
- [ ] lint_strict_new
  - Ran `make lint-strict-new` (or `make lint-strict-new BASE=origin/main`).
- [ ] harness_presets
  - Required if touching `internal/ui/`, `internal/vterm/`, or `cmd/amux-harness/`.
  - Ran `make harness-center`, `make harness-sidebar`, and `make harness-monitor`.
- [ ] tmux_e2e
  - Required if touching `internal/tmux/`, `internal/e2e/`, or `internal/pty/`.
  - Ran `go test ./internal/tmux ./internal/e2e`.
- [ ] lint_policy_review
  - Required if touching `.golangci.yml`, `.golangci.strict.yml`, `LINTING.md`, `Makefile`, or `.github/workflows/ci.yml`.
  - Verified that lint policy/gate changes are intentional and documented.
