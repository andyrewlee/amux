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

	// Create a sandbox for diagnostics
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, err := sandbox.LoadConfig()
	if err != nil {
		return err
	}
	providerInstance, _, err := sandbox.ResolveProvider(cfg, cwd, "")
	if err != nil {
		return err
	}

	snapshotID := sandbox.ResolveSnapshotID(cfg)

	spinner := NewSpinner("Connecting to sandbox")
	spinner.Start()

	sb, _, err := sandbox.CreateSandboxSession(providerInstance, cwd, sandbox.SandboxConfig{
		Agent:                 sandbox.Agent(agentName),
		Snapshot:              snapshotID,
		Ephemeral:             true,
		PersistenceVolumeName: sandbox.ResolvePersistenceVolumeName(cfg),
	})

	if err != nil {
		spinner.StopWithMessage("✗ Could not connect to sandbox")
		return err
	}
	spinner.StopWithMessage("✓ Connected to sandbox")

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = sb.Stop(ctx)
		_ = providerInstance.DeleteSandbox(ctx, sb.ID())
		_ = sandbox.RemoveSandboxMetaByID(sb.ID())
	}
	defer cleanup()

	if err := sandbox.SetupCredentials(sb, sandbox.CredentialsConfig{
		Mode:             "sandbox",
		Agent:            sandbox.Agent(agentName),
		SettingsSyncMode: "skip",
	}, false); err != nil {
		return err
	}

	// Get Daytona client for health checks
	client, err := sandbox.GetDaytonaClient()
	if err != nil {
		return err
	}

	// Run health checks
	health, err := sandbox.NewSandboxHealth(client, sb, sandbox.Agent(agentName))
	if err != nil {
		return err
	}
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
		fmt.Println("  - Run `amux sandbox rm --project` and try again for a fresh start")
		fmt.Println()
	}

	return nil
}
