package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

// buildAgentAliasCommand creates a top-level alias for `amux sandbox run <agent>`.
// This allows users to simply run `amux claude` instead of `amux sandbox run claude`.
func buildAgentAliasCommand(agent string, description string) *cobra.Command {
	var envVars []string
	var volumes []string
	var credentials string
	var recreate bool
	var snapshot string
	var noSync bool
	var autoStop int32

	cmd := &cobra.Command{
		Use:   agent + " [-- agent-args...]",
		Short: description,
		Long:  description + "\n\nThis is a shortcut for `amux sandbox run " + agent + "`.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentAlias(agent, envVars, volumes, credentials, recreate, snapshot, noSync, autoStop, args)
		},
	}

	cmd.Flags().StringArrayVarP(&envVars, "env", "e", []string{}, "Environment variable (repeatable)")
	cmd.Flags().StringArrayVarP(&volumes, "volume", "v", []string{}, "Volume mount (repeatable)")
	cmd.Flags().StringVar(&credentials, "credentials", "auto", "Credentials mode (sandbox|none|auto)")
	cmd.Flags().BoolVar(&recreate, "recreate", false, "Recreate sandbox with new config")
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Use a specific snapshot")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip workspace sync")
	cmd.Flags().Int32Var(&autoStop, "auto-stop", 30, "Auto-stop interval in minutes (0 to disable)")
	cmd.Flags().BoolVarP(&Verbose, "verbose", "V", false, "Enable verbose output")

	return cmd
}

func runAgentAlias(agentName string, envVars, volumes []string, credentials string, recreate bool, snapshotID string, noSync bool, autoStop int32, passthroughArgs []string) error {
	agent := sandbox.Agent(agentName)

	if err := sandbox.RunPreflight(); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, err := sandbox.LoadConfig()
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
	volumeSpecs := []sandbox.VolumeSpec{}
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
	case "sandbox", "none", "auto", "":
		if credMode == "" {
			credMode = "auto"
		}
	default:
		return fmt.Errorf("invalid credentials mode: use sandbox, none, or auto")
	}
	if credMode == "auto" {
		if agent == sandbox.AgentShell {
			credMode = "none"
		} else {
			credMode = "sandbox"
		}
	}

	// Handle Codex TUI2 auto-enable
	agentArgs := passthroughArgs
	if agent == sandbox.AgentCodex && getenvFallback("AMUX_CODEX_TUI2") != "0" {
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
		snapshotID = sandbox.ResolveSnapshotID(cfg)
	}
	if Verbose && snapshotID != "" {
		fmt.Printf("Using snapshot: %s\n", snapshotID)
	}

	syncEnabled := !noSync

	// Use the shared runAgent function for consistent behavior
	return runAgent(runAgentParams{
		agent:       agent,
		cwd:         cwd,
		envMap:      envMap,
		volumeSpecs: volumeSpecs,
		credMode:    credMode,
		autoStop:    autoStop,
		snapshotID:  snapshotID,
		recreate:    recreate,
		syncEnabled: syncEnabled,
		agentArgs:   agentArgs,
	})
}
