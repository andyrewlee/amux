package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/computer"
)

// buildEnhancedDoctorCommand creates the enhanced doctor command.
func buildEnhancedDoctorCommand() *cobra.Command {
	var deep bool
	var fix bool
	var agent string
	var provider string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose and fix common issues",
		Long: `Run diagnostic checks to identify and fix common issues.

By default, runs quick local checks. Use --deep for comprehensive computer checks.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			if deep {
				return runDeepDoctor(ctx, agent, fix, provider)
			}
			return runQuickDoctor(ctx, fix, provider)
		},
	}

	cmd.Flags().BoolVar(&deep, "deep", false, "Run comprehensive computer health checks")
	cmd.Flags().BoolVar(&fix, "fix", false, "Attempt to automatically fix issues")
	cmd.Flags().StringVar(&agent, "agent", "claude", "Agent to check (for --deep)")
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")

	return cmd
}

// runQuickDoctor performs quick local checks.
func runQuickDoctor(ctx context.Context, fix bool, provider string) error {
	fmt.Println("\033[1mRunning diagnostics...\033[0m")
	fmt.Println()

	report, err := computer.RunEnhancedPreflight(ctx, provider, true)
	if err != nil {
		return err
	}

	fmt.Println()
	if report.Passed {
		fmt.Println("\033[32m✓ All checks passed\033[0m")
	} else {
		fmt.Println("\033[31m✗ Some checks failed\033[0m")

		if fix {
			fmt.Println()
			fmt.Println("Attempting fixes...")
			// Run fixes for known issues
			for _, errMsg := range report.Errors {
				if strings.Contains(errMsg, "api_key") {
					fmt.Println("  Run `amux setup` to configure your API key")
				}
				if strings.Contains(errMsg, "ssh") {
					fmt.Println("  Install OpenSSH client for your platform")
				}
			}
		}
	}

	return nil
}

// runDeepDoctor performs comprehensive computer health checks.
func runDeepDoctor(ctx context.Context, agentName string, fix bool, provider string) error {
	fmt.Println("\033[1mRunning deep diagnostics...\033[0m")
	fmt.Println()

	// First run quick checks
	report, err := computer.RunEnhancedPreflight(ctx, provider, true)
	if err != nil {
		return err
	}

	if !report.Passed {
		fmt.Println()
		fmt.Println("\033[31m✗ Basic checks failed - fix these first\033[0m")
		return fmt.Errorf("preflight checks failed")
	}

	fmt.Println()
	fmt.Println("\033[1mChecking computer health...\033[0m")
	fmt.Println()

	// Get or create computer
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, err := computer.LoadConfig()
	if err != nil {
		return err
	}
	providerName := computer.ResolveProviderName(cfg, provider)
	if providerName != computer.DefaultProviderName {
		return fmt.Errorf("deep doctor is only supported for provider %q", computer.DefaultProviderName)
	}
	providerInstance, _, err := computer.ResolveProvider(cfg, cwd, provider)
	if err != nil {
		return err
	}

	snapshotID := computer.ResolveSnapshotID(cfg)

	spinner := NewSpinner("Connecting to computer")
	spinner.Start()

	sb, _, err := computer.EnsureComputer(providerInstance, cwd, computer.ComputerConfig{
		Agent:           computer.Agent(agentName),
		CredentialsMode: "computer",
		Snapshot:        snapshotID,
	}, false)

	if err != nil {
		spinner.StopWithMessage("✗ Could not connect to computer")
		return err
	}
	spinner.StopWithMessage("✓ Connected to computer")

	// Get Daytona client for health checks
	client, err := computer.GetDaytonaClient()
	if err != nil {
		return err
	}

	// Run health checks
	health, err := computer.NewComputerHealth(client, sb, computer.Agent(agentName))
	if err != nil {
		return err
	}
	health.SetVerbose(true)

	fmt.Println()
	fmt.Println("\033[1mComputer Health Checks:\033[0m")
	fmt.Println()

	healthReport := health.Check(ctx)
	fmt.Print(computer.FormatReport(healthReport))

	// Attempt repairs if requested
	if fix && healthReport.Overall != computer.HealthStatusHealthy {
		fmt.Println()
		fmt.Println("\033[1mAttempting repairs...\033[0m")

		if err := health.Repair(ctx); err != nil {
			fmt.Printf("\033[31m✗ Some repairs failed: %v\033[0m\n", err)
		} else {
			fmt.Println("\033[32m✓ Repairs completed\033[0m")

			// Re-check health
			fmt.Println()
			fmt.Println("Re-checking health...")
			newReport := health.Check(ctx)
			fmt.Print(computer.FormatReport(newReport))
		}
	}

	fmt.Println()

	// Show credentials status
	fmt.Println("\033[1mCredential Status:\033[0m")
	fmt.Println()

	credentials := computer.CheckAllAgentCredentials(sb)
	for _, cred := range credentials {
		icon := "\033[31m✗\033[0m"
		status := "not configured"
		if cred.HasCredential {
			icon = "\033[32m✓\033[0m"
			status = "configured"
		}
		fmt.Printf("  %s %s: %s\n", icon, cred.Agent, status)
	}

	// GitHub
	if computer.HasGitHubCredentials(sb) {
		fmt.Printf("  \033[32m✓\033[0m GitHub CLI: authenticated\n")
	} else {
		fmt.Printf("  \033[33m!\033[0m GitHub CLI: not authenticated\n")
	}

	fmt.Println()

	// Show tips
	if healthReport.Overall != computer.HealthStatusHealthy {
		fmt.Println("\033[1mTips:\033[0m")
		fmt.Println("  - Run `amux doctor --deep --fix` to attempt automatic repairs")
		fmt.Println("  - Run `amux computer rm --project` and try again for a fresh start")
		fmt.Println()
	}

	return nil
}

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

Credentials are stored directly on the computer's filesystem in the
home directory. They persist as long as the computer exists.

Storage locations in computer's home directory:
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
1. When you run an agent, amux ensures credential directories exist
2. When the agent authenticates via OAuth, credentials are saved
3. On subsequent runs, credentials are already available
4. Credentials persist as long as the shared computer exists
5. If you delete the computer, you'll need to re-authenticate

Commands:
  amux auth status --all    # Check all credential status
  amux doctor --deep        # Verify credential directories
`,

		"sync": `
╭─────────────────────────────────────────────────────────────────╮
│                      WORKSPACE SYNCING                           │
╰─────────────────────────────────────────────────────────────────╯

amux syncs your local workspace to the computer so agents can access
and modify your files.

Sync Methods:
1. Full Sync (default first time)
   - Creates a tarball of your workspace
   - Uploads and extracts in the computer
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
  amux computer run <agent>  # Run any agent
  amux computer update       # Update agents to latest version
`,

		"snapshots": `
╭─────────────────────────────────────────────────────────────────╮
│                         SNAPSHOTS                                │
╰─────────────────────────────────────────────────────────────────╯

Snapshots are pre-built computer images that include installed agents
and dependencies. They make computer startup much faster.

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
the computer. This is opt-in and requires explicit consent.

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

amux provides ONE shared cloud computer for all your projects:

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
│                   Provider (Daytona/Sprites/Docker)             │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                   Shared Computer                            ││
│  │  ┌─────────────────────────────────────────────────────────┐││
│  │  │  ~/ (home directory)                                    │││
│  │  │    ├── .claude/          # Claude credentials           │││
│  │  │    ├── .codex/           # Codex credentials            │││
│  │  │    ├── .config/gh/       # GitHub CLI credentials       │││
│  │  │    └── ...               # Other agent credentials      │││
│  │  └─────────────────────────────────────────────────────────┘││
│  │  ┌─────────────────────────────────────────────────────────┐││
│  │  │  ~/.amux/workspaces/{worktreeID}/                       │││
│  │  │    (per-project workspace isolation)                    │││
│  │  └─────────────────────────────────────────────────────────┘││
│  │  ┌─────────────────────────────────────────────────────────┐││
│  │  │  Agent (claude/codex/opencode/amp/gemini/droid)         │││
│  │  └─────────────────────────────────────────────────────────┘││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘

Key Components:
  - Provider Interface: Abstracts computer backends (Daytona, Sprites, Docker)
  - Credential Manager: Sets up home directory structure
  - Sync Engine: Uploads/downloads workspace files
  - Agent Plugins: Modular agent installation and configuration
`,
	}

	explanation, ok := explanations[strings.ToLower(topic)]
	if !ok {
		fmt.Println("Unknown topic:", topic)
		fmt.Println()
		fmt.Println("Available topics:")
		fmt.Println("  credentials   How credentials are stored and persisted")
		fmt.Println("  sync          How workspace syncing works")
		fmt.Println("  agents        Supported AI coding agents")
		fmt.Println("  snapshots     Using snapshots for faster startup")
		fmt.Println("  settings      Settings sync configuration")
		fmt.Println("  architecture  Overall system architecture")
		return nil
	}

	fmt.Print(explanation)
	return nil
}

