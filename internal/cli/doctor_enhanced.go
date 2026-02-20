package cli

import (
	"context"
	"errors"
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
	fmt.Fprintln(cliStdout, "\033[1mRunning diagnostics...\033[0m")
	fmt.Fprintln(cliStdout)

	report, err := sandbox.RunEnhancedPreflight(ctx, true)
	if err != nil {
		return err
	}

	fmt.Fprintln(cliStdout)
	if report.Passed {
		fmt.Fprintln(cliStdout, "\033[32m✓ All checks passed\033[0m")
	} else {
		fmt.Fprintln(cliStdout, "\033[31m✗ Some checks failed\033[0m")

		if fix {
			fmt.Fprintln(cliStdout)
			fmt.Fprintln(cliStdout, "Attempting fixes...")
			// Run fixes for known issues
			for _, errMsg := range report.Errors {
				if strings.Contains(errMsg, "api_key") {
					fmt.Fprintln(cliStdout, "  Run `amux setup` to configure your API key")
				}
				if strings.Contains(errMsg, "ssh") {
					fmt.Fprintln(cliStdout, "  Install OpenSSH client for your platform")
				}
			}
		}
	}

	return nil
}

// runDeepDoctor performs comprehensive sandbox health checks.
func runDeepDoctor(ctx context.Context, agentName string, fix bool) error {
	fmt.Fprintln(cliStdout, "\033[1mRunning deep diagnostics...\033[0m")
	fmt.Fprintln(cliStdout)

	// First run quick checks
	report, err := sandbox.RunEnhancedPreflight(ctx, true)
	if err != nil {
		return err
	}

	if !report.Passed {
		fmt.Fprintln(cliStdout)
		fmt.Fprintln(cliStdout, "\033[31m✗ Basic checks failed - fix these first\033[0m")
		return errors.New("preflight checks failed")
	}

	fmt.Fprintln(cliStdout)
	fmt.Fprintln(cliStdout, "\033[1mChecking sandbox health...\033[0m")
	fmt.Fprintln(cliStdout)

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

	fmt.Fprintln(cliStdout)
	fmt.Fprintln(cliStdout, "\033[1mSandbox Health Checks:\033[0m")
	fmt.Fprintln(cliStdout)

	healthReport := health.Check(ctx)
	fmt.Fprint(cliStdout, sandbox.FormatReport(healthReport))

	// Attempt repairs if requested
	if fix && healthReport.Overall != sandbox.HealthStatusHealthy {
		fmt.Fprintln(cliStdout)
		fmt.Fprintln(cliStdout, "\033[1mAttempting repairs...\033[0m")

		if err := health.Repair(ctx); err != nil {
			fmt.Fprintf(cliStdout, "\033[31m✗ Some repairs failed: %v\033[0m\n", err)
		} else {
			fmt.Fprintln(cliStdout, "\033[32m✓ Repairs completed\033[0m")

			// Re-check health
			fmt.Fprintln(cliStdout)
			fmt.Fprintln(cliStdout, "Re-checking health...")
			newReport := health.Check(ctx)
			fmt.Fprint(cliStdout, sandbox.FormatReport(newReport))
		}
	}

	fmt.Fprintln(cliStdout)

	// Show credentials status
	fmt.Fprintln(cliStdout, "\033[1mCredential Status:\033[0m")
	fmt.Fprintln(cliStdout)

	credentials := sandbox.CheckAllAgentCredentials(sb)
	for _, cred := range credentials {
		icon := "\033[31m✗\033[0m"
		status := "not configured"
		if cred.HasCredential {
			icon = "\033[32m✓\033[0m"
			status = "configured"
		}
		fmt.Fprintf(cliStdout, "  %s %s: %s\n", icon, cred.Agent, status)
	}

	// GitHub
	if sandbox.HasGitHubCredentials(sb) {
		fmt.Fprintf(cliStdout, "  \033[32m✓\033[0m GitHub CLI: authenticated\n")
	} else {
		fmt.Fprintf(cliStdout, "  \033[33m!\033[0m GitHub CLI: not authenticated\n")
	}

	fmt.Fprintln(cliStdout)

	// Show tips
	if healthReport.Overall != sandbox.HealthStatusHealthy {
		fmt.Fprintln(cliStdout, "\033[1mTips:\033[0m")
		fmt.Fprintln(cliStdout, "  - Run `amux doctor --deep --fix` to attempt automatic repairs")
		fmt.Fprintln(cliStdout, "  - Run `amux sandbox rm --project` and try again for a fresh start")
		fmt.Fprintln(cliStdout)
	}

	return nil
}
