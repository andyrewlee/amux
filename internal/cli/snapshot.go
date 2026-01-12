package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/computer"
)

func buildSnapshotCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage snapshots",
	}
	cmd.AddCommand(buildSnapshotCreateCommand())
	cmd.AddCommand(buildSnapshotUpdateCommand())
	cmd.AddCommand(buildSnapshotListCommand())
	return cmd
}

func buildSnapshotCreateCommand() *cobra.Command {
	var agents string
	var baseImage string
	var name string
	var setDefault bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a snapshot with preinstalled agent CLIs",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureDaytonaAPIKey(); err != nil {
				return err
			}
			client, err := computer.GetDaytonaClient()
			if err != nil {
				return err
			}
			agentsList, err := computer.ParseAgentList(agents)
			if err != nil {
				return err
			}
			if baseImage == "" {
				baseImage = computer.DefaultSnapshotBaseImage
			}
			if name == "" {
				name = computer.BuildSnapshotName("amux")
			}
			fmt.Printf("Creating snapshot \"%s\"...\n", name)
			snap, err := computer.CreateSnapshot(client, name, agentsList, baseImage, func(chunk string) {
				fmt.Println(chunk)
			})
			if err != nil {
				return err
			}
			fmt.Printf("Snapshot created: %s\n", snap.Name)
			if setDefault {
				cfg, err := computer.LoadConfig()
				if err != nil {
					return err
				}
				cfg.DefaultSnapshotName = snap.Name
				cfg.SnapshotAgents = agentsToStrings(agentsList)
				cfg.SnapshotBaseImage = baseImage
				if err := computer.SaveConfig(cfg); err != nil {
					return err
				}
				fmt.Printf("Saved default snapshot: %s\n", snap.Name)
				fmt.Println("New computers will use this snapshot.")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&agents, "agents", "", "Comma-separated agents to preinstall (claude,codex,opencode,amp,gemini,droid)")
	cmd.Flags().StringVar(&baseImage, "base-image", computer.DefaultSnapshotBaseImage, "Base image for the snapshot")
	cmd.Flags().StringVar(&name, "name", "", "Snapshot name (optional)")
	cmd.Flags().BoolVar(&setDefault, "set-default", false, "Save snapshot as default")

	return cmd
}

func buildSnapshotUpdateCommand() *cobra.Command {
	var addAgents string
	var removeAgents string
	var baseImage string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Rebuild the default snapshot with additional agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureDaytonaAPIKey(); err != nil {
				return err
			}
			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}
			current := cfg.SnapshotAgents
			if len(current) == 0 {
				return fmt.Errorf("no snapshot agents configured. Run `amux snapshot create` first")
			}
			currentAgents, err := computer.ParseAgentList(strings.Join(current, ","))
			if err != nil {
				return err
			}

			addList := []computer.Agent{}
			if addAgents != "" {
				addList, err = computer.ParseAgentList(addAgents)
				if err != nil {
					return err
				}
			}
			removeList := []computer.Agent{}
			if removeAgents != "" {
				removeList, err = computer.ParseAgentList(removeAgents)
				if err != nil {
					return err
				}
			}

			next := filterAgents(currentAgents, removeList)
			next = appendMissingAgents(next, addList)
			if len(next) == 0 {
				return fmt.Errorf("snapshot must include at least one agent")
			}
			if baseImage == "" {
				baseImage = cfg.SnapshotBaseImage
			}
			if baseImage == "" {
				baseImage = computer.DefaultSnapshotBaseImage
			}
			name := computer.BuildSnapshotName("amux")

			client, err := computer.GetDaytonaClient()
			if err != nil {
				return err
			}
			fmt.Printf("Creating snapshot \"%s\" with agents: %s\n", name, joinAgents(next))
			snap, err := computer.CreateSnapshot(client, name, next, baseImage, func(chunk string) {
				fmt.Println(chunk)
			})
			if err != nil {
				return err
			}

			cfg.DefaultSnapshotName = snap.Name
			cfg.SnapshotAgents = agentsToStrings(next)
			cfg.SnapshotBaseImage = baseImage
			if err := computer.SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("Updated default snapshot: %s\n", snap.Name)
			fmt.Println("New computers will use this snapshot.")
			return nil
		},
	}

	cmd.Flags().StringVar(&addAgents, "add", "", "Comma-separated agents to add (claude,codex,opencode,amp,gemini,droid)")
	cmd.Flags().StringVar(&removeAgents, "remove", "", "Comma-separated agents to remove (claude,codex,opencode,amp,gemini,droid)")
	cmd.Flags().StringVar(&baseImage, "base-image", "", "Override base image for the new snapshot")

	return cmd
}

func filterAgents(current []computer.Agent, remove []computer.Agent) []computer.Agent {
	removeSet := map[computer.Agent]bool{}
	for _, agent := range remove {
		removeSet[agent] = true
	}
	out := []computer.Agent{}
	for _, agent := range current {
		if !removeSet[agent] {
			out = append(out, agent)
		}
	}
	return out
}

func appendMissingAgents(current []computer.Agent, add []computer.Agent) []computer.Agent {
	set := map[computer.Agent]bool{}
	for _, agent := range current {
		set[agent] = true
	}
	for _, agent := range add {
		if !set[agent] {
			current = append(current, agent)
			set[agent] = true
		}
	}
	return current
}

func buildSnapshotListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List available snapshots",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureDaytonaAPIKey(); err != nil {
				return err
			}
			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}
			defaultSnapshot := computer.ResolveSnapshotID(cfg)

			client, err := computer.GetDaytonaClient()
			if err != nil {
				return err
			}
			snapshots, err := client.Snapshot.List()
			if err != nil {
				return err
			}
			if len(snapshots) == 0 {
				fmt.Println("No snapshots found")
				fmt.Println("Run `amux setup` or `amux snapshot create` to create one")
				return nil
			}
			fmt.Println("amux snapshots:")
			fmt.Println(strings.Repeat("─", 60))
			for _, snap := range snapshots {
				marker := "  "
				if snap.Name == defaultSnapshot {
					marker = "* "
				}
				fmt.Printf("%s%s (%s)\n", marker, snap.Name, snap.State)
			}
			fmt.Println(strings.Repeat("─", 60))
			if defaultSnapshot != "" {
				fmt.Printf("* = default snapshot (%s)\n", defaultSnapshot)
			}
			return nil
		},
	}
	return cmd
}
