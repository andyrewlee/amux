package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

// buildEnhancedDoctorCommand creates the enhanced doctor command.
func buildEnhancedDoctorCommand() *cobra.Command {
	var deep bool
	var fix bool
	var agent string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose and fix common issues",
		Long: `Run diagnostic checks to identify and fix common issues.

By default, runs quick local checks. Use --deep for comprehensive sandbox checks.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			if deep {
				return runDeepDoctor(ctx, agent, fix)
			}
			return runQuickDoctor(ctx, fix)
		},
	}

	cmd.Flags().BoolVar(&deep, "deep", false, "Run comprehensive sandbox health checks")
	cmd.Flags().BoolVar(&fix, "fix", false, "Attempt to automatically fix issues")
	cmd.Flags().StringVar(&agent, "agent", "claude", "Agent to check (for --deep)")

	return cmd
}

// runQuickDoctor performs quick local checks.
func runQuickDoctor(ctx context.Context, fix bool) error {
	fmt.Println("\033[1mRunning diagnostics...\033[0m")
	fmt.Println()

	report, err := sandbox.RunEnhancedPreflight(ctx, true)
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

// runDeepDoctor performs comprehensive sandbox health checks.
func runDeepDoctor(ctx context.Context, agentName string, fix bool) error {
	fmt.Println("\033[1mRunning deep diagnostics...\033[0m")
	fmt.Println()

	// First run quick checks
	report, err := sandbox.RunEnhancedPreflight(ctx, true)
	if err != nil {
		return err
	}

	if !report.Passed {
		fmt.Println()
		fmt.Println("\033[31m✗ Basic checks failed - fix these first\033[0m")
		return fmt.Errorf("preflight checks failed")
	}

	fmt.Println()
	fmt.Println("\033[1mChecking sandbox health...\033[0m")
	fmt.Println()

	// Get or create sandbox
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, err := sandbox.LoadConfig()
	if err != nil {
		return err
	}

	snapshotID := sandbox.ResolveSnapshotID(cfg)

	spinner := NewSpinner("Connecting to sandbox")
	spinner.Start()

	sb, _, err := sandbox.EnsureSandbox(cwd, sandbox.SandboxConfig{
		Agent:           sandbox.Agent(agentName),
		CredentialsMode: "sandbox",
		Snapshot:        snapshotID,
	}, false)

	if err != nil {
		spinner.StopWithMessage("✗ Could not connect to sandbox")
		return err
	}
	spinner.StopWithMessage("✓ Connected to sandbox")

	// Get Daytona client for health checks
	client, err := sandbox.GetDaytonaClient()
	if err != nil {
		return err
	}

	// Run health checks
	health := sandbox.NewSandboxHealth(client, sb, sandbox.Agent(agentName))
	health.SetVerbose(true)

	fmt.Println()
	fmt.Println("\033[1mSandbox Health Checks:\033[0m")
	fmt.Println()

	healthReport := health.Check(ctx)
	fmt.Print(sandbox.FormatReport(healthReport))

	// Attempt repairs if requested
	if fix && healthReport.Overall != sandbox.HealthStatusHealthy {
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
			fmt.Print(sandbox.FormatReport(newReport))
		}
	}

	fmt.Println()

	// Show credentials status
	fmt.Println("\033[1mCredential Status:\033[0m")
	fmt.Println()

	credentials := sandbox.CheckAllAgentCredentials(sb)
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
	if sandbox.HasGitHubCredentials(sb) {
		fmt.Printf("  \033[32m✓\033[0m GitHub CLI: authenticated\n")
	} else {
		fmt.Printf("  \033[33m!\033[0m GitHub CLI: not authenticated\n")
	}

	fmt.Println()

	// Show tips
	if healthReport.Overall != sandbox.HealthStatusHealthy {
		fmt.Println("\033[1mTips:\033[0m")
		fmt.Println("  - Run `amux doctor --deep --fix` to attempt automatic repairs")
		fmt.Println("  - Run `amux sandbox rm --workspace` and try again for a fresh start")
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

Credentials are stored in a persistent Daytona volume that survives
sandbox restarts and recreations.

Volume: amux-credentials
Mount:  /mnt/amux-credentials

Structure:
├── claude/          # ~/.claude symlink target
│   └── .credentials.json
├── codex/           # ~/.codex, ~/.config/codex symlink target
│   └── auth.json
├── opencode/        # ~/.local/share/opencode symlink target
├── amp/             # ~/.config/amp, ~/.local/share/amp symlink target
├── gemini/          # ~/.gemini symlink target
├── factory/         # ~/.factory symlink target (Droid)
├── gh/              # ~/.config/gh symlink target (GitHub CLI)
└── git/             # .gitconfig symlink target

How it works:
1. When you run an agent, amux creates symlinks from the sandbox's
   home directory to the credentials volume
2. When the agent authenticates, credentials are written to the volume
3. On subsequent runs, credentials are already available via the symlinks
4. Credentials persist even if the sandbox is deleted and recreated

Commands:
  amux auth status --all    # Check all credential status
  amux doctor --deep        # Verify credential symlinks
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
  amux sandbox run <agent>  # Run any agent
  amux sandbox update       # Update agents to latest version
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

amux uses Daytona for cloud sandbox execution:

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
│                      Daytona Cloud                               │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐   ┌─────────────────────────────────────────┐  │
│  │  Volumes    │   │            Sandbox                       │  │
│  │             │   │  ┌─────────────────────────────────────┐ │  │
│  │ credentials ├───┼──│  /mnt/amux-credentials              │ │  │
│  │             │   │  └─────────────────────────────────────┘ │  │
│  └─────────────┘   │  ┌─────────────────────────────────────┐ │  │
│                    │  │  /workspace                          │ │  │
│                    │  │  (your synced code)                  │ │  │
│                    │  └─────────────────────────────────────┘ │  │
│                    │  ┌─────────────────────────────────────┐ │  │
│                    │  │  Agent (claude/codex/etc)           │ │  │
│                    │  └─────────────────────────────────────┘ │  │
│                    └─────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘

Key Components:
  - Provider Interface: Abstracts sandbox backends (Daytona, future: E2B)
  - Credential Manager: Handles volume mounting and symlinks
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

// buildLogsCommand creates the logs command for viewing sandbox output.
func buildLogsCommand() *cobra.Command {
	var follow bool
	var lines int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View sandbox logs and output",
		Long:  "View logs and output from the current workspace's sandbox.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}

			snapshotID := sandbox.ResolveSnapshotID(cfg)

			// Get sandbox
			sb, _, err := sandbox.EnsureSandbox(cwd, sandbox.SandboxConfig{
				Agent:           sandbox.AgentShell,
				CredentialsMode: "none",
				Snapshot:        snapshotID,
			}, false)
			if err != nil {
				return err
			}

			// Get logs from sandbox
			logCmd := fmt.Sprintf("journalctl --no-pager -n %d", lines)
			if follow {
				logCmd = "journalctl -f"
			}

			resp, err := sb.Process.ExecuteCommand(logCmd)
			if err != nil {
				// Fallback to dmesg
				resp, err = sb.Process.ExecuteCommand(fmt.Sprintf("dmesg | tail -n %d", lines))
				if err != nil {
					return fmt.Errorf("could not retrieve logs: %w", err)
				}
			}

			if resp.Artifacts != nil && resp.Artifacts.Stdout != "" {
				fmt.Print(resp.Artifacts.Stdout)
			} else if resp.Result != "" {
				fmt.Print(resp.Result)
			} else {
				fmt.Println("No logs available")
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of lines to show")

	return cmd
}
