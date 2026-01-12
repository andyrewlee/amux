package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/computer"
)

// Verbose controls whether verbose output is enabled.
var Verbose bool

func buildComputerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "computer",
		Short: "Manage computers",
	}
	cmd.AddCommand(buildComputerRunCommand())
	cmd.AddCommand(buildComputerUpdateCommand())
	cmd.AddCommand(buildComputerPreviewCommand())
	cmd.AddCommand(buildComputerDesktopCommand())
	cmd.AddCommand(buildComputerLsCommand())
	cmd.AddCommand(buildComputerRmCommand())
	return cmd
}

func buildComputerRunCommand() *cobra.Command {
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
		Use:   "run <agent>",
		Short: "Run Claude Code, Codex, OpenCode, Amp, Gemini CLI, Droid, or a shell in a computer",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentName := args[0]
			if !computer.IsValidAgent(agentName) {
				return fmt.Errorf("invalid agent: use claude, codex, opencode, amp, gemini, droid, or shell")
			}
			agent := computer.Agent(agentName)

			cwd := os.Getenv("INIT_CWD")
			if cwd == "" {
				var err error
				cwd, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
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

			// Parse environment variables
			envMap := map[string]string{}
			for _, env := range envVars {
				parts := strings.SplitN(env, "=", 2)
				if len(parts) == 2 && parts[0] != "" {
					envMap[parts[0]] = parts[1]
				}
			}

			// Parse volume specs
			volumeSpecs := []computer.VolumeSpec{}
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
			if agent == computer.AgentCodex && getenvFallback("AMUX_CODEX_TUI2") != "0" {
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
				snapshotID = computer.ResolveSnapshotID(cfg)
			}
			if Verbose && snapshotID != "" {
				fmt.Printf("Using snapshot: %s\n", snapshotID)
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
				agent:            agent,
				cwd:              cwd,
				envMap:           envMap,
				volumeSpecs:      volumeSpecs,
				credMode:         credMode,
				autoStop:         autoStop,
				snapshotID:       snapshotID,
				recreate:         recreate,
				syncEnabled:      syncEnabled,
				forceUpdate:      update,
				agentArgs:        agentArgs,
				provider:         provider,
				settingsSyncMode: settingsSyncMode,
			})
		},
	}

	cmd.Flags().StringArrayVarP(&envVars, "env", "e", []string{}, "Environment variable (repeatable)")
	cmd.Flags().StringArrayVarP(&volumes, "volume", "v", []string{}, "Volume mount (repeatable)")
	cmd.Flags().StringVarP(&credentials, "credentials", "c", "auto", "Credentials mode (computer|none|auto)")
	cmd.Flags().BoolVar(&recreate, "recreate", false, "Recreate computer with new config")
	cmd.Flags().StringVarP(&snapshot, "snapshot", "s", "", "Use a specific snapshot")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip workspace sync")
	cmd.Flags().Int32VarP(&autoStop, "auto-stop", "a", 30, "Auto-stop interval in minutes (0 to disable)")
	cmd.Flags().BoolVarP(&update, "update", "u", false, "Update agent to latest version")
	cmd.Flags().BoolVarP(&Verbose, "verbose", "V", false, "Enable verbose output")
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")
	cmd.Flags().BoolVar(&syncSettings, "sync-settings", false, "Sync local settings files to computer")
	cmd.Flags().BoolVar(&noSyncSettings, "no-sync-settings", false, "Skip settings sync even if enabled globally")

	return cmd
}