// buildLogsCommand creates the logs command for viewing computer output.
func buildLogsCommand() *cobra.Command {
	var follow bool
	var lines int
	var provider string

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View computer logs and output",
		Long:  "View logs and output from the current workspace's computer.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}

			snapshotID := computer.ResolveSnapshotID(cfg)

			providerInstance, _, err := computer.ResolveProvider(cfg, cwd, provider)
			if err != nil {
				return err
			}

			// Get computer
			sb, _, err := computer.EnsureComputer(providerInstance, cwd, computer.ComputerConfig{
				Agent:           computer.AgentShell,
				CredentialsMode: "none",
				Snapshot:        snapshotID,
			}, false)
			if err != nil {
				return err
			}

			// Get logs from computer
			logCmd := fmt.Sprintf("journalctl --no-pager -n %d", lines)
			if follow {
				logCmd = "journalctl -f"
			}

			resp, err := sb.Exec(context.Background(), logCmd, nil)
			if err != nil {
				// Fallback to dmesg
				resp, err = sb.Exec(context.Background(), fmt.Sprintf("dmesg | tail -n %d", lines), nil)
				if err != nil {
					return fmt.Errorf("could not retrieve logs: %w", err)
				}
			}

			if resp.Stdout != "" {
				fmt.Print(resp.Stdout)
			} else {
				fmt.Println("No logs available")
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of lines to show")
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")

	return cmd
}
