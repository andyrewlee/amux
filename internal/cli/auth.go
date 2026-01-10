package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andyrewlee/amux/internal/daytona"
	"github.com/andyrewlee/amux/internal/sandbox"
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
					if provider != "gh" && provider != "github" {
						return fmt.Errorf("unknown provider: use gh")
					}
					return runGhAuthLogin()
				}
				cfg, err := sandbox.LoadConfig()
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
				anthropicKey, err := promptInput("Anthropic API key (optional): ")
				if err != nil {
					return err
				}
				openaiKey, err := promptInput("OpenAI API key (optional): ")
				if err != nil {
					return err
				}
				cfg.DaytonaAPIKey = apiKey
				if strings.TrimSpace(anthropicKey) != "" {
					cfg.AnthropicAPIKey = strings.TrimSpace(anthropicKey)
				}
				if strings.TrimSpace(openaiKey) != "" {
					cfg.OpenAIAPIKey = strings.TrimSpace(openaiKey)
				}
				if err := sandbox.SaveConfig(cfg); err != nil {
					return err
				}
				fmt.Println("Saved credentials to ~/.amux/config.json")
				return nil
			case "status":
				cfg, err := sandbox.LoadConfig()
				if err != nil {
					return err
				}
				showAll := len(args) > 1 && args[1] == "--all"

				fmt.Println("amux auth status")
				fmt.Println(strings.Repeat("─", 50))
				fmt.Println()

				// Daytona API key
				if sandbox.ResolveAPIKey(cfg) != "" {
					fmt.Println("✓ Daytona API key configured")
				} else {
					fmt.Println("✗ Daytona API key not set")
					fmt.Println("  Run: amux auth login")
				}

				if showAll {
					fmt.Println()

					// Anthropic API key
					anthropicKey := cfg.AnthropicAPIKey
					if anthropicKey == "" {
						anthropicKey = os.Getenv("ANTHROPIC_API_KEY")
					}
					if anthropicKey != "" {
						fmt.Println("✓ Anthropic API key configured (Claude)")
					} else {
						fmt.Println("• Anthropic API key not set (needed for Claude)")
					}

					// OpenAI API key
					openaiKey := cfg.OpenAIAPIKey
					if openaiKey == "" {
						openaiKey = os.Getenv("OPENAI_API_KEY")
					}
					if openaiKey != "" {
						fmt.Println("✓ OpenAI API key configured (Codex)")
					} else {
						fmt.Println("• OpenAI API key not set (needed for Codex)")
					}

					// Gemini API key
					geminiKey := os.Getenv("GEMINI_API_KEY")
					if geminiKey == "" {
						geminiKey = os.Getenv("GOOGLE_API_KEY")
					}
					if geminiKey != "" {
						fmt.Println("✓ Gemini API key configured")
					} else {
						fmt.Println("• Gemini API key not set (needed for Gemini CLI)")
					}
				} else {
					fmt.Println()
					fmt.Println("Run `amux auth status --all` to see all API keys")
				}

				fmt.Println()
				fmt.Println(strings.Repeat("─", 50))
				return nil
			case "logout":
				if err := sandbox.ClearConfigKeys(); err != nil {
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
	if err := sandbox.RunPreflight(); err != nil {
		return err
	}
	client, err := sandbox.GetDaytonaClient()
	if err != nil {
		return err
	}
	credMount, err := sandbox.GetCredentialsVolumeMount(client)
	if err != nil {
		return err
	}

	params := &daytona.CreateSandboxParams{
		Language:         "typescript",
		Labels:           map[string]string{"amux.purpose": "auth", "amux.provider": "gh"},
		AutoStopInterval: 10,
		Volumes:          []daytona.VolumeMount{credMount},
	}
	sb, err := client.Create(params, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = client.Delete(sb)
	}()
	if err := sb.WaitUntilStarted(60 * time.Second); err != nil {
		return err
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

	if err := sandbox.SetupCredentials(client, sb, sandbox.CredentialsConfig{Mode: "sandbox", Agent: sandbox.AgentShell}, false); err != nil {
		return err
	}

	if !ensureGhCli(sb) {
		return fmt.Errorf("GitHub CLI is required for device login")
	}

	status, _ := sb.Process.ExecuteCommand(`bash -lc "gh auth status -h github.com >/dev/null 2>&1"`)
	if status != nil && status.ExitCode == 0 {
		fmt.Println("GitHub is already authenticated in the credentials volume")
		return nil
	}

	fmt.Println("\namux GitHub login")
	fmt.Println("1. A one-time device code will appear below")
	fmt.Println("2. Open https://github.com/login/device locally")
	fmt.Println("3. Paste the code, finish the login, then return here")
	fmt.Println("If prompted, choose GitHub.com + HTTPS")
	fmt.Println("Tip: if you see \"Press Enter\", just hit Enter")

	homeDir := resolveSandboxHome(sb)
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
		"  echo 'GitHub auth saved in the credentials volume'",
		"else",
		"  echo ''",
		"  echo 'GitHub auth not confirmed - run `amux auth login gh` again'",
		"fi",
	}, "\n")

	raw := false
	exitCode, err := sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
		Agent:         sandbox.AgentShell,
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

func ensureGhCli(sb *daytona.Sandbox) bool {
	check, _ := sb.Process.ExecuteCommand("command -v gh")
	if check != nil && check.ExitCode == 0 {
		return true
	}
	fmt.Println("GitHub CLI not found, attempting install...")
	installCmd := `bash -lc "if command -v apt-get >/dev/null 2>&1; then (apt-get update -y || sudo apt-get update -y) >/dev/null 2>&1; (apt-get install -y gh || sudo apt-get install -y gh) >/dev/null 2>&1; elif command -v apk >/dev/null 2>&1; then (apk add --no-cache github-cli) >/dev/null 2>&1; elif command -v yum >/dev/null 2>&1; then (yum install -y gh || sudo yum install -y gh) >/dev/null 2>&1; elif command -v dnf >/dev/null 2>&1; then (dnf install -y gh || sudo dnf install -y gh) >/dev/null 2>&1; else exit 1; fi"`
	resp, _ := sb.Process.ExecuteCommand(installCmd)
	if resp != nil && resp.ExitCode == 0 {
		return true
	}
	fmt.Println("Failed to install GitHub CLI - install gh manually and run `gh auth login` inside a sandbox shell")
	return false
}
