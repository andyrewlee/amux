# amux Sandbox Architecture

## Overview

The amux sandbox system provides a unified interface for running coding agents
(Claude, Codex, OpenCode, Amp, Gemini, Droid) in cloud sandboxes with persistent
credentials and settings.

## Core Design Principles

### 1. Provider Agnostic

The `Provider` interface (`provider.go`) abstracts sandbox backends:

```go
type Provider interface {
    Name() string
    CreateSandbox(ctx, config) (RemoteSandbox, error)
    GetSandbox(ctx, id) (RemoteSandbox, error)
    ListSandboxes(ctx) ([]RemoteSandbox, error)
    DeleteSandbox(ctx, id) error
    Volumes() VolumeManager
    Snapshots() SnapshotManager
    SupportsFeature(feature) bool
}
```

Currently Daytona is the default provider. Future providers (E2B, Modal, etc.)
can be added by implementing this interface.

### 2. Persistent Credentials

Credentials persist across sandbox sessions via a shared volume:

```
Volume: amux-credentials
Mount: /mnt/amux-credentials

Structure:
├── claude/          # ~/.claude symlink target
├── codex/           # ~/.codex, ~/.config/codex symlink target
├── opencode/        # ~/.local/share/opencode symlink target
├── amp/             # ~/.config/amp, ~/.local/share/amp symlink target
├── gemini/          # ~/.gemini symlink target
├── factory/         # ~/.factory symlink target (Droid)
├── gh/              # ~/.config/gh symlink target (GitHub CLI)
└── git/             # .gitconfig symlink target
```

Each agent's credential directory is symlinked from the sandbox home to the
volume, ensuring credentials survive sandbox restarts and recreations.

### 3. Opt-in Settings Sync

Users can opt-in to sync local settings (not credentials) to sandboxes:

```bash
amux settings sync --enable --claude --git
```

This copies configuration files like `~/.claude/settings.json` to the volume,
with sensitive keys automatically filtered out. See `settings.go` for the
filtering logic.

### 4. TUI Integration Architecture

The sandbox system is designed for TUI integration:

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
│  1. TUI calls sandbox.EnsureSandbox() with agent config       │
│  2. Sandbox creates/reuses sandbox with credentials volume    │
│  3. TUI gets RemoteSandbox handle                              │
│  4. PTY streams through sandbox.RunAgentInteractive()         │
└─────────────────────────────────────────────────────────────────┘
```

**Key integration points:**

1. **Sandbox Creation**: `EnsureSandbox()` handles idempotent sandbox creation
2. **Credential Setup**: `SetupCredentials()` prepares the volume mounts
3. **Agent Execution**: `RunAgentInteractive()` provides PTY integration
4. **Workspace Sync**: `UploadWorkspace()`/`DownloadWorkspace()` for file sync

### 5. Credential Flow

```
First Run:
1. User runs `amux claude`
2. amux creates sandbox with credentials volume
3. Agent prompts for login (if needed)
4. Credentials stored in volume
5. User exits

Subsequent Runs:
1. User runs `amux claude`
2. amux reuses/creates sandbox with same volume
3. Credentials already present via symlinks
4. Agent starts immediately (no login needed)
```

## File Structure

```
internal/
├── cli/
│   ├── aliases.go      # Agent shortcuts (amux claude, etc.)
│   ├── auth.go         # Auth commands
│   ├── cli.go          # Root command
│   ├── doctor.go       # Health checks
│   ├── sandbox.go      # Sandbox subcommands
│   ├── settings.go     # Settings sync CLI
│   ├── spinner.go      # Progress indicators
│   ├── status.go       # Status, SSH, exec commands
│   └── ...
├── daytona/
│   ├── client.go       # Daytona API client
│   ├── sandbox.go      # Sandbox operations
│   ├── volume.go       # Volume management
│   ├── snapshot.go     # Snapshot management
│   └── ...
└── sandbox/
    ├── provider.go     # Provider interface (multi-provider support)
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
func (p *E2BProvider) CreateSandbox(...) (RemoteSandbox, error) { ... }
// ... implement remaining methods

// Registration
registry := sandbox.NewProviderRegistry()
registry.Register(daytona.NewProvider(config))
registry.Register(e2b.NewProvider(config))
registry.SetDefault("daytona")
```

## Security Considerations

1. **Credential Isolation**: Each user has their own credentials volume
2. **Settings Filtering**: Sensitive keys stripped from synced settings
3. **No Credential Logging**: Credentials never appear in logs or output
4. **Volume Permissions**: Volumes are private to the user's account

## Future Enhancements

1. **Multi-workspace volumes**: Separate credential volumes per project
2. **Credential rotation**: Automatic refresh of expired tokens
3. **Backup/restore**: Export/import credential volumes
4. **Audit logging**: Track credential access patterns
