package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/computer"
)

// buildAgentAliasCommand creates a top-level alias for `amux computer run <agent>`.
// This allows users to simply run `amux claude` instead of `amux computer run claude`.
func buildAgentAliasCommand(agent string, description string) *cobra.Command {
	var envVars []string
	var volumes []string
	var credentials string
	var recreate bool
	var snapshot string
	var noSync bool
	var autoStop int32
	var update bool
	var provider string
	var syncSettings bool
	var noSyncSettings bool

	cmd := &cobra.Command{
		Use:   agent + " [-- agent-args...]",
		Short: description,
		Long:  description + "\n\nThis is a shortcut for `amux computer run " + agent + "`.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentAlias(agent, envVars, volumes, credentials, recreate, snapshot, noSync, autoStop, update, provider, syncSettings, noSyncSettings, args)
		},
	}

	cmd.Flags().StringArrayVarP(&envVars, "env", "e", []string{}, "Environment variable (repeatable)")
	cmd.Flags().StringArrayVarP(&volumes, "volume", "v", []string{}, "Volume mount (repeatable)")
	cmd.Flags().StringVar(&credentials, "credentials", "auto", "Credentials mode (computer|none|auto)")
	cmd.Flags().BoolVar(&recreate, "recreate", false, "Recreate computer with new config")
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Use a specific snapshot")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip workspace sync")
	cmd.Flags().Int32Var(&autoStop, "auto-stop", 30, "Auto-stop interval in minutes (0 to disable)")
	cmd.Flags().BoolVarP(&update, "update", "u", false, "Update agent to latest version")
	cmd.Flags().BoolVarP(&Verbose, "verbose", "V", false, "Enable verbose output")
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")
	cmd.Flags().BoolVar(&syncSettings, "sync-settings", false, "Sync local settings files to computer")
	cmd.Flags().BoolVar(&noSyncSettings, "no-sync-settings", false, "Skip settings sync even if enabled globally")

	return cmd
}

func runAgentAlias(agentName string, envVars, volumes []string, credentials string, recreate bool, snapshotID string, noSync bool, autoStop int32, forceUpdate bool, provider string, syncSettings, noSyncSettings bool, passthroughArgs []string) error {
	agent := computer.Agent(agentName)

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, err := computer.LoadConfig()
	if err != nil {
		return err
	}

	// Parse environment variables
	envMap := map[string]string{}
	for _, e := range envVars {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Parse volume specs
	volumeSpecs := []computer.VolumeSpec{}
	for _, v := range volumes {
		spec, err := parseVolumeSpec(v)
		if err != nil {
			return err
		}
		volumeSpecs = append(volumeSpecs, spec)
	}

	// Parse credentials mode
	credMode := strings.ToLower(credentials)
	switch credMode {
	case "computer", "none", "auto", "":
		if credMode == "" {
			credMode = "auto"
		}
	default:
		return fmt.Errorf("invalid credentials mode: use computer, none, or auto")
	}
	if credMode == "auto" {
		if agent == computer.AgentShell {
			credMode = "none"
		} else {
			credMode = "computer"
		}
	}

	// Handle Codex TUI2 auto-enable
	agentArgs := passthroughArgs
	if agent == computer.AgentCodex && getenvFallback("AMUX_CODEX_TUI2") != "0" {
		hasTui2Flag := false
		for _, arg := range agentArgs {
			if strings.Contains(arg, "tui2") || strings.Contains(arg, "features.tui2") {
				hasTui2Flag = true
				break
			}
		}
		if !hasTui2Flag {
			agentArgs = append([]string{"--enable", "tui2"}, agentArgs...)
		}
	}

	// Resolve snapshot
	if snapshotID == "" {
		snapshotID = computer.ResolveSnapshotID(cfg)
	}
	if Verbose && snapshotID != "" {
		fmt.Printf("Using snapshot: %s\n", snapshotID)
	}

	syncEnabled := !noSync

	// Determine settings sync mode based on flags
	settingsSyncMode := "auto" // default: use global config
	if syncSettings {
		settingsSyncMode = "force"
	} else if noSyncSettings {
		settingsSyncMode = "skip"
	}

	// Use the shared runAgent function for consistent behavior
	return runAgent(runAgentParams{
		agent:            agent,
		cwd:              cwd,
		envMap:           envMap,
		volumeSpecs:      volumeSpecs,
		credMode:         credMode,
		autoStop:         autoStop,
		snapshotID:       snapshotID,
		recreate:         recreate,
		syncEnabled:      syncEnabled,
		forceUpdate:      forceUpdate,
		agentArgs:        agentArgs,
		provider:         provider,
		settingsSyncMode: settingsSyncMode,
	})
}
