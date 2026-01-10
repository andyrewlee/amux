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
	var skipSnapshot bool
	var withGh bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "One-time setup: create credentials volume, build a snapshot, and save defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Amux setup")
			fmt.Println("• Credentials will persist across sandboxes")
			if err := ensureDaytonaAPIKey(); err != nil {
				return err
			}

			client, err := sandbox.GetDaytonaClient()
			if err != nil {
				return err
			}
			if _, err := sandbox.GetCredentialsVolumeMount(client); err != nil {
				return err
			}

			if !skipSnapshot {
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
				fmt.Println("• Building snapshot (this can take a few minutes)")
				fmt.Printf("Creating snapshot \"%s\" with agents: %s\n", name, joinAgents(parsedAgents))
				snap, err := sandbox.CreateSnapshot(client, name, parsedAgents, baseImage, func(chunk string) {
					fmt.Println(chunk)
				})
				if err != nil {
					return err
				}
				cfg, err := sandbox.LoadConfig()
				if err != nil {
					return err
				}
				cfg.DefaultSnapshotName = snap.Name
				cfg.SnapshotAgents = agentsToStrings(parsedAgents)
				cfg.SnapshotBaseImage = baseImage
				if err := sandbox.SaveConfig(cfg); err != nil {
					return err
				}
				fmt.Printf("Saved default snapshot: %s\n", snap.Name)
			} else {
				fmt.Println("• Skipping snapshot build (per --skip-snapshot)")
			}

			if withGh {
				if err := runGhAuthLogin(); err != nil {
					return err
				}
			} else {
				fmt.Println("Optional: run `amux auth login gh` to enable git push from sandboxes.")
			}

			fmt.Println("Setup complete.")
			fmt.Println("Next:")
			fmt.Println("  1) amux doctor")
			fmt.Println("  2) amux sandbox run claude")
			return nil
		},
	}

	cmd.Flags().StringVar(&agents, "agents", "", "Comma-separated agents to preinstall (claude,codex,opencode,amp,gemini,droid)")
	cmd.Flags().StringVar(&baseImage, "base-image", sandbox.DefaultSnapshotBaseImage, "Base image for the snapshot")
	cmd.Flags().StringVar(&snapshotName, "snapshot-name", "", "Snapshot name (optional)")
	cmd.Flags().BoolVar(&skipSnapshot, "skip-snapshot", false, "Skip snapshot creation")
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
