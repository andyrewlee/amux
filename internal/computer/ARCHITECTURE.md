# amux Computer Architecture

## Overview

The amux computer system provides a unified interface for running coding agents
(Claude, Codex, OpenCode, Amp, Gemini, Droid) in cloud computers with persistent
credentials and settings.

## Core Design Principles

### 1. One Shared Computer

amux uses a **single shared computer** per provider (named `amux`) for all projects.
Workspace isolation is achieved via `worktreeID` - each project gets its own
workspace directory inside the shared computer:

```
~/.amux/workspaces/{worktreeID}/repo   # Each project's workspace
```

The `worktreeID` is a SHA256 hash of the absolute working directory path, ensuring
each project has a unique workspace without needing separate computers.

**Benefits:**
- Simpler mental model: "amux gives you one cloud computer"
- Credentials persist in one place
- No per-project computer management complexity

### 2. Provider Agnostic

The `Provider` interface (`provider.go`) abstracts computer backends. Providers are
resolved via config/env (`provider`, `AMUX_PROVIDER`) and CLI flags
(`--provider`), so multiple providers can coexist per project.

```go
type Provider interface {
    Name() string
    CreateComputer(ctx, config) (RemoteComputer, error)
    GetComputer(ctx, id) (RemoteComputer, error)
    ListComputers(ctx) ([]RemoteComputer, error)
    DeleteComputer(ctx, id) error
    Volumes() VolumeManager
    Snapshots() SnapshotManager
    SupportsFeature(feature) bool
}
```

Providers are selected explicitly (no default). Providers (Daytona, Sprites, Docker)
can be added by implementing this interface and registering them in the
provider registry (`providers.go`).

### 3. Persistent Credentials (On Computer)

Credentials are stored directly on the computer's filesystem in the home directory:

```
~/.claude/           # Claude credentials
~/.codex/            # Codex credentials
~/.config/codex/     # Codex config
~/.local/share/opencode/  # OpenCode credentials
~/.config/amp/       # Amp config
~/.local/share/amp/  # Amp data
~/.gemini/           # Gemini credentials
~/.factory/          # Factory (Droid) credentials
~/.config/gh/        # GitHub CLI credentials
~/.gitconfig         # Git config
```

**No volumes or symlinks needed.** Credentials persist with the computer and are
available across sessions. If the computer is deleted, credentials are lost and
the user must re-authenticate.

### 4. Opt-in Settings Sync

Users can opt-in to sync local settings (not credentials) to computers:

```bash
amux settings sync --enable --claude --git
```

This copies configuration files like `~/.claude/settings.json` to the computer,
with sensitive keys automatically filtered out. See `settings.go` for the
filtering logic.

### 5. TUI Integration Architecture

The computer system is designed for TUI integration:

```
┌─────────────────────────────────────────────────────────────────┐
│                        TUI (Bubble Tea)                         │
├─────────────────────────────────────────────────────────────────┤
│  Agent Tab                                                      │
│  ┌─────────────────────┬─────────────────────────────────────┐ │
│  │ Mode: [Local|Cloud] │  Cloud Provider: [Daytona|...]      │ │
│  └─────────────────────┴─────────────────────────────────────┘ │
│                                                                 │
│  When "Cloud" selected:                                        │
│  1. TUI calls computer.EnsureComputer() with agent config       │
│  2. Computer creates/reuses the shared computer                 │
│  3. TUI gets RemoteComputer handle                              │
│  4. PTY streams through computer.RunAgentInteractive()         │
└─────────────────────────────────────────────────────────────────┘
```

**Key integration points:**

1. **Computer Creation**: `EnsureComputer()` handles idempotent computer creation
2. **Credential Setup**: `SetupCredentials()` creates credential directories in home
3. **Agent Execution**: `RunAgentInteractive()` provides PTY integration
4. **Workspace Sync**: `UploadWorkspace()`/`DownloadWorkspace()` for file sync

### 6. Credential Flow

```
First Run:
1. User runs `amux claude`
2. amux creates the shared computer (or reuses existing)
3. Agent prompts for login (OAuth in browser)
4. Credentials stored in ~/.claude/ on the computer
5. User exits

Subsequent Runs:
1. User runs `amux claude`
2. amux reuses the shared computer
3. Credentials already present in ~/.claude/
4. Agent starts immediately (no login needed)

Computer Recreation (--recreate):
1. Old computer deleted, credentials lost
2. New computer created
3. User must re-authenticate
```

## File Structure

```
internal/
├── cli/
│   ├── aliases.go      # Agent shortcuts (amux claude, etc.)
│   ├── auth.go         # Auth commands
│   ├── cli.go          # Root command
│   ├── doctor.go       # Health checks
│   ├── computer.go      # Computer subcommands
│   ├── settings.go     # Settings sync CLI
│   ├── spinner.go      # Progress indicators
│   ├── status.go       # Status, SSH, exec commands
│   └── ...
├── daytona/
│   ├── client.go       # Daytona API client
│   ├── computer.go      # Computer operations
│   ├── volume.go       # Volume management
│   ├── snapshot.go     # Snapshot management
│   └── ...
└── computer/
    ├── provider.go     # Provider interface (multi-provider support)
    ├── providers.go    # Provider registry + resolution (config/env/flags)
    ├── config.go       # Configuration
    ├── credentials.go  # Credential management
    ├── settings.go     # Settings sync
    ├── agent.go        # Agent installation/execution
    ├── sync.go         # Workspace sync
    └── ...
```

## Adding a New Provider

1. Implement the `Provider` interface in a new package (e.g., `internal/e2b/`)
2. Register the provider in the registry
3. Add provider selection to CLI/TUI

Example:

```go
// internal/e2b/provider.go
type E2BProvider struct { ... }

func (p *E2BProvider) Name() string { return "e2b" }
func (p *E2BProvider) CreateComputer(...) (RemoteComputer, error) { ... }
// ... implement remaining methods

// Registration
registry, _ := computer.DefaultProviderRegistry(cfg)
registry.Register(sprites.NewProvider(cfg))
registry.Register(docker.NewProvider(cfg))
// Providers are selected explicitly (no default)
```

## Security Considerations

1. **Credential Isolation**: Each user has their own computer with isolated credentials
2. **Settings Filtering**: Sensitive keys stripped from synced settings
3. **No Credential Logging**: Credentials never appear in logs or output
4. **OAuth-first Authentication**: Agents authenticate via browser/OAuth inside the sandbox

## Future Enhancements

1. **Credential rotation**: Automatic refresh of expired tokens
2. **Backup/restore**: Export/import credentials from the computer
3. **Audit logging**: Track credential access patterns
