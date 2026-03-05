package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func buildSandboxUpdateCommand() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "update [agent]",
		Short: "Update agent CLIs to latest versions",
		Long: `Update agent CLIs to their latest versions in the current project sandbox.

If no agent is specified, updates the default agent (claude).
Use --all to update all supported agents.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			// Get current sandbox
			spinner := NewSpinner("Connecting to sandbox")
			spinner.Start()
			sb, _, err := resolveCurrentSandbox(providerInstance, cwd)
			if err != nil {
				spinner.StopWithMessage("✗ Failed to connect")
				return err
			}
			spinner.StopWithMessage("✓ Connected")

			if all {
				// Update all agents
				fmt.Fprintln(cliStdout, "Updating all agents...")
				if err := sandbox.UpdateAllAgents(sb, true); err != nil {
					return err
				}
				fmt.Fprintln(cliStdout, "✓ All agents updated")
			} else {
				// Update specific agent
				agentName := "claude"
				if len(args) > 0 {
					agentName = args[0]
				}
				if !sandbox.IsValidAgent(agentName) {
					return errors.New("invalid agent: use claude, codex, opencode, amp, gemini, or droid")
				}
				agent := sandbox.Agent(agentName)

				spinner := NewSpinner(fmt.Sprintf("Updating %s", agent))
				spinner.Start()
				if err := sandbox.UpdateAgent(sb, agent, false); err != nil {
					spinner.StopWithMessage(fmt.Sprintf("✗ Failed to update %s", agent))
					return err
				}
				spinner.StopWithMessage(fmt.Sprintf("✓ %s updated", agent))
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Update all agents")

	return cmd
}

func buildSandboxRmCommand() *cobra.Command {
	var project bool
	cmd := &cobra.Command{
		Use:   "rm [id]",
		Short: "Remove a sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			if project {
				if err := sandbox.RemoveSandbox(providerInstance, cwd, ""); err != nil {
					return err
				}
				fmt.Fprintln(cliStdout, "Removed sandbox for current project")
				return nil
			}
			if len(args) == 0 {
				return errors.New("provide a sandbox ID or use --project to remove the current project sandbox")
			}
			if err := sandbox.RemoveSandbox(providerInstance, cwd, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cliStdout, "Removed sandbox %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "Remove sandbox for current project")
	return cmd
}

func buildSandboxResetCommand() *cobra.Command {
	var name string
	var yes bool

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset persistent sandbox data (credentials and CLI caches)",
		Long: `Reset persistent sandbox data by switching to a new persistence volume.

This does NOT delete the old volume; it simply rotates to a new one so future
sandboxes start clean without requiring manual Daytona cleanup.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}
			current := sandbox.ResolvePersistenceVolumeName(cfg)

			next := strings.TrimSpace(name)
			if next == "" {
				next = "amux-persist-" + time.Now().UTC().Format("20060102-150405")
			}
			if next == current {
				return fmt.Errorf("new persistence volume name matches current: %s", current)
			}

			if !yes {
				fmt.Fprintln(cliStdout, "This will switch to a fresh persistence volume.")
				fmt.Fprintf(cliStdout, "Current volume: %s\n", current)
				fmt.Fprintf(cliStdout, "New volume:     %s\n", next)
				fmt.Fprintln(cliStdout, "You will need to re-authenticate agents in the new sandbox.")
				if !confirmChoice("Continue? [y/N]: ") {
					fmt.Fprintln(cliStdout, "Canceled.")
					return nil
				}
			}

			cfg.PersistenceVolumeName = next
			if err := sandbox.SaveConfig(cfg); err != nil {
				return err
			}

			// Best-effort: create the new volume now so first run is fast.
			cwd, err := os.Getwd()
			if err == nil {
				if provider, _, err := sandbox.ResolveProvider(cfg, cwd, ""); err == nil {
					if provider.SupportsFeature(sandbox.FeatureVolumes) && provider.Volumes() != nil {
						if _, err := provider.Volumes().GetOrCreate(context.Background(), next); err == nil {
							_, _ = provider.Volumes().WaitReady(context.Background(), next, 0)
						}
					}
				}
			}

			fmt.Fprintln(cliStdout, "Persistence reset complete.")
			fmt.Fprintf(cliStdout, "New volume: %s\n", next)
			fmt.Fprintf(cliStdout, "Old volume retained: %s\n", current)
			fmt.Fprintln(cliStdout, "To delete old volumes, use the Daytona UI.")
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Explicit name for the new persistence volume")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

func confirmChoice(prompt string) bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	fmt.Fprint(cliStdout, prompt)
	var resp string
	if _, err := fmt.Fscanln(os.Stdin, &resp); err != nil {
		return false
	}
	resp = strings.TrimSpace(strings.ToLower(resp))
	return resp == "y" || resp == "yes"
}

func resolveCurrentSandbox(provider sandbox.Provider, cwd string) (sandbox.RemoteSandbox, bool, error) {
	if provider == nil {
		return nil, false, errors.New("provider is required")
	}
	meta, err := sandbox.LoadSandboxMeta(cwd, provider.Name())
	if err != nil {
		return nil, false, err
	}
	if meta != nil {
		sb, err := provider.GetSandbox(context.Background(), meta.SandboxID)
		if err == nil {
			if err := sb.Start(context.Background()); err == nil {
				if waitErr := sb.WaitReady(context.Background(), 60*time.Second); waitErr != nil {
					if Verbose {
						fmt.Fprintf(os.Stderr, "Warning: sandbox may not be fully ready: %v\n", waitErr)
					}
				}
				return sb, true, nil
			}
		}
		fmt.Fprintln(os.Stderr, "Existing sandbox not found. Run `amux sandbox run <agent>` to create one.")
	}
	return nil, false, errors.New("no sandbox for this project - run `amux sandbox run <agent>` first")
}

// exitError lets commands return a specific exit code without printing an error.
type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit with code %d", e.code)
}
