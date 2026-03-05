package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// buildExplainCommand creates the explain command for learning about amux concepts.
func buildExplainCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <topic>",
		Short: "Explain amux concepts and architecture",
		Long: `Get detailed explanations of how amux works.

Available topics:
  credentials   How credentials are stored and persisted
  sync          How workspace syncing works
  agents        Supported AI coding agents
  snapshots     Using snapshots for faster startup
  settings      Settings sync configuration
  architecture  Overall system architecture`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return explainTopic(args[0])
		},
	}

	return cmd
}

func explainTopic(topic string) error {
	explanations := map[string]string{
		"credentials": `
╭─────────────────────────────────────────────────────────────────╮
│                    CREDENTIALS PERSISTENCE                       │
╰─────────────────────────────────────────────────────────────────╯

Credentials are stored on a persistent volume mounted at /amux and
symlinked into the sandbox home directory. They persist across sandboxes.

Storage locations in sandbox's home directory (backed by /amux/home):
├── ~/.claude/               # Claude CLI credentials
│   └── .credentials.json
├── ~/.codex/                # Codex CLI credentials
│   └── auth.json
├── ~/.config/codex/         # Codex config
├── ~/.local/share/opencode/ # OpenCode credentials
├── ~/.config/amp/           # Amp config
├── ~/.local/share/amp/      # Amp data
├── ~/.gemini/               # Gemini CLI credentials
├── ~/.factory/              # Droid credentials
├── ~/.config/gh/            # GitHub CLI credentials
└── ~/.gitconfig             # Git configuration

How it works:
1. amux mounts a persistent volume at /amux for each sandbox
2. It symlinks credential + cache dirs (e.g., ~/.config, ~/.local, ~/.claude)
3. When the agent authenticates via OAuth, credentials are saved there
4. New sandboxes reuse the same volume, so credentials and CLI installs persist
5. To reset, delete the amux-persist volume in Daytona

Commands:
  amux auth status --all    # Check all credential status
  amux doctor --deep        # Verify credential directories
`,

		"sync": `
╭─────────────────────────────────────────────────────────────────╮
│                      WORKSPACE SYNCING                           │
╰─────────────────────────────────────────────────────────────────╯

amux syncs your local workspace to the sandbox so agents can access
and modify your files.

Sync Methods:
1. Full Sync (default first time)
   - Creates a tarball of your workspace
   - Uploads and extracts in the sandbox
   - Respects .amuxignore patterns

2. Incremental Sync (subsequent runs)
   - Computes file hashes and timestamps
   - Only transfers changed files
   - Much faster for large workspaces

Ignored by default:
  - .git/
  - node_modules/
  - __pycache__/
  - .env files
  - Build artifacts

Custom ignores (.amuxignore):
  # Add patterns like .gitignore
  *.log
  dist/
  .cache/

Commands:
  amux claude                # Syncs workspace automatically
  amux claude --no-sync      # Skip sync (use existing files)
`,

		"agents": `
╭─────────────────────────────────────────────────────────────────╮
│                      SUPPORTED AGENTS                            │
╰─────────────────────────────────────────────────────────────────╯

amux supports multiple AI coding agents:

┌──────────┬─────────────────────┬─────────────────────────────────┐
│ Agent    │ Provider            │ Installation                    │
├──────────┼─────────────────────┼─────────────────────────────────┤
│ claude   │ Anthropic           │ npm install -g @anthropic-ai/   │
│          │                     │ claude-code                     │
├──────────┼─────────────────────┼─────────────────────────────────┤
│ codex    │ OpenAI              │ npm install -g @openai/codex    │
├──────────┼─────────────────────┼─────────────────────────────────┤
│ gemini   │ Google              │ npm install -g @google/         │
│          │                     │ gemini-cli                      │
├──────────┼─────────────────────┼─────────────────────────────────┤
│ opencode │ Open Source         │ curl opencode.ai/install | bash │
├──────────┼─────────────────────┼─────────────────────────────────┤
│ amp      │ Sourcegraph         │ curl ampcode.com/install.sh |   │
│          │                     │ bash                            │
├──────────┼─────────────────────┼─────────────────────────────────┤
│ droid    │ Factory             │ curl app.factory.ai/cli | sh    │
├──────────┼─────────────────────┼─────────────────────────────────┤
│ shell    │ -                   │ Built-in bash shell             │
└──────────┴─────────────────────┴─────────────────────────────────┘

Commands:
  amux claude               # Run Claude Code
  amux codex                # Run Codex
  amux sandbox run <agent>   # Run any agent
  amux sandbox update        # Update agents to latest version
`,

		"snapshots": `
╭─────────────────────────────────────────────────────────────────╮
│                         SNAPSHOTS                                │
╰─────────────────────────────────────────────────────────────────╯

Snapshots are pre-built sandbox images that include installed agents
and dependencies. They make sandbox startup much faster.

Benefits:
  - Instant startup (vs 30+ seconds for fresh install)
  - Consistent environment across sessions
  - Pre-configured tools and settings

Creating a snapshot:
  amux snapshot create --name my-snapshot
  amux snapshot create --agents claude,codex  # With specific agents

Using a snapshot:
  amux claude --snapshot my-snapshot
  amux config set defaultSnapshot my-snapshot  # Use by default

Listing snapshots:
  amux snapshot ls

Commands:
  amux snapshot create    # Create a new snapshot
  amux snapshot ls        # List all snapshots
  amux snapshot rm <id>   # Delete a snapshot
`,

		"settings": `
╭─────────────────────────────────────────────────────────────────╮
│                      SETTINGS SYNC                               │
╰─────────────────────────────────────────────────────────────────╯

Settings sync copies your local preferences (NOT credentials) to
the sandbox. This is opt-in and requires explicit consent.

What gets synced:
  ✓ Claude settings (~/.claude/settings.json)
    - Model preferences
    - Feature flags
    - Permission settings

  ✓ Git config (~/.gitconfig - safe keys only)
    - user.name, user.email
    - Aliases
    - Editor preferences
    - NOT credentials or tokens

What does NOT get synced:
  ✗ API keys
  ✗ Tokens
  ✗ Passwords
  ✗ Private keys
  ✗ Credential helpers

Enabling settings sync:
  amux settings sync --enable --claude --git

Checking status:
  amux settings status

Disabling:
  amux settings sync --disable
`,

		"architecture": `
╭─────────────────────────────────────────────────────────────────╮
│                      ARCHITECTURE                                │
╰─────────────────────────────────────────────────────────────────╯

amux creates a fresh Daytona sandbox per run and mounts a persistent volume:

┌─────────────────────────────────────────────────────────────────┐
│                        Your Machine                              │
├─────────────────────────────────────────────────────────────────┤
│  amux CLI                                                        │
│    ├─ Preflight checks                                          │
│    ├─ Workspace sync                                            │
│    └─ SSH connection                                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ HTTPS API
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                           Daytona                               │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                    Ephemeral Sandbox                         ││
│  │  ┌─────────────────────────────────────────────────────────┐││
│  │  │  /amux (persistent volume)                               │││
│  │  │    └── /amux/home/... (credentials + CLI caches)         │││
│  │  └─────────────────────────────────────────────────────────┘││
│  │  ┌─────────────────────────────────────────────────────────┐││
│  │  │  /workspace/{worktreeID}/                                │││
│  │  │    (per-project workspace isolation)                     │││
│  │  └─────────────────────────────────────────────────────────┘││
│  │  ┌─────────────────────────────────────────────────────────┐││
│  │  │  Agent (claude/codex/opencode/amp/gemini/droid)          │││
│  │  └─────────────────────────────────────────────────────────┘││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘

Key Components:
  - Provider Interface: Daytona provider (additional providers removed)
  - Persistence Manager: Mounts volume + home directory symlinks
  - Sync Engine: Uploads/downloads workspace files
  - Agent Plugins: Modular agent installation and configuration
`,
	}

	explanation, ok := explanations[strings.ToLower(topic)]
	if !ok {
		fmt.Fprintln(cliStdout, "Unknown topic:", topic)
		fmt.Fprintln(cliStdout)
		fmt.Fprintln(cliStdout, "Available topics:")
		fmt.Fprintln(cliStdout, "  credentials   How credentials are stored and persisted")
		fmt.Fprintln(cliStdout, "  sync          How workspace syncing works")
		fmt.Fprintln(cliStdout, "  agents        Supported AI coding agents")
		fmt.Fprintln(cliStdout, "  snapshots     Using snapshots for faster startup")
		fmt.Fprintln(cliStdout, "  settings      Settings sync configuration")
		fmt.Fprintln(cliStdout, "  architecture  Overall system architecture")
		return nil
	}

	fmt.Fprint(cliStdout, explanation)
	return nil
}
