package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

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
					switch provider {
					case "gh", "github":
						return runGhAuthLogin()
					default:
						return errors.New("unknown provider: use gh")
					}
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
					return errors.New("no API key provided")
				}
				cfg.DaytonaAPIKey = apiKey
				if err := sandbox.SaveConfig(cfg); err != nil {
					return err
				}
				fmt.Fprintln(cliStdout, "Saved credentials to ~/.amux/config.json")
				fmt.Fprintln(cliStdout)
				fmt.Fprintln(cliStdout, "Note: Agent authentication (Claude, Codex, etc.) happens inside the sandbox")
				fmt.Fprintln(cliStdout, "via OAuth/browser login on first run - no API keys needed here.")
				return nil
			case "status":
				cfg, err := sandbox.LoadConfig()
				if err != nil {
					return err
				}
				showAll := len(args) > 1 && args[1] == "--all"

				fmt.Fprintln(cliStdout, "amux auth status")
				fmt.Fprintln(cliStdout, strings.Repeat("─", 50))
				fmt.Fprintln(cliStdout)

				// Daytona API key
				if sandbox.ResolveAPIKey(cfg) != "" {
					fmt.Fprintln(cliStdout, "✓ Daytona API key configured")
				} else {
					fmt.Fprintln(cliStdout, "✗ Daytona API key not set")
					fmt.Fprintln(cliStdout, "  Run: amux auth login")
				}

				if showAll {
					fmt.Fprintln(cliStdout)
					fmt.Fprintln(cliStdout, "Agent authentication (Claude, Codex, Gemini, etc.):")
					fmt.Fprintln(cliStdout, "  Agents authenticate via OAuth/browser login inside the sandbox.")
					fmt.Fprintln(cliStdout, "  Credentials persist across sandboxes for future sessions.")
					fmt.Fprintln(cliStdout, "  Optional: pass API keys via --env flag to skip OAuth.")
				} else {
					fmt.Fprintln(cliStdout)
					fmt.Fprintln(cliStdout, "Run `amux auth status --all` for more details")
				}

				fmt.Fprintln(cliStdout)
				fmt.Fprintln(cliStdout, strings.Repeat("─", 50))
				return nil
			case "logout":
				if err := sandbox.ClearConfigKeys(); err != nil {
					return err
				}
				fmt.Fprintln(cliStdout, "Removed saved credentials from ~/.amux/config.json")
				fmt.Fprintln(cliStdout, "If you use env vars, unset AMUX_DAYTONA_API_KEY")
				return nil
			default:
				return errors.New("unknown action: use login, logout, or status")
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

	cfg, err := sandbox.LoadConfig()
	if err != nil {
		return err
	}
	providerInstance, _, err := sandbox.ResolveProvider(cfg, cwd, "")
	if err != nil {
		return err
	}

	// Load existing sandbox metadata - require sandbox to exist
	meta, err := sandbox.LoadSandboxMeta(cwd, providerInstance.Name())
	if err != nil {
		return err
	}
	if meta == nil {
		return errors.New("no sandbox exists - run `amux sandbox run <agent>` first to create one")
	}

	sb, err := providerInstance.GetSandbox(context.Background(), meta.SandboxID)
	if err != nil {
		return errors.New("sandbox not found - run `amux sandbox run <agent>` to create one")
	}

	// Ensure sandbox is started
	if sb.State() != sandbox.StateStarted {
		fmt.Fprintln(os.Stderr, "Starting sandbox...")
		if err := sb.Start(context.Background()); err != nil {
			return fmt.Errorf("failed to start sandbox: %w", err)
		}
		if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: sandbox may not be fully ready: %v\n", err)
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

	if err := sandbox.SetupCredentials(sb, sandbox.CredentialsConfig{Mode: "sandbox", Agent: sandbox.AgentShell}, false); err != nil {
		return err
	}

	if !ensureGhCli(sb) {
		return errors.New("github CLI is required for device login")
	}

	status, _ := sb.Exec(context.Background(), `bash -lc "gh auth status -h github.com >/dev/null 2>&1"`, nil)
	if status != nil && status.ExitCode == 0 {
		fmt.Fprintln(cliStdout, "GitHub is already authenticated on this sandbox")
		return nil
	}

	fmt.Fprintln(cliStdout, "\namux GitHub login")
	fmt.Fprintln(cliStdout, "1. A one-time device code will appear below")
	fmt.Fprintln(cliStdout, "2. Open https://github.com/login/device locally")
	fmt.Fprintln(cliStdout, "3. Paste the code, finish the login, then return here")
	fmt.Fprintln(cliStdout, "If prompted, choose GitHub.com + HTTPS")
	fmt.Fprintln(cliStdout, "Tip: if you see \"Press Enter\", just hit Enter")

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
		"  echo 'GitHub auth saved on this sandbox'",
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
		return fmt.Errorf("github auth session exited with code %d", exitCode)
	}
	return nil
}

func ensureGhCli(sb sandbox.RemoteSandbox) bool {
	check, _ := sb.Exec(context.Background(), "command -v gh", nil)
	if check != nil && check.ExitCode == 0 {
		return true
	}
	fmt.Fprintln(cliStdout, "GitHub CLI not found, attempting install...")
	installCmd := `bash -lc "if command -v apt-get >/dev/null 2>&1; then (apt-get update -y || sudo apt-get update -y) >/dev/null 2>&1; (apt-get install -y gh || sudo apt-get install -y gh) >/dev/null 2>&1; elif command -v apk >/dev/null 2>&1; then (apk add --no-cache github-cli) >/dev/null 2>&1; elif command -v yum >/dev/null 2>&1; then (yum install -y gh || sudo yum install -y gh) >/dev/null 2>&1; elif command -v dnf >/dev/null 2>&1; then (dnf install -y gh || sudo dnf install -y gh) >/dev/null 2>&1; else exit 1; fi"`
	resp, _ := sb.Exec(context.Background(), installCmd, nil)
	if resp != nil && resp.ExitCode == 0 {
		return true
	}
	fmt.Fprintln(cliStdout, "Failed to install GitHub CLI - install gh manually and run `gh auth login` inside a sandbox shell")
	return false
}
