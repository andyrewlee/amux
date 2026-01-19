# amux Sandbox Architecture

## Overview

The amux sandbox system provides a unified interface for running coding agents
(Claude, Codex, OpenCode, Amp, Gemini, Droid) in Daytona sandboxes with
persistent credentials and settings.

## Core Design Principles

### 1. Fresh Sandbox Per Run

amux creates a **new sandbox per run**. Workspace isolation is achieved via a
`worktreeID` - each project gets its own workspace directory inside the sandbox:

```
/workspace/{worktreeID}/repo   # Each project's workspace
```

The `worktreeID` is a SHA256 hash of the absolute working directory path,
ensuring each project has a unique workspace without reusing sandboxes.

**Benefits:**
- Clean environments for each session
- No per-project sandbox lifecycle management
- Easy cleanup with ephemeral sandboxes

### 2. Daytona Provider (Today)

The `Provider` interface (`provider.go`) abstracts sandbox backends. amux ships
with the Daytona provider only; provider selection flags/env have been removed.

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

### 3. Persistent Credentials via Volume

Credentials and CLI caches are stored on a persistent volume mounted at `/amux`.
On sandbox startup, amux symlinks credential/cache directories into the sandbox
home directory so they persist across sandboxes.

```
/amux/home/.claude
/amux/home/.codex
/amux/home/.config
/amux/home/.local
/amux/home/.npm
/amux/home/.factory
```

Deleting a sandbox does **not** delete credentials. The volume name is stored in
`~/.amux/config.json` as `persistenceVolumeName` (default `amux-persist`).

To reset persistence, amux rotates to a new volume via `amux sandbox reset`.
Old volumes are retained for manual cleanup in Daytona.

Recorded session logs (when `--record` is used) are stored under:

```
/amux/logs/{worktreeId}/YYYYMMDD-HHMMSS-agent.log
```

### 4. Opt-in Settings Sync

Users can opt-in to sync local settings (not credentials) to sandboxes:

```bash
amux settings sync --enable --claude --git
```

This copies configuration files like `~/.claude/settings.json` to the sandbox,
with sensitive keys automatically filtered out. See `settings.go` for the
filtering logic.

### 5. TUI Integration Architecture

The sandbox system is designed for TUI integration:

- TUI (Bubble Tea) Agent Tab
- When "Cloud" is selected:
  1. TUI calls `sandbox.CreateSandboxSession()` with agent config
  2. New sandbox created (ephemeral)
  3. TUI gets `RemoteSandbox` handle
  4. PTY streams through `sandbox.RunAgentInteractive()`

**Key integration points:**

1. **Sandbox Creation**: `CreateSandboxSession()` always creates a new sandbox
2. **Credential Setup**: `SetupCredentials()` mounts persistence + home symlinks
3. **Agent Execution**: `RunAgentInteractive()` provides PTY integration
4. **Workspace Sync**: `UploadWorkspace()`/`DownloadWorkspace()` for file sync

### 6. Credential Flow

```
First Run:
1. User runs `amux claude`
2. amux creates a new sandbox
3. amux mounts /amux and symlinks home directories
4. Agent prompts for login (OAuth in browser)
5. Credentials stored under /amux/home
6. User exits; sandbox is deleted

Subsequent Runs:
1. User runs `amux claude`
2. amux creates a new sandbox
3. /amux is mounted again; credentials already present
4. Agent starts immediately (no login needed)
```

## File Structure

```
internal/
|-- cli/
|   |-- aliases.go      # Agent shortcuts (amux claude, etc.)
|   |-- auth.go         # Auth commands
|   |-- cli.go          # Root command
|   |-- doctor.go       # Health checks
|   |-- sandbox.go      # Sandbox subcommands
|   |-- settings.go     # Settings sync CLI
|   |-- spinner.go      # Progress indicators
|   |-- status.go       # Status, SSH, exec commands
|   `-- ...
|-- daytona/
|   |-- client.go       # Daytona API client
|   |-- sandbox.go      # Sandbox operations
|   |-- volume.go       # Volume management
|   |-- snapshot.go     # Snapshot management
|   `-- ...
`-- sandbox/
    |-- provider.go     # Provider interface (Daytona)
    |-- providers.go    # Provider registry + resolution
    |-- config.go       # Configuration
    |-- credentials.go  # Credential management
    |-- settings.go     # Settings sync
    |-- agent.go        # Agent installation/execution
    |-- sync.go         # Workspace sync
    `-- ...
```

## Adding a New Provider (Optional)

If additional providers are reintroduced in the future:

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
registry, _ := sandbox.DefaultProviderRegistry(cfg)
registry.Register(newE2BProvider(cfg))
```

## Security Considerations

1. **Credential Isolation**: Each user has their own persistent volume
2. **Settings Filtering**: Sensitive keys stripped from synced settings
3. **No Credential Logging**: Credentials never appear in logs or output
4. **OAuth-first Authentication**: Agents authenticate via browser/OAuth inside the sandbox

## Future Enhancements

1. **Credential rotation**: Automatic refresh of expired tokens
2. **Backup/restore**: Export/import credentials from the persistent volume
3. **Audit logging**: Track credential access patterns
