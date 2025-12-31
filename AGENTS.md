# Agent Instructions

## Project: amux

A Go TUI for managing AI coding agents across git worktrees. See `CLAUDE.md` for architecture details.

### Build & Test
```bash
go build ./...                           # Quick compile check
go build -o amux ./cmd/amux && ./amux   # Build and run
go vet ./...                             # Lint
```

### Key Files to Understand First
- `internal/app/app.go` - Root model, message routing
- `internal/ui/dashboard/model.go` - Project/worktree list
- `internal/messages/messages.go` - All message types
- `internal/ui/common/dialog.go` - Dialog system

### Running for Testing
Build and run: `go build -o amux ./cmd/amux && ./amux`
- Use `a` to add a project (any git repo path)
- Use `n` to create worktree
- Use `Ctrl+T` to launch agent
- Use `?` for help overlay

---

## Issue Tracking (beads)

This project uses **bd** (beads) for issue tracking. Run `bd onboard` to get started.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id>         # Complete work
bd sync               # Sync with git
```

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

