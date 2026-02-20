package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/sandbox"
)

// buildAgentAliasCommand creates a top-level alias for `amux sandbox run <agent>`.
// This allows users to simply run `amux claude` instead of `amux sandbox run claude`.
func buildAgentAliasCommand(agent, description string) *cobra.Command {
	var envVars []string
	var volumes []string
	var credentials string
	var snapshot string
	var noSync bool
	var autoStop int32
	var update bool
	var keep bool
	var syncSettings bool
	var noSyncSettings bool
	var previewPort int
	var previewNoOpen bool
	var recordLogs bool

	cmd := &cobra.Command{
		Use:   agent + " [-- agent-args...]",
		Short: description,
		Long:  description + "\n\nThis is a shortcut for `amux sandbox run " + agent + "`.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentAlias(
				agent,
				envVars,
				volumes,
				credentials,
				snapshot,
				noSync,
				autoStop,
				update,
				keep,
				cmd.Flags().Changed("keep"),
				syncSettings,
				noSyncSettings,
				previewPort,
				cmd.Flags().Changed("preview"),
				previewNoOpen,
				recordLogs,
				args,
			)
		},
	}

	cmd.Flags().StringArrayVarP(&envVars, "env", "e", []string{}, "Environment variable (repeatable)")
	cmd.Flags().StringArrayVarP(&volumes, "volume", "v", []string{}, "Volume mount (repeatable)")
	cmd.Flags().StringVar(&credentials, "credentials", "auto", "Credentials mode (sandbox|none|auto)")
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Use a specific snapshot")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip workspace sync")
	cmd.Flags().Int32Var(&autoStop, "auto-stop", 30, "Auto-stop interval in minutes (0 to disable)")
	cmd.Flags().BoolVarP(&update, "update", "u", false, "Update agent to latest version")
	cmd.Flags().BoolVarP(&Verbose, "verbose", "V", false, "Enable verbose output")
	cmd.Flags().BoolVar(&keep, "keep", false, "Keep sandbox after the session exits")
	cmd.Flags().BoolVar(&syncSettings, "sync-settings", false, "Sync local settings files to sandbox")
	cmd.Flags().BoolVar(&noSyncSettings, "no-sync-settings", false, "Skip settings sync even if enabled globally")
	cmd.Flags().IntVar(&previewPort, "preview", 0, "Open a preview URL for the given port (implies --keep unless --keep=false)")
	cmd.Flags().BoolVar(&previewNoOpen, "no-open", false, "Do not open the preview URL automatically")
	cmd.Flags().BoolVar(&recordLogs, "record", false, "Record the session output to persistent logs")

	return cmd
}

func runAgentAlias(agentName string, envVars, volumes []string, credentials, snapshotID string, noSync bool, autoStop int32, forceUpdate, keep, keepExplicit, syncSettings, noSyncSettings bool, previewPort int, previewExplicit, previewNoOpen, recordLogs bool, passthroughArgs []string) error {
	agent := sandbox.Agent(agentName)

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
		return errors.New("invalid credentials mode: use sandbox, none, or auto")
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
		fmt.Fprintf(cliStdout, "Using snapshot: %s\n", snapshotID)
	}

	if previewExplicit && (previewPort < 1 || previewPort > 65535) {
		return errors.New("preview port must be between 1 and 65535")
	}
	if previewPort != 0 && !keepExplicit {
		keep = true
		fmt.Fprintln(cliStdout, "Preview enabled; keeping sandbox after exit. Use --keep=false to override.")
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
		agent:                 agent,
		cwd:                   cwd,
		envMap:                envMap,
		volumeSpecs:           volumeSpecs,
		credMode:              credMode,
		autoStop:              autoStop,
		snapshotID:            snapshotID,
		syncEnabled:           syncEnabled,
		forceUpdate:           forceUpdate,
		agentArgs:             agentArgs,
		keepSandbox:           keep,
		settingsSyncMode:      settingsSyncMode,
		persistenceVolumeName: sandbox.ResolvePersistenceVolumeName(cfg),
		previewPort:           previewPort,
		previewNoOpen:         previewNoOpen,
		recordLogs:            recordLogs,
	})
}
