package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/computer"
)

func buildAuthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <action> [provider]",
		Short: "Authentication commands",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := args[0]
			provider := ""
			if len(args) > 1 {
				provider = args[1]
			}

			switch action {
			case "login":
				if provider != "" {
					switch provider {
					case "gh", "github":
						return runGhAuthLogin()
					case "sprites":
						return runSpritesAuthLogin()
					default:
						return fmt.Errorf("unknown provider: use gh or sprites")
					}
				}
				cfg, err := computer.LoadConfig()
				if err != nil {
					return err
				}
				apiKey, err := promptInput("Daytona API key: ")
				if err != nil {
					return err
				}
				if apiKey == "" {
					return fmt.Errorf("no API key provided")
				}
				cfg.DaytonaAPIKey = apiKey
				if err := computer.SaveConfig(cfg); err != nil {
					return err
				}
				fmt.Println("Saved credentials to ~/.amux/config.json")
				fmt.Println()
				fmt.Println("Note: Agent authentication (Claude, Codex, etc.) happens inside the sandbox")
				fmt.Println("via OAuth/browser login on first run - no API keys needed here.")
				return nil
			case "status":
				cfg, err := computer.LoadConfig()
				if err != nil {
					return err
				}
				showAll := len(args) > 1 && args[1] == "--all"

				fmt.Println("amux auth status")
				fmt.Println(strings.Repeat("─", 50))
				fmt.Println()

				// Daytona API key
				if computer.ResolveAPIKey(cfg) != "" {
					fmt.Println("✓ Daytona API key configured")
				} else {
					fmt.Println("✗ Daytona API key not set")
					fmt.Println("  Run: amux auth login")
				}

				if showAll {
					fmt.Println()
					if computer.ResolveSpritesToken(cfg) != "" {
						fmt.Println("✓ Sprites token configured")
					} else {
						fmt.Println("• Sprites token not set (needed for Sprites provider)")
					}

					fmt.Println()
					fmt.Println("Agent authentication (Claude, Codex, Gemini, etc.):")
					fmt.Println("  Agents authenticate via OAuth/browser login inside the sandbox.")
					fmt.Println("  Credentials persist on the computer for future sessions.")
					fmt.Println("  Optional: pass API keys via --env flag to skip OAuth.")
				} else {
					fmt.Println()
					fmt.Println("Run `amux auth status --all` for more details")
				}

				fmt.Println()
				fmt.Println(strings.Repeat("─", 50))
				return nil
			case "logout":
				if err := computer.ClearConfigKeys(); err != nil {
					return err
				}
				fmt.Println("Removed saved credentials from ~/.amux/config.json")
				fmt.Println("If you use env vars, unset AMUX_DAYTONA_API_KEY")
				return nil
			default:
				return fmt.Errorf("unknown action: use login, logout, or status")
			}
		},
	}
	return cmd
}

