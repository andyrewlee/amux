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

	cmd := &cobra.Command{
		Use:   "run <agent>",
		Short: "Run Claude Code, Codex, OpenCode, Amp, Gemini CLI, Droid, or a shell in a sandbox",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentName := args[0]
			if !sandbox.IsValidAgent(agentName) {
				return fmt.Errorf("invalid agent. Use: claude, codex, opencode, amp, gemini, droid, or shell")
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

			credMode := strings.ToLower(credentials)
			switch credMode {
			case "sandbox", "none", "auto", "":
				if credMode == "" {
					credMode = "auto"
				}
			default:
				return fmt.Errorf("invalid credentials mode. Use: sandbox, none, or auto")
			}
			if credMode == "auto" {
				if agent == sandbox.AgentShell {
					credMode = "none"
				} else {
					credMode = "sandbox"
				}
			}

			envMap := map[string]string{}
			for _, env := range envVars {
				parts := strings.SplitN(env, "=", 2)
				if len(parts) == 2 && parts[0] != "" {
					envMap[parts[0]] = parts[1]
				}
			}

			volumeSpecs := []sandbox.VolumeSpec{}
			for _, spec := range volumes {
				vol, err := parseVolumeSpec(spec)
				if err != nil {
					return err
				}
				volumeSpecs = append(volumeSpecs, vol)
			}

			syncEnabled := !noSync
			userArgs := getAgentArgs(os.Args, agentName)
			agentArgs := append([]string{}, userArgs...)
			hasUserArgs := len(userArgs) > 0
			if agent == sandbox.AgentCodex {
				pref := getenvFallback("AMUX_CODEX_TUI2")
				if pref != "0" {
					hasFlag := false
					for i := 0; i < len(agentArgs); i++ {
						arg := agentArgs[i]
						if (arg == "--enable" || arg == "--disable") && i+1 < len(agentArgs) && agentArgs[i+1] == "tui2" {
							hasFlag = true
							break
						}
						if arg == "-c" && i+1 < len(agentArgs) && strings.HasPrefix(agentArgs[i+1], "features.tui2") {
							hasFlag = true
							break
						}
					}
					if !hasFlag {
						agentArgs = append([]string{"--enable", "tui2"}, agentArgs...)
					}
				}
			}

			snapshotID := snapshot
			if snapshotID == "" {
				snapshotID = sandbox.ResolveSnapshotID(cfg)
			}
			if snapshotID != "" {
				fmt.Printf("Using snapshot: %s\n", snapshotID)
			}

			fmt.Printf("Starting %s sandbox...\n", agent)
			sb, meta, err := sandbox.EnsureSandbox(cwd, sandbox.SandboxConfig{
				Agent:           agent,
				EnvVars:         envMap,
				Volumes:         volumeSpecs,
				CredentialsMode: credMode,
				Snapshot:        snapshotID,
			}, recreate)
			if err != nil {
				return err
			}

			fmt.Println("Sandbox ID: " + sb.ID)
			fmt.Println("Sandbox state: " + string(sb.State))

			workspacePath := sandbox.GetWorkspaceRepoPath(sb, sandbox.SyncOptions{Cwd: cwd, WorkspaceID: meta.WorkspaceID})

			if syncEnabled {
				fmt.Println("\nSyncing workspace to sandbox...")
				if err := sandbox.UploadWorkspace(sb, sandbox.SyncOptions{Cwd: cwd, WorkspaceID: meta.WorkspaceID}); err != nil {
					return err
				}
			} else {
				_, _ = sb.Process.ExecuteCommand(fmt.Sprintf("mkdir -p %s", workspacePath))
			}

			fmt.Println("\nConfiguring credentials...")
			client, err := sandbox.GetDaytonaClient()
			if err != nil {
				return err
			}
			if err := sandbox.SetupCredentials(client, sb, sandbox.CredentialsConfig{Mode: credMode, Agent: agent}); err != nil {
				return err
			}
			if credMode != "none" {
				fmt.Println("Credentials: stored in shared volume (login once).")
			}

			fmt.Println("\nInstalling agent...")
			if err := sandbox.EnsureAgentInstalled(sb, agent); err != nil {
				return err
			}

			var exitCode *int
			attemptedCodexLogin := false
			attemptedOpenCodeLogin := false

			if agent == sandbox.AgentCodex && credMode != "none" && getenvFallback("AMUX_CODEX_AUTO_LOGIN") != "0" && !hasUserArgs {
				attemptedCodexLogin = true
				loginArgs := []string{"login"}
				if getenvFallback("AMUX_CODEX_DEVICE_AUTH") != "0" {
					loginArgs = append(loginArgs, "--device-auth")
				}
				fmt.Println("\nCodex login required (first run).")
				fmt.Println("Complete login once; credentials persist in the volume.")
				raw := false
				code, err := sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
					Agent:         sandbox.AgentCodex,
					WorkspacePath: workspacePath,
					Args:          loginArgs,
					Env:           envMap,
					RawMode:       &raw,
				})
				if err != nil {
					return err
				}
				if code != 0 {
					exitCode = &code
				} else {
					fmt.Println("\nCodex login complete.")
					fmt.Println()
				}
			}

			if exitCode == nil && credMode != "none" && !hasUserArgs {
				switch agent {
				case sandbox.AgentOpenCode:
					if getenvFallback("AMUX_OPENCODE_AUTO_LOGIN") != "0" {
						check, _ := sb.Process.ExecuteCommand(`sh -lc "test -f ~/.local/share/opencode/auth.json"`)
						if check == nil || check.ExitCode != 0 {
							attemptedOpenCodeLogin = true
							fmt.Println("\nOpenCode login required (first run).")
							fmt.Println("Complete login once; credentials persist in the volume.")
							raw := false
							code, err := sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
								Agent:         sandbox.AgentOpenCode,
								WorkspacePath: workspacePath,
								Args:          []string{"auth", "login"},
								Env:           envMap,
								RawMode:       &raw,
							})
							if err != nil {
								return err
							}
							if code != 0 {
								exitCode = &code
							} else {
								fmt.Println("\nOpenCode login complete.")
								fmt.Println()
							}
						}
					}
				case sandbox.AgentAmp:
					if getenvFallback("AMUX_AMP_AUTO_LOGIN") != "0" && envMap["AMP_API_KEY"] == "" {
						check, _ := sb.Process.ExecuteCommand(`sh -lc "test -f ~/.local/share/amp/secrets.json"`)
						if check == nil || check.ExitCode != 0 {
							fmt.Println("\nAmp login required (first run).")
							fmt.Println("Complete login once; credentials persist in the volume.")
							raw := false
							code, err := sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
								Agent:         sandbox.AgentAmp,
								WorkspacePath: workspacePath,
								Args:          []string{"login"},
								Env:           envMap,
								RawMode:       &raw,
							})
							if err != nil {
								return err
							}
							if code != 0 {
								exitCode = &code
							} else {
								fmt.Println("\nAmp login complete.")
								fmt.Println()
							}
						}
					}
				case sandbox.AgentGemini:
					if getenvFallback("AMUX_GEMINI_AUTO_LOGIN") != "0" && envMap["GEMINI_API_KEY"] == "" && envMap["GOOGLE_API_KEY"] == "" && envMap["GOOGLE_APPLICATION_CREDENTIALS"] == "" {
						check, _ := sb.Process.ExecuteCommand(`sh -lc "test -f ~/.gemini/oauth_creds.json"`)
						if check == nil || check.ExitCode != 0 {
							fmt.Println("\nGemini login required (first run). Choose a login method inside the CLI.")
							fmt.Println("Complete login once; credentials persist in the volume.")
						}
					}
				case sandbox.AgentDroid:
					if getenvFallback("AMUX_DROID_AUTO_LOGIN") != "0" && envMap["FACTORY_API_KEY"] == "" {
						check, _ := sb.Process.ExecuteCommand(`sh -lc "test -f ~/.factory/config.json"`)
						if check == nil || check.ExitCode != 0 {
							fmt.Println("\nDroid login required (first run). Run `/login` inside Droid to authenticate.")
							fmt.Println("Complete login once; credentials persist in the volume.")
						}
					}
				}
			}

			if exitCode == nil {
				fmt.Printf("\nStarting %s in interactive mode...\n", agent)
				fmt.Println("Workspace: " + workspacePath)
				fmt.Println()
				if agent == sandbox.AgentClaude {
					fmt.Println("Tip: if Claude doesn't prompt, pass `--env ANTHROPIC_BASE_URL=...` or `--env HTTP_PROXY=...` to match your local setup.")
				}
				code, err := sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
					Agent:         agent,
					WorkspacePath: workspacePath,
					Args:          agentArgs,
					Env:           envMap,
				})
				if err != nil {
					return err
				}
				exitCode = &code

				if agent == sandbox.AgentCodex && code == 255 && !attemptedCodexLogin && credMode != "none" && getenvFallback("AMUX_CODEX_AUTO_LOGIN") != "0" && !hasUserArgs {
					fmt.Println("\nCodex exited unexpectedly. Attempting login...")
					raw := false
					loginExit, err := sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
						Agent:         sandbox.AgentCodex,
						WorkspacePath: workspacePath,
						Args:          []string{"login"},
						Env:           envMap,
						RawMode:       &raw,
					})
					if err != nil {
						return err
					}
					if loginExit == 0 {
						fmt.Println("\nCodex login complete.")
						fmt.Println()
						code, err = sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
							Agent:         sandbox.AgentCodex,
							WorkspacePath: workspacePath,
							Args:          agentArgs,
							Env:           envMap,
						})
						if err != nil {
							return err
						}
						exitCode = &code
					} else {
						exitCode = &loginExit
					}
				}

				if agent == sandbox.AgentOpenCode && code == 255 && !attemptedOpenCodeLogin && credMode != "none" && getenvFallback("AMUX_OPENCODE_AUTO_LOGIN") != "0" && !hasUserArgs {
					fmt.Println("\nOpenCode exited unexpectedly. Attempting login...")
					raw := false
					loginExit, err := sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
						Agent:         sandbox.AgentOpenCode,
						WorkspacePath: workspacePath,
						Args:          []string{"auth", "login"},
						Env:           envMap,
						RawMode:       &raw,
					})
					if err != nil {
						return err
					}
					if loginExit == 0 {
						fmt.Println("\nOpenCode login complete.")
						fmt.Println()
						code, err = sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
							Agent:         sandbox.AgentOpenCode,
							WorkspacePath: workspacePath,
							Args:          agentArgs,
							Env:           envMap,
						})
						if err != nil {
							return err
						}
						exitCode = &code
					} else {
						exitCode = &loginExit
					}
				}
			}

			finalCode := 0
			if exitCode != nil {
				finalCode = *exitCode
			}
			fmt.Printf("\nAgent exited with code: %d\n", finalCode)

			if credMode != "none" {
				_ = sandbox.SyncCredentialsFromSandbox(sb, sandbox.CredentialsConfig{Mode: credMode, Agent: agent})
			}

			if finalCode == 127 {
				switch agent {
				case sandbox.AgentClaude:
					if envMap["ANTHROPIC_API_KEY"] == "" && envMap["CLAUDE_API_KEY"] == "" && envMap["ANTHROPIC_AUTH_TOKEN"] == "" {
						fmt.Println("\nClaude Code requires credentials in the sandbox (ANTHROPIC_AUTH_TOKEN from `claude /login`, or ANTHROPIC_API_KEY/CLAUDE_API_KEY). Log in inside the sandbox or pass --env ANTHROPIC_AUTH_TOKEN=... then retry.")
					}
				case sandbox.AgentCodex:
					if envMap["OPENAI_API_KEY"] == "" {
						fmt.Println("\nCodex requires OpenAI credentials in the sandbox. Log in inside the sandbox (`codex login`) or pass --env OPENAI_API_KEY=... then retry.")
					}
				case sandbox.AgentOpenCode:
					fmt.Println("\nOpenCode stores credentials in ~/.local/share/opencode/auth.json. Run `opencode auth login` inside the sandbox or pass provider keys via --env/your project .env.")
				case sandbox.AgentAmp:
					fmt.Println("\nAmp stores credentials in ~/.local/share/amp/secrets.json. Run `amp login` inside the sandbox or pass --env AMP_API_KEY=... then retry.")
				case sandbox.AgentGemini:
					fmt.Println("\nGemini stores OAuth credentials in ~/.gemini/oauth_creds.json. Run `gemini` and choose Login with Google, or pass --env GEMINI_API_KEY=.../GOOGLE_API_KEY=... then retry.")
				case sandbox.AgentDroid:
					fmt.Println("\nDroid stores settings in ~/.factory. Run `/login` inside Droid or pass --env FACTORY_API_KEY=... then retry.")
				}
			}

			if syncEnabled {
				fmt.Println("\nSyncing workspace from sandbox...")
				if err := sandbox.DownloadWorkspace(sb, sandbox.SyncOptions{Cwd: cwd, WorkspaceID: meta.WorkspaceID}); err != nil {
					return err
				}
				fmt.Println("\nWorkspace synced successfully")
			}

			if finalCode != 0 {
				return exitError{code: finalCode}
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVarP(&envVars, "env", "e", []string{}, "Environment variable (repeatable)")
	cmd.Flags().StringArrayVarP(&volumes, "volume", "v", []string{}, "Volume mount (repeatable)")
	cmd.Flags().StringVar(&credentials, "credentials", "auto", "Credentials mode (sandbox|none|auto)")
	cmd.Flags().BoolVar(&recreate, "recreate", false, "Recreate sandbox with new config")
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Use a specific snapshot")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip workspace sync")

	return cmd
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
		Short: "List all AMUX sandboxes",
		RunE: func(cmd *cobra.Command, args []string) error {
			sandboxes, err := sandbox.ListAmuxSandboxes()
			if err != nil {
				return err
			}
			if len(sandboxes) == 0 {
				fmt.Println("No AMUX sandboxes found")
				return nil
			}
			fmt.Println("AMUX Sandboxes:")
			fmt.Println(strings.Repeat("-", 80))
			for _, sb := range sandboxes {
				fmt.Println("\n  ID: " + sb.ID)
				fmt.Println("  State: " + string(sb.State))
				agent := "unknown"
				workspace := "unknown"
				if sb.Labels != nil {
					if val, ok := sb.Labels["amux.agent"]; ok {
						agent = val
					}
					if val, ok := sb.Labels["amux.workspaceId"]; ok {
						workspace = val
					}
				}
				fmt.Println("  Agent: " + agent)
				fmt.Println("  Workspace ID: " + workspace)
				fmt.Printf("  CPU: %.2f, RAM: %.2fGiB\n", sb.CPU, sb.Memory)
			}
			fmt.Println("\n" + strings.Repeat("-", 80))
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
				return fmt.Errorf("either provide a sandbox ID or use --workspace")
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
