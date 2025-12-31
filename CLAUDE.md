# amux - Project Context for Claude

## Overview
amux is a Go TUI (Terminal User Interface) application for managing AI coding agents across git worktrees. Built with the [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework.

## Architecture

### Message-Driven TUI (Bubble Tea Pattern)
```
User Input → tea.Msg → Model.Update() → tea.Cmd → async work → tea.Msg → ...
```

All state changes flow through `Update()`. Side effects return `tea.Cmd` which execute asynchronously and return new messages.

### 3-Pane Layout
```
┌─────────────┬─────────────────────┬────────────┐
│  Dashboard  │       Center        │  Sidebar   │
│  (projects, │    (terminal tabs)  │ (git       │
│  worktrees) │                     │  status)   │
└─────────────┴─────────────────────┴────────────┘
```

### Key Directories
```
internal/
├── app/          # Root App model, global orchestration
├── ui/
│   ├── dashboard/   # Left pane: project/worktree list
│   ├── center/      # Center: terminal tabs with VTerm
│   ├── sidebar/     # Right: git status, file explorer
│   ├── layout/      # Responsive 3-pane layout manager
│   └── common/      # Shared: dialogs, styles, colors, icons
├── data/         # Data models: Project, Worktree, Registry
├── git/          # Git operations, status parsing, file watcher
├── vterm/        # Virtual terminal emulator with ANSI parsing
├── pty/          # PTY wrapper for running agents
├── messages/     # All cross-component message types
├── config/       # App configuration, paths
├── process/      # Script runner, port management
├── validation/   # Input validation helpers
└── logging/      # Debug logging to ~/.amux/logs
```

## Key Patterns

### Component Structure
Each UI component follows this pattern:
```go
type Model struct {
    // State fields
}

func New() *Model { ... }                    // Constructor
func (m *Model) Init() tea.Cmd { ... }       // Initial commands
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) { ... }  // State changes
func (m *Model) View() string { ... }        // Render to string
```

### Message Types (`internal/messages/messages.go`)
- `ProjectsLoaded` - Registry loaded all projects
- `WorktreeActivated` - User selected a worktree
- `LaunchAgent` - Start AI agent in terminal
- `GitStatusResult` - Git status refresh complete
- `ShowAddProjectDialog`, `ShowCreateWorktreeDialog` - Open dialogs
- `DialogResult` - User completed a dialog

### Dashboard Row Types (`internal/ui/dashboard/model.go`)
```go
const (
    RowHome        // "~" home row
    RowAddProject  // "+ Add project"
    RowProject     // Project header (uppercase name)
    RowWorktree    // Worktree entry with status
    RowCreate      // "+ New worktree" button
    RowSpacer      // Empty row
)
```

### Dialog System (`internal/ui/common/dialog.go`)
```go
dialog := common.NewInputDialog("id", "Title", "placeholder")
dialog := common.NewConfirmDialog("id", "Title", "message")
dialog := common.NewSelectDialog("id", "Title", []string{"opt1", "opt2"})
dialog := common.NewAgentPicker("id")
```
Dialogs return `DialogResult` message when complete.

### Styling (`internal/ui/common/colors.go`, `styles.go`)
- Tokyo Night color palette
- Use `lipgloss` for styling: borders, colors, padding
- Common styles: `styles.ProjectHeader`, `styles.StatusClean`, `styles.StatusDirty`

## Development

### Build & Run
```bash
go build -o amux ./cmd/amux && ./amux
```

### Quick Test
```bash
go build ./...
```

### Debug Logging
Logs go to `~/.amux/logs/`. Use:
```go
logging.Debug("message", "key", value)
```

### File Locations
- Registry: `~/.amux/registry.json` - registered projects
- Metadata: `~/.amux/metadata/` - per-worktree state
- Logs: `~/.amux/logs/` - debug logs

## Common Tasks

### Adding a new message type
1. Add to `internal/messages/messages.go`
2. Handle in receiving component's `Update()`
3. Return from sending component as `tea.Cmd`

### Adding a new row type to dashboard
1. Add constant to `RowType` enum
2. Update `rebuildRows()` to include new row
3. Update `renderRow()` for display
4. Update `handleEnter()` for interaction

### Adding a dialog
1. Create with `common.NewInputDialog()` etc
2. Store in `a.dialog`
3. Call `a.dialog.Show()`
4. Handle `DialogResult` in `app.Update()`

### Adding a keyboard shortcut
1. Add to `KeyMap` in `internal/app/keybindings.go`
2. Handle in component's `Update()` with `key.Matches()`

## Conventions

- Use `tea.Batch()` to combine multiple commands
- Return `nil` cmd when no async work needed
- Prefer message passing over direct method calls between components
- All file paths should be absolute
- Validate user input with `internal/validation/`
