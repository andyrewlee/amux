package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/daytona"
	"github.com/andyrewlee/amux/internal/sandbox"
)

// Verbose controls whether verbose output is enabled.
var Verbose bool

func buildSandboxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage sandboxes",
	}
	cmd.AddCommand(buildSandboxRunCommand())
	cmd.AddCommand(buildSandboxPreviewCommand())
	cmd.AddCommand(buildSandboxDesktopCommand())
	cmd.AddCommand(buildSandboxLsCommand())
	cmd.AddCommand(buildSandboxRmCommand())
	return cmd
}

func buildSandboxRunCommand() *cobra.Command {
	var envVars []string
	var volumes []string
	var credentials string
	var recreate bool
	var snapshot string
	var noSync bool
	var autoStop int32

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

			if err := sandbox.RunPreflight(); err != nil {
				return err
			}

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
				fmt.Printf("Using snapshot: %s\n", snapshotID)
			}

			// Run the agent with clean output
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

type runAgentParams struct {
	agent       sandbox.Agent
	cwd         string
	envMap      map[string]string
	volumeSpecs []sandbox.VolumeSpec
	credMode    string
	autoStop    int32
	snapshotID  string
	recreate    bool
	syncEnabled bool
	agentArgs   []string
}

// runAgent is the core logic for running an agent in a sandbox.
// It provides a clean, minimal output experience similar to docker sandbox run.
func runAgent(p runAgentParams) error {
	var sb *daytona.Sandbox
	var meta *sandbox.WorkspaceMeta
	var err error

	// Step 1: Create/get sandbox
	if Verbose {
		fmt.Printf("Starting %s sandbox...\n", p.agent)
		sb, meta, err = sandbox.EnsureSandbox(p.cwd, sandbox.SandboxConfig{
			Agent:            p.agent,
			EnvVars:          p.envMap,
			Volumes:          p.volumeSpecs,
			CredentialsMode:  p.credMode,
			AutoStopInterval: p.autoStop,
			Snapshot:         p.snapshotID,
		}, p.recreate)
	} else {
		spinner := NewSpinner(fmt.Sprintf("Starting %s sandbox", p.agent))
		spinner.Start()
		sb, meta, err = sandbox.EnsureSandbox(p.cwd, sandbox.SandboxConfig{
			Agent:            p.agent,
			EnvVars:          p.envMap,
			Volumes:          p.volumeSpecs,
			CredentialsMode:  p.credMode,
			AutoStopInterval: p.autoStop,
			Snapshot:         p.snapshotID,
		}, p.recreate)
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
		fmt.Println("Sandbox ID: " + sb.ID)
	}

	workspacePath := sandbox.GetWorkspaceRepoPath(sb, sandbox.SyncOptions{Cwd: p.cwd, WorkspaceID: meta.WorkspaceID})

	// Step 2: Sync workspace
	if p.syncEnabled {
		if Verbose {
			fmt.Println("Syncing workspace...")
			if err := sandbox.UploadWorkspace(sb, sandbox.SyncOptions{Cwd: p.cwd, WorkspaceID: meta.WorkspaceID}, Verbose); err != nil {
				return err
			}
		} else {
			spinner := NewSpinner("Syncing workspace")
			spinner.Start()
			syncErr := sandbox.UploadWorkspace(sb, sandbox.SyncOptions{Cwd: p.cwd, WorkspaceID: meta.WorkspaceID}, false)
			if syncErr != nil {
				spinner.StopWithMessage("✗ Sync failed")
				return syncErr
			}
			spinner.StopWithMessage("✓ Workspace synced")
		}
	} else {
		_, _ = sb.Process.ExecuteCommand(fmt.Sprintf("mkdir -p %s", workspacePath))
	}

	// Step 3: Setup credentials
	client, err := sandbox.GetDaytonaClient()
	if err != nil {
		return err
	}
	if Verbose {
		fmt.Println("Setting up environment...")
		if err := sandbox.SetupCredentials(client, sb, sandbox.CredentialsConfig{Mode: p.credMode, Agent: p.agent}, Verbose); err != nil {
			return err
		}
		if err := sandbox.EnsureAgentInstalled(sb, p.agent, Verbose); err != nil {
			return err
		}
	} else {
		spinner := NewSpinner("Setting up environment")
		spinner.Start()
		if err := sandbox.SetupCredentials(client, sb, sandbox.CredentialsConfig{Mode: p.credMode, Agent: p.agent}, false); err != nil {
			spinner.StopWithMessage("✗ Setup failed")
			return err
		}
		if err := sandbox.EnsureAgentInstalled(sb, p.agent, false); err != nil {
			spinner.StopWithMessage("✗ Agent install failed")
			return err
		}
		spinner.StopWithMessage("✓ Ready")
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
	exitCode, err = sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
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
func checkNeedsLogin(sb *daytona.Sandbox, agent sandbox.Agent, envMap map[string]string) bool {
	// Check if credentials already exist in the volume
	credStatus := sandbox.CheckAgentCredentials(sb, agent)
	if credStatus.HasCredential {
		return false
	}

	// Check if API key is provided via environment
	switch agent {
	case sandbox.AgentClaude:
		if envMap["ANTHROPIC_API_KEY"] != "" || envMap["CLAUDE_API_KEY"] != "" || envMap["ANTHROPIC_AUTH_TOKEN"] != "" {
			return false
		}
	case sandbox.AgentCodex:
		if envMap["OPENAI_API_KEY"] != "" {
			return false
		}
	case sandbox.AgentGemini:
		if envMap["GEMINI_API_KEY"] != "" || envMap["GOOGLE_API_KEY"] != "" || envMap["GOOGLE_APPLICATION_CREDENTIALS"] != "" {
			return false
		}
	case sandbox.AgentDroid:
		if envMap["FACTORY_API_KEY"] != "" {
			return false
		}
	case sandbox.AgentAmp:
		if envMap["AMP_API_KEY"] != "" {
			return false
		}
	}

	// Agents that need explicit login
	switch agent {
	case sandbox.AgentCodex, sandbox.AgentOpenCode, sandbox.AgentAmp:
		return true
	}

	return false
}

// handleAgentLogin runs the login flow for agents that need it
func handleAgentLogin(sb *daytona.Sandbox, agent sandbox.Agent, workspacePath string, envMap map[string]string) (int, error) {
	fmt.Printf("\n%s requires authentication (first run)\n", agent)
	fmt.Println("Credentials will persist for future sessions.")
	fmt.Println()

	var loginArgs []string
	switch agent {
	case sandbox.AgentCodex:
		loginArgs = []string{"login"}
		if getenvFallback("AMUX_CODEX_DEVICE_AUTH") != "0" {
			loginArgs = append(loginArgs, "--device-auth")
		}
	case sandbox.AgentOpenCode:
		loginArgs = []string{"auth", "login"}
	case sandbox.AgentAmp:
		loginArgs = []string{"login"}
	default:
		return 0, nil
	}

	raw := false
	exitCode, err := sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
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

// handleAgentExit handles post-exit tasks (credential sync, workspace download, exit tips)
func handleAgentExit(sb *daytona.Sandbox, agent sandbox.Agent, exitCode int, credMode string, syncEnabled bool, cwd string, meta *sandbox.WorkspaceMeta) error {
	// Sync credentials back (no-op for volume-based credentials)
	if credMode != "none" {
		_ = sandbox.SyncCredentialsFromSandbox(sb, sandbox.CredentialsConfig{Mode: credMode, Agent: agent})
	}

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
		if Verbose {
			fmt.Println("\nSyncing changes...")
			if err := sandbox.DownloadWorkspace(sb, sandbox.SyncOptions{Cwd: cwd, WorkspaceID: meta.WorkspaceID}, Verbose); err != nil {
				return err
			}
			fmt.Println("Done")
		} else {
			spinner := NewSpinner("Syncing changes")
			spinner.Start()
			if err := sandbox.DownloadWorkspace(sb, sandbox.SyncOptions{Cwd: cwd, WorkspaceID: meta.WorkspaceID}, false); err != nil {
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
func showAgentTips(agent sandbox.Agent) {
	fmt.Println()
	switch agent {
	case sandbox.AgentClaude:
		fmt.Println("Tip: Claude requires authentication. Run `claude` and complete login,")
		fmt.Println("     or pass --env ANTHROPIC_API_KEY=...")
	case sandbox.AgentCodex:
		fmt.Println("Tip: Codex requires OpenAI credentials. Login will start automatically,")
		fmt.Println("     or pass --env OPENAI_API_KEY=...")
	case sandbox.AgentOpenCode:
		fmt.Println("Tip: OpenCode requires authentication. Login will start automatically,")
		fmt.Println("     or pass provider API keys via --env")
	case sandbox.AgentAmp:
		fmt.Println("Tip: Amp requires authentication. Login will start automatically,")
		fmt.Println("     or pass --env AMP_API_KEY=...")
	case sandbox.AgentGemini:
		fmt.Println("Tip: Gemini requires authentication. Choose a login method in the CLI,")
		fmt.Println("     or pass --env GEMINI_API_KEY=...")
	case sandbox.AgentDroid:
		fmt.Println("Tip: Droid requires authentication. Run `/login` inside Droid,")
		fmt.Println("     or pass --env FACTORY_API_KEY=...")
	}
}

func buildSandboxPreviewCommand() *cobra.Command {
	var snapshot string
	var noOpen bool

	cmd := &cobra.Command{
		Use:   "preview <port>",
		Short: "Open a browser preview for a sandbox port",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := strconv.Atoi(args[0])
			if err != nil || port <= 0 {
				return fmt.Errorf("port must be a positive number")
			}
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
			snapshotID := snapshot
			if snapshotID == "" {
				snapshotID = sandbox.ResolveSnapshotID(cfg)
			}
			fmt.Printf("Preparing preview for port %d...\n", port)
			sb, _, err := resolveWorkspaceSandbox(cwd, snapshotID)
			if err != nil {
				return err
			}
			preview, err := sb.GetPreviewLink(port)
			if err != nil {
				return err
			}
			url := buildPreviewURL(preview)
			if url == "" {
				return fmt.Errorf("unable to construct a preview URL")
			}
			fmt.Printf("Preview URL: %s\n", url)
			if !noOpen {
				if !tryOpenURL(url) {
					fmt.Println("Open the URL in your browser.")
				}
			}
			fmt.Printf("Tip: Ensure your app listens on 0.0.0.0:%d inside the sandbox.\n", port)
			return nil
		},
	}
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Use a specific snapshot if a new sandbox is created")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open the URL automatically")
	return cmd
}

func buildSandboxDesktopCommand() *cobra.Command {
	var port string
	var snapshot string
	var noOpen bool

	cmd := &cobra.Command{
		Use:   "desktop",
		Short: "Open a remote desktop (VNC) for the sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := strconv.Atoi(port)
			if err != nil || p <= 0 {
				return fmt.Errorf("port must be a positive number")
			}
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
			snapshotID := snapshot
			if snapshotID == "" {
				snapshotID = sandbox.ResolveSnapshotID(cfg)
			}
			fmt.Println("Checking desktop status...")
			sb, _, err := resolveWorkspaceSandbox(cwd, snapshotID)
			if err != nil {
				return err
			}
			status, err := sb.GetComputerUseStatus()
			if err != nil {
				return fmt.Errorf("desktop is not available in this sandbox image. Tip: use a Daytona desktop-enabled base image and rebuild your snapshot")
			}
			if status == nil || status.Status != "active" {
				fmt.Println("Starting desktop...")
				if _, err := sb.StartComputerUse(); err != nil {
					return fmt.Errorf("failed to start desktop services. Tip: your snapshot may be missing VNC dependencies (xvfb/novnc)")
				}
				time.Sleep(5 * time.Second)
				status, err = sb.GetComputerUseStatus()
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
			preview, err := sb.GetPreviewLink(p)
			if err != nil {
				return err
			}
			url := buildVncURL(preview)
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
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Use a specific snapshot if a new sandbox is created")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open the URL automatically")
	return cmd
}

func buildSandboxLsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List all amux sandboxes",
		RunE: func(cmd *cobra.Command, args []string) error {
			sandboxes, err := sandbox.ListAmuxSandboxes()
			if err != nil {
				return err
			}
			if len(sandboxes) == 0 {
				fmt.Println("No sandboxes found")
				return nil
			}
			fmt.Printf("%-12s %-10s %-10s %s\n", "ID", "STATE", "AGENT", "WORKSPACE")
			fmt.Println(strings.Repeat("─", 60))
			for _, sb := range sandboxes {
				agent := "unknown"
				workspace := "unknown"
				if sb.Labels != nil {
					if val, ok := sb.Labels["amux.agent"]; ok {
						agent = val
					}
					if val, ok := sb.Labels["amux.workspaceId"]; ok {
						if len(val) > 8 {
							workspace = val[:8]
						} else {
							workspace = val
						}
					}
				}
				id := sb.ID
				if len(id) > 12 {
					id = id[:12]
				}
				fmt.Printf("%-12s %-10s %-10s %s\n", id, sb.State, agent, workspace)
			}
			return nil
		},
	}
	return cmd
}

func buildSandboxRmCommand() *cobra.Command {
	var workspace bool
	cmd := &cobra.Command{
		Use:   "rm [id]",
		Short: "Remove a sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if workspace {
				if err := sandbox.RemoveSandbox(cwd, ""); err != nil {
					return err
				}
				fmt.Println("Removed sandbox for current workspace")
				return nil
			}
			if len(args) == 0 {
				return fmt.Errorf("provide a sandbox ID or use --workspace to remove the current workspace sandbox")
			}
			if err := sandbox.RemoveSandbox(cwd, args[0]); err != nil {
				return err
			}
			fmt.Printf("Removed sandbox %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&workspace, "workspace", false, "Remove sandbox for current workspace")
	return cmd
}

func resolveWorkspaceSandbox(cwd string, snapshotID string) (*daytona.Sandbox, bool, error) {
	client, err := sandbox.GetDaytonaClient()
	if err != nil {
		return nil, false, err
	}
	meta, err := sandbox.LoadWorkspaceMeta(cwd)
	if err != nil {
		return nil, false, err
	}
	if meta != nil {
		sb, err := client.Get(meta.SandboxID)
		if err == nil {
			if err := sb.Start(60 * time.Second); err == nil {
				return sb, true, nil
			}
		}
		fmt.Println("Existing sandbox not found, creating a new one...")
	}
	agent := sandbox.AgentShell
	if meta != nil && meta.Agent != "" {
		agent = meta.Agent
	}
	sb, _, err := sandbox.EnsureSandbox(cwd, sandbox.SandboxConfig{
		Agent:           agent,
		CredentialsMode: "sandbox",
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
