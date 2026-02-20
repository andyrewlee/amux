package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func buildSetupCommand() *cobra.Command {
	var agents string
	var baseImage string
	var snapshotName string
	var createSnapshot bool
	var withGh bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Quick setup: validate credentials (optionally build a snapshot)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cliStdout, "amux setup")
			fmt.Fprintln(cliStdout, strings.Repeat("─", 50))
			fmt.Fprintln(cliStdout)

			if err := ensureDaytonaAPIKey(); err != nil {
				return err
			}
			fmt.Fprintln(cliStdout, "✓ Daytona API key configured")

			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
			}

			client, err := sandbox.GetDaytonaClient()
			if err != nil {
				return err
			}

			if createSnapshot {
				parsedAgents, err := sandbox.ParseAgentList(agents)
				if err != nil {
					return err
				}
				if baseImage == "" {
					baseImage = sandbox.DefaultSnapshotBaseImage
				}
				name := snapshotName
				if name == "" {
					name = sandbox.BuildSnapshotName("amux")
				}
				fmt.Fprintln(cliStdout, "\nBuilding snapshot (this can take a few minutes)...")
				fmt.Fprintf(cliStdout, "Creating snapshot %q with agents: %s\n", name, joinAgents(parsedAgents))
				snap, err := sandbox.CreateSnapshot(client, name, parsedAgents, baseImage, func(chunk string) {
					fmt.Fprintln(cliStdout, chunk)
				})
				if err != nil {
					return err
				}
				cfg.DefaultSnapshotName = snap.Name
				cfg.SnapshotAgents = agentsToStrings(parsedAgents)
				cfg.SnapshotBaseImage = baseImage
				if err := sandbox.SaveConfig(cfg); err != nil {
					return err
				}
				fmt.Fprintf(cliStdout, "✓ Saved default snapshot: %s\n", snap.Name)
			}

			if withGh {
				if err := runGhAuthLogin(); err != nil {
					return err
				}
			}

			fmt.Fprintln(cliStdout)
			fmt.Fprintln(cliStdout, strings.Repeat("─", 50))
			fmt.Fprintln(cliStdout, "Setup complete!")
			fmt.Fprintln(cliStdout)
			fmt.Fprintln(cliStdout, "Next steps:")
			fmt.Fprintln(cliStdout, "  amux claude              # Run Claude Code")
			fmt.Fprintln(cliStdout, "  amux doctor              # Verify setup")
			if !createSnapshot {
				fmt.Fprintln(cliStdout)
				fmt.Fprintln(cliStdout, "Optional:")
				fmt.Fprintln(cliStdout, "  amux setup --create-snapshot --agents claude,codex")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&agents, "agents", "", "Agents to preinstall (claude,codex,opencode,amp,gemini,droid)")
	cmd.Flags().StringVar(&baseImage, "base-image", sandbox.DefaultSnapshotBaseImage, "Base image for the snapshot")
	cmd.Flags().StringVar(&snapshotName, "snapshot-name", "", "Snapshot name")
	cmd.Flags().BoolVar(&createSnapshot, "create-snapshot", false, "Build a snapshot with preinstalled agents")
	cmd.Flags().BoolVar(&withGh, "with-gh", false, "Run GitHub CLI login helper")

	return cmd
}

func agentsToStrings(agents []sandbox.Agent) []string {
	out := make([]string, 0, len(agents))
	for _, agent := range agents {
		out = append(out, agent.String())
	}
	return out
}

func joinAgents(agents []sandbox.Agent) string {
	parts := make([]string, 0, len(agents))
	for _, agent := range agents {
		parts = append(parts, agent.String())
	}
	return strings.Join(parts, ", ")
}
