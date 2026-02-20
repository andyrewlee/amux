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

func buildSandboxRunCommand() *cobra.Command {
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
		Use:   "run <agent>",
		Short: "Run Claude Code, Codex, OpenCode, Amp, Gemini CLI, Droid, or a shell in a sandbox",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentName := args[0]
			if !sandbox.IsValidAgent(agentName) {
				return fmt.Errorf("invalid agent: use claude, codex, opencode, amp, gemini, droid, or shell")
			}
			agent := sandbox.Agent(agentName)

			cwd := os.Getenv("INIT_CWD")
			if cwd == "" {
				var err error
				cwd, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			cfg, err := sandbox.LoadConfig()
			if err != nil {
				return err
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

			// Parse environment variables
			envMap := map[string]string{}
			for _, env := range envVars {
				parts := strings.SplitN(env, "=", 2)
				if len(parts) == 2 && parts[0] != "" {
					envMap[parts[0]] = parts[1]
				}
			}

			// Parse volume specs
			volumeSpecs := []sandbox.VolumeSpec{}
			for _, spec := range volumes {
				vol, err := parseVolumeSpec(spec)
				if err != nil {
					return err
				}
				volumeSpecs = append(volumeSpecs, vol)
			}

			// Parse agent args
			syncEnabled := !noSync
			userArgs := getAgentArgs(os.Args, agentName)
			agentArgs := append([]string{}, userArgs...)
			if agent == sandbox.AgentCodex && getenvFallback("AMUX_CODEX_TUI2") != "0" {
				hasTui2Flag := false
				for i := 0; i < len(agentArgs); i++ {
					arg := agentArgs[i]
					if (arg == "--enable" || arg == "--disable") && i+1 < len(agentArgs) && agentArgs[i+1] == "tui2" {
						hasTui2Flag = true
						break
					}
					if arg == "-c" && i+1 < len(agentArgs) && strings.HasPrefix(agentArgs[i+1], "features.tui2") {
						hasTui2Flag = true
						break
					}
				}
				if !hasTui2Flag {
					agentArgs = append([]string{"--enable", "tui2"}, agentArgs...)
				}
			}

			// Resolve snapshot
			snapshotID := snapshot
			if snapshotID == "" {
				snapshotID = sandbox.ResolveSnapshotID(cfg)
			}
			if Verbose && snapshotID != "" {
				fmt.Fprintf(cliStdout, "Using snapshot: %s\n", snapshotID)
			}

			previewExplicit := cmd.Flags().Changed("preview")
			if previewExplicit && (previewPort < 1 || previewPort > 65535) {
				return fmt.Errorf("preview port must be between 1 and 65535")
			}
			keepExplicit := cmd.Flags().Changed("keep")
			if previewPort != 0 && !keepExplicit {
				keep = true
				fmt.Fprintln(cliStdout, "Preview enabled; keeping sandbox after exit. Use --keep=false to override.")
			}

			// Determine settings sync mode based on flags
			settingsSyncMode := "auto" // default: use global config
			if syncSettings {
				settingsSyncMode = "force"
			} else if noSyncSettings {
				settingsSyncMode = "skip"
			}

			// Run the agent with clean output
			return runAgent(runAgentParams{
				agent:                 agent,
				cwd:                   cwd,
				envMap:                envMap,
				volumeSpecs:           volumeSpecs,
				credMode:              credMode,
				autoStop:              autoStop,
				snapshotID:            snapshotID,
				syncEnabled:           syncEnabled,
				forceUpdate:           update,
				agentArgs:             agentArgs,
				keepSandbox:           keep,
				settingsSyncMode:      settingsSyncMode,
				persistenceVolumeName: sandbox.ResolvePersistenceVolumeName(cfg),
				previewPort:           previewPort,
				previewNoOpen:         previewNoOpen,
				recordLogs:            recordLogs,
			})
		},
	}

	cmd.Flags().StringArrayVarP(&envVars, "env", "e", []string{}, "Environment variable (repeatable)")
	cmd.Flags().StringArrayVarP(&volumes, "volume", "v", []string{}, "Volume mount (repeatable)")
	cmd.Flags().StringVarP(&credentials, "credentials", "c", "auto", "Credentials mode (sandbox|none|auto)")
	cmd.Flags().StringVarP(&snapshot, "snapshot", "s", "", "Use a specific snapshot")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip workspace sync")
	cmd.Flags().Int32VarP(&autoStop, "auto-stop", "a", 30, "Auto-stop interval in minutes (0 to disable)")
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

type runAgentParams struct {
	agent                 sandbox.Agent
	cwd                   string
	envMap                map[string]string
	volumeSpecs           []sandbox.VolumeSpec
	credMode              string
	autoStop              int32
	snapshotID            string
	syncEnabled           bool
	forceUpdate           bool
	agentArgs             []string
	keepSandbox           bool
	settingsSyncMode      string // "auto" (use global config), "force" (always sync), "skip" (never sync)
	persistenceVolumeName string
	previewPort           int
	previewNoOpen         bool
	recordLogs            bool
}

// runAgent is the core logic for running an agent in a sandbox.
// It provides a clean, minimal output experience similar to `docker run`.
func runAgent(p runAgentParams) error {
	var sb sandbox.RemoteSandbox
	var err error

	cfg, err := sandbox.LoadConfig()
	if err != nil {
		return err
	}
	provider, _, err := sandbox.ResolveProvider(cfg, p.cwd, "")
	if err != nil {
		return err
	}
	if err := sandbox.RunPreflight(); err != nil {
		return err
	}
	if p.previewPort != 0 && !provider.SupportsFeature(sandbox.FeaturePreviewURLs) {
		return fmt.Errorf("preview URLs are not supported by the selected provider")
	}

	// Step 1: Create sandbox
	if Verbose {
		fmt.Fprintf(cliStdout, "Starting %s sandbox...\n", p.agent)
		sb, _, err = sandbox.CreateSandboxSession(provider, p.cwd, sandbox.SandboxConfig{
			Agent:                 p.agent,
			EnvVars:               p.envMap,
			Volumes:               p.volumeSpecs,
			CredentialsMode:       p.credMode,
			AutoStopInterval:      p.autoStop,
			Snapshot:              p.snapshotID,
			Ephemeral:             !p.keepSandbox,
			PersistenceVolumeName: p.persistenceVolumeName,
		})
	} else {
		spinner := NewSpinner(fmt.Sprintf("Starting %s sandbox", p.agent))
		spinner.Start()
		sb, _, err = sandbox.CreateSandboxSession(provider, p.cwd, sandbox.SandboxConfig{
			Agent:                 p.agent,
			EnvVars:               p.envMap,
			Volumes:               p.volumeSpecs,
			CredentialsMode:       p.credMode,
			AutoStopInterval:      p.autoStop,
			Snapshot:              p.snapshotID,
			Ephemeral:             !p.keepSandbox,
			PersistenceVolumeName: p.persistenceVolumeName,
		})
		if err != nil {
			spinner.StopWithMessage("✗ Failed to start sandbox")
		} else {
			spinner.StopWithMessage("✓ Sandbox ready")
		}
	}
	if err != nil {
		return err
	}

	if Verbose {
		fmt.Fprintln(cliStdout, "Sandbox ID: "+sb.ID())
	}

	cleanup := func() {
		if p.keepSandbox || sb == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = sb.Stop(ctx)
		_ = provider.DeleteSandbox(ctx, sb.ID())
		_ = sandbox.RemoveSandboxMetaByID(sb.ID())
	}
	if !p.keepSandbox {
		defer cleanup()
	}

	worktreeID := sandbox.ComputeWorktreeID(p.cwd)
	workspacePath := sandbox.GetWorktreeRepoPath(sb, sandbox.SyncOptions{Cwd: p.cwd, WorktreeID: worktreeID})
	logDir := fmt.Sprintf("/amux/logs/%s", worktreeID)
	recordPath := ""

	// Step 2: Sync workspace
	if p.syncEnabled {
		if Verbose {
			fmt.Fprintln(cliStdout, "Syncing workspace...")
			if err := sandbox.UploadWorkspace(sb, sandbox.SyncOptions{Cwd: p.cwd, WorktreeID: worktreeID}, Verbose); err != nil {
				return err
			}
		} else {
			spinner := NewSpinner("Syncing workspace")
			spinner.Start()
			syncErr := sandbox.UploadWorkspace(sb, sandbox.SyncOptions{Cwd: p.cwd, WorktreeID: worktreeID}, false)
			if syncErr != nil {
				spinner.StopWithMessage("✗ Sync failed")
				return syncErr
			}
			spinner.StopWithMessage("✓ Workspace synced")
		}
	} else {
		_, _ = sb.Exec(context.Background(), fmt.Sprintf("mkdir -p %s", workspacePath), nil)
	}

	// Step 3: Setup credentials
	// Determine spinner message based on update flag
	setupMsg := "Setting up environment"
	if p.forceUpdate {
		setupMsg = "Updating agent"
	}

	if Verbose {
		fmt.Fprintln(cliStdout, setupMsg+"...")
		if err := sandbox.SetupCredentials(sb, sandbox.CredentialsConfig{Mode: p.credMode, Agent: p.agent, SettingsSyncMode: p.settingsSyncMode}, Verbose); err != nil {
			return err
		}
		if err := sandbox.EnsureAgentInstalled(sb, p.agent, Verbose, p.forceUpdate); err != nil {
			return err
		}
	} else {
		spinner := NewSpinner(setupMsg)
		spinner.Start()
		if err := sandbox.SetupCredentials(sb, sandbox.CredentialsConfig{Mode: p.credMode, Agent: p.agent, SettingsSyncMode: p.settingsSyncMode}, false); err != nil {
			spinner.StopWithMessage("✗ Setup failed")
			return err
		}
		if err := sandbox.EnsureAgentInstalled(sb, p.agent, false, p.forceUpdate); err != nil {
			spinner.StopWithMessage("✗ Agent install failed")
			return err
		}
		if p.forceUpdate {
			spinner.StopWithMessage("✓ Updated")
		} else {
			spinner.StopWithMessage("✓ Ready")
		}
	}

	if p.recordLogs {
		if _, err := sb.Exec(context.Background(), sandbox.SafeCommands.MkdirP(logDir), nil); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create log directory: %v\n", err)
		} else {
			timestamp := time.Now().UTC().Format("20060102-150405")
			recordPath = fmt.Sprintf("%s/%s-%s.log", logDir, timestamp, p.agent)
			fmt.Fprintf(cliStdout, "Recording session to %s\n", recordPath)
			fmt.Fprintln(cliStdout, "Tip: Use `amux sandbox logs` to view from another terminal.")
		}
	}

	if p.previewPort != 0 {
		url, err := sb.GetPreviewURL(context.Background(), p.previewPort)
		if err != nil {
			return err
		}
		if url == "" {
			return fmt.Errorf("unable to construct a preview URL")
		}
		fmt.Fprintf(cliStdout, "Preview URL: %s\n", url)
		if !p.previewNoOpen {
			if !tryOpenURL(url) {
				fmt.Fprintln(cliStdout, "Open the URL in your browser.")
			}
		}
		fmt.Fprintf(cliStdout, "Tip: Ensure your app listens on 0.0.0.0:%d inside the sandbox.\n", p.previewPort)
		if !p.keepSandbox {
			fmt.Fprintln(cliStdout, "Tip: Use --keep to leave the sandbox running for preview.")
		}
	}

	// Step 4: Check credentials and handle login if needed
	needsLogin := false
	if p.credMode != "none" && len(p.agentArgs) == 0 {
		needsLogin = checkNeedsLogin(sb, p.agent, p.envMap)
	}

	var exitCode int

	if needsLogin {
		// Handle first-time login
		exitCode, err = handleAgentLogin(sb, p.agent, workspacePath, p.envMap)
		if err != nil {
			return err
		}
		if exitCode != 0 {
			return handleAgentExit(sb, p.agent, exitCode, p.syncEnabled, p.cwd)
		}
	}

	// Step 5: Run the agent
	fmt.Fprintln(cliStdout) // Clean line before agent starts
	exitCode, err = sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
		Agent:         p.agent,
		WorkspacePath: workspacePath,
		Args:          p.agentArgs,
		Env:           p.envMap,
		RecordPath:    recordPath,
	})
	if err != nil {
		return err
	}

	// Handle agent exit
	return handleAgentExit(sb, p.agent, exitCode, p.syncEnabled, p.cwd)
}