func runGhAuthLogin() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, err := computer.LoadConfig()
	if err != nil {
		return err
	}
	providerInstance, _, err := computer.ResolveProvider(cfg, cwd, "")
	if err != nil {
		return err
	}

	// Load existing computer metadata - require computer to exist
	meta, err := computer.LoadComputerMeta(cwd, providerInstance.Name())
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("no computer exists - run `amux computer run <agent>` first to create one")
	}

	sb, err := providerInstance.GetComputer(context.Background(), meta.ComputerID)
	if err != nil {
		return fmt.Errorf("computer not found - run `amux computer run <agent>` to create one")
	}

	// Ensure computer is started
	if sb.State() != computer.StateStarted {
		fmt.Fprintln(os.Stderr, "Starting computer...")
		if err := sb.Start(context.Background()); err != nil {
			return fmt.Errorf("failed to start computer: %w", err)
		}
		if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: computer may not be fully ready: %v\n", err)
		}
	}

	prevShellRaw := os.Getenv("AMUX_SHELL_RAW")
	os.Setenv("AMUX_SHELL_RAW", "0")
	defer func() {
		if prevShellRaw == "" {
			_ = os.Unsetenv("AMUX_SHELL_RAW")
		} else {
			_ = os.Setenv("AMUX_SHELL_RAW", prevShellRaw)
		}
	}()

	if err := computer.SetupCredentials(sb, computer.CredentialsConfig{Mode: "computer", Agent: computer.AgentShell}, false); err != nil {
		return err
	}

	if !ensureGhCli(sb) {
		return fmt.Errorf("GitHub CLI is required for device login")
	}

	status, _ := sb.Exec(context.Background(), `bash -lc "gh auth status -h github.com >/dev/null 2>&1"`, nil)
	if status != nil && status.ExitCode == 0 {
		fmt.Println("GitHub is already authenticated on this computer")
		return nil
	}

	fmt.Println("\namux GitHub login")
	fmt.Println("1. A one-time device code will appear below")
	fmt.Println("2. Open https://github.com/login/device locally")
	fmt.Println("3. Paste the code, finish the login, then return here")
	fmt.Println("If prompted, choose GitHub.com + HTTPS")
	fmt.Println("Tip: if you see \"Press Enter\", just hit Enter")

	homeDir := resolveComputerHome(sb)
	script := strings.Join([]string{
		"echo ''",
		"echo 'GitHub device login starting'",
		"echo 'Open https://github.com/login/device on your local machine'",
		"echo 'Paste the one-time code shown below'",
		"echo ''",
		"gh auth login --hostname github.com --git-protocol https --device --skip-ssh-key",
		"gh auth setup-git",
		"if gh auth status -h github.com >/dev/null 2>&1; then",
		"  echo ''",
		"  echo 'GitHub auth saved on this computer'",
		"else",
		"  echo ''",
		"  echo 'GitHub auth not confirmed - run `amux auth login gh` again'",
		"fi",
	}, "\n")

	raw := false
	exitCode, err := computer.RunAgentInteractive(sb, computer.AgentConfig{
		Agent:         computer.AgentShell,
		WorkspacePath: homeDir,
		Args:          []string{"-lc", script},
		Env:           map[string]string{"BROWSER": "echo"},
		RawMode:       &raw,
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("GitHub auth session exited with code %d", exitCode)
	}
	return nil
}

func runSpritesAuthLogin() error {
	cfg, err := computer.LoadConfig()
	if err != nil {
		return err
	}
	token, err := promptInput("Sprites token: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("no token provided")
	}
	apiURL, err := promptInput("Sprites API URL (optional): ")
	if err != nil {
		return err
	}
	cfg.SpritesToken = strings.TrimSpace(token)
	if strings.TrimSpace(apiURL) != "" {
		cfg.SpritesAPIURL = strings.TrimSpace(apiURL)
	}
	if err := computer.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Println("Saved Sprites credentials to ~/.amux/config.json")
	return nil
}

func ensureGhCli(sb computer.RemoteComputer) bool {
	check, _ := sb.Exec(context.Background(), "command -v gh", nil)
	if check != nil && check.ExitCode == 0 {
		return true
	}
	fmt.Println("GitHub CLI not found, attempting install...")
	installCmd := `bash -lc "if command -v apt-get >/dev/null 2>&1; then (apt-get update -y || sudo apt-get update -y) >/dev/null 2>&1; (apt-get install -y gh || sudo apt-get install -y gh) >/dev/null 2>&1; elif command -v apk >/dev/null 2>&1; then (apk add --no-cache github-cli) >/dev/null 2>&1; elif command -v yum >/dev/null 2>&1; then (yum install -y gh || sudo yum install -y gh) >/dev/null 2>&1; elif command -v dnf >/dev/null 2>&1; then (dnf install -y gh || sudo dnf install -y gh) >/dev/null 2>&1; else exit 1; fi"`
	resp, _ := sb.Exec(context.Background(), installCmd, nil)
	if resp != nil && resp.ExitCode == 0 {
		return true
	}
	fmt.Println("Failed to install GitHub CLI - install gh manually and run `gh auth login` inside a computer shell")
	return false
}