func buildComputerUpdateCommand() *cobra.Command {
	var all bool
	var snapshot string
	var provider string

	cmd := &cobra.Command{
		Use:   "update [agent]",
		Short: "Update agent CLIs to latest versions",
		Long: `Update agent CLIs to their latest versions in the current project computer.

If no agent is specified, updates the default agent (claude).
Use --all to update all supported agents.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}

			snapshotID := snapshot
			if snapshotID == "" {
				snapshotID = computer.ResolveSnapshotID(cfg)
			}
			providerInstance, _, err := computer.ResolveProvider(cfg, cwd, provider)
			if err != nil {
				return err
			}

			// Get or create computer
			spinner := NewSpinner("Connecting to computer")
			spinner.Start()
			sb, _, err := resolveProjectComputer(providerInstance, cwd, snapshotID)
			if err != nil {
				spinner.StopWithMessage("✗ Failed to connect")
				return err
			}
			spinner.StopWithMessage("✓ Connected")

			if all {
				// Update all agents
				fmt.Println("Updating all agents...")
				if err := computer.UpdateAllAgents(sb, true); err != nil {
					return err
				}
				fmt.Println("✓ All agents updated")
			} else {
				// Update specific agent
				agentName := "claude"
				if len(args) > 0 {
					agentName = args[0]
				}
				if !computer.IsValidAgent(agentName) {
					return fmt.Errorf("invalid agent: use claude, codex, opencode, amp, gemini, or droid")
				}
				agent := computer.Agent(agentName)

				spinner := NewSpinner(fmt.Sprintf("Updating %s", agent))
				spinner.Start()
				if err := computer.UpdateAgent(sb, agent, false); err != nil {
					spinner.StopWithMessage(fmt.Sprintf("✗ Failed to update %s", agent))
					return err
				}
				spinner.StopWithMessage(fmt.Sprintf("✓ %s updated", agent))
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Update all agents")
	cmd.Flags().StringVarP(&snapshot, "snapshot", "s", "", "Use a specific snapshot")
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")

	return cmd
}

type runAgentParams struct {
	agent            computer.Agent
	cwd              string
	envMap           map[string]string
	volumeSpecs      []computer.VolumeSpec
	credMode         string
	autoStop         int32
	snapshotID       string
	recreate         bool
	syncEnabled      bool
	forceUpdate      bool
	agentArgs        []string
	provider         string
	settingsSyncMode string // "auto" (use global config), "force" (always sync), "skip" (never sync)
}

// runAgent is the core logic for running an agent in a computer.
// It provides a clean, minimal output experience similar to docker computer run.
func runAgent(p runAgentParams) error {
	var sb computer.RemoteComputer
	var meta *computer.ComputerMeta
	var err error

	cfg, err := computer.LoadConfig()
	if err != nil {
		return err
	}
	provider, providerName, err := computer.ResolveProvider(cfg, p.cwd, p.provider)
	if err != nil {
		return err
	}
	if err := computer.RunPreflight(providerName); err != nil {
		return err
	}

	// Step 1: Create/get computer
	if Verbose {
		fmt.Printf("Starting %s computer...\n", p.agent)
		sb, meta, err = computer.EnsureComputer(provider, p.cwd, computer.ComputerConfig{
			Agent:            p.agent,
			EnvVars:          p.envMap,
			Volumes:          p.volumeSpecs,
			CredentialsMode:  p.credMode,
			AutoStopInterval: p.autoStop,
			Snapshot:         p.snapshotID,
		}, p.recreate)
	} else {
		spinner := NewSpinner(fmt.Sprintf("Starting %s computer", p.agent))
		spinner.Start()
		sb, meta, err = computer.EnsureComputer(provider, p.cwd, computer.ComputerConfig{
			Agent:            p.agent,
			EnvVars:          p.envMap,
			Volumes:          p.volumeSpecs,
			CredentialsMode:  p.credMode,
			AutoStopInterval: p.autoStop,
			Snapshot:         p.snapshotID,
		}, p.recreate)
		if err != nil {
			spinner.StopWithMessage("✗ Failed to start computer")
		} else {
			spinner.StopWithMessage("✓ Computer ready")
		}
	}
	if err != nil {
		return err
	}

	if Verbose {
		fmt.Println("Computer ID: " + sb.ID())
	}

	worktreeID := computer.ComputeWorktreeID(p.cwd)
	workspacePath := computer.GetWorktreeRepoPath(sb, computer.SyncOptions{Cwd: p.cwd, WorktreeID: worktreeID})

	// Step 2: Sync workspace
	if p.syncEnabled {
		if Verbose {
			fmt.Println("Syncing workspace...")
			if err := computer.UploadWorkspace(sb, computer.SyncOptions{Cwd: p.cwd, WorktreeID: worktreeID}, Verbose); err != nil {
				return err
			}
		} else {
			spinner := NewSpinner("Syncing workspace")
			spinner.Start()
			syncErr := computer.UploadWorkspace(sb, computer.SyncOptions{Cwd: p.cwd, WorktreeID: worktreeID}, false)
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
		fmt.Println(setupMsg + "...")
		if err := computer.SetupCredentials(sb, computer.CredentialsConfig{Mode: p.credMode, Agent: p.agent, SettingsSyncMode: p.settingsSyncMode}, Verbose); err != nil {
			return err
		}
		if err := computer.EnsureAgentInstalled(sb, p.agent, Verbose, p.forceUpdate); err != nil {
			return err
		}
	} else {
		spinner := NewSpinner(setupMsg)
		spinner.Start()
		if err := computer.SetupCredentials(sb, computer.CredentialsConfig{Mode: p.credMode, Agent: p.agent, SettingsSyncMode: p.settingsSyncMode}, false); err != nil {
			spinner.StopWithMessage("✗ Setup failed")
			return err
		}
		if err := computer.EnsureAgentInstalled(sb, p.agent, false, p.forceUpdate); err != nil {
			spinner.StopWithMessage("✗ Agent install failed")
			return err
		}
		if p.forceUpdate {
			spinner.StopWithMessage("✓ Updated")
		} else {
			spinner.StopWithMessage("✓ Ready")
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
			return handleAgentExit(sb, p.agent, exitCode, p.credMode, p.syncEnabled, p.cwd, meta)
		}
	}

	// Step 5: Run the agent
	fmt.Println() // Clean line before agent starts
	exitCode, err = computer.RunAgentInteractive(sb, computer.AgentConfig{
		Agent:         p.agent,
		WorkspacePath: workspacePath,
		Args:          p.agentArgs,
		Env:           p.envMap,
	})
	if err != nil {
		return err
	}

	// Handle agent exit
	return handleAgentExit(sb, p.agent, exitCode, p.credMode, p.syncEnabled, p.cwd, meta)
}

// checkNeedsLogin determines if an agent needs login based on stored credentials
func checkNeedsLogin(sb computer.RemoteComputer, agent computer.Agent, envMap map[string]string) bool {
	// Check if credentials already exist on the computer
	credStatus := computer.CheckAgentCredentials(sb, agent)
	if credStatus.HasCredential {
		return false
	}

	// Check if API key is provided via environment
	switch agent {
	case computer.AgentClaude:
		if envMap["ANTHROPIC_API_KEY"] != "" || envMap["CLAUDE_API_KEY"] != "" || envMap["ANTHROPIC_AUTH_TOKEN"] != "" {
			return false
		}
	case computer.AgentCodex:
		if envMap["OPENAI_API_KEY"] != "" {
			return false
		}
	case computer.AgentGemini:
		if envMap["GEMINI_API_KEY"] != "" || envMap["GOOGLE_API_KEY"] != "" || envMap["GOOGLE_APPLICATION_CREDENTIALS"] != "" {
			return false
		}
	case computer.AgentDroid:
		if envMap["FACTORY_API_KEY"] != "" {
			return false
		}
	case computer.AgentAmp:
		if envMap["AMP_API_KEY"] != "" {
			return false
		}
	}

	// Agents that need explicit login
	switch agent {
	case computer.AgentCodex, computer.AgentOpenCode, computer.AgentAmp:
		return true
	}

	return false
}

// handleAgentLogin runs the login flow for agents that need it
func handleAgentLogin(sb computer.RemoteComputer, agent computer.Agent, workspacePath string, envMap map[string]string) (int, error) {
	fmt.Printf("\n%s requires authentication (first run)\n", agent)
	fmt.Println("Credentials will persist for future sessions.")
	fmt.Println()

	var loginArgs []string
	switch agent {
	case computer.AgentCodex:
		loginArgs = []string{"login"}
		if getenvFallback("AMUX_CODEX_DEVICE_AUTH") != "0" {
			loginArgs = append(loginArgs, "--device-auth")
		}
	case computer.AgentOpenCode:
		loginArgs = []string{"auth", "login"}
	case computer.AgentAmp:
		loginArgs = []string{"login"}
	default:
		return 0, nil
	}

	raw := false
	exitCode, err := computer.RunAgentInteractive(sb, computer.AgentConfig{
		Agent:         agent,
		WorkspacePath: workspacePath,
		Args:          loginArgs,
		Env:           envMap,
		RawMode:       &raw,
	})
	if err != nil {
		return 1, err
	}

	if exitCode == 0 {
		fmt.Println("\n✓ Authentication complete")
	}

	return exitCode, nil
}

// handleAgentExit handles post-exit tasks (workspace download, exit tips)
func handleAgentExit(sb computer.RemoteComputer, agent computer.Agent, exitCode int, credMode string, syncEnabled bool, cwd string, meta *computer.ComputerMeta) error {
	// Show tips for exit code 127 (command not found)
	if exitCode == 127 {
		showAgentTips(agent)
	}

	// Show exit code if non-zero
	if exitCode != 0 && exitCode != 127 {
		fmt.Printf("\nExited with code %d\n", exitCode)
	}

	// Sync workspace back
	if syncEnabled {
		worktreeID := computer.ComputeWorktreeID(cwd)
		if Verbose {
			fmt.Println("\nSyncing changes...")
			if err := computer.DownloadWorkspace(sb, computer.SyncOptions{Cwd: cwd, WorktreeID: worktreeID}, Verbose); err != nil {
				return err
			}
			fmt.Println("Done")
		} else {
			spinner := NewSpinner("Syncing changes")
			spinner.Start()
			if err := computer.DownloadWorkspace(sb, computer.SyncOptions{Cwd: cwd, WorktreeID: worktreeID}, false); err != nil {
				spinner.StopWithMessage("✗ Sync failed")
				return err
			}
			spinner.StopWithMessage("✓ Changes synced")
		}
	}

	if exitCode != 0 {
		return exitError{code: exitCode}
	}
	return nil
}

// showAgentTips displays helpful tips when an agent fails to start
func showAgentTips(agent computer.Agent) {
	fmt.Println()
	switch agent {
	case computer.AgentClaude:
		fmt.Println("Tip: Claude requires authentication. Run `claude` and complete login,")
		fmt.Println("     or pass --env ANTHROPIC_API_KEY=...")
	case computer.AgentCodex:
		fmt.Println("Tip: Codex requires OpenAI credentials. Login will start automatically,")
		fmt.Println("     or pass --env OPENAI_API_KEY=...")
	case computer.AgentOpenCode:
		fmt.Println("Tip: OpenCode requires authentication. Login will start automatically,")
		fmt.Println("     or pass provider API keys via --env")
	case computer.AgentAmp:
		fmt.Println("Tip: Amp requires authentication. Login will start automatically,")
		fmt.Println("     or pass --env AMP_API_KEY=...")
	case computer.AgentGemini:
		fmt.Println("Tip: Gemini requires authentication. Choose a login method in the CLI,")
		fmt.Println("     or pass --env GEMINI_API_KEY=...")
	case computer.AgentDroid:
		fmt.Println("Tip: Droid requires authentication. Run `/login` inside Droid,")
		fmt.Println("     or pass --env FACTORY_API_KEY=...")
	}
}

func buildComputerPreviewCommand() *cobra.Command {
	var snapshot string
	var noOpen bool
	var provider string

	cmd := &cobra.Command{
		Use:   "preview <port>",
		Short: "Open a browser preview for a computer port",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := strconv.Atoi(args[0])
			if err != nil || port <= 0 || port > 65535 {
				return fmt.Errorf("port must be a number between 1 and 65535")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}
			snapshotID := snapshot
			if snapshotID == "" {
				snapshotID = computer.ResolveSnapshotID(cfg)
			}
			providerInstance, _, err := computer.ResolveProvider(cfg, cwd, provider)
			if err != nil {
				return err
			}
			if !providerInstance.SupportsFeature(computer.FeaturePreviewURLs) {
				return fmt.Errorf("preview URLs are not supported by the selected provider")
			}
			fmt.Printf("Preparing preview for port %d...\n", port)
			sb, _, err := resolveProjectComputer(providerInstance, cwd, snapshotID)
			if err != nil {
				return err
			}
			url, err := sb.GetPreviewURL(context.Background(), port)
			if err != nil {
				return err
			}
			if url == "" {
				return fmt.Errorf("unable to construct a preview URL")
			}
			fmt.Printf("Preview URL: %s\n", url)
			if !noOpen {
				if !tryOpenURL(url) {
					fmt.Println("Open the URL in your browser.")
				}
			}
			fmt.Printf("Tip: Ensure your app listens on 0.0.0.0:%d inside the computer.\n", port)
			return nil
		},
	}
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Use a specific snapshot if a new computer is created")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open the URL automatically")
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")
	return cmd
}

func buildComputerDesktopCommand() *cobra.Command {
	var port string
	var snapshot string
	var noOpen bool
	var provider string

	cmd := &cobra.Command{
		Use:   "desktop",
		Short: "Open a remote desktop (VNC) for the computer",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := strconv.Atoi(port)
			if err != nil || p <= 0 || p > 65535 {
				return fmt.Errorf("port must be a number between 1 and 65535")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}
			snapshotID := snapshot
			if snapshotID == "" {
				snapshotID = computer.ResolveSnapshotID(cfg)
			}
			providerInstance, _, err := computer.ResolveProvider(cfg, cwd, provider)
			if err != nil {
				return err
			}
			if !providerInstance.SupportsFeature(computer.FeatureDesktop) {
				return fmt.Errorf("desktop is not supported by the selected provider")
			}
			fmt.Println("Checking desktop status...")
			sb, _, err := resolveProjectComputer(providerInstance, cwd, snapshotID)
			if err != nil {
				return err
			}
			desktop, ok := sb.(computer.DesktopAccess)
			if !ok {
				return fmt.Errorf("desktop is not available for this provider")
			}
			status, err := desktop.DesktopStatus(context.Background())
			if err != nil {
				return fmt.Errorf("desktop is not available in this computer image. Tip: use a desktop-enabled base image and rebuild your snapshot")
			}
			if status == nil || status.Status != "active" {
				fmt.Println("Starting desktop...")
				if err := desktop.StartDesktop(context.Background()); err != nil {
					return fmt.Errorf("failed to start desktop services. Tip: your snapshot may be missing VNC dependencies (xvfb/novnc)")
				}
				time.Sleep(5 * time.Second)
				status, err = desktop.DesktopStatus(context.Background())
				if err != nil {
					return err
				}
			}
			if status == nil || status.Status != "active" {
				return fmt.Errorf("desktop failed to start (status: %s)", func() string {
					if status == nil {
						return "unknown"
					}
					return status.Status
				}())
			}
			url, err := sb.GetPreviewURL(context.Background(), p)
			if err != nil {
				return err
			}
			url = buildVncURL(url)
			if url == "" {
				return fmt.Errorf("unable to construct the desktop URL")
			}
			fmt.Printf("Desktop URL: %s\n", url)
			if !noOpen {
				if !tryOpenURL(url) {
					fmt.Println("Open the URL in your browser.")
				}
			}
			fmt.Println("Tip: If the page is blank, wait a few seconds and refresh.")
			return nil
		},
	}
	cmd.Flags().StringVar(&port, "port", "6080", "VNC port (default: 6080)")
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Use a specific snapshot if a new computer is created")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open the URL automatically")
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")
	return cmd
}

// ComputerListItem represents a single computer in JSON output
type ComputerListItem struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Agent   string `json:"agent"`
	Project string `json:"project,omitempty"`
}

func buildComputerLsCommand() *cobra.Command {
	var provider string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List all amux computers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}
			providerInstance, _, err := computer.ResolveProvider(cfg, cwd, provider)
			if err != nil {
				return err
			}
			computers, err := computer.ListAmuxComputers(providerInstance)
			if err != nil {
				return err
			}

			if jsonOutput {
				items := make([]ComputerListItem, 0, len(computers))
				for _, sb := range computers {
					item := ComputerListItem{
						ID:    sb.ID(),
						State: string(sb.State()),
						Agent: "unknown",
					}
					labels := sb.Labels()
					if labels != nil {
						if val, ok := labels["amux.agent"]; ok {
							item.Agent = val
						}
						if val, ok := labels["amux.projectId"]; ok {
							item.Project = val
						}
					}
					items = append(items, item)
				}
				data, _ := json.MarshalIndent(items, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			if len(computers) == 0 {
				fmt.Println("No computers found")
				return nil
			}
			fmt.Printf("%-12s %-10s %-10s %s\n", "ID", "STATE", "AGENT", "PROJECT")
			fmt.Println(strings.Repeat("─", 60))
			for _, sb := range computers {
				agent := "unknown"
				project := "unknown"
				labels := sb.Labels()
				if labels != nil {
					if val, ok := labels["amux.agent"]; ok {
						agent = val
					}
					if val, ok := labels["amux.projectId"]; ok {
						if len(val) > 8 {
							project = val[:8]
						} else {
							project = val
						}
					}
				}
				id := sb.ID()
				if len(id) > 12 {
					id = id[:12]
				}
				fmt.Printf("%-12s %-10s %-10s %s\n", id, sb.State(), agent, project)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	return cmd
}

func buildComputerRmCommand() *cobra.Command {
	var project bool
	var provider string
	cmd := &cobra.Command{
		Use:   "rm [id]",
		Short: "Remove a computer",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cfg, err := computer.LoadConfig()
			if err != nil {
				return err
			}
			providerInstance, _, err := computer.ResolveProvider(cfg, cwd, provider)
			if err != nil {
				return err
			}
			if project {
				if err := computer.RemoveComputer(providerInstance, cwd, ""); err != nil {
					return err
				}
				fmt.Println("Removed computer for current project")
				return nil
			}
			if len(args) == 0 {
				return fmt.Errorf("provide a computer ID or use --project to remove the current project computer")
			}
			if err := computer.RemoveComputer(providerInstance, cwd, args[0]); err != nil {
				return err
			}
			fmt.Printf("Removed computer %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "Remove computer for current project")
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider: daytona, sprites, or docker (required unless AMUX_PROVIDER is set)")
	return cmd
}

func resolveProjectComputer(provider computer.Provider, cwd string, snapshotID string) (computer.RemoteComputer, bool, error) {
	if provider == nil {
		return nil, false, fmt.Errorf("provider is required")
	}
	meta, err := computer.LoadComputerMeta(cwd, provider.Name())
	if err != nil {
		return nil, false, err
	}
	if meta != nil {
		sb, err := provider.GetComputer(context.Background(), meta.ComputerID)
		if err == nil {
			if err := sb.Start(context.Background()); err == nil {
				if waitErr := sb.WaitReady(context.Background(), 60*time.Second); waitErr != nil {
					if Verbose {
						fmt.Fprintf(os.Stderr, "Warning: computer may not be fully ready: %v\n", waitErr)
					}
				}
				return sb, true, nil
			}
		}
		fmt.Fprintln(os.Stderr, "Existing computer not found, creating a new one...")
	}
	agent := computer.AgentShell
	if meta != nil && meta.Agent != "" {
		agent = meta.Agent
	}
	sb, _, err := computer.EnsureComputer(provider, cwd, computer.ComputerConfig{
		Agent:           agent,
		CredentialsMode: "computer",
		Snapshot:        snapshotID,
	}, false)
	if err != nil {
		return nil, false, err
	}
	return sb, false, nil
}

// exitError lets commands return a specific exit code without printing an error.
type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit with code %d", e.code)
}
